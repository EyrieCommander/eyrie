package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// PicoClawAdapter communicates with a PicoClaw instance via the web backend's
// REST API (sessions, config, lifecycle) and the gateway's health endpoint.
// Chat streaming uses the Pico Protocol WebSocket with delta diffing.
//
// Architecture: PicoClaw has a two-tier design — a gateway (agent runtime,
// default port 18790) and a web backend (launcher + API proxy, default port
// 18800 = gatewayPort + 10). Eyrie stores the gateway port in DiscoveredAgent
// for health probing (consistent with all adapters) and derives the web port.
type PicoClawAdapter struct {
	id          string
	name        string
	host        string
	gatewayPort int    // health probe port (default 18790)
	webPort     int    // REST/WebSocket port (default 18800, derived as gatewayPort + 10)
	token       string // Pico channel token for WebSocket auth
	configPath  string // path to config.json for offline fallback
	client      *http.Client
}

func NewPicoClawAdapter(id, name, host string, gatewayPort int, token, configPath string) *PicoClawAdapter {
	return &PicoClawAdapter{
		id:          id,
		name:        name,
		host:        host,
		gatewayPort: gatewayPort,
		webPort:     gatewayPort + 10, // PicoClaw convention: web backend = gateway + 10
		token:       token,
		configPath:  configPath,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *PicoClawAdapter) ID() string       { return p.id }
func (p *PicoClawAdapter) Name() string     { return p.name }
func (p *PicoClawAdapter) Framework() string { return "picoclaw" }
func (p *PicoClawAdapter) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", p.host, p.webPort)
}

// Health probes the gateway's /health endpoint for status, uptime, and PID,
// then enriches with process-level stats from ps.
func (p *PicoClawAdapter) Health(ctx context.Context) (*HealthStatus, error) {
	var resp struct {
		Status string  `json:"status"`
		Uptime float64 `json:"uptime"`
		PID    int     `json:"pid"`
	}

	url := fmt.Sprintf("http://%s:%d/health", p.host, p.gatewayPort)
	if err := p.getJSONURL(ctx, url, &resp); err != nil {
		return &HealthStatus{Alive: false}, err
	}

	hs := &HealthStatus{
		Alive:  resp.Status == "ok",
		PID:    resp.PID,
		Uptime: time.Duration(resp.Uptime * float64(time.Second)),
	}

	if hs.PID > 0 {
		hs.RAM, hs.CPU, _ = processStats(hs.PID)
	}

	return hs, nil
}

// Status queries the web backend for gateway status when online, falling back
// to parsing the config file for model/provider/channels when offline.
func (p *PicoClawAdapter) Status(ctx context.Context) (*AgentStatus, error) {
	as := &AgentStatus{
		GatewayPort: p.gatewayPort,
	}

	var resp struct {
		GatewayStatus string `json:"gateway_status"`
		PID           int    `json:"pid"`
	}
	if err := p.getJSON(ctx, "/api/gateway/status", &resp); err == nil {
		// Online — enrich from config for model/provider/channels
		p.enrichStatusFromConfig(as)
		return as, nil
	}

	// Offline fallback: parse config.json directly
	return p.statusFromConfig()
}

// statusFromConfig extracts provider, model, and channels from the JSON config file.
func (p *PicoClawAdapter) statusFromConfig() (*AgentStatus, error) {
	if p.configPath == "" {
		return nil, fmt.Errorf("no config path available")
	}

	data, err := os.ReadFile(p.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	as := &AgentStatus{
		GatewayPort: p.gatewayPort,
	}

	var cfg struct {
		Model struct {
			Default  string `json:"default"`
			Provider string `json:"provider"`
		} `json:"model"`
		Channels map[string]json.RawMessage `json:"channels"`
	}
	if json.Unmarshal(data, &cfg) == nil {
		as.Model = cfg.Model.Default
		as.Provider = cfg.Model.Provider
		if as.Provider == "" && as.Model != "" {
			if idx := strings.Index(as.Model, "/"); idx > 0 {
				as.Provider = as.Model[:idx]
			}
		}
		for ch := range cfg.Channels {
			as.Channels = append(as.Channels, ch)
		}
	}

	return as, nil
}

// enrichStatusFromConfig fills in model/provider/channels from the config file.
func (p *PicoClawAdapter) enrichStatusFromConfig(as *AgentStatus) {
	if p.configPath == "" {
		return
	}
	data, err := os.ReadFile(p.configPath)
	if err != nil {
		return
	}

	var cfg struct {
		Model struct {
			Default  string `json:"default"`
			Provider string `json:"provider"`
		} `json:"model"`
		Channels map[string]json.RawMessage `json:"channels"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return
	}

	if as.Model == "" {
		as.Model = cfg.Model.Default
	}
	if as.Provider == "" {
		as.Provider = cfg.Model.Provider
		if as.Provider == "" && as.Model != "" {
			if idx := strings.Index(as.Model, "/"); idx > 0 {
				as.Provider = as.Model[:idx]
			}
		}
	}
	if len(as.Channels) == 0 {
		for ch := range cfg.Channels {
			as.Channels = append(as.Channels, ch)
		}
	}
}

// Config reads the config.json from disk. Returns the raw JSON content.
func (p *PicoClawAdapter) Config(ctx context.Context) (*AgentConfig, error) {
	// Try the API first when online
	body, err := p.getRaw(ctx, "/api/config")
	if err == nil {
		return &AgentConfig{Raw: body, Format: "json"}, nil
	}

	// Fall back to reading config file directly when agent is offline
	if p.configPath != "" {
		data, readErr := os.ReadFile(p.configPath)
		if readErr == nil {
			return &AgentConfig{Raw: string(data), Format: "json"}, nil
		}
	}

	return nil, err
}

// Start sends a POST to the web backend to start the gateway.
func (p *PicoClawAdapter) Start(ctx context.Context) error {
	return p.postLifecycle(ctx, "start")
}

// Stop sends a POST to the web backend to stop the gateway.
func (p *PicoClawAdapter) Stop(ctx context.Context) error {
	return p.postLifecycle(ctx, "stop")
}

// Restart sends a POST to the web backend to restart the gateway.
func (p *PicoClawAdapter) Restart(ctx context.Context) error {
	return p.postLifecycle(ctx, "restart")
}

// postLifecycle POSTs to /api/gateway/{action} and checks for success.
func (p *PicoClawAdapter) postLifecycle(ctx context.Context, action string) error {
	url := fmt.Sprintf("http://%s:%d/api/gateway/%s", p.host, p.webPort, action)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("creating %s request: %w", action, err)
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("gateway %s: %w", action, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("gateway %s returned %d: %s", action, resp.StatusCode, string(body))
	}

	// Check response for error status
	var result struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(body, &result) == nil && result.Error != "" {
		return fmt.Errorf("gateway %s failed: %s", action, result.Error)
	}

	return nil
}

// TailLogs reads historical log entries from the gateway log file (zerolog JSON),
// then polls the web backend's /api/gateway/logs endpoint every 2 seconds for
// live updates.
func (p *PicoClawAdapter) TailLogs(ctx context.Context) (<-chan LogEntry, error) {
	// Pre-read historical entries from the on-disk log file
	var history []LogEntry
	if p.configPath != "" {
		logPath := picoClawLogPath(p.configPath)
		history = readHistoricalLogs(logPath, defaultHistoryLines, parsePicoClawLogLine)
	}

	ch := make(chan LogEntry, 64)
	go func() {
		defer close(ch)

		// Emit historical entries
		for _, entry := range history {
			select {
			case ch <- entry:
			case <-ctx.Done():
				return
			}
		}

		// Poll /api/gateway/logs for live entries
		var offset int
		var runID string

		// 2-second poll interval for live log updates
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				entries, newOffset, newRunID := p.pollLogs(ctx, offset, runID)
				if newOffset > offset || newRunID != runID {
					offset = newOffset
					runID = newRunID
				}
				for _, entry := range entries {
					select {
					case ch <- entry:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return ch, nil
}

// pollLogs fetches new log entries from the web backend's polling API.
func (p *PicoClawAdapter) pollLogs(ctx context.Context, offset int, runID string) ([]LogEntry, int, string) {
	url := fmt.Sprintf("/api/gateway/logs?log_offset=%d", offset)
	if runID != "" {
		url += "&log_run_id=" + runID
	}

	var resp struct {
		Entries []struct {
			Message string `json:"message"`
			Level   string `json:"level"`
			Time    string `json:"time"`
		} `json:"entries"`
		Offset int    `json:"offset"`
		RunID  string `json:"run_id"`
	}

	if err := p.getJSON(ctx, url, &resp); err != nil {
		return nil, offset, runID
	}

	var entries []LogEntry
	for _, e := range resp.Entries {
		ts, err := time.Parse(time.RFC3339Nano, e.Time)
		if err != nil {
			ts = time.Now()
		}
		entries = append(entries, LogEntry{
			Timestamp: ts,
			Level:     strings.ToLower(e.Level),
			Message:   e.Message,
		})
	}

	newOffset := offset
	if resp.Offset > 0 {
		newOffset = resp.Offset
	} else if len(entries) > 0 {
		newOffset = offset + len(entries)
	}
	newRunID := runID
	if resp.RunID != "" {
		newRunID = resp.RunID
	}

	return entries, newOffset, newRunID
}

// TailActivity reads historical log entries and classifies them as activity
// events, then polls for live updates.
func (p *PicoClawAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
	// Pre-read historical activity from the on-disk log file
	var history []ActivityEvent
	if p.configPath != "" {
		logPath := picoClawLogPath(p.configPath)
		history = readHistoricalActivity(logPath, 50, parsePicoClawActivityLine)
	}

	ch := make(chan ActivityEvent, 64)
	go func() {
		defer close(ch)

		// Emit historical activity
		for _, ev := range history {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}

		// Poll for live activity using the same log polling mechanism
		var offset int
		var runID string

		// 2-second poll interval (same cadence as TailLogs)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				entries, newOffset, newRunID := p.pollLogs(ctx, offset, runID)
				if newOffset > offset || newRunID != runID {
					offset = newOffset
					runID = newRunID
				}
				for _, entry := range entries {
					if ev := picoLogEntryToActivity(&entry); ev != nil {
						select {
						case ch <- *ev:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()
	return ch, nil
}

// picoLogEntryToActivity converts a PicoClaw log entry to an ActivityEvent
// using keyword-based classification, following the same pattern as Hermes's
// logEntryToActivity.
func picoLogEntryToActivity(entry *LogEntry) *ActivityEvent {
	msg := entry.Message
	low := strings.ToLower(msg)

	// Tool execution events
	if strings.Contains(low, "tool") {
		if strings.Contains(low, "result") || strings.Contains(low, "complete") {
			return &ActivityEvent{
				Timestamp:   entry.Timestamp,
				Type:        "tool_call",
				Summary:     msg,
				FullContent: msg,
			}
		}
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "tool_call_start",
			Summary:     msg,
			FullContent: msg,
		}
	}

	// LLM request/response events
	if strings.Contains(low, "llm") || strings.Contains(low, "chat") || strings.Contains(low, "completion") {
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "llm_request",
			Summary:     msg,
			FullContent: msg,
		}
	}

	// Session/agent lifecycle events
	if strings.Contains(low, "gateway") && (strings.Contains(low, "start") || strings.Contains(low, "listen")) {
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "agent_start",
			Summary:     msg,
			FullContent: msg,
		}
	}
	if strings.Contains(low, "session") {
		if strings.Contains(low, "start") || strings.Contains(low, "creat") {
			return &ActivityEvent{
				Timestamp:   entry.Timestamp,
				Type:        "agent_start",
				Summary:     msg,
				FullContent: msg,
			}
		}
		if strings.Contains(low, "end") || strings.Contains(low, "clos") {
			return &ActivityEvent{
				Timestamp:   entry.Timestamp,
				Type:        "agent_end",
				Summary:     msg,
				FullContent: msg,
			}
		}
	}

	// Message events
	if strings.Contains(low, "message") || strings.Contains(low, "received") || strings.Contains(low, "sent") {
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "chat",
			Summary:     msg,
			FullContent: msg,
		}
	}

	// Error events
	if entry.Level == "error" || entry.Level == "fatal" {
		return &ActivityEvent{
			Timestamp:   entry.Timestamp,
			Type:        "error",
			Summary:     msg,
			FullContent: msg,
		}
	}

	// Skip debug/info logs that don't match activity patterns
	return nil
}

// Sessions lists active sessions from the web backend.
func (p *PicoClawAdapter) Sessions(ctx context.Context) ([]Session, error) {
	var resp []struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Preview      string `json:"preview"`
		MessageCount int    `json:"message_count"`
		Created      string `json:"created"`
		Updated      string `json:"updated"`
	}

	if err := p.getJSON(ctx, "/api/sessions", &resp); err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	sessions := make([]Session, 0, len(resp))
	for _, s := range resp {
		sess := Session{
			Key:   s.ID,
			Title: s.Title,
		}
		if sess.Title == "" {
			if s.Preview != "" {
				sess.Title = s.Preview
				if len(sess.Title) > 60 {
					sess.Title = sess.Title[:60] + "..."
				}
			} else {
				sess.Title = "Session " + s.ID[:8]
			}
		}
		if t, err := time.Parse(time.RFC3339Nano, s.Updated); err == nil {
			sess.LastMsg = &t
		} else if t, err := time.Parse(time.RFC3339, s.Updated); err == nil {
			sess.LastMsg = &t
		}
		sessions = append(sessions, sess)
	}

	return sessions, nil
}

// ChatHistory reads message history for a session from the web backend.
// Falls back to reading JSONL from disk if the API is unavailable.
func (p *PicoClawAdapter) ChatHistory(ctx context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	var resp struct {
		Messages []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		} `json:"messages"`
	}

	url := fmt.Sprintf("/api/sessions/%s", sessionKey)
	if err := p.getJSON(ctx, url, &resp); err != nil {
		// Offline fallback: read JSONL from workspace directory
		return p.chatHistoryFromDisk(sessionKey, limit)
	}

	var messages []ChatMessage
	for _, m := range resp.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		var ts time.Time
		if t, err := time.Parse(time.RFC3339Nano, m.Timestamp); err == nil {
			ts = t
		}
		messages = append(messages, ChatMessage{
			Timestamp: ts,
			Role:      m.Role,
			Content:   m.Content,
		})
	}

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// chatHistoryFromDisk reads session JSONL from the workspace directory.
// PicoClaw session keys are "agent:main:pico:direct:pico:<uuid>" on disk,
// but Eyrie uses just the UUID portion.
func (p *PicoClawAdapter) chatHistoryFromDisk(sessionKey string, limit int) ([]ChatMessage, error) {
	if p.configPath == "" {
		return nil, nil
	}
	dir := filepath.Dir(p.configPath)
	// PicoClaw stores sessions as: {workspace}/sessions/agent_main_pico_direct_pico_{uuid}.jsonl
	sessFile := filepath.Join(dir, "workspace", "sessions", "agent_main_pico_direct_pico_"+sessionKey+".jsonl")

	f, err := os.Open(sessFile)
	if err != nil {
		return nil, nil // No transcript file yet — return empty history
	}
	defer f.Close()

	var messages []ChatMessage
	dec := json.NewDecoder(f)
	for dec.More() {
		var entry struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		}
		if dec.Decode(&entry) != nil {
			continue
		}
		if entry.Role != "user" && entry.Role != "assistant" {
			continue
		}
		var ts time.Time
		if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			ts = t
		}
		messages = append(messages, ChatMessage{
			Timestamp: ts,
			Role:      entry.Role,
			Content:   entry.Content,
		})
	}

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// CreateSession generates a new session UUID. PicoClaw creates sessions
// implicitly when a message is sent with a new session_id.
func (p *PicoClawAdapter) CreateSession(_ context.Context, name string) (*Session, error) {
	sessionID := uuid.New().String()
	title := name
	if title == "" {
		title = "Session " + sessionID[:8]
	}
	return &Session{Key: sessionID, Title: title}, nil
}

// ResetSession archives/deletes a session via the web backend API.
func (p *PicoClawAdapter) ResetSession(ctx context.Context, sessionKey string) error {
	return p.deleteSession(ctx, sessionKey)
}

// DeleteSession permanently removes a session via the web backend API.
func (p *PicoClawAdapter) DeleteSession(ctx context.Context, sessionKey string) error {
	return p.deleteSession(ctx, sessionKey)
}

func (p *PicoClawAdapter) deleteSession(ctx context.Context, sessionKey string) error {
	url := fmt.Sprintf("http://%s:%d/api/sessions/%s", p.host, p.webPort, sessionKey)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("creating delete request: %w", err)
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	defer resp.Body.Close()
	// 404 is fine — session is already gone
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete session returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// SendMessage sends a message and collects the full response. Delegates to
// StreamMessage and reads until done, following the ZeroClaw pattern.
func (p *PicoClawAdapter) SendMessage(ctx context.Context, message, sessionKey string) (*ChatMessage, error) {
	ch, err := p.StreamMessage(ctx, message, sessionKey)
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
		return nil, fmt.Errorf("connection closed before terminal frame (session %s)", sessionKey)
	}
	return &ChatMessage{
		Timestamp: time.Now(),
		Role:      "assistant",
		Content:   content,
	}, nil
}

// StreamMessage connects to the Pico Protocol WebSocket and streams the response.
//
// Protocol:
// - Send:    {type: "message.send", session_id, timestamp, payload: {content}}
// - Receive: message.update (streaming delta via full-content diffing)
//            message.create (final complete response)
//            error (error event)
// - Ignored: typing.start, typing.stop, pong
//
// message.update delivers full-content replacements. The adapter diffs successive
// updates to emit append-only deltas for Eyrie's ChatEvent model.
func (p *PicoClawAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	if sessionKey == "" {
		sessionKey = uuid.New().String()
	}

	wsURL := fmt.Sprintf("ws://%s:%d/pico/ws?session_id=%s", p.host, p.webPort, sessionKey)

	opts := &websocket.DialOptions{}
	if p.token != "" {
		opts.HTTPHeader = http.Header{
			"Authorization": []string{"Bearer " + p.token},
		}
	}

	// Short dial timeout to fail fast if unreachable
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	conn, _, err := websocket.Dial(dialCtx, wsURL, opts)
	dialCancel()
	if err != nil {
		return nil, fmt.Errorf("connecting to PicoClaw WS: %w", err)
	}
	conn.SetReadLimit(4 * 1024 * 1024) // 4 MB

	ch := make(chan ChatEvent, 64)
	go func() {
		defer close(ch)
		defer conn.CloseNow()

		// 5-minute read timeout for the entire exchange
		readCtx, readCancel := context.WithTimeout(ctx, 5*time.Minute)
		defer readCancel()

		// Send the user message using the Pico Protocol format
		outgoing := map[string]any{
			"type":       "message.send",
			"session_id": sessionKey,
			"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
			"payload": map[string]string{
				"content": message,
			},
		}
		data, _ := json.Marshal(outgoing)
		if err := conn.Write(readCtx, websocket.MessageText, data); err != nil {
			select {
			case ch <- ChatEvent{Type: "error", Error: fmt.Sprintf("sending message: %v", err)}:
			case <-readCtx.Done():
			}
			return
		}

		// Read response frames, diffing message.update for append-only deltas
		var lastContent string

		for {
			_, data, err := conn.Read(readCtx)
			if err != nil {
				select {
				case ch <- ChatEvent{Type: "error", Error: fmt.Sprintf("connection read: %v", err)}:
				case <-readCtx.Done():
				}
				return
			}

			var frame struct {
				Type    string `json:"type"`
				Payload struct {
					Content string `json:"content"`
				} `json:"payload"`
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if json.Unmarshal(data, &frame) != nil {
				continue
			}

			switch frame.Type {
			case "message.update":
				// Full-content replacement — diff against previous to get the delta
				newContent := frame.Payload.Content
				if len(newContent) > len(lastContent) {
					delta := newContent[len(lastContent):]
					select {
					case ch <- ChatEvent{Type: "delta", Content: delta}:
					case <-readCtx.Done():
						return
					}
				}
				lastContent = newContent

			case "message.create":
				// Final complete response
				finalContent := frame.Payload.Content
				if finalContent == "" {
					finalContent = lastContent
				}
				select {
				case ch <- ChatEvent{Type: "done", Content: finalContent}:
				case <-readCtx.Done():
				}
				return

			case "error":
				errMsg := frame.Error
				if errMsg == "" {
					errMsg = frame.Message
				}
				if errMsg == "" {
					errMsg = "unknown PicoClaw error"
				}
				select {
				case ch <- ChatEvent{Type: "error", Error: errMsg}:
				case <-readCtx.Done():
				}
				return

			case "typing.start", "typing.stop", "pong":
				// Ignore protocol chatter
				continue

			default:
				slog.Debug("picoclaw: unknown frame type", "type", frame.Type)
				continue
			}
		}
	}()
	return ch, nil
}

// Personality reads SOUL.md and IDENTITY.md from the workspace directory,
// and returns config.json as an identity file.
func (p *PicoClawAdapter) Personality(ctx context.Context) (*Personality, error) {
	personality := &Personality{
		Name:          p.name,
		IdentityFiles: make(map[string]string),
	}

	if p.configPath != "" {
		dir := filepath.Dir(p.configPath)
		workspaceDir := filepath.Join(dir, "workspace")

		// Read SOUL.md
		if data, err := os.ReadFile(filepath.Join(workspaceDir, "SOUL.md")); err == nil {
			personality.IdentityFiles["SOUL.md"] = string(data)
			// Extract system prompt from SOUL.md content
			personality.SystemPrompt = extractSoulPrompt(string(data))
		}

		// Read IDENTITY.md
		if data, err := os.ReadFile(filepath.Join(workspaceDir, "IDENTITY.md")); err == nil {
			personality.IdentityFiles["IDENTITY.md"] = string(data)
		}

		// Include config.json as an identity file
		cfg, err := p.Config(ctx)
		if err == nil {
			personality.IdentityFiles["config.json"] = cfg.Raw
		}
	}

	return personality, nil
}

// extractSoulPrompt extracts the system prompt from SOUL.md content,
// skipping comments and empty lines, following the Hermes pattern.
func extractSoulPrompt(content string) string {
	lines := strings.Split(content, "\n")
	var soulContent []string
	inComment := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<!--") {
			inComment = true
		}
		if strings.HasSuffix(trimmed, "-->") {
			inComment = false
			continue
		}
		if inComment || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		soulContent = append(soulContent, trimmed)
	}
	if len(soulContent) > 0 {
		return strings.Join(soulContent, " ")
	}
	return ""
}

// Capabilities reports that PicoClaw is commander-capable.
func (p *PicoClawAdapter) Capabilities() AgentCapabilities {
	return AgentCapabilities{CommanderCapable: true}
}

// --- HTTP helpers ---

// getJSON fetches a JSON response from the web backend and unmarshals it.
func (p *PicoClawAdapter) getJSON(ctx context.Context, path string, target any) error {
	body, err := p.getRaw(ctx, path)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(body), target)
}

// getJSONURL fetches a JSON response from an absolute URL and unmarshals it.
func (p *PicoClawAdapter) getJSONURL(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d: %s", url, resp.StatusCode, string(data))
	}

	return json.Unmarshal(data, target)
}

// getRaw fetches a raw response body from the web backend.
func (p *PicoClawAdapter) getRaw(ctx context.Context, path string) (string, error) {
	url := fmt.Sprintf("http://%s:%d%s", p.host, p.webPort, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request to %s: %w", path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned %d: %s", path, resp.StatusCode, string(data))
	}

	return string(data), nil
}

// picoClawLogPath derives the gateway log path from the config directory.
func picoClawLogPath(configPath string) string {
	dir := filepath.Dir(configPath)
	return filepath.Join(dir, "logs", "gateway.log")
}

// Compile-time interface assertion
var _ Agent = (*PicoClawAdapter)(nil)
