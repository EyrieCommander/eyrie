// Package approvals is Eyrie's durable record of Dan's decisions on actions
// that need his approval before they run.
//
// An approval is bound to the exact action it covers: who asked (actor), the
// project, the target, the action name, and a SHA-256 of the payload. It is
// consumed once, atomically, by the code that dispatches the action, and only
// if every binding field matches and it hasn't expired. Anything else fails
// closed: no record, a malformed record, a mismatch, a replay, an expiry or a
// denial all mean "do not dispatch".
//
// Decisions can only come in through the local Eyrie UI (ViaLocalUI). Chat
// messages, chief replies and bridge traffic are not approval channels, and
// Decide rejects them by type rather than by convention.
package approvals

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// State of an approval request.
type State string

const (
	Requested State = "requested"
	Approved  State = "approved"
	Denied    State = "denied"
	Expired   State = "expired"
	Consumed  State = "consumed"
)

func (s State) valid() bool {
	switch s {
	case Requested, Approved, Denied, Expired, Consumed:
		return true
	}
	return false
}

// Via is the channel a decision arrived through.
type Via string

// ViaLocalUI is the only channel Decide accepts: Dan acting in the Eyrie UI
// on his own machine. There is deliberately no constant for chat, chief or
// bridge; any other value is rejected.
const ViaLocalUI Via = "local-ui"

var (
	ErrNotFound        = errors.New("approvals: request not found")
	ErrUntrustedVia    = errors.New("approvals: decisions are accepted only from the local UI")
	ErrNotPending      = errors.New("approvals: request is not awaiting a decision")
	ErrExpired         = errors.New("approvals: request has expired")
	ErrNotApproved     = errors.New("approvals: request is not approved")
	ErrAlreadyConsumed = errors.New("approvals: approval was already used")
	ErrMismatch        = errors.New("approvals: approval does not cover this action")
	ErrMalformed       = errors.New("approvals: stored record is malformed")
	ErrInsecurePath    = errors.New("approvals: directory is group- or world-writable")
)

// Binding is what an approval covers. Dispatch code builds it from the action
// it is about to run, not from the stored request.
type Binding struct {
	Actor       string
	Project     string
	Target      string
	Action      string
	PayloadHash string // HashPayload of the exact payload to dispatch
}

func (b Binding) validate() error {
	if b.Actor == "" || b.Target == "" || b.Action == "" {
		return errors.New("approvals: actor, target and action are required")
	}
	if !validHash(b.PayloadHash) {
		return errors.New("approvals: payload hash must be 64 lowercase hex chars (use HashPayload)")
	}
	return nil
}

// HashPayload is the canonical payload hash: lowercase hex SHA-256.
func HashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Request is one approval record.
type Request struct {
	ID string
	Binding
	Summary    string // what Dan sees; no secrets
	State      State
	CreatedAt  time.Time
	ExpiresAt  time.Time
	DecidedBy  string
	DecidedVia Via
	DecidedAt  time.Time
	ConsumedAt time.Time
}

// Options for Open. Zero values take defaults.
type Options struct {
	Now func() time.Time
}

// Store is an approvals store backed by a SQLite file.
type Store struct {
	db  *sql.DB
	mu  sync.Mutex
	now func() time.Time
}

// Open opens (creating if needed) the store at path, with the same file
// hygiene as the mailbox: 0700 new directory, refuse a group/world-writable
// directory, force 0600 on the db files, refuse a non-regular db file.
func Open(path string, opts Options) (*Store, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if err := securePaths(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("approvals: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("approvals: schema: %w", err)
	}
	return &Store{db: db, now: opts.Now}, nil
}

// sqliteDSN builds a file: URI SQLite opens at exactly path. SQLite
// percent-decodes URI paths and the driver splits on the first '?', so a raw
// concatenation would let '?', '#' or '%xx' in path open a different file
// from the one securePaths checked.
func sqliteDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	u := url.URL{Scheme: "file", Path: path, OmitHost: true}
	return u.String() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

// Close closes the store.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS approvals (
	id           TEXT PRIMARY KEY,
	actor        TEXT NOT NULL,
	project      TEXT NOT NULL,
	target       TEXT NOT NULL,
	action       TEXT NOT NULL,
	payload_hash TEXT NOT NULL,
	summary      TEXT NOT NULL,
	state        TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	expires_at   INTEGER NOT NULL,
	decided_by   TEXT NOT NULL DEFAULT '',
	decided_via  TEXT NOT NULL DEFAULT '',
	decided_at   INTEGER NOT NULL DEFAULT 0,
	consumed_at  INTEGER NOT NULL DEFAULT 0
);
`

// Create records a new request awaiting Dan's decision. It grants nothing.
func (s *Store) Create(ctx context.Context, b Binding, summary string, ttl time.Duration) (Request, error) {
	if err := b.validate(); err != nil {
		return Request{}, err
	}
	if ttl <= 0 {
		return Request{}, errors.New("approvals: ttl must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO approvals (id, actor, project, target, action, payload_hash, summary, state, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, b.Actor, b.Project, b.Target, b.Action, b.PayloadHash, summary, Requested,
		now.UnixNano(), now.Add(ttl).UnixNano()); err != nil {
		return Request{}, fmt.Errorf("approvals: create: %w", err)
	}
	return s.getLocked(ctx, id)
}

// Decide records Dan's approve/deny on a pending, unexpired request. via must
// be ViaLocalUI; by names the person (for the audit trail).
func (s *Store) Decide(ctx context.Context, id string, approve bool, by string, via Via) (Request, error) {
	if via != ViaLocalUI {
		return Request{}, ErrUntrustedVia
	}
	if by == "" {
		return Request{}, errors.New("approvals: decider is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if r.State == Expired {
		return Request{}, ErrExpired
	}
	if r.State != Requested {
		return Request{}, ErrNotPending
	}
	next := Denied
	if approve {
		next = Approved
	}
	now := s.now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE approvals SET state = ?, decided_by = ?, decided_via = ?, decided_at = ?
		 WHERE id = ? AND state = ? AND expires_at > ?`,
		next, by, via, now.UnixNano(), id, Requested, now.UnixNano())
	if err != nil {
		return Request{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Request{}, ErrNotPending
	}
	return s.getLocked(ctx, id)
}

// Consume uses an approval for the action described by b, exactly once. It
// succeeds only if the request is Approved, unexpired, and every Binding
// field matches. Callers dispatch only on a nil error.
func (s *Store) Consume(ctx context.Context, id string, b Binding) (Request, error) {
	if err := b.validate(); err != nil {
		return Request{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// BEGIN IMMEDIATE takes SQLite's write lock (waiting up to busy_timeout
	// for another Store). The clock is read only after that, so an approval
	// that expires while we wait is seen as expired.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := s.getTx(ctx, tx, id)
	if err != nil {
		return Request{}, err
	}
	switch r.State {
	case Consumed:
		return Request{}, ErrAlreadyConsumed
	case Expired:
		return Request{}, ErrExpired
	case Approved:
	default:
		return Request{}, ErrNotApproved
	}
	if r.Binding != b {
		return Request{}, ErrMismatch
	}
	if testHookBeforeConsumeWrite != nil {
		testHookBeforeConsumeWrite()
	}
	now := s.now().UTC()
	// The UPDATE re-checks everything, as a second guard alongside the
	// transaction: a concurrent Consume can't use the same approval twice.
	res, err := tx.ExecContext(ctx,
		`UPDATE approvals SET state = ?, consumed_at = ?
		 WHERE id = ? AND state = ? AND expires_at > ? AND consumed_at = 0
		   AND actor = ? AND project = ? AND target = ? AND action = ? AND payload_hash = ?`,
		Consumed, now.UnixNano(), id, Approved, now.UnixNano(),
		b.Actor, b.Project, b.Target, b.Action, b.PayloadHash)
	if err != nil {
		return Request{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		if r.ExpiresAt.After(now) {
			return Request{}, ErrNotApproved
		}
		return Request{}, ErrExpired
	}
	out, err := s.getTx(ctx, tx, id)
	if err != nil {
		return Request{}, err
	}
	if err := tx.Commit(); err != nil {
		return Request{}, err
	}
	return out, nil
}

// testHookBeforeConsumeWrite runs between Consume's read and its write;
// tests use it to force another Store to consume in that window.
var testHookBeforeConsumeWrite func()

// Get returns a request. A Requested or Approved request past its expiry is
// reported (and stored) as Expired.
func (s *Store) Get(ctx context.Context, id string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(ctx, id)
}

// Pending lists requests awaiting a decision, oldest first.
func (s *Store) Pending(ctx context.Context) ([]Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expireDueLocked(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM approvals WHERE state = ? ORDER BY created_at, id`, Requested)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) expireDueLocked(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE approvals SET state = ? WHERE state IN (?, ?) AND expires_at <= ?`,
		Expired, Requested, Approved, s.now().UTC().UnixNano())
	return err
}

type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// getTx is getLocked inside a transaction, with the clock read after the
// transaction began.
func (s *Store) getTx(ctx context.Context, q execQuerier, id string) (Request, error) {
	if _, err := q.ExecContext(ctx,
		`UPDATE approvals SET state = ? WHERE state IN (?, ?) AND expires_at <= ?`,
		Expired, Requested, Approved, s.now().UTC().UnixNano()); err != nil {
		return Request{}, err
	}
	r, err := scan(q.QueryRowContext(ctx, `SELECT `+cols+` FROM approvals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	return r, err
}

func (s *Store) getLocked(ctx context.Context, id string) (Request, error) {
	if err := s.expireDueLocked(ctx); err != nil {
		return Request{}, err
	}
	r, err := scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM approvals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	return r, err
}

const cols = `id, actor, project, target, action, payload_hash, summary, state, created_at, expires_at, decided_by, decided_via, decided_at, consumed_at`

type scanner interface{ Scan(dest ...any) error }

// scan reads one row and fails closed on anything that doesn't look like a
// record this package wrote.
func scan(r scanner) (Request, error) {
	var q Request
	var state, via string
	var created, expires, decided, consumed int64
	if err := r.Scan(&q.ID, &q.Actor, &q.Project, &q.Target, &q.Action, &q.PayloadHash, &q.Summary,
		&state, &created, &expires, &q.DecidedBy, &via, &decided, &consumed); err != nil {
		return Request{}, err
	}
	q.State, q.DecidedVia = State(state), Via(via)
	q.CreatedAt, q.ExpiresAt = fromNanos(created), fromNanos(expires)
	q.DecidedAt, q.ConsumedAt = fromNanos(decided), fromNanos(consumed)
	if err := q.Binding.validate(); err != nil || !q.State.valid() || expires <= created {
		return Request{}, fmt.Errorf("%w: %s", ErrMalformed, q.ID)
	}
	if (q.State == Approved || q.State == Denied || q.State == Consumed) && (q.DecidedVia != ViaLocalUI || q.DecidedBy == "" || decided == 0) {
		return Request{}, fmt.Errorf("%w: %s has a decision without a local-UI decider", ErrMalformed, q.ID)
	}
	// consumed_at is set exactly when the state is Consumed. A nonzero
	// consumed_at on any other state means the row was tampered with or
	// corrupted, and must never authorise a dispatch.
	if (q.State == Consumed) != (consumed != 0) {
		return Request{}, fmt.Errorf("%w: %s has state %s with consumed_at=%d", ErrMalformed, q.ID, q.State, consumed)
	}
	// A pending request has no decision.
	if q.State == Requested && (q.DecidedVia != "" || q.DecidedBy != "" || decided != 0) {
		return Request{}, fmt.Errorf("%w: %s is requested but carries a decision", ErrMalformed, q.ID)
	}
	return q, nil
}

func fromNanos(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func securePaths(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("approvals: %w", err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("approvals: %w", err)
	}
	if di.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: %s (%v)", ErrInsecurePath, dir, di.Mode().Perm())
	}
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("approvals: %s is not a regular file", path)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("approvals: %w", err)
	}
	_ = f.Close()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("approvals: %w", err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("approvals: %s is not a regular file", p)
		}
		if fi.Mode().Perm() != 0o600 {
			if err := os.Chmod(p, 0o600); err != nil {
				return fmt.Errorf("approvals: securing %s: %w", p, err)
			}
		}
	}
	return nil
}
