package bridge

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Message states.
const (
	StatePending  = "pending"  // saved, not yet accepted by the chief's wake endpoint
	StateWaiting  = "waiting"  // wake accepted; waiting on a final reply
	StateAnswered = "answered" // a final reply arrived
	StateFailed   = "failed"   // wake retries exhausted; UI shows Retry
)

// Message is a prompt from Dan to the chief.
type Message struct {
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"message_id"`
	Text           string    `json:"text"`
	TS             time.Time `json:"ts"`
	State          string    `json:"state"`
	Attempts       int       `json:"attempts"`
	LastError      string    `json:"last_error,omitempty"`
	Replies        []Reply   `json:"replies,omitempty"`
}

// Reply is display-only text from the chief.
type Reply struct {
	ReplyID   string    `json:"reply_id"`
	InReplyTo string    `json:"in_reply_to"`
	Text      string    `json:"text"`
	Final     bool      `json:"final"`
	TS        time.Time `json:"ts"`
}

// ErrUnknownMessage: in_reply_to is not a message in that conversation.
var ErrUnknownMessage = errors.New("unknown message")

// Store is the durable prompt/reply store (SQLite under ~/.eyrie/bridge).
type Store struct {
	db *sql.DB
	mu sync.Mutex // serialises writes; sqlite is single-writer anyway
}

// DefaultStorePath is ~/.eyrie/bridge/chief.db.
func DefaultStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".eyrie", "bridge", "chief.db"), nil
}

// OpenStore opens (creating if needed) the store at path.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	const schema = `
CREATE TABLE IF NOT EXISTS messages (
  message_id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL,
  text TEXT NOT NULL,
  ts INTEGER NOT NULL,
  state TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS messages_conv ON messages(conversation_id, ts);
CREATE TABLE IF NOT EXISTS replies (
  reply_id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL REFERENCES messages(message_id),
  text TEXT NOT NULL,
  final INTEGER NOT NULL,
  ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS replies_msg ON replies(message_id, ts);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("bridge store schema: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	return &Store{db: db}, nil
}

// Close closes the store.
func (s *Store) Close() error { return s.db.Close() }

// NewID returns a time-ordered UUIDv7 string.
func NewID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString()
}

// CreateMessage saves a prompt with state pending. Saved before any send.
func (s *Store) CreateMessage(conversationID, text string) (*Message, error) {
	m := &Message{ConversationID: conversationID, MessageID: NewID(), Text: text, TS: time.Now().UTC(), State: StatePending}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO messages(message_id, conversation_id, text, ts, state) VALUES(?,?,?,?,?)`,
		m.MessageID, m.ConversationID, m.Text, m.TS.UnixMilli(), m.State)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// SetState records a send outcome. It never moves an answered message back.
func (s *Store) SetState(messageID, state string, attempts int, lastErr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE messages SET state=?, attempts=?, last_error=? WHERE message_id=? AND state != ?`,
		state, attempts, lastErr, messageID, StateAnswered)
	return err
}

// RecordAttempt records one wake attempt with conditional writes, so a
// delivery outcome never demotes a message that a reply already moved on.
//   - success: pending/failed -> waiting (waiting/answered untouched).
//   - failure: attempts and last_error only; the state is left alone.
func (s *Store) RecordAttempt(messageID string, attempt int, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if errText == "" {
		_, err := s.db.Exec(`UPDATE messages SET state=?, attempts=?, last_error='' WHERE message_id=? AND state IN (?, ?)`,
			StateWaiting, attempt, messageID, StatePending, StateFailed)
		return err
	}
	_, err := s.db.Exec(`UPDATE messages SET attempts=?, last_error=? WHERE message_id=?`, attempt, errText, messageID)
	return err
}

// MarkFailed moves a message to failed only if it is still pending.
func (s *Store) MarkFailed(messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE messages SET state=? WHERE message_id=? AND state=?`, StateFailed, messageID, StatePending)
	return err
}

// ResetForRetry moves a failed or waiting message back to pending. Returns
// false (and changes nothing) if it is answered or unknown.
func (s *Store) ResetForRetry(messageID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE messages SET state=?, attempts=0, last_error='' WHERE message_id=? AND state IN (?, ?, ?)`,
		StatePending, messageID, StatePending, StateFailed, StateWaiting)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// GetMessage returns one message with its replies.
func (s *Store) GetMessage(messageID string) (*Message, error) {
	row := s.db.QueryRow(`SELECT message_id, conversation_id, text, ts, state, attempts, last_error FROM messages WHERE message_id=?`, messageID)
	m, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	reps, err := s.replies([]string{m.MessageID})
	if err != nil {
		return nil, err
	}
	m.Replies = reps[m.MessageID]
	return m, nil
}

type scanner interface{ Scan(...any) error }

func scanMessage(r scanner) (*Message, error) {
	var m Message
	var ts int64
	if err := r.Scan(&m.MessageID, &m.ConversationID, &m.Text, &ts, &m.State, &m.Attempts, &m.LastError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnknownMessage
		}
		return nil, err
	}
	m.TS = time.UnixMilli(ts).UTC()
	return &m, nil
}

// Conversation returns the messages of a conversation, oldest first, with replies.
func (s *Store) Conversation(conversationID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT message_id, conversation_id, text, ts, state, attempts, last_error FROM
	  (SELECT * FROM messages WHERE conversation_id=? ORDER BY ts DESC LIMIT ?) ORDER BY ts ASC`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	var ids []string
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
		ids = append(ids, m.MessageID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reps, err := s.replies(ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Replies = reps[out[i].MessageID]
	}
	return out, nil
}

func (s *Store) replies(ids []string) (map[string][]Reply, error) {
	out := map[string][]Reply{}
	for _, id := range ids {
		rows, err := s.db.Query(`SELECT reply_id, message_id, text, final, ts FROM replies WHERE message_id=? ORDER BY ts ASC`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r Reply
			var fin int
			var ts int64
			if err := rows.Scan(&r.ReplyID, &r.InReplyTo, &r.Text, &fin, &ts); err != nil {
				rows.Close()
				return nil, err
			}
			r.Final = fin == 1
			r.TS = time.UnixMilli(ts).UTC()
			out[id] = append(out[id], r)
		}
		rows.Close()
	}
	return out, nil
}

// AddReply stores a chief reply. duplicate=true when reply_id was already
// stored (nothing changes). ErrUnknownMessage when in_reply_to is not a
// message of conversationID.
func (s *Store) AddReply(conversationID, inReplyTo, replyID, text string, final bool) (duplicate bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var conv string
	if err := tx.QueryRow(`SELECT conversation_id FROM messages WHERE message_id=?`, inReplyTo).Scan(&conv); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrUnknownMessage
		}
		return false, err
	}
	if conv != conversationID {
		return false, ErrUnknownMessage
	}
	var existing string
	err = tx.QueryRow(`SELECT message_id FROM replies WHERE reply_id=?`, replyID).Scan(&existing)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	fin := 0
	if final {
		fin = 1
	}
	if _, err := tx.Exec(`INSERT INTO replies(reply_id, message_id, text, final, ts) VALUES(?,?,?,?,?)`,
		replyID, inReplyTo, text, fin, time.Now().UTC().UnixMilli()); err != nil {
		return false, err
	}
	if final {
		if _, err := tx.Exec(`UPDATE messages SET state=?, last_error='' WHERE message_id=?`, StateAnswered, inReplyTo); err != nil {
			return false, err
		}
	} else {
		// An interim reply proves the wake landed even if our send loop is
		// still retrying; move pending/failed to waiting.
		if _, err := tx.Exec(`UPDATE messages SET state=? WHERE message_id=? AND state IN (?, ?)`, StateWaiting, inReplyTo, StatePending, StateFailed); err != nil {
			return false, err
		}
	}
	return false, tx.Commit()
}

// Unsent returns messages still pending (e.g. Eyrie restarted mid-send).
func (s *Store) Unsent() ([]Message, error) {
	rows, err := s.db.Query(`SELECT message_id, conversation_id, text, ts, state, attempts, last_error FROM messages WHERE state=? ORDER BY ts ASC`, StatePending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}
