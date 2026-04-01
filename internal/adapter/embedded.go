package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/embedded"
	"github.com/google/uuid"
)

// identityFiles lists the workspace files that contribute to the system prompt
// and personality. Declared once to avoid duplication between buildSystemPrompt
// and Personality.
var identityFiles = []string{"SOUL.md", "IDENTITY.md", "TOOLS.md", "MEMORY.md"}

// identityCache holds the cached contents of workspace identity files.
// Avoids re-reading 4 files on every StreamMessage call.
type identityCache struct {
	mu       sync.RWMutex
	contents map[string]string // filename -> content
	cachedAt time.Time
}

// 30 seconds TTL — short enough to pick up edits quickly, long enough
// to avoid repeated disk reads during burst message activity.
const identityCacheTTL = 30 * time.Second

// EmbeddedAdapter implements the Agent interface for EyrieClaw — an agent
// that runs inside the Eyrie process as a goroutine. No separate binary,
// no gateway, no HTTP roundtrip. It calls LLM APIs directly and streams
// responses through the existing ChatEvent model.
type EmbeddedAdapter struct {
	id            string
	name          string
	provider      string // provider name (e.g. "openrouter")
	model         string
	configPath    string
	workspacePath string

	// Commander capability flag (read from config, default false)
	commanderCapable bool

	// Enabled tools for this instance
	enabledTools []string

	mu        sync.Mutex
	running   bool
	startTime time.Time
	cancelCtx context.Context    // parent context for all agent goroutines
	cancelFn  context.CancelFunc // cancels cancelCtx

	// Core components, initialized on Start()
	llmProvider  embedded.LLMProvider
	tools        *embedded.ToolRegistry
	sessions     *embedded.SessionStore
	logBuf       *embedded.LogBuffer
	loop         *embedded.AgentLoop
	vault        *config.KeyVault

	// Cached identity files to avoid re-reading disk on every message
	idCache identityCache
}

// EmbeddedConfig holds the JSON config format for embedded agents.
type EmbeddedConfig struct {
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	Tools            []string `json:"tools"`
	Workspace        string   `json:"workspace"`
	CommanderCapable bool     `json:"commander_capable"`
}

// NewEmbeddedAdapter creates an adapter from the instance metadata.
func NewEmbeddedAdapter(id, name, configPath, workspacePath string) *EmbeddedAdapter {
	a := &EmbeddedAdapter{
		id:            id,
		name:          name,
		configPath:    configPath,
		workspacePath: workspacePath,
		logBuf:        embedded.NewLogBuffer(500),
	}

	// Read config to populate provider, model, tools, and capability flags
	a.loadConfig()

	return a
}

// loadConfig reads the embedded agent's JSON config file and populates
// the adapter's fields.
func (a *EmbeddedAdapter) loadConfig() {
	if a.configPath == "" {
		return
	}
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		slog.Debug("embedded: failed to read config", "path", a.configPath, "error", err)
		return
	}
	var cfg EmbeddedConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Debug("embedded: failed to parse config", "path", a.configPath, "error", err)
		return
	}
	a.provider = cfg.Provider
	a.model = cfg.Model
	a.enabledTools = cfg.Tools
	a.commanderCapable = cfg.CommanderCapable
	if cfg.Workspace != "" {
		a.workspacePath = cfg.Workspace
	}
}

// --- Identity ---

func (a *EmbeddedAdapter) ID() string        { return a.id }
func (a *EmbeddedAdapter) Name() string      { return a.name }
func (a *EmbeddedAdapter) Framework() string  { return FrameworkEmbedded }
func (a *EmbeddedAdapter) BaseURL() string    { return "" }

// --- Probing ---

func (a *EmbeddedAdapter) Health(_ context.Context) (*HealthStatus, error) {
	a.mu.Lock()
	running := a.running
	startTime := a.startTime
	a.mu.Unlock()

	hs := &HealthStatus{
		Alive: running,
		PID:   os.Getpid(), // Same process as Eyrie
	}
	if running {
		hs.Uptime = time.Since(startTime)
	}

	return hs, nil
}

func (a *EmbeddedAdapter) Status(ctx context.Context) (*AgentStatus, error) {
	provider := a.provider
	model := a.model

	// If provider is empty, try to extract from model (e.g. "anthropic/claude-3-haiku")
	if provider == "" && model != "" {
		if idx := strings.Index(model, "/"); idx > 0 {
			provider = model[:idx]
		}
	}

	as := &AgentStatus{
		Provider:    provider,
		Model:       model,
		Channels:    []string{"embedded"},
		GatewayPort: 0, // No gateway — runs in-process
	}

	// Check provider reachability using the caller's context
	as.ProviderStatus = ProbeProvider(ctx, provider)
	as.InferBusyState()

	return as, nil
}

func (a *EmbeddedAdapter) Config(_ context.Context) (*AgentConfig, error) {
	if a.configPath == "" {
		return nil, fmt.Errorf("no config path available")
	}
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return &AgentConfig{Raw: string(data), Format: "json"}, nil
}

// --- Lifecycle ---

func (a *EmbeddedAdapter) Start(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil // Already running
	}

	// Resolve API key from the centralized vault. Falls back to env vars
	// automatically (vault.Get checks env first, then on-disk store).
	var apiKey string
	if a.vault != nil {
		apiKey = a.vault.Get(a.provider)
	}
	if apiKey == "" {
		a.logBuf.Add("warn", fmt.Sprintf("no API key found for provider %q", a.provider))
	}

	// Resolve base URL from provider name (using adapter's exported function
	// to avoid duplication — embedded package no longer has its own copy)
	baseURL := ProviderBaseURL(a.provider)
	if baseURL == "" {
		return fmt.Errorf("unknown provider %q: cannot determine API base URL", a.provider)
	}

	// Initialize LLM provider
	a.llmProvider = embedded.NewOpenAICompatProvider(apiKey, baseURL)

	// Initialize tool registry with the configured tool set
	a.tools = embedded.NewToolRegistry()
	a.tools.RegisterBuiltins(a.enabledTools, a.workspacePath)

	// Initialize session store from workspace
	sessionDir := filepath.Join(a.workspacePath, "sessions")
	a.sessions = embedded.NewSessionStore(sessionDir)

	// Initialize agent loop
	loopCfg := embedded.DefaultLoopConfig()
	a.loop = embedded.NewAgentLoop(a.llmProvider, a.tools, loopCfg, a.logBuf)

	// Create cancel context for lifecycle management — all long-running
	// operations (agent loop, tool calls) use this as parent so Stop()
	// can propagate cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelCtx = ctx
	a.cancelFn = cancel

	a.running = true
	a.startTime = time.Now()
	a.clearIdentityCache()
	a.logBuf.Add("info", fmt.Sprintf("embedded agent started: provider=%s model=%s tools=%v", a.provider, a.model, a.enabledTools))

	return nil
}

func (a *EmbeddedAdapter) Stop(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil // Already stopped
	}

	if a.cancelFn != nil {
		a.cancelFn()
		a.cancelFn = nil
	}

	a.running = false
	a.logBuf.Add("info", "embedded agent stopped")

	return nil
}

func (a *EmbeddedAdapter) Restart(ctx context.Context) error {
	if err := a.Stop(ctx); err != nil {
		return fmt.Errorf("stopping: %w", err)
	}
	// Re-read config in case it changed
	a.loadConfig()
	return a.Start(ctx)
}

// --- Streaming ---

func (a *EmbeddedAdapter) TailLogs(_ context.Context) (<-chan LogEntry, error) {
	entries := a.logBuf.Entries()
	ch := make(chan LogEntry, len(entries)+1)
	for _, e := range entries {
		ch <- embeddedLogToAdapterLog(e)
	}
	close(ch)
	return ch, nil
}

func (a *EmbeddedAdapter) TailActivity(_ context.Context) (<-chan ActivityEvent, error) {
	entries := a.logBuf.Entries()
	ch := make(chan ActivityEvent, len(entries)+1)
	for _, e := range entries {
		// Convert log entries to activity events
		actType := "log"
		if strings.Contains(e.Message, "tool call:") {
			actType = "tool_call_start"
		} else if strings.Contains(e.Message, "turn complete") {
			actType = "agent_end"
		} else if strings.Contains(e.Message, "started") {
			actType = "agent_start"
		} else if e.Level == "error" {
			actType = "error"
		}
		ch <- ActivityEvent{
			Timestamp:   e.Timestamp,
			Type:        actType,
			Summary:     e.Message,
			FullContent: e.Message,
		}
	}
	close(ch)
	return ch, nil
}

// --- Sessions ---

func (a *EmbeddedAdapter) Sessions(_ context.Context) ([]Session, error) {
	a.mu.Lock()
	store := a.sessions
	a.mu.Unlock()

	if store == nil {
		return nil, nil
	}

	keys := store.Keys()
	sessions := make([]Session, 0, len(keys))
	for _, key := range keys {
		title := key
		if len(title) > 8 {
			title = "Session " + title[:8]
		}
		sessions = append(sessions, Session{
			Key:   key,
			Title: title,
		})
	}
	return sessions, nil
}

func (a *EmbeddedAdapter) ChatHistory(_ context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	a.mu.Lock()
	store := a.sessions
	a.mu.Unlock()

	if store == nil {
		return nil, nil
	}

	msgs := store.Get(sessionKey)
	if msgs == nil {
		return nil, nil
	}

	// Convert embedded.Message to adapter.ChatMessage
	var chatMsgs []ChatMessage
	for _, m := range msgs {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		chatMsgs = append(chatMsgs, ChatMessage{
			Timestamp: time.Now(), // Sessions don't store timestamps per-message yet
			Role:      m.Role,
			Content:   m.Content,
		})
	}

	if limit > 0 && len(chatMsgs) > limit {
		chatMsgs = chatMsgs[len(chatMsgs)-limit:]
	}
	return chatMsgs, nil
}

func (a *EmbeddedAdapter) Interrupt(_ context.Context, _ string) error { return nil }

func (a *EmbeddedAdapter) CreateSession(_ context.Context, name string) (*Session, error) {
	sessionID := uuid.New().String()
	title := name
	if title == "" {
		title = "Session " + sessionID[:8]
	}
	return &Session{Key: sessionID, Title: title}, nil
}

func (a *EmbeddedAdapter) ResetSession(_ context.Context, sessionKey string) error {
	a.mu.Lock()
	store := a.sessions
	a.mu.Unlock()

	if store == nil {
		return nil
	}
	return store.Clear(sessionKey)
}

func (a *EmbeddedAdapter) DeleteSession(_ context.Context, sessionKey string) error {
	a.mu.Lock()
	store := a.sessions
	a.mu.Unlock()

	if store == nil {
		return nil
	}
	return store.Delete(sessionKey)
}

// --- Chat ---

func (a *EmbeddedAdapter) SendMessage(ctx context.Context, message, sessionKey string) (*ChatMessage, error) {
	ch, err := a.StreamMessage(ctx, message, sessionKey)
	if err != nil {
		return nil, err
	}
	var content string
	var seenTerminal bool
	for ev := range ch {
		if ev.Type == "done" {
			content = ev.Content
			seenTerminal = true
		} else if ev.Type == "error" {
			return nil, fmt.Errorf("agent error: %s", ev.Error)
		}
	}
	if !seenTerminal {
		return nil, fmt.Errorf("stream closed before terminal event")
	}
	return &ChatMessage{
		Timestamp: time.Now(),
		Role:      "assistant",
		Content:   content,
	}, nil
}

// StreamMessage constructs the system prompt from workspace identity files,
// appends the user message to the session, and delegates to the agent loop.
func (a *EmbeddedAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	a.mu.Lock()
	running := a.running
	loop := a.loop
	sessions := a.sessions
	model := a.model
	a.mu.Unlock()

	if !running || loop == nil {
		return nil, fmt.Errorf("embedded agent is not running")
	}

	if sessionKey == "" {
		sessionKey = uuid.New().String()
	}

	// Build system prompt from workspace identity files
	systemPrompt := a.buildSystemPrompt()

	// Load session history
	var history []embedded.Message
	if sessions != nil {
		history = sessions.Get(sessionKey)
	}

	// Append the new user message
	userMsg := embedded.Message{Role: "user", Content: message}
	if sessions != nil {
		if err := sessions.Append(sessionKey, userMsg); err != nil {
			slog.Warn("embedded: failed to persist user message", "error", err)
		}
	}
	history = append(history, userMsg)

	// Run the agent loop
	eventCh := loop.Run(ctx, systemPrompt, history, model)

	// Convert embedded.Event to adapter.ChatEvent and persist assistant response
	outCh := make(chan ChatEvent, 64)
	go func() {
		defer close(outCh)
		for ev := range eventCh {
			// Persist assistant response on completion
			if ev.Type == "done" && sessions != nil {
				assistantMsg := embedded.Message{Role: "assistant", Content: ev.Content}
				if err := sessions.Append(sessionKey, assistantMsg); err != nil {
					slog.Warn("embedded: failed to persist assistant message", "error", err)
				}
			}
			chatEv := embeddedEventToChatEvent(ev)
			select {
			case outCh <- chatEv:
			case <-ctx.Done():
				return
			}
		}
	}()

	return outCh, nil
}

// buildSystemPrompt reads identity files from the workspace and concatenates
// them into a system prompt. Results are cached for 30s to avoid disk reads
// on every message.
func (a *EmbeddedAdapter) buildSystemPrompt() string {
	contents := a.cachedIdentityFiles()

	var parts []string
	for _, filename := range identityFiles {
		if content, ok := contents[filename]; ok && content != "" {
			parts = append(parts, content)
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("You are %s, an AI assistant.", a.name)
	}
	return strings.Join(parts, "\n\n")
}

// cachedIdentityFiles returns identity file contents, reading from disk only
// when the cache has expired (30s TTL).
func (a *EmbeddedAdapter) cachedIdentityFiles() map[string]string {
	a.idCache.mu.RLock()
	if a.idCache.contents != nil && time.Since(a.idCache.cachedAt) < identityCacheTTL {
		result := a.idCache.contents
		a.idCache.mu.RUnlock()
		return result
	}
	a.idCache.mu.RUnlock()

	// Cache miss or expired — read from disk
	contents := make(map[string]string, len(identityFiles))
	for _, filename := range identityFiles {
		path := filepath.Join(a.workspacePath, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content != "" {
			contents[filename] = content
		}
	}

	a.idCache.mu.Lock()
	a.idCache.contents = contents
	a.idCache.cachedAt = time.Now()
	a.idCache.mu.Unlock()

	return contents
}

// clearIdentityCache invalidates the cached identity files so they are
// re-read from disk on the next buildSystemPrompt call.
func (a *EmbeddedAdapter) clearIdentityCache() {
	a.idCache.mu.Lock()
	a.idCache.contents = nil
	a.idCache.cachedAt = time.Time{}
	a.idCache.mu.Unlock()
}

// --- Personality ---

func (a *EmbeddedAdapter) Personality(_ context.Context) (*Personality, error) {
	personality := &Personality{
		Name:          a.name,
		IdentityFiles: make(map[string]string),
	}

	for _, filename := range identityFiles {
		path := filepath.Join(a.workspacePath, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		personality.IdentityFiles[filename] = string(data)
	}

	// Include config as an identity file
	if data, err := os.ReadFile(a.configPath); err == nil {
		personality.IdentityFiles["config.json"] = string(data)
	}

	return personality, nil
}

// --- Capabilities ---

func (a *EmbeddedAdapter) Capabilities() AgentCapabilities {
	return AgentCapabilities{CommanderCapable: a.commanderCapable}
}

// SetVault injects the centralized key vault after construction. Called by
// discovery after creating the adapter singleton, avoiding import cycles
// between adapter and config packages at construction time.
func (a *EmbeddedAdapter) SetVault(v *config.KeyVault) {
	a.vault = v
}

// IsRunning returns whether the embedded agent is currently active.
// Used by discovery/probe to check health without HTTP.
func (a *EmbeddedAdapter) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// --- Type conversion helpers ---

// embeddedEventToChatEvent converts the embedded package's Event type to the
// adapter's ChatEvent type. This breaks the import cycle between embedded and
// adapter packages.
func embeddedEventToChatEvent(ev embedded.Event) ChatEvent {
	return ChatEvent{
		Type:         ev.Type,
		Content:      ev.Content,
		Tool:         ev.Tool,
		ToolID:       ev.ToolID,
		Args:         ev.Args,
		Output:       ev.Output,
		Success:      ev.Success,
		Error:        ev.Error,
		InputTokens:  ev.InputTokens,
		OutputTokens: ev.OutputTokens,
	}
}

// embeddedLogToAdapterLog converts the embedded package's LogEntry to the
// adapter package's LogEntry.
func embeddedLogToAdapterLog(le embedded.LogEntry) LogEntry {
	return LogEntry{
		Timestamp: le.Timestamp,
		Level:     le.Level,
		Message:   le.Message,
	}
}

// Compile-time interface assertion
var _ Agent = (*EmbeddedAdapter)(nil)
