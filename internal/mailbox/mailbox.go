// Package mailbox is a durable per-agent message queue for Eyrie.
//
// It holds messages for agents that are offline or busy and hands them out
// under a lease, so a delivery attempt that dies part-way is retried instead of
// lost, and a message that keeps failing is parked instead of retried forever.
//
// Delivery is at-least-once: a deliverer claims a message, delivers it, then
// acks it. If the lease expires before the ack, the message is claimable again.
// Receivers dedupe on the message ID, which the sender chooses and which
// Enqueue also dedupes on.
//
// This package is storage only. Transport (who claims, how delivery happens)
// lives elsewhere.
package mailbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// State is a message's position in its lifecycle.
type State string

const (
	// Queued: waiting for a claim (possibly not before NextAttemptAt).
	Queued State = "queued"
	// Leased: claimed by a deliverer until LeaseUntil.
	Leased State = "leased"
	// Delivered: the deliverer acked it; DeliveredAt is the receipt.
	Delivered State = "delivered"
	// Dead: gave up after MaxAttempts; kept for inspection, never redelivered.
	Dead State = "dead"
)

var (
	// ErrNotFound: no message with that ID.
	ErrNotFound = errors.New("mailbox: message not found")
	// ErrConflict: Enqueue reused an ID with a different recipient or body.
	ErrConflict = errors.New("mailbox: message id reused with different content")
	// ErrNotLeaseHolder: Ack or Release with a lease token that is not the
	// message's current lease (it was reclaimed, even by the same owner, or
	// never existed).
	ErrNotLeaseHolder = errors.New("mailbox: caller does not hold the lease")
	// ErrInsecurePath: the mailbox directory is writable by group or others.
	ErrInsecurePath = errors.New("mailbox: directory is group- or world-writable")
)

// Message is one queued item.
type Message struct {
	ID            string
	Agent         string
	Sender        string
	Body          string
	State         State
	Attempts      int
	CreatedAt     time.Time
	NextAttemptAt time.Time
	LeaseOwner    string
	// LeaseToken identifies one claim. Ack and Release must present it, so a
	// call left over from an earlier attempt can't touch a newer one.
	LeaseToken  string
	LeaseUntil  time.Time
	DeliveredAt time.Time
	// DeliveredToken is the lease token whose Ack delivered the message.
	DeliveredToken string
	LastError      string
}

// Options tune retry behaviour. Zero values take the defaults.
type Options struct {
	// MaxAttempts is how many claims a message gets before it goes Dead.
	MaxAttempts int
	// Backoff returns the delay before the next claim after the given number
	// of failed attempts (1-based).
	Backoff func(attempts int) time.Duration
	// Now is the clock; tests replace it.
	Now func() time.Time
}

const defaultMaxAttempts = 5

func defaultBackoff(attempts int) time.Duration {
	d := 10 * time.Second
	for i := 1; i < attempts && d < 30*time.Minute; i++ {
		d *= 3
	}
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}

// Store is a mailbox backed by a SQLite file.
type Store struct {
	db   *sql.DB
	mu   sync.Mutex // serialises read-modify-write; SQLite is single-writer
	opts Options
}

// Open opens (creating if needed) the mailbox at path. Messages can carry
// private prompts, so the database and its -wal/-shm files are forced to
// 0600, a new directory is created 0700, and an existing directory that group
// or others can write is refused (they could swap the files).
func Open(path string, opts Options) (*Store, error) {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultMaxAttempts
	}
	if opts.Backoff == nil {
		opts.Backoff = defaultBackoff
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	path, err := resolvePath(path)
	if err != nil {
		return nil, err
	}
	if err := securePaths(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("mailbox: %w", err)
	}
	// One connection per Store, and transactions take the write lock at BEGIN
	// (_txlock=immediate): a deferred read-then-write transaction racing
	// another Store on the same file fails with SQLITE_BUSY_SNAPSHOT instead
	// of waiting on busy_timeout.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mailbox: schema: %w", err)
	}
	return &Store{db: db, opts: opts}, nil
}

func securePaths(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mailbox: %w", err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("mailbox: %w", err)
	}
	if di.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: %s (%v)", ErrInsecurePath, dir, di.Mode().Perm())
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("mailbox: %w", err)
	}
	_ = f.Close()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("mailbox: %w", err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("mailbox: %s is not a regular file", p)
		}
		if fi.Mode().Perm() != 0o600 {
			if err := os.Chmod(p, 0o600); err != nil {
				return fmt.Errorf("mailbox: securing %s: %w", p, err)
			}
		}
	}
	return nil
}

// sqliteDSN builds a file: URI SQLite opens at exactly path. SQLite
// percent-decodes URI paths and the driver splits on the first '?', so a raw
// concatenation would let '?', '#' or '%xx' in path open a different file
// from the one securePaths checked.
func sqliteDSN(path string) string {
	u := url.URL{Scheme: "file", Path: path, OmitHost: true}
	return u.String() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

// resolvePath turns path into the one absolute, symlink-free path that both
// the permission checks and SQLite then use. The directory is created first
// and resolved the way the OS resolves it (EvalSymlinks follows a symlink
// before applying "..", as open(2) does); a lexical filepath.Abs would turn
// "/safe/link/../a.db" into "/safe/a.db" while the OS opens
// "<link target>/../a.db".
// resolveExisting resolves dir the way open(2) would. EvalSymlinks does
// that for an existing path. If dir doesn't exist yet, the missing tail is
// peeled off component by component (by hand, no lexical clean), the
// existing head is resolved, and the tail is re-attached. A ".." in the
// missing tail is refused, because it can't be resolved without the
// directory existing.
func resolveExisting(dir string) (string, error) {
	var tail []string
	cur := dir
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			r, err = filepath.Abs(r)
			if err != nil {
				return "", err
			}
			for i := len(tail) - 1; i >= 0; i-- {
				r = filepath.Join(r, tail[i])
			}
			return r, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		i := strings.LastIndexByte(cur, filepath.Separator)
		if i < 0 {
			if cur == "" || cur == "." {
				return "", fmt.Errorf("cannot resolve %q", dir)
			}
			tail = append(tail, cur)
			cur = "."
			continue
		}
		comp := cur[i+1:]
		if comp == ".." {
			return "", fmt.Errorf("%q has '..' below a directory that doesn't exist yet", dir)
		}
		if comp != "" && comp != "." {
			tail = append(tail, comp)
		}
		cur = cur[:i]
		if cur == "" {
			cur = string(filepath.Separator)
		}
	}
}

func resolvePath(path string) (string, error) {
	// Split on the last separator by hand: filepath.Dir/Base clean the path
	// lexically, which is the bug this function exists to avoid.
	dir, base := ".", path
	if i := strings.LastIndexByte(path, filepath.Separator); i >= 0 {
		dir, base = path[:i], path[i+1:]
		if dir == "" {
			dir = string(filepath.Separator)
		}
	}
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("mailbox: %q does not name a file", path)
	}
	// Resolve the existing part of dir as the OS would, before anything
	// touches it: os.MkdirAll, filepath.Join and Abs all clean lexically.
	real, err := resolveExisting(dir)
	if err != nil {
		return "", fmt.Errorf("mailbox: %w", err)
	}
	if err := os.MkdirAll(real, 0o700); err != nil {
		return "", fmt.Errorf("mailbox: %w", err)
	}
	return filepath.Join(real, base), nil
}

const schema = `
CREATE TABLE IF NOT EXISTS messages (
	id              TEXT PRIMARY KEY,
	agent           TEXT NOT NULL,
	sender          TEXT NOT NULL,
	body            TEXT NOT NULL,
	state           TEXT NOT NULL,
	attempts        INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	next_attempt_at INTEGER NOT NULL,
	lease_owner     TEXT NOT NULL DEFAULT '',
	lease_token     TEXT NOT NULL DEFAULT '',
	delivered_token TEXT NOT NULL DEFAULT '',
	lease_until     INTEGER NOT NULL DEFAULT 0,
	delivered_at    INTEGER NOT NULL DEFAULT 0,
	last_error      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS messages_claim ON messages (agent, state, next_attempt_at, created_at);
`

// Close closes the store.
func (s *Store) Close() error { return s.db.Close() }

// Enqueue adds a message for agent. Re-enqueueing the same ID with the same
// agent, sender and body is a no-op that returns the stored message and
// duplicate=true, whatever state it is in. The same ID with different content
// is ErrConflict.
func (s *Store) Enqueue(ctx context.Context, id, agent, sender, body string) (msg Message, duplicate bool, err error) {
	if id == "" || agent == "" {
		return Message{}, false, errors.New("mailbox: id and agent are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// One immediate transaction: insert-if-absent, then read back whatever
	// row owns the ID. Two Stores racing on the same ID serialise on the
	// write lock, so the loser sees the winner's row (duplicate or conflict)
	// instead of a UNIQUE error.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	now := s.opts.Now().UTC()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages (id, agent, sender, body, state, created_at, next_attempt_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		id, agent, sender, body, Queued, now.UnixNano(), now.UnixNano())
	if err != nil {
		return Message{}, false, fmt.Errorf("mailbox: enqueue: %w", err)
	}
	inserted, _ := res.RowsAffected()
	m, err := s.get(ctx, tx, id)
	if err != nil {
		return Message{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, false, err
	}
	if inserted == 1 {
		return m, false, nil
	}
	if m.Agent != agent || m.Sender != sender || m.Body != body {
		return Message{}, false, ErrConflict
	}
	return m, true, nil
}

// Claim leases the oldest deliverable message for agent to owner for lease.
// Deliverable means Queued and due, or Leased with an expired lease (the
// previous deliverer died). Each claim counts as an attempt; a message whose
// expired lease would push it past MaxAttempts goes Dead instead. Returns
// ErrNotFound when nothing is deliverable.
func (s *Store) Claim(ctx context.Context, agent, owner string, lease time.Duration) (Message, error) {
	if owner == "" || lease <= 0 {
		return Message{}, errors.New("mailbox: owner and a positive lease are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		tx, err := s.db.BeginTx(ctx, nil) // waits for the write lock
		if err != nil {
			return Message{}, err
		}
		// Read the clock only once the lock is held: a lease computed before
		// a busy_timeout wait could already be expired when Claim returns.
		now := s.opts.Now().UTC()
		var id string
		var attempts int
		err = tx.QueryRowContext(ctx, `
			SELECT id, attempts FROM messages
			WHERE agent = ? AND (
				(state = ? AND next_attempt_at <= ?) OR
				(state = ? AND lease_until <= ?))
			ORDER BY created_at, id LIMIT 1`,
			agent, Queued, now.UnixNano(), Leased, now.UnixNano()).Scan(&id, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return Message{}, ErrNotFound
		}
		if err != nil {
			_ = tx.Rollback()
			return Message{}, err
		}
		// Every write below is conditional on the row still being in the
		// state we just read (same attempts count, still deliverable), so a
		// second Store on the same file that got there first wins cleanly and
		// this one retries.
		stillDeliverable := ` WHERE id = ? AND attempts = ? AND ((state = ? AND next_attempt_at <= ?) OR (state = ? AND lease_until <= ?))`
		cond := []any{id, attempts, Queued, now.UnixNano(), Leased, now.UnixNano()}
		if attempts >= s.opts.MaxAttempts {
			// Only reachable via an expired lease: the last attempt never acked.
			if _, err := tx.ExecContext(ctx,
				`UPDATE messages SET state = ?, lease_owner = '', lease_token = '', lease_until = 0, last_error = CASE WHEN last_error = '' THEN 'lease expired' ELSE last_error END`+stillDeliverable,
				append([]any{Dead}, cond...)...); err != nil {
				_ = tx.Rollback()
				return Message{}, err
			}
			if err := tx.Commit(); err != nil {
				return Message{}, err
			}
			continue
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE messages SET state = ?, attempts = attempts + 1, lease_owner = ?, lease_token = ?, lease_until = ?`+stillDeliverable,
			append([]any{Leased, owner, uuid.NewString(), now.Add(lease).UnixNano()}, cond...)...)
		if err != nil {
			_ = tx.Rollback()
			return Message{}, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			_ = tx.Rollback()
			continue
		}
		m, err := s.get(ctx, tx, id)
		if err != nil {
			_ = tx.Rollback()
			return Message{}, err
		}
		return m, tx.Commit()
	}
}

// Ack records delivery with the lease token from Claim: the message becomes
// Delivered with DeliveredAt as the receipt. Acking an already-Delivered
// message is a no-op (safe retry of the ack itself). A lease that expired but
// was not reclaimed is still current, so a late ack is accepted. Once the
// message is reclaimed (by anyone, including the same owner) the old token
// gets ErrNotLeaseHolder: it is out for delivery again and the receiver
// dedupes on ID.
func (s *Store) Ack(ctx context.Context, id, token string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.opts.Now().UTC()
	if token == "" {
		if _, err := s.get(ctx, s.db, id); err != nil {
			return Message{}, err
		}
		return Message{}, ErrNotLeaseHolder
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE messages SET state = ?, delivered_at = ?, delivered_token = lease_token, lease_owner = '', lease_token = '', lease_until = 0, last_error = ''
		 WHERE id = ? AND state = ? AND lease_token = ?`,
		Delivered, now.UnixNano(), id, Leased, token)
	if err != nil {
		return Message{}, err
	}
	m, err := s.get(ctx, s.db, id)
	if err != nil {
		return Message{}, err
	}
	// A repeat of the ack that delivered it is fine; an ack carrying any
	// other token (an earlier, reclaimed attempt) is not, even though the
	// message is delivered.
	if n, _ := res.RowsAffected(); n == 1 || (m.State == Delivered && m.DeliveredToken == token) {
		return m, nil
	}
	return Message{}, ErrNotLeaseHolder
}

// Release gives a leased message back after a failed delivery attempt. It is
// requeued after Backoff(attempts), or goes Dead once attempts reach
// MaxAttempts. reason is stored as LastError; keep it free of message content.
func (s *Store) Release(ctx context.Context, id, token, reason string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.get(ctx, s.db, id)
	if err != nil {
		return Message{}, err
	}
	if m.State != Leased || token == "" || m.LeaseToken != token {
		return Message{}, ErrNotLeaseHolder
	}
	now := s.opts.Now().UTC()
	var res sql.Result
	// Conditional on the token, so a reclaim between the read and this write
	// (another Store on the same file) makes this a no-op, not an overwrite.
	if m.Attempts >= s.opts.MaxAttempts {
		res, err = s.db.ExecContext(ctx,
			`UPDATE messages SET state = ?, lease_owner = '', lease_token = '', lease_until = 0, last_error = ? WHERE id = ? AND state = ? AND lease_token = ?`,
			Dead, reason, id, Leased, token)
	} else {
		res, err = s.db.ExecContext(ctx,
			`UPDATE messages SET state = ?, lease_owner = '', lease_token = '', lease_until = 0, next_attempt_at = ?, last_error = ? WHERE id = ? AND state = ? AND lease_token = ?`,
			Queued, now.Add(s.opts.Backoff(m.Attempts)).UnixNano(), reason, id, Leased, token)
	}
	if err != nil {
		return Message{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Message{}, ErrNotLeaseHolder
	}
	return s.get(ctx, s.db, id)
}

// Get returns one message.
func (s *Store) Get(ctx context.Context, id string) (Message, error) {
	return s.get(ctx, s.db, id)
}

// List returns agent's messages in the given states (all states if none),
// oldest first.
func (s *Store) List(ctx context.Context, agent string, states ...State) ([]Message, error) {
	q := `SELECT ` + cols + ` FROM messages WHERE agent = ?`
	args := []any{agent}
	if len(states) > 0 {
		q += ` AND state IN (?` + repeat(",?", len(states)-1) + `)`
		for _, st := range states {
			args = append(args, st)
		}
	}
	q += ` ORDER BY created_at, id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const cols = `id, agent, sender, body, state, attempts, created_at, next_attempt_at, lease_owner, lease_token, lease_until, delivered_at, last_error, delivered_token`

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type scanner interface{ Scan(dest ...any) error }

func (s *Store) get(ctx context.Context, q querier, id string) (Message, error) {
	m, err := scan(q.QueryRowContext(ctx, `SELECT `+cols+` FROM messages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	return m, err
}

func scan(r scanner) (Message, error) {
	var m Message
	var state string
	var created, next, leaseUntil, delivered int64
	if err := r.Scan(&m.ID, &m.Agent, &m.Sender, &m.Body, &state, &m.Attempts,
		&created, &next, &m.LeaseOwner, &m.LeaseToken, &leaseUntil, &delivered, &m.LastError, &m.DeliveredToken); err != nil {
		return Message{}, err
	}
	m.State = State(state)
	m.CreatedAt = fromNanos(created)
	m.NextAttemptAt = fromNanos(next)
	m.LeaseUntil = fromNanos(leaseUntil)
	m.DeliveredAt = fromNanos(delivered)
	return m, nil
}

func fromNanos(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
