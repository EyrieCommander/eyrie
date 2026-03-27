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
)

// ChatPart is an ordered content element (text or tool call) within a message.
type ChatPart struct {
	Type   string         `json:"type"`             // "text" or "tool_call"
	Text   string         `json:"text,omitempty"`
	ID     string         `json:"id,omitempty"`
	Name   string         `json:"name,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
	Output string         `json:"output,omitempty"`
	Error  bool           `json:"error,omitempty"`
}

// ChatMessage is a single message in a project's shared conversation.
// Unlike adapter.ChatMessage, this includes sender attribution for
// multi-participant conversations.
type ChatMessage struct {
	ID        string     `json:"id"`
	Sender    string     `json:"sender"`              // "user", agent name, or instance name
	Role      string     `json:"role"`                // "user", "commander", "captain", "talon"
	Content   string     `json:"content"`
	Timestamp time.Time  `json:"timestamp"`
	Mention   string     `json:"mention,omitempty"`   // "@captain", "@commander", etc.
	Parts     []ChatPart `json:"parts,omitempty"`     // tool calls + text parts
}

// ChatStore manages the shared conversation for a project.
// Messages are stored as append-only JSONL at ~/.eyrie/projects/{id}/chat.jsonl.
type ChatStore struct {
	dir       string
	mu        sync.RWMutex
	// listening tracks which agent is in [LISTENING] state per project.
	// Key: projectID, Value: agent name. Empty means nobody is listening.
	listening map[string]string
	lisMu     sync.RWMutex
}

func NewChatStore() (*ChatStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".eyrie", "projects")
	return &ChatStore{dir: dir, listening: make(map[string]string)}, nil
}

// SetListening marks an agent as listening for the next message in a project.
func (cs *ChatStore) SetListening(projectID, agentName string) {
	cs.lisMu.Lock()
	defer cs.lisMu.Unlock()
	cs.listening[projectID] = agentName
}

// ClearListening removes the listening state for a project.
func (cs *ChatStore) ClearListening(projectID string) {
	cs.lisMu.Lock()
	defer cs.lisMu.Unlock()
	delete(cs.listening, projectID)
}

// Listener returns the agent currently listening in a project, or "".
func (cs *ChatStore) Listener(projectID string) string {
	cs.lisMu.RLock()
	defer cs.lisMu.RUnlock()
	return cs.listening[projectID]
}

func (cs *ChatStore) chatPath(projectID string) string {
	return filepath.Join(cs.dir, projectID, "chat.jsonl")
}

// ClearChat removes the project's chat history file.
func (cs *ChatStore) ClearChat(projectID string) error {
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
