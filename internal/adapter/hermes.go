package adapter

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite" // SQLite driver
)

// HermesAdapter implements the Agent interface for Hermes (Python-based agent)
// using CLI invocation and file-based status checks.
type HermesAdapter struct {
	id         string
	name       string
	configPath string
	configDir  string
	binaryPath string
}

// NewHermesAdapter creates a new Hermes adapter
func NewHermesAdapter(id, name, configPath, binaryPath string) *HermesAdapter {
	// Expand ~ in configPath
	expandedPath := expandHome(configPath)

	return &HermesAdapter{
		id:         id,
		name:       name,
		configPath: expandedPath,
		configDir:  filepath.Dir(expandedPath),
		binaryPath: binaryPath,
	}
}

// expandHome expands ~ in paths
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// ID returns the unique identifier
func (h *HermesAdapter) ID() string {
	return h.id
}

// Name returns the agent name
func (h *HermesAdapter) Name() string {
	return h.name
}

// Framework returns "hermes"
func (h *HermesAdapter) Framework() string {
	return "hermes"
}

// BaseURL returns empty string (no HTTP API)
func (h *HermesAdapter) BaseURL() string {
	return ""
}

// Health checks if the Hermes gateway is running by reading PID file and state file
func (h *HermesAdapter) Health(ctx context.Context) (*HealthStatus, error) {
	pidFile := filepath.Join(h.configDir, "gateway.pid")
	stateFile := filepath.Join(h.configDir, "gateway_state.json")

	// Read PID file (Hermes uses JSON format)
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return &HealthStatus{Alive: false}, nil
	}

	// Parse JSON to extract PID
	var pidInfo struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(pidData, &pidInfo); err != nil {
		// Try plain text format as fallback
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if parseErr != nil {
			return &HealthStatus{Alive: false}, nil
		}
		pidInfo.PID = pid
	}

	pid := pidInfo.PID
	if pid <= 0 {
		return &HealthStatus{Alive: false}, nil
	}

	// Check if process is alive (send signal 0)
	process, err := os.FindProcess(pid)
	if err != nil {
		return &HealthStatus{Alive: false}, nil
	}

	// On Unix, Signal(0) checks process existence without affecting it
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return &HealthStatus{Alive: false}, nil
	}

	// Process is alive, now get stats
	rss, cpu, uptime := processStats(pid)

	health := &HealthStatus{
		Alive:  true,
		PID:    pid,
		RAM:    rss,
		CPU:    cpu,
		Uptime: uptime,
	}

	// Try to read state file for additional info
	stateData, err := os.ReadFile(stateFile)
	if err == nil {
		var state map[string]interface{}
		if err := json.Unmarshal(stateData, &state); err == nil {
			// Extract component health if available
			if components, ok := state["components"].(map[string]interface{}); ok {
				health.Components = make(map[string]ComponentHealth)
				for name, data := range components {
					if comp, ok := data.(map[string]interface{}); ok {
						health.Components[name] = parseComponentHealth(comp)
					}
				}
			}
		}
	}

	return health, nil
}

// parseComponentHealth extracts component health from state JSON
func parseComponentHealth(data map[string]interface{}) ComponentHealth {
	comp := ComponentHealth{}

	if status, ok := data["status"].(string); ok {
		comp.Status = status
	}

	if lastError, ok := data["last_error"].(string); ok {
		comp.LastError = lastError
	}

	if restarts, ok := data["restart_count"].(float64); ok {
		comp.RestartCount = int(restarts)
	}

	return comp
}

// Status returns agent status by parsing config file
func (h *HermesAdapter) Status(ctx context.Context) (*AgentStatus, error) {
	// Parse config.yaml to extract provider, model, channels
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config struct {
		Model struct {
			Default  string `yaml:"default"`
			Provider string `yaml:"provider"`
		} `yaml:"model"`
		Channels map[string]interface{} `yaml:"channels"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Extract enabled channels
	channels := make([]string, 0)
	for name, settings := range config.Channels {
		if settingsMap, ok := settings.(map[string]interface{}); ok {
			if enabled, ok := settingsMap["enabled"].(bool); ok && enabled {
				channels = append(channels, name)
			}
		}
	}

	provider := config.Model.Provider
	if provider == "" || provider == "auto" {
		provider = "openrouter" // Default inference provider
	}

	return &AgentStatus{
		Provider:    provider,
		Model:       config.Model.Default,
		Channels:    channels,
		Skills:      0,
		GatewayPort: 0, // Hermes doesn't have a single gateway port
	}, nil
}

// Config returns the agent configuration
func (h *HermesAdapter) Config(ctx context.Context) (*AgentConfig, error) {
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	return &AgentConfig{
		Raw:    string(data),
		Format: "yaml",
	}, nil
}

// Start starts the Hermes gateway
func (h *HermesAdapter) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, h.binaryPath, "gateway", "start")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Stop stops the Hermes gateway by killing the process
func (h *HermesAdapter) Stop(ctx context.Context) error {
	pidFile := filepath.Join(h.configDir, "gateway.pid")

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	// Parse JSON to extract PID (same logic as Health method)
	var pidInfo struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(pidData, &pidInfo); err != nil {
		// Try plain text format as fallback
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if parseErr != nil {
			return fmt.Errorf("invalid PID format: %w", parseErr)
		}
		pidInfo.PID = pid
	}

	pid := pidInfo.PID
	if pid <= 0 {
		return fmt.Errorf("invalid PID value: %d", pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop process: %w", err)
	}

	// Wait up to 10 seconds for graceful shutdown
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		if err := process.Signal(syscall.Signal(0)); err != nil {
			// Process is gone
			return nil
		}
	}

	// Force kill if still running
	return process.Kill()
}

// Restart restarts the Hermes gateway
func (h *HermesAdapter) Restart(ctx context.Context) error {
	if err := h.Stop(ctx); err != nil {
		return err
	}

	// Wait a bit before restarting
	time.Sleep(2 * time.Second)

	return h.Start(ctx)
}

// TailLogs tails the Hermes log file
func (h *HermesAdapter) TailLogs(ctx context.Context) (<-chan LogEntry, error) {
	logDir := filepath.Join(h.configDir, "logs")
	logFile := filepath.Join(logDir, "gateway.log")

	ch := make(chan LogEntry, 100)

	go func() {
		defer close(ch)

		// Read last 100 lines of existing log
		if entries := readHistoricalLogs(logFile, 100, parseHermesLogLine); len(entries) > 0 {
			for _, entry := range entries {
				select {
				case ch <- entry:
				case <-ctx.Done():
					return
				}
			}
		}

		// Tail the log file
		cmd := exec.CommandContext(ctx, "tail", "-f", "-n", "0", logFile)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}

		if err := cmd.Start(); err != nil {
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if entry := parseHermesLogLine(line); entry != nil {
				select {
				case ch <- *entry:
				case <-ctx.Done():
					cmd.Process.Kill()
					return
				}
			}
		}

		cmd.Wait()
	}()

	return ch, nil
}

// parseHermesLogLine parses a Hermes log line
// Expected format: "2026-03-20 22:33:02,232 INFO gateway.run: message"
func parseHermesLogLine(raw string) *LogEntry {
	// Parse Hermes Python log format: "YYYY-MM-DD HH:MM:SS,mmm LEVEL module.function: message"
	parts := strings.SplitN(raw, " ", 4)
	if len(parts) < 4 {
		return &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   raw,
		}
	}

	// Combine date and time parts: "2026-03-20 22:33:02,232"
	timestampStr := parts[0] + " " + parts[1]
	// Replace comma with dot for milliseconds
	timestampStr = strings.Replace(timestampStr, ",", ".", 1)

	timestamp, err := time.Parse("2006-01-02 15:04:05.000", timestampStr)
	if err != nil {
		timestamp = time.Now()
	}

	level := strings.ToUpper(parts[2])
	message := parts[3]

	return &LogEntry{
		Timestamp: timestamp,
		Level:     level,
		Message:   message,
	}
}

// TailActivity tails the Hermes log file and extracts activity events
func (h *HermesAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
	logDir := filepath.Join(h.configDir, "logs")
	logFile := filepath.Join(logDir, "gateway.log")

	ch := make(chan ActivityEvent, 100)

	go func() {
		defer close(ch)

		// Read last 50 lines of existing log for recent activity
		if entries := readHistoricalLogs(logFile, 50, parseHermesLogLine); len(entries) > 0 {
			for _, entry := range entries {
				if event := logEntryToActivity(&entry); event != nil {
					select {
					case ch <- *event:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		// Tail the log file for new activity
		cmd := exec.CommandContext(ctx, "tail", "-f", "-n", "0", logFile)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}

		if err := cmd.Start(); err != nil {
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if entry := parseHermesLogLine(line); entry != nil {
				if event := logEntryToActivity(entry); event != nil {
					select {
					case ch <- *event:
					case <-ctx.Done():
						cmd.Process.Kill()
						return
					}
				}
			}
		}

		cmd.Wait()
	}()

	return ch, nil
}

// logEntryToActivity converts a Hermes log entry to an ActivityEvent
func logEntryToActivity(entry *LogEntry) *ActivityEvent {
	msg := entry.Message

	// Tool execution events
	if strings.Contains(msg, "tool:") || strings.Contains(msg, "executing") {
		if strings.Contains(msg, "result") {
			return &ActivityEvent{
				Timestamp:   entry.Timestamp,
				Type:        "tool_call",
				Summary:     extractToolSummary(msg),
				FullContent: msg,
			}
		}
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "tool_call_start",
			Summary:     extractToolSummary(msg),
			FullContent: msg,
		}
	}

	// LLM request/response events
	if strings.Contains(msg, "LLM") || strings.Contains(msg, "model") || strings.Contains(msg, "completion") {
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "llm_request",
			Summary:     extractLLMSummary(msg),
			FullContent: msg,
		}
	}

	// Session events
	if strings.Contains(msg, "session") {
		if strings.Contains(msg, "start") || strings.Contains(msg, "creat") {
			return &ActivityEvent{
				Timestamp:   entry.Timestamp,
				Type:        "agent_start",
				Summary:     "Session started",
				FullContent: msg,
			}
		}
		if strings.Contains(msg, "end") || strings.Contains(msg, "clos") {
			return &ActivityEvent{
				Timestamp:   entry.Timestamp,
				Type:        "agent_end",
				Summary:     "Session ended",
				FullContent: msg,
			}
		}
	}

	// Chat/message events
	if strings.Contains(msg, "message") || strings.Contains(msg, "received") || strings.Contains(msg, "sent") {
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "chat",
			Summary:     extractMessageSummary(msg),
			FullContent: msg,
		}
	}

	// Error events
	if entry.Level == "ERROR" || entry.Level == "CRITICAL" {
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "error",
			Summary:     extractErrorSummary(msg),
			FullContent: msg,
		}
	}

	// Skip debug/info logs that don't match activity patterns
	return nil
}

// extractToolSummary extracts a summary from tool-related log messages
func extractToolSummary(msg string) string {
	// Try to extract tool name from patterns like "tool: name" or "executing: name"
	if idx := strings.Index(msg, "tool:"); idx != -1 {
		rest := strings.TrimSpace(msg[idx+5:])
		if parts := strings.Fields(rest); len(parts) > 0 {
			return "Tool: " + parts[0]
		}
	}
	if idx := strings.Index(msg, "executing"); idx != -1 {
		rest := strings.TrimSpace(msg[idx+9:])
		if parts := strings.Fields(rest); len(parts) > 0 {
			return "Executing: " + parts[0]
		}
	}
	return "Tool execution"
}

// extractLLMSummary extracts a summary from LLM-related log messages
func extractLLMSummary(msg string) string {
	if strings.Contains(msg, "request") {
		return "LLM request sent"
	}
	if strings.Contains(msg, "response") || strings.Contains(msg, "completion") {
		return "LLM response received"
	}
	return "LLM activity"
}

// extractMessageSummary extracts a summary from message-related log messages
func extractMessageSummary(msg string) string {
	if strings.Contains(msg, "received") {
		return "Message received"
	}
	if strings.Contains(msg, "sent") {
		return "Message sent"
	}
	return "Message activity"
}

// extractErrorSummary extracts a summary from error log messages
func extractErrorSummary(msg string) string {
	// Take first 100 chars as summary
	if len(msg) > 100 {
		return msg[:100] + "..."
	}
	return msg
}

// Sessions returns conversation sessions
func (h *HermesAdapter) Sessions(ctx context.Context) ([]Session, error) {
	// Open Hermes SQLite database
	dbPath := filepath.Join(h.configDir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Query sessions ordered by most recent
	rows, err := db.QueryContext(ctx, `
		SELECT id, source, title, started_at, message_count
		FROM sessions
		ORDER BY started_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		var (
			id           string
			source       string
			title        sql.NullString
			startedAt    float64
			messageCount int
		)

		if err := rows.Scan(&id, &source, &title, &startedAt, &messageCount); err != nil {
			continue
		}

		// Convert Unix timestamp to time
		lastMsg := time.Unix(int64(startedAt), 0)

		sessionTitle := title.String
		if sessionTitle == "" {
			sessionTitle = fmt.Sprintf("Session %s", id[:8])
		}

		sessions = append(sessions, Session{
			Key:      id,
			Title:    sessionTitle,
			LastMsg:  &lastMsg,
			Channel:  source,
			ReadOnly: false,
		})
	}

	return sessions, nil
}

// ChatHistory returns chat messages for a session
func (h *HermesAdapter) ChatHistory(ctx context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	// Open Hermes SQLite database
	dbPath := filepath.Join(h.configDir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Query messages for this session
	query := `
		SELECT role, content, tool_calls, timestamp
		FROM messages
		WHERE session_id = ?
		ORDER BY timestamp ASC
	`
	if limit > 0 {
		query = fmt.Sprintf("%s LIMIT %d", query, limit)
	}

	rows, err := db.QueryContext(ctx, query, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var (
			role      string
			content   sql.NullString
			toolCalls sql.NullString
			timestamp float64
		)

		if err := rows.Scan(&role, &content, &toolCalls, &timestamp); err != nil {
			continue
		}

		// Convert Unix timestamp
		msgTime := time.Unix(int64(timestamp), 0)

		msg := ChatMessage{
			Timestamp: msgTime,
			Role:      role,
			Content:   content.String,
			Channel:   "hermes",
		}

		// Parse tool calls if present
		if toolCalls.Valid && toolCalls.String != "" {
			var calls []map[string]interface{}
			if err := json.Unmarshal([]byte(toolCalls.String), &calls); err == nil {
				for _, call := range calls {
					if name, ok := call["function"].(map[string]interface{})["name"].(string); ok {
						msg.Parts = append(msg.Parts, ChatPart{
							Type: "tool_call",
							Name: name,
						})
					}
				}
			}
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// SendMessage sends a message and waits for the response
func (h *HermesAdapter) SendMessage(ctx context.Context, message, sessionKey string) (*ChatMessage, error) {
	args := []string{"chat", "-q", message}

	// If a session key is provided, use --resume to continue that session
	if sessionKey != "" && sessionKey != "new" {
		args = []string{"chat", "--resume", sessionKey, "-q", message}
	}

	cmd := exec.CommandContext(ctx, h.binaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("hermes chat failed: %w\nOutput: %s", err, string(output))
	}

	// Parse the output - hermes chat returns the assistant's response
	response := strings.TrimSpace(string(output))
	if response == "" {
		return nil, fmt.Errorf("empty response from hermes")
	}

	return &ChatMessage{
		Timestamp: time.Now(),
		Role:      "assistant",
		Content:   response,
	}, nil
}

// StreamMessage sends a message and streams the response
func (h *HermesAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	args := []string{"chat"}

	// If a session key is provided, use --resume to continue that session
	if sessionKey != "" && sessionKey != "new" {
		args = append(args, "--resume", sessionKey)
	}

	cmd := exec.CommandContext(ctx, h.binaryPath, args...)

	// Create pipes for stdin/stdout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start hermes chat: %w", err)
	}

	// Send the message to stdin
	if _, err := stdin.Write([]byte(message + "\n")); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("failed to write message: %w", err)
	}
	stdin.Close()

	ch := make(chan ChatEvent, 64)
	go h.readChatStream(ctx, cmd, stdout, ch)
	return ch, nil
}

// readChatStream reads from hermes chat stdout and emits chat events
func (h *HermesAdapter) readChatStream(ctx context.Context, cmd *exec.Cmd, stdout io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer stdout.Close()
	defer cmd.Wait()

	scanner := bufio.NewScanner(stdout)
	var fullResponse strings.Builder

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		// Hermes CLI outputs the response line by line
		// Treat each line as a delta
		fullResponse.WriteString(line)
		fullResponse.WriteString("\n")

		select {
		case ch <- ChatEvent{Type: "delta", Content: line + "\n"}:
		case <-ctx.Done():
			cmd.Process.Kill()
			return
		}
	}

	// Send final done event with full response
	select {
	case ch <- ChatEvent{Type: "done", Content: strings.TrimSpace(fullResponse.String())}:
	case <-ctx.Done():
	}
}

// Interrupt is a no-op for Hermes — cancellation already works via
// exec.CommandContext, which kills the subprocess when ctx is cancelled.
func (h *HermesAdapter) Interrupt(_ context.Context, _ string) error { return nil }

// CreateSession creates a new conversation session
func (h *HermesAdapter) CreateSession(ctx context.Context, name string) (*Session, error) {
	// Hermes creates sessions automatically when chatting begins.
	// Sessions are created implicitly when:
	// 1. A message arrives from a messaging platform (Telegram, Discord, etc.)
	// 2. The user runs: hermes chat (starts a new session)
	// 3. The user runs: hermes chat --resume <session_id> (resumes existing session)
	// There is no standalone command to create an empty session.
	return nil, fmt.Errorf("create session not supported for Hermes (sessions are created automatically when chatting; use 'hermes chat' to start a new session)")
}

// ResetSession deletes a conversation session
func (h *HermesAdapter) ResetSession(ctx context.Context, sessionKey string) error {
	cmd := exec.CommandContext(ctx, h.binaryPath, "sessions", "delete", sessionKey)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete session: %w (output: %s)", err, string(output))
	}
	return nil
}

// DeleteSession permanently removes a session
func (h *HermesAdapter) DeleteSession(ctx context.Context, sessionKey string) error {
	// Hermes doesn't distinguish between delete and purge - both permanently remove
	return h.ResetSession(ctx, sessionKey)
}

// Personality returns the agent's personality
func (h *HermesAdapter) Personality(ctx context.Context) (*Personality, error) {
	personality := &Personality{
		Name: h.name,
	}

	// Try to extract personality from SOUL.md
	soulPath := filepath.Join(h.configDir, "SOUL.md")
	if data, err := os.ReadFile(soulPath); err == nil {
		if prompt := extractSoulPrompt(string(data)); prompt != "" {
			personality.SystemPrompt = prompt
		}
	}

	// If no SOUL.md personality found, try to get default from config
	if personality.SystemPrompt == "" {
		if data, err := os.ReadFile(h.configPath); err == nil {
			var config struct {
				Model struct {
					Personalities map[string]string `yaml:"personalities"`
				} `yaml:"model"`
			}
			if err := yaml.Unmarshal(data, &config); err == nil {
				// Use "helpful" as default personality
				if helpful, ok := config.Model.Personalities["helpful"]; ok {
					personality.SystemPrompt = helpful
				}
			}
		}
	}

	// Store the raw SOUL.md file
	if soulData, err := os.ReadFile(soulPath); err == nil {
		personality.IdentityFiles = map[string]string{
			"SOUL.md": string(soulData),
		}
	}

	return personality, nil
}

func (h *HermesAdapter) Capabilities() AgentCapabilities {
	return AgentCapabilities{CommanderCapable: true}
}
