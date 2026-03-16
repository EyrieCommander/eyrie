package adapter

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
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

	// Chat: send a message and get the assistant's reply.
	// sessionKey identifies the conversation (e.g. "agent:main:main"); empty means default.
	SendMessage(ctx context.Context, message, sessionKey string) (*ChatMessage, error)

	// DeleteSession resets/removes a conversation session.
	DeleteSession(ctx context.Context, sessionKey string) error

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
	CPU        float64                    `json:"cpu_percent"`
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
	Timestamp   time.Time      `json:"timestamp"`
	Type        string         `json:"type"` // "agent_start", "agent_end", "tool_call", "tool_call_start", "llm_request", "error", "chat", "separator"
	Summary     string         `json:"summary"`
	FullContent string         `json:"full_content,omitempty"`
	Fields      map[string]any `json:"fields,omitempty"`
}

type Session struct {
	Key      string     `json:"key"`
	Title    string     `json:"title"`
	LastMsg  *time.Time `json:"last_message,omitempty"`
	Channel  string     `json:"channel,omitempty"`
	ReadOnly bool       `json:"readonly,omitempty"`
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

// pidFromPort returns the PID of the process listening on the given TCP port.
// Returns 0 if the lookup fails.
func pidFromPort(port int) int {
	if port <= 0 {
		return 0
	}
	out, err := exec.Command("lsof", "-ti", "tcp:"+strconv.Itoa(port), "-sTCP:LISTEN").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// processStats returns RSS (bytes), CPU (percent), and uptime for the given PID
// in a single ps call. Returns zeros if the PID is invalid or lookup fails.
func processStats(pid int) (rss uint64, cpu float64, uptime time.Duration) {
	if pid <= 0 {
		return 0, 0, 0
	}
	out, err := exec.Command("ps", "-o", "rss=,pcpu=,etime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	kb, _ := strconv.ParseUint(fields[0], 10, 64)
	pct, _ := strconv.ParseFloat(fields[1], 64)
	return kb * 1024, pct, parseEtime(fields[2])
}

// parseEtime parses ps etime format: [[dd-]hh:]mm:ss
func parseEtime(s string) time.Duration {
	var days, hours, mins, secs int
	// Split off days if present
	if idx := strings.Index(s, "-"); idx >= 0 {
		days, _ = strconv.Atoi(s[:idx])
		s = s[idx+1:]
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 3:
		hours, _ = strconv.Atoi(parts[0])
		mins, _ = strconv.Atoi(parts[1])
		secs, _ = strconv.Atoi(parts[2])
	case 2:
		mins, _ = strconv.Atoi(parts[0])
		secs, _ = strconv.Atoi(parts[1])
	}
	return time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(mins)*time.Minute +
		time.Duration(secs)*time.Second
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
