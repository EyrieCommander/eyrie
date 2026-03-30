package embedded

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SessionStore manages conversation history for embedded agents. Sessions
// are held in memory for fast access and persisted to JSONL files in the
// workspace so they survive Eyrie restarts.
type SessionStore struct {
	mu         sync.RWMutex
	sessions   map[string][]Message
	sessionDir string // e.g. {workspace}/sessions
	dirCreated sync.Once // ensures os.MkdirAll runs only once
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
// Disk write happens first — if it fails, in-memory state is not modified
// so the two stay consistent.
func (ss *SessionStore) Append(key string, msg Message) error {
	if err := ss.appendToDisk(key, msg); err != nil {
		return err
	}
	ss.mu.Lock()
	ss.sessions[key] = append(ss.sessions[key], msg)
	ss.mu.Unlock()
	return nil
}

// AppendMany adds multiple messages to the session and persists them.
// Opens the file once for all messages instead of N times.
// All messages are written to disk first; only after success is the
// in-memory state updated.
func (ss *SessionStore) AppendMany(key string, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	if err := ss.appendManyToDisk(key, msgs); err != nil {
		return err
	}
	ss.mu.Lock()
	ss.sessions[key] = append(ss.sessions[key], msgs...)
	ss.mu.Unlock()
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
// Disk removal happens first to avoid orphaned files on failure.
func (ss *SessionStore) Delete(key string) error {
	path := ss.filePath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing session file: %w", err)
	}
	ss.mu.Lock()
	delete(ss.sessions, key)
	ss.mu.Unlock()
	return nil
}

// Clear removes all messages from a session (in memory and on disk) without
// deleting the session key. Disk truncation happens first to avoid
// inconsistency on failure.
func (ss *SessionStore) Clear(key string) error {
	path := ss.filePath(key)
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("truncating session file: %w", err)
	}
	ss.mu.Lock()
	ss.sessions[key] = nil
	ss.mu.Unlock()
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
	// WHY hex encoding: filepath.Base strips directory components, causing
	// keys like "project/main" and "other/main" to collide on "main.jsonl".
	// Hex encoding is reversible and produces filesystem-safe filenames.
	encoded := hex.EncodeToString([]byte(key))
	return filepath.Join(ss.sessionDir, encoded+".jsonl")
}

// ensureDir creates the session directory once per SessionStore lifetime.
func (ss *SessionStore) ensureDir() error {
	var dirErr error
	ss.dirCreated.Do(func() {
		dirErr = os.MkdirAll(ss.sessionDir, 0o755)
	})
	return dirErr
}

func (ss *SessionStore) appendToDisk(key string, msg Message) error {
	if err := ss.ensureDir(); err != nil {
		return fmt.Errorf("creating session dir: %w", err)
	}

	f, err := os.OpenFile(ss.filePath(key), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	return ss.writeEntry(f, msg)
}

// appendManyToDisk writes multiple messages in a single file open/close cycle.
func (ss *SessionStore) appendManyToDisk(key string, msgs []Message) error {
	if err := ss.ensureDir(); err != nil {
		return fmt.Errorf("creating session dir: %w", err)
	}

	f, err := os.OpenFile(ss.filePath(key), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	for _, msg := range msgs {
		if err := ss.writeEntry(f, msg); err != nil {
			return err
		}
	}
	return nil
}

// writeEntry marshals and writes a single session entry to an open file.
func (ss *SessionStore) writeEntry(f *os.File, msg Message) error {
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
		encoded := e.Name()[:len(e.Name())-len(".jsonl")]
		// Decode hex-encoded key; fall back to raw name for pre-encoding files
		key := encoded
		if decoded, err := hex.DecodeString(encoded); err == nil {
			key = string(decoded)
		}
		// Legacy: also accept old filepath.Base-style names
		if strings.Contains(encoded, "/") || strings.Contains(encoded, "\\") {
			key = filepath.Base(encoded)
		}
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
	return strings.HasSuffix(name, ".jsonl")
}
