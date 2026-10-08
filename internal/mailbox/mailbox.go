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
	"os"
	"path/filepath"
	"sync"
	"time"

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
	// ErrNotLeaseHolder: Ack or Release by someone who doesn't hold the lease
	// (it expired and was reclaimed, or never existed).
	ErrNotLeaseHolder = errors.New("mailbox: caller does not hold the lease")
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
	LeaseUntil    time.Time
	DeliveredAt   time.Time
	LastError     string
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

// Open opens (creating if needed) the mailbox at path. The file and its
// directory are created owner-only: messages can carry private prompts.
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mailbox: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("mailbox: %w", err)
	}
	_ = f.Close()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("mailbox: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mailbox: schema: %w", err)
	}
	return &Store{db: db, opts: opts}, nil
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
	existing, err := s.get(ctx, s.db, id)
	switch {
	case err == nil:
		if existing.Agent != agent || existing.Sender != sender || existing.Body != body {
			return Message{}, false, ErrConflict
		}
		return existing, true, nil
	case !errors.Is(err, ErrNotFound):
		return Message{}, false, err
	}
	now := s.opts.Now().UTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, agent, sender, body, state, created_at, next_attempt_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, agent, sender, body, Queued, now.UnixNano(), now.UnixNano()); err != nil {
		return Message{}, false, fmt.Errorf("mailbox: enqueue: %w", err)
	}
	m, err := s.get(ctx, s.db, id)
	return m, false, err
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
		now := s.opts.Now().UTC()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Message{}, err
		}
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
		if attempts >= s.opts.MaxAttempts {
			// Only reachable via an expired lease: the last attempt never acked.
			if _, err := tx.ExecContext(ctx,
				`UPDATE messages SET state = ?, lease_owner = '', lease_until = 0, last_error = CASE WHEN last_error = '' THEN 'lease expired' ELSE last_error END WHERE id = ?`,
				Dead, id); err != nil {
				_ = tx.Rollback()
				return Message{}, err
			}
			if err := tx.Commit(); err != nil {
				return Message{}, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET state = ?, attempts = attempts + 1, lease_owner = ?, lease_until = ? WHERE id = ?`,
			Leased, owner, now.Add(lease).UnixNano(), id); err != nil {
			_ = tx.Rollback()
			return Message{}, err
		}
		m, err := s.get(ctx, tx, id)
		if err != nil {
			_ = tx.Rollback()
			return Message{}, err
		}
		return m, tx.Commit()
	}
}

// Ack records delivery: the message becomes Delivered with DeliveredAt as the
// receipt. Acking an already-Delivered message is a no-op (safe retry of the
// ack itself). A lease that expired but was not reclaimed still belongs to its
// owner, so a late ack is accepted. Once another deliverer has reclaimed the
// message, the old owner gets ErrNotLeaseHolder: the message is out for
// delivery again and the receiver dedupes on ID.
func (s *Store) Ack(ctx context.Context, id, owner string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.get(ctx, s.db, id)
	if err != nil {
		return Message{}, err
	}
	if m.State == Delivered {
		return m, nil
	}
	now := s.opts.Now().UTC()
	if m.State != Leased || m.LeaseOwner != owner {
		return Message{}, ErrNotLeaseHolder
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE messages SET state = ?, delivered_at = ?, lease_owner = '', lease_until = 0, last_error = '' WHERE id = ?`,
		Delivered, now.UnixNano(), id); err != nil {
		return Message{}, err
	}
	return s.get(ctx, s.db, id)
}

// Release gives a leased message back after a failed delivery attempt. It is
// requeued after Backoff(attempts), or goes Dead once attempts reach
// MaxAttempts. reason is stored as LastError; keep it free of message content.
func (s *Store) Release(ctx context.Context, id, owner, reason string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.get(ctx, s.db, id)
	if err != nil {
		return Message{}, err
	}
	now := s.opts.Now().UTC()
	if m.State != Leased || m.LeaseOwner != owner {
		return Message{}, ErrNotLeaseHolder
	}
	if m.Attempts >= s.opts.MaxAttempts {
		_, err = s.db.ExecContext(ctx,
			`UPDATE messages SET state = ?, lease_owner = '', lease_until = 0, last_error = ? WHERE id = ?`,
			Dead, reason, id)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE messages SET state = ?, lease_owner = '', lease_until = 0, next_attempt_at = ?, last_error = ? WHERE id = ?`,
			Queued, now.Add(s.opts.Backoff(m.Attempts)).UnixNano(), reason, id)
	}
	if err != nil {
		return Message{}, err
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

const cols = `id, agent, sender, body, state, attempts, created_at, next_attempt_at, lease_owner, lease_until, delivered_at, last_error`

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
		&created, &next, &m.LeaseOwner, &leaseUntil, &delivered, &m.LastError); err != nil {
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
