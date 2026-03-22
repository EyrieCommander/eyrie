package project

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChatMessage is a single message in a project's shared conversation.
// Unlike adapter.ChatMessage, this includes sender attribution for
// multi-participant conversations.
type ChatMessage struct {
	ID        string    `json:"id"`
	Sender    string    `json:"sender"`              // "user", agent name, or instance name
	Role      string    `json:"role"`                // "user", "commander", "captain", "talon"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Mention   string    `json:"mention,omitempty"`   // "@captain", "@commander", etc.
}

// ChatStore manages the shared conversation for a project.
// Messages are stored as append-only JSONL at ~/.eyrie/projects/{id}/chat.jsonl.
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

func (cs *ChatStore) chatPath(projectID string) string {
	return filepath.Join(cs.dir, projectID, "chat.jsonl")
}

// Append adds a message to the project's shared conversation.
func (cs *ChatStore) Append(projectID string, msg ChatMessage) error {
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

	f, err := os.OpenFile(cs.chatPath(projectID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening chat file: %w", err)
	}
	defer f.Close()

	f.Write(data)
	f.Write([]byte("\n"))
	return nil
}

// Messages returns all messages in a project's conversation, optionally
// limited to the last N messages.
func (cs *ChatStore) Messages(projectID string, limit int) ([]ChatMessage, error) {
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
		if json.Unmarshal(scanner.Bytes(), &msg) == nil {
			messages = append(messages, msg)
		}
	}

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
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
