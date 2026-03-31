package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for adapter-to-gateway communication failures.
// Server handlers use these to return appropriate HTTP status codes
// instead of collapsing all failures into 500 Internal Server Error.
var (
	ErrUnauthorized     = errors.New("authentication expired or invalid")
	ErrForbidden        = errors.New("access denied")
	ErrAgentUnreachable = errors.New("agent is not reachable")
	ErrAgentTimeout     = errors.New("agent did not respond in time")
)

// httpStatusError wraps a gateway HTTP error response with the appropriate
// sentinel error based on status code. Falls through to a plain fmt.Errorf
// for unrecognized codes.
func httpStatusError(statusCode int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	switch statusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	default:
		return errors.New(msg)
	}
}

// wrapConnError wraps a net/http client error with the appropriate sentinel
// based on the underlying cause (connection refused → unreachable, deadline
// exceeded → timeout).
func wrapConnError(err error, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %s: %w", ErrAgentTimeout, msg, err)
	case isConnectionRefused(err):
		return fmt.Errorf("%w: %s: %w", ErrAgentUnreachable, msg, err)
	default:
		return fmt.Errorf("%s: %w", msg, err)
	}
}

// isConnectionRefused checks whether the error chain contains a
// "connection refused" failure. We use string matching because the
// syscall error may be wrapped in multiple layers of net/url errors.
func isConnectionRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "connection refused")
}

// Framework name constants. Use these instead of string literals to avoid
// typos and enable grep-ability. Files in the embedded package that cannot
// import adapter keep using the string literal "embedded" to avoid cycles.
const (
	FrameworkZeroClaw = "zeroclaw"
	FrameworkOpenClaw = "openclaw"
	FrameworkHermes   = "hermes"
	FrameworkPicoClaw = "picoclaw"
	FrameworkEmbedded = "embedded"
)

// Agent is the common interface that every Claw framework adapter implements.
// Eyrie's CLI, web server, and discovery system all work through this interface.
type Agent interface {
	// Identity
	ID() string
	Name() string
	Framework() string // "zeroclaw", "openclaw", "picoclaw", "hermes", "embedded"

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

	// StreamMessage sends a message and streams the response as ChatEvent values.
	// The channel is closed when the response is complete (after a "done" or "error" event).
	StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error)

	// Interrupt asks the framework to cancel an in-flight response for the
	// given session. Best-effort — returns nil if the framework doesn't
	// support interruption or no response is active. The partial response
	// should NOT be stored as if it completed.
	Interrupt(ctx context.Context, sessionKey string) error

	// CreateSession creates a new conversation session with the given name. Returns the new session.
	CreateSession(ctx context.Context, name string) (*Session, error)

	// ResetSession archives the current transcript and starts a fresh session.
	ResetSession(ctx context.Context, sessionKey string) error

	// DeleteSession permanently removes a session from disk.
	DeleteSession(ctx context.Context, sessionKey string) error

	// Personality
	Personality(ctx context.Context) (*Personality, error)

	// Capabilities reports what roles this agent can fill in the hierarchy.
	Capabilities() AgentCapabilities
}

type AgentCapabilities struct {
	CommanderCapable bool `json:"commander_capable"` // Can serve as Commander (requires tool execution, session context, API access)
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
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	Channels       []string `json:"channels"`
	Skills         int      `json:"skills"`
	LastTask       *time.Time `json:"last_task,omitempty"`
	Errors24h      int      `json:"errors_24h"`
	GatewayPort    int      `json:"gateway_port"`
	ProviderStatus string   `json:"provider_status,omitempty"` // "ok", "error", or "" (unknown/not checked)
	BusyState      string   `json:"busy_state,omitempty"`      // "idle", "busy", "error"
	CurrentTask    string   `json:"current_task,omitempty"`     // description of current activity
}

// InferBusyState populates BusyState based on LastTask timestamp.
// Call after Status() to enrich the response.
func (s *AgentStatus) InferBusyState() {
	if s.Errors24h > 5 {
		s.BusyState = "error"
		return
	}
	if s.LastTask != nil && time.Since(*s.LastTask) < 60*time.Second {
		s.BusyState = "busy"
		return
	}
	s.BusyState = "idle"
}

// ProbeProvider checks whether the LLM provider is reachable by hitting its
// /v1/models endpoint. It understands "custom:<url>" format and well-known
// provider names. Returns "ok", "error", or "" if the URL can't be determined.
func ProbeProvider(ctx context.Context, provider string) string {
	probeURL := providerProbeURL(provider)
	if probeURL == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", probeURL, nil)
	if err != nil {
		return "error"
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "error"
	}
	defer resp.Body.Close()
	// Drain body to enable connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	// Accept 2xx and 4xx as "reachable". 4xx means the provider is up but
	// the request was rejected (e.g. missing auth key), which still indicates
	// the endpoint is reachable.
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return "ok"
	}
	return "error"
}

// ProviderBaseURL extracts an OpenAI-compatible base URL from a provider string.
// Handles "custom:http://host:port/v1" and well-known providers.
func ProviderBaseURL(provider string) string {
	// Custom provider: "custom:http://127.0.0.1:3456/v1"
	if strings.HasPrefix(provider, "custom:") {
		u := strings.TrimPrefix(provider, "custom:")
		u = strings.TrimRight(u, "/")
		// Strip trailing /models or /models/ so ProbeProvider won't double it
		u = strings.TrimSuffix(u, "/models")
		u = strings.TrimRight(u, "/")
		// Ensure it ends with /v1
		if !strings.HasSuffix(u, "/v1") {
			u += "/v1"
		}
		return u
	}

	// Well-known providers
	switch {
	case strings.Contains(provider, "openrouter"):
		return "https://openrouter.ai/api/v1"
	case strings.Contains(provider, "openai"):
		return "https://api.openai.com/v1"
	case strings.Contains(provider, "anthropic"):
		// Anthropic doesn't have /v1/models, skip
		return ""
	}

	return ""
}

// providerProbeURL returns a fast, lightweight URL for health-probing a provider.
// WHY not /models: OpenRouter's /models endpoint returns a huge JSON list and
// routinely takes 3-4s, causing timeout-based false "unreachable" results.
// /auth/key returns 401 in ~1.5s (no auth needed, just confirms reachability).
func providerProbeURL(provider string) string {
	// Custom provider: use /models (we don't know what endpoints it has)
	if strings.HasPrefix(provider, "custom:") {
		base := ProviderBaseURL(provider)
		if base != "" {
			return base + "/models"
		}
		return ""
	}

	switch {
	case strings.Contains(provider, "openrouter"):
		return "https://openrouter.ai/api/v1/auth/key"
	case strings.Contains(provider, "openai"):
		return "https://api.openai.com/v1/models"
	case strings.Contains(provider, "anthropic"):
		return ""
	}
	return ""
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

// ChatMessage is the unified message type used across both 1:1 agent chats
// and multi-agent project conversations. Fields like ID, Sender, and Mention
// are only populated in project chat contexts; 1:1 chat leaves them empty.
type ChatMessage struct {
	ID        string         `json:"id,omitempty"`
	Sender    string         `json:"sender,omitempty"`    // agent name or "user" (project chat)
	Role      string         `json:"role"`                // "user", "assistant", "commander", "captain", "talon", "system"
	Content   string         `json:"content"`
	Timestamp time.Time      `json:"timestamp"`
	Channel   string         `json:"channel,omitempty"`
	Mention   string         `json:"mention,omitempty"`   // "@captain", "@commander", etc. (project chat)
	Parts     []ChatPart     `json:"parts,omitempty"`
}

// ChatPart is an ordered content element within a message.
// Type is "text" or "tool_call".
type ChatPart struct {
	Type   string         `json:"type"`
	Text   string         `json:"text,omitempty"`
	ID     string         `json:"id,omitempty"`
	Name   string         `json:"name,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
	Output string         `json:"output,omitempty"`
	Error  bool           `json:"error,omitempty"`
}

type ChatEvent struct {
	Type    string         `json:"type"` // "delta", "tool_start", "tool_result", "done", "error"
	Content string         `json:"content,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	ToolID  string         `json:"tool_id,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Output  string         `json:"output,omitempty"`
	Success *bool          `json:"success,omitempty"`
	Error   string         `json:"error,omitempty"`
	// Usage stats (populated on "done" events when available)
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// DiscoveredAgent holds the result of auto-discovery before a full adapter is created.
type DiscoveredAgent struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Framework   string `json:"framework"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	ConfigPath  string `json:"config_path"`
	Token       string `json:"-"`
	InstanceID  string `json:"instance_id,omitempty"`
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
