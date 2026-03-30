package embedded

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionStore manages conversation history for embedded agents. Sessions
// are held in memory for fast access and persisted to JSONL files in the
// workspace so they survive Eyrie restarts.
type SessionStore struct {
	mu        sync.RWMutex
	sessions  map[string][]Message
	sessionDir string // e.g. {workspace}/sessions
}

// NewSessionStore creates a store backed by the given directory. Any existing
// JSONL session files are loaded into memory.
func NewSessionStore(sessionDir string) *SessionStore {
	ss := &SessionStore{
		sessions:   make(map[string][]Message),
		sessionDir: sessionDir,
	}
	ss.loadAll()
	return ss
}

// Get returns the message history for a session key. Returns nil if the
// session does not exist.
func (ss *SessionStore) Get(key string) []Message {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	msgs := ss.sessions[key]
	if msgs == nil {
		return nil
	}
	// Return a copy to prevent external mutation
	cp := make([]Message, len(msgs))
	copy(cp, msgs)
	return cp
}

// Append adds a message to the session and persists it to disk.
func (ss *SessionStore) Append(key string, msg Message) error {
	ss.mu.Lock()
	ss.sessions[key] = append(ss.sessions[key], msg)
	ss.mu.Unlock()

	return ss.appendToDisk(key, msg)
}

// AppendMany adds multiple messages to the session and persists them.
func (ss *SessionStore) AppendMany(key string, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	ss.mu.Lock()
	ss.sessions[key] = append(ss.sessions[key], msgs...)
	ss.mu.Unlock()

	for _, msg := range msgs {
		if err := ss.appendToDisk(key, msg); err != nil {
			return err
		}
	}
	return nil
}

// Keys returns all session keys.
func (ss *SessionStore) Keys() []string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	keys := make([]string, 0, len(ss.sessions))
	for k := range ss.sessions {
		keys = append(keys, k)
	}
	return keys
}

// Delete removes a session from memory and disk.
func (ss *SessionStore) Delete(key string) error {
	ss.mu.Lock()
	delete(ss.sessions, key)
	ss.mu.Unlock()

	path := ss.filePath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing session file: %w", err)
	}
	return nil
}

// Clear removes all messages from a session (in memory and on disk) without
// deleting the session key.
func (ss *SessionStore) Clear(key string) error {
	ss.mu.Lock()
	ss.sessions[key] = nil
	ss.mu.Unlock()

	// Truncate the file on disk
	path := ss.filePath(key)
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("truncating session file: %w", err)
	}
	return nil
}

// --- Persistence ---

// sessionEntry is the on-disk JSONL format per line.
type sessionEntry struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Timestamp  string     `json:"timestamp"`
}

func (ss *SessionStore) filePath(key string) string {
	// Sanitize key for filename safety
	safe := filepath.Base(key)
	return filepath.Join(ss.sessionDir, safe+".jsonl")
}

func (ss *SessionStore) appendToDisk(key string, msg Message) error {
	if err := os.MkdirAll(ss.sessionDir, 0o755); err != nil {
		return fmt.Errorf("creating session dir: %w", err)
	}

	f, err := os.OpenFile(ss.filePath(key), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	entry := sessionEntry{
		Role:       msg.Role,
		Content:    msg.Content,
		ToolCalls:  msg.ToolCalls,
		ToolCallID: msg.ToolCallID,
		Name:       msg.Name,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling session entry: %w", err)
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// loadAll reads all .jsonl files from the session directory and populates
// the in-memory map.
func (ss *SessionStore) loadAll() {
	if ss.sessionDir == "" {
		return
	}
	entries, err := os.ReadDir(ss.sessionDir)
	if err != nil {
		return // Directory doesn't exist yet — will be created on first write
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	for _, e := range entries {
		if e.IsDir() || !isJSONL(e.Name()) {
			continue
		}
		key := e.Name()[:len(e.Name())-len(".jsonl")]
		msgs := ss.loadFile(filepath.Join(ss.sessionDir, e.Name()))
		if len(msgs) > 0 {
			ss.sessions[key] = msgs
		}
	}
}

func (ss *SessionStore) loadFile(path string) []Message {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var msgs []Message
	scanner := bufio.NewScanner(f)
	// 1 MB max line size for large messages
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry sessionEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue // skip malformed lines
		}
		msgs = append(msgs, Message{
			Role:       entry.Role,
			Content:    entry.Content,
			ToolCalls:  entry.ToolCalls,
			ToolCallID: entry.ToolCallID,
			Name:       entry.Name,
		})
	}
	return msgs
}

func isJSONL(name string) bool {
	return len(name) > 6 && name[len(name)-6:] == ".jsonl"
}
