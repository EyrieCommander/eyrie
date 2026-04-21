// Package commander implements the built-in commander — an LLM loop that
// runs inside the Eyrie process. The user chats directly with Eyrie, and
// the commander dispatches tools that call into Eyrie's stores directly.
//
// Unlike captains and talons, which run as external framework processes
// for workspace-sandboxed work, the commander has no sandbox and no
// subprocess. Its tools are ordinary Go functions.
package commander

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/embedded"
	"github.com/Audacity88/eyrie/internal/fileutil"
)

// Store persists the commander's conversation as an append-only JSONL file.
// One file per process — there is exactly one commander conversation.
//
// WHY one file (not per-session): The commander has a single persistent
// relationship with the user, not ephemeral per-project sessions. Memory
// and context span across projects; fragmenting into sessions would
// complicate that relationship. A single long-running file is simpler.
//
// WHY JSONL not SQLite: Same reasoning as project chat — append-only,
// simple to inspect with jq, no WAL/locking complexity. If the commander
// ever needs random-access queries (e.g. "find the first time I mentioned
// X"), we can migrate to SQLite then.
type Store struct {
	path string
	mu   sync.RWMutex
}

// NewStore creates a Store backed by ~/.eyrie/commander/chat.jsonl.
// The directory is created on first append, not here, so the store
// can be constructed before the config dir exists.
func NewStore() (*Store, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{
		path: filepath.Join(dir, "commander", "chat.jsonl"),
	}, nil
}

// Append adds one message to the conversation.
func (s *Store) Append(msg embedded.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("creating commander dir: %w", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening conversation file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	return nil
}

// All returns every message in the conversation in insertion order.
// Returns (nil, nil) if the file does not yet exist.
func (s *Store) All() ([]embedded.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening conversation file: %w", err)
	}
	defer f.Close()

	var messages []embedded.Message
	scanner := bufio.NewScanner(f)
	// 1 MB initial buffer, 10 MB max — matches the embedded session store.
	// Tool results (e.g. list_projects output) can be large.
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var msg embedded.Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			// Skip malformed lines rather than fail the whole read.
			continue
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading conversation file: %w", err)
	}
	return messages, nil
}

// Clear removes the conversation file entirely. Safe to call on a
// nonexistent file.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing conversation file: %w", err)
	}
	return nil
}

// Rewrite replaces the entire conversation file atomically. Used for
// operations that need to modify or prune history (not used in the
// skeleton, but exposed for future memory/summarization work).
func (s *Store) Rewrite(messages []embedded.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("creating commander dir: %w", err)
	}

	var buf []byte
	for _, m := range messages {
		line, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshaling message: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return fileutil.AtomicWrite(s.path, buf, 0o600)
}
