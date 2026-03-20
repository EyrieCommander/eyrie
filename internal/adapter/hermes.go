package adapter

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return fmt.Errorf("invalid PID: %w", err)
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
// Expected format: "2026-03-19T12:34:56.789+00:00 [INFO] message"
func parseHermesLogLine(raw string) *LogEntry {
	// Simple parsing - TODO: improve with actual Hermes log format
	parts := strings.SplitN(raw, " ", 3)
	if len(parts) < 3 {
		return &LogEntry{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   raw,
		}
	}

	timestamp, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		timestamp = time.Now()
	}

	level := strings.Trim(parts[1], "[]")
	message := parts[2]

	return &LogEntry{
		Timestamp: timestamp,
		Level:     level,
		Message:   message,
	}
}

// TailActivity is not implemented for Hermes yet
func (h *HermesAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
	return nil, fmt.Errorf("activity streaming not implemented for Hermes")
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
	// TODO: Implement via CLI invocation
	return nil, fmt.Errorf("send message not implemented for Hermes")
}

// StreamMessage sends a message and streams the response
func (h *HermesAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	// TODO: Implement via CLI invocation with streaming
	return nil, fmt.Errorf("stream message not implemented for Hermes")
}

// CreateSession creates a new conversation session
func (h *HermesAdapter) CreateSession(ctx context.Context, name string) (*Session, error) {
	// TODO: Implement via CLI invocation
	return nil, fmt.Errorf("create session not implemented for Hermes")
}

// DeleteSession deletes a conversation session
func (h *HermesAdapter) DeleteSession(ctx context.Context, sessionKey string) error {
	// TODO: Implement via CLI invocation
	return fmt.Errorf("delete session not implemented for Hermes")
}

// PurgeSession permanently removes a session
func (h *HermesAdapter) PurgeSession(ctx context.Context, sessionKey string) error {
	// TODO: Implement via CLI invocation
	return fmt.Errorf("purge session not implemented for Hermes")
}

// Personality returns the agent's personality
func (h *HermesAdapter) Personality(ctx context.Context) (*Personality, error) {
	// TODO: Extract from Hermes config
	return &Personality{
		Name: h.name,
	}, nil
}
