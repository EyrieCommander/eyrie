package project

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/fileutil"
)

// ChatMessage is now a unified type defined in adapter package.
// Type alias preserves backward compatibility — existing code that
// references project.ChatMessage or project.ChatPart continues to work.
type ChatMessage = adapter.ChatMessage
type ChatPart = adapter.ChatPart

// ChatStore manages the shared conversation for a project.
//
// WHY JSONL: Append-only file format. Each message is one JSON line. This is
// simpler than SQLite for our use case (sequential chat), easy to inspect with
// standard tools (cat, jq), and avoids WAL/locking complexity. The ChatStore
// interface allows swapping for SQLite if we need random-access queries later.
//
// WHY package-level singleton for listening state:
// ChatStore is recreated on every HTTP request (via NewChatStore()). If
// listening state were stored on the ChatStore instance, it would be lost
// between the POST that sets [LISTENING] and the next POST that checks it.
// The package-level map survives across requests within the same process.
//
// WHY in-memory only (not persisted to disk):
// If Eyrie restarts, the listening state is lost — the agent simply won't
// claim the next message, and routing falls back to the default (captain).
// This is acceptable: the user can @mention the agent to re-engage, and
// persisting would add complexity for minimal benefit.
var (
	listeningMap = make(map[string]string) // projectID → agent name
	listeningMu  sync.RWMutex
)

type ChatStore struct {
	dir string
	mu  sync.RWMutex
}

func NewChatStore() (*ChatStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".eyrie", "projects")
	return &ChatStore{dir: dir}, nil
}

// SetListening marks an agent as listening for the next message in a project.
func (cs *ChatStore) SetListening(projectID, agentName string) {
	listeningMu.Lock()
	defer listeningMu.Unlock()
	listeningMap[projectID] = agentName
}

// ClearListening removes the listening state for a project.
func (cs *ChatStore) ClearListening(projectID string) {
	listeningMu.Lock()
	defer listeningMu.Unlock()
	delete(listeningMap, projectID)
}

// Listener returns the agent currently listening in a project, or "".
func (cs *ChatStore) Listener(projectID string) string {
	listeningMu.RLock()
	defer listeningMu.RUnlock()
	return listeningMap[projectID]
}

func (cs *ChatStore) chatPath(projectID string) string {
	return filepath.Join(cs.dir, projectID, "chat.jsonl")
}

// ClearChat removes the project's chat history file.
func (cs *ChatStore) ClearChat(projectID string) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	path := cs.chatPath(projectID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear chat: %w", err)
	}
	return nil
}



// Append adds a message to the project's shared conversation.
func (cs *ChatStore) Append(projectID string, msg ChatMessage) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()

	dir := filepath.Join(cs.dir, projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating project chat dir: %w", err)
	}

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling chat message: %w", err)
	}

	f, err := os.OpenFile(cs.chatPath(projectID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening chat file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing chat message: %w", err)
	}
	return nil
}

// Messages returns all messages in a project's conversation, optionally
// limited to the last N messages.
func (cs *ChatStore) Messages(projectID string, limit int) ([]ChatMessage, error) {
	if err := validateProjectID(projectID); err != nil {
		return nil, err
	}
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	f, err := os.Open(cs.chatPath(projectID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening chat file: %w", err)
	}
	defer f.Close()

	var messages []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var msg ChatMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			slog.Warn("skipping malformed chat line", "project", projectID, "error", err)
			continue
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading chat file: %w", err)
	}

	messages = dedupMessages(messages)

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// Compact rewrites the chat JSONL file without duplicate IDs.
// WHY: Incremental persistence appends partial snapshots during streaming
// (same ID, growing content) for crash recovery. After the final message is
// written, compaction removes the partials so the file stays clean and
// Messages() doesn't need to dedup on every read.
func (cs *ChatStore) Compact(projectID string) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()

	path := cs.chatPath(projectID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("opening chat file for compaction: %w", err)
	}

	var messages []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var msg ChatMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	f.Close()
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading chat file for compaction: %w", err)
	}

	deduped := dedupMessages(messages)
	if len(deduped) == len(messages) {
		return nil // Already clean, nothing to compact
	}

	// Rewrite atomically
	var buf []byte
	for _, m := range deduped {
		line, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshaling during compaction: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}

	return fileutil.AtomicWrite(path, buf, 0o600)
}

// dedupMessages keeps only the last occurrence of each message ID.
// WHY: Incremental persistence appends partial snapshots with the same ID.
// Keeping the last occurrence ensures the most complete version is returned.
func dedupMessages(messages []ChatMessage) []ChatMessage {
	if len(messages) == 0 {
		return messages
	}
	seen := make(map[string]int, len(messages))
	for i, m := range messages {
		seen[m.ID] = i
	}
	if len(seen) == len(messages) {
		return messages // No duplicates
	}
	deduped := make([]ChatMessage, 0, len(seen))
	for i, m := range messages {
		if seen[m.ID] == i {
			deduped = append(deduped, m)
		}
	}
	return deduped
}

// FormatForAgent formats the conversation history as a text block suitable
// for injecting into an agent's context. Each message is labeled with the
// sender so the agent knows who said what.
func FormatForAgent(messages []ChatMessage) string {
	var b []byte
	for _, msg := range messages {
		label := msg.Sender
		if msg.Role != "" && msg.Role != msg.Sender {
			label = fmt.Sprintf("%s (%s)", msg.Sender, msg.Role)
		}
		line := fmt.Sprintf("[%s] %s: %s\n\n",
			msg.Timestamp.Format("15:04"),
			label,
			msg.Content,
		)
		b = append(b, line...)
	}
	return string(b)
}
