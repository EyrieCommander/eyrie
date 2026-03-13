package adapter

import (
	"context"
	"time"
)

// Agent is the common interface that every Claw framework adapter implements.
// Eyrie's CLI, web server, and discovery system all work through this interface.
type Agent interface {
	// Identity
	ID() string
	Name() string
	Framework() string // "zeroclaw", "openclaw", etc.

	// Gateway connection info
	BaseURL() string

	// Probing
	Health(ctx context.Context) (*HealthStatus, error)
	Status(ctx context.Context) (*AgentStatus, error)
	Config(ctx context.Context) (*AgentConfig, error)

	// Lifecycle (delegated to framework CLI)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error

	// Streaming
	TailLogs(ctx context.Context) (<-chan LogEntry, error)
	TailActivity(ctx context.Context) (<-chan ActivityEvent, error)

	// Conversation history (framework-dependent; may return nil, nil if unsupported)
	Sessions(ctx context.Context) ([]Session, error)
	ChatHistory(ctx context.Context, sessionKey string, limit int) ([]ChatMessage, error)

	// Personality
	Personality(ctx context.Context) (*Personality, error)
}

type ComponentHealth struct {
	Status      string     `json:"status"`
	LastOK      *time.Time `json:"last_ok,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	RestartCount int       `json:"restart_count"`
}

type HealthStatus struct {
	Alive      bool                       `json:"alive"`
	Uptime     time.Duration              `json:"uptime"`
	RAM        uint64                     `json:"ram_bytes"`
	PID        int                        `json:"pid,omitempty"`
	Components map[string]ComponentHealth `json:"components,omitempty"`
}

type AgentStatus struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Channels    []string `json:"channels"`
	Skills      int      `json:"skills"`
	LastTask    *time.Time `json:"last_task,omitempty"`
	Errors24h   int      `json:"errors_24h"`
	GatewayPort int      `json:"gateway_port"`
}

type AgentConfig struct {
	Raw    string `json:"raw"`
	Format string `json:"format"` // "toml" or "json"
}

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type Personality struct {
	Name        string            `json:"name"`
	SystemPrompt string           `json:"system_prompt,omitempty"`
	Traits      map[string]string `json:"traits,omitempty"`
	// Raw workspace identity files (IDENTITY.md, SOUL.md, etc.)
	IdentityFiles map[string]string `json:"identity_files,omitempty"`
}

type ActivityEvent struct {
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"` // "agent_start", "agent_end", "tool_call", "tool_call_start", "llm_request", "error", "chat"
	Summary   string         `json:"summary"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type Session struct {
	Key     string     `json:"key"`
	Title   string     `json:"title"`
	LastMsg *time.Time `json:"last_message,omitempty"`
	Channel string     `json:"channel,omitempty"`
}

type ChatMessage struct {
	Timestamp time.Time `json:"timestamp"`
	Role      string    `json:"role"` // "user", "assistant"
	Content   string    `json:"content"`
	Channel   string    `json:"channel,omitempty"`
}

// DiscoveredAgent holds the result of auto-discovery before a full adapter is created.
type DiscoveredAgent struct {
	Name      string `json:"name"`
	Framework string `json:"framework"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	ConfigPath string `json:"config_path"`
	Token     string `json:"-"`
}

func (d *DiscoveredAgent) URL() string {
	return "http://" + d.Host + ":" + itoa(d.Port)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
