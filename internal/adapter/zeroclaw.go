package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

func isLogLine(line string) bool {
	// ZeroClaw CLI log lines start with a timestamp like "2026-03-22T13:48:53..."
	// or contain log-level markers from the tracing crate
	if len(line) > 20 && (line[4] == '-' && line[7] == '-' && line[10] == 'T') {
		return true
	}
	for _, prefix := range []string{"INFO ", "WARN ", "ERROR ", "DEBUG ", "TRACE "} {
		if strings.Contains(line, prefix) && strings.Contains(line, "zeroclaw") {
			return true
		}
	}
	return false
}

// ZeroClawAdapter communicates with a ZeroClaw instance via its HTTP REST gateway.
// ZeroClaw exposes: GET /health, GET /api/status, GET /api/config, GET /api/events (SSE).
type ZeroClawAdapter struct {
	id         string
	name       string
	baseURL    string
	token      string
	configPath string
	client     *http.Client
}

func NewZeroClawAdapter(id, name, baseURL, token, configPath string) *ZeroClawAdapter {
	return &ZeroClawAdapter{
		id:         id,
		name:       name,
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		configPath: configPath,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (z *ZeroClawAdapter) ID() string        { return z.id }
func (z *ZeroClawAdapter) Name() string      { return z.name }
func (z *ZeroClawAdapter) Framework() string  { return "zeroclaw" }
func (z *ZeroClawAdapter) BaseURL() string    { return z.baseURL }

func (z *ZeroClawAdapter) Health(ctx context.Context) (*HealthStatus, error) {
	var resp struct {
		Status         string `json:"status"`
		Paired         bool   `json:"paired"`
		RequirePairing bool   `json:"require_pairing"`
		Runtime        *struct {
			PID        int `json:"pid"`
			Uptime     int `json:"uptime_seconds"`
			Components map[string]struct {
				Status       string  `json:"status"`
				LastOK       *string `json:"last_ok,omitempty"`
				LastError    string  `json:"last_error,omitempty"`
				RestartCount int     `json:"restart_count"`
			} `json:"components"`
		} `json:"runtime"`
	}

	if err := z.getJSON(ctx, "/health", &resp); err != nil {
		return &HealthStatus{Alive: false}, err
	}

	hs := &HealthStatus{
		Alive:      resp.Status == "ok",
		Components: make(map[string]ComponentHealth),
	}

	if resp.Runtime != nil {
		hs.PID = resp.Runtime.PID
		hs.Uptime = time.Duration(resp.Runtime.Uptime) * time.Second
		hs.RAM, hs.CPU, _ = processStats(hs.PID)
		for name, c := range resp.Runtime.Components {
			ch := ComponentHealth{
				Status:       c.Status,
				LastError:    c.LastError,
				RestartCount: c.RestartCount,
			}
			if c.LastOK != nil {
				t, err := time.Parse(time.RFC3339, *c.LastOK)
				if err == nil {
					ch.LastOK = &t
				}
			}
			hs.Components[name] = ch
		}
	}

	return hs, nil
}

func (z *ZeroClawAdapter) Status(ctx context.Context) (*AgentStatus, error) {
	var resp struct {
		Provider    string          `json:"provider"`
		Model       string          `json:"model"`
		Channels    map[string]bool `json:"channels"`
		GatewayPort int             `json:"gateway_port"`
	}

	if err := z.getJSON(ctx, "/api/status", &resp); err != nil {
		return nil, err
	}

	var channels []string
	for name, enabled := range resp.Channels {
		if enabled {
			channels = append(channels, name)
		}
	}

	return &AgentStatus{
		Provider:    resp.Provider,
		Model:       resp.Model,
		Channels:    channels,
		GatewayPort: resp.GatewayPort,
	}, nil
}

func (z *ZeroClawAdapter) Config(ctx context.Context) (*AgentConfig, error) {
	body, err := z.getRaw(ctx, "/api/config")
	if err != nil {
		return nil, err
	}
	return &AgentConfig{Raw: body, Format: "toml"}, nil
}

func (z *ZeroClawAdapter) Start(ctx context.Context) error {
	return runCLI(ctx, "zeroclaw", "service", "start")
}

func (z *ZeroClawAdapter) Stop(ctx context.Context) error {
	return runCLI(ctx, "zeroclaw", "service", "stop")
}

func (z *ZeroClawAdapter) Restart(ctx context.Context) error {
	return runCLI(ctx, "zeroclaw", "service", "restart")
}

// TailLogs emits historical log entries from the daemon log file, then
// connects to the ZeroClaw SSE event stream for live entries.
func (z *ZeroClawAdapter) TailLogs(ctx context.Context) (<-chan LogEntry, error) {
	// Pre-read historical entries before creating the channel
	var history []LogEntry
	if z.configPath != "" {
		history = readHistoricalLogs(zeroclawLogPath(z.configPath), defaultHistoryLines, parseZeroClawLogLine)
	}

	// Connect to live stream before returning so callers get an immediate error on auth failure
	req, err := http.NewRequestWithContext(ctx, "GET", z.baseURL+"/api/events", nil)
	if err != nil {
		return nil, fmt.Errorf("creating SSE request: %w", err)
	}
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to SSE: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("SSE returned status %d", resp.StatusCode)
	}

	ch := make(chan LogEntry, 64)
	go func() {
		// Emit history first, then hand off to the live SSE reader
		for _, entry := range history {
			select {
			case ch <- entry:
			case <-ctx.Done():
				resp.Body.Close()
				close(ch)
				return
			}
		}
		z.readSSE(ctx, resp.Body, ch)
	}()
	return ch, nil
}

func (z *ZeroClawAdapter) readSSE(ctx context.Context, body io.ReadCloser, ch chan<- LogEntry) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var dataBuf strings.Builder

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
		} else if line == "" && dataBuf.Len() > 0 {
			entry := LogEntry{
				Timestamp: time.Now(),
				Level:     "info",
				Message:   dataBuf.String(),
			}
			// Try to parse as JSON for structured logs
			var structured map[string]any
			if json.Unmarshal([]byte(dataBuf.String()), &structured) == nil {
				if msg, ok := structured["message"].(string); ok {
					entry.Message = msg
				}
				if level, ok := structured["level"].(string); ok {
					entry.Level = level
				}
				entry.Fields = structured
			}
			select {
			case ch <- entry:
			case <-ctx.Done():
				return
			}
			dataBuf.Reset()
		}
	}
}

// TailActivity emits historical activity from the daemon log, then connects to
// the ZeroClaw SSE event stream for live events.
func (z *ZeroClawAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
	history := z.recentConversationActivity(ctx)

	req, err := http.NewRequestWithContext(ctx, "GET", z.baseURL+"/api/events", nil)
	if err != nil {
		return nil, fmt.Errorf("creating SSE request: %w", err)
	}
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to SSE: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("SSE returned status %d", resp.StatusCode)
	}

	ch := make(chan ActivityEvent, 64)
	go func() {
		for _, ev := range history {
			select {
			case ch <- ev:
			case <-ctx.Done():
				resp.Body.Close()
				close(ch)
				return
			}
		}
		z.readActivitySSE(ctx, resp.Body, ch)
	}()
	return ch, nil
}

// recentConversationActivity loads conversation memory entries and converts
// them to ActivityEvents so the Activity tab has meaningful content on load.
// Inserts separator events at conversation boundaries detected by session_id
// changes or time gaps >30 minutes.
func (z *ZeroClawAdapter) recentConversationActivity(ctx context.Context) []ActivityEvent {
	entries, err := z.fetchMemoryEntries(ctx)
	if err != nil || len(entries) == 0 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	limit := 50
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	events := make([]ActivityEvent, 0, len(entries)*2)
	for i, e := range entries {
		if i > 0 {
			if sep := detectConversationBoundary(entries[i-1], entries[i]); sep != nil {
				events = append(events, *sep)
			}
		}

		role := "user"
		if strings.HasPrefix(e.Key, "assistant_resp") {
			role = "assistant"
		}
		summary := e.Content
		fullContent := ""
		if len(summary) > 100 {
			fullContent = fmt.Sprintf("[%s] %s", role, summary)
			summary = summary[:100] + "..."
		}
		events = append(events, ActivityEvent{
			Timestamp:   e.Timestamp,
			Type:        "chat",
			Summary:     fmt.Sprintf("[%s] %s", role, summary),
			FullContent: fullContent,
		})
	}
	return events
}

const conversationGap = 30 * time.Minute

func detectConversationBoundary(prev, cur zcMemoryEntry) *ActivityEvent {
	if prev.SessionID != "" && cur.SessionID != "" && prev.SessionID != cur.SessionID {
		return &ActivityEvent{
			Timestamp: cur.Timestamp,
			Type:      "separator",
			Summary:   cur.Timestamp.Format("Jan 2, 3:04 PM"),
		}
	}
	if cur.Timestamp.Sub(prev.Timestamp) >= conversationGap {
		return &ActivityEvent{
			Timestamp: cur.Timestamp,
			Type:      "separator",
			Summary:   cur.Timestamp.Format("Jan 2, 3:04 PM"),
		}
	}
	return nil
}

func (z *ZeroClawAdapter) readActivitySSE(ctx context.Context, body io.ReadCloser, ch chan<- ActivityEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var eventType string
	var dataBuf strings.Builder

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
		} else if line == "" && dataBuf.Len() > 0 {
			if ev := z.parseActivityEvent(eventType, dataBuf.String()); ev != nil {
				select {
				case ch <- *ev:
				case <-ctx.Done():
					return
				}
			}
			eventType = ""
			dataBuf.Reset()
		}
	}
}

func (z *ZeroClawAdapter) parseActivityEvent(eventType, data string) *ActivityEvent {
	var fields map[string]any
	_ = json.Unmarshal([]byte(data), &fields)
	if fields == nil {
		fields = map[string]any{}
	}

	ev := &ActivityEvent{
		Timestamp: time.Now(),
		Fields:    fields,
	}

	str := func(key string) string {
		if v, ok := fields[key].(string); ok {
			return v
		}
		return ""
	}
	num := func(key string) float64 {
		if v, ok := fields[key].(float64); ok {
			return v
		}
		return 0
	}

	// Use the SSE event: field if present, otherwise infer from JSON "type" field
	typ := eventType
	if typ == "" {
		typ = str("type")
	}
	if typ == "" {
		typ = "log"
	}

	ev.Type = typ

	switch typ {
	case "agent_start":
		ev.Summary = fmt.Sprintf("Agent session started (%s)", str("model"))
	case "agent_end":
		dur := time.Duration(num("duration_ms")) * time.Millisecond
		cost := num("cost_usd")
		if cost > 0 {
			ev.Summary = fmt.Sprintf("Session complete (%s, $%.4f)", dur.Round(time.Millisecond), cost)
		} else {
			ev.Summary = fmt.Sprintf("Session complete (%s)", dur.Round(time.Millisecond))
		}
	case "tool_call_start":
		ev.Summary = fmt.Sprintf("Calling tool: %s", str("tool"))
	case "tool_call":
		dur := time.Duration(num("duration_ms")) * time.Millisecond
		ok := "OK"
		if v, exists := fields["success"]; exists {
			if b, isBool := v.(bool); isBool && !b {
				ok = "FAIL"
			}
		}
		ev.Summary = fmt.Sprintf("%s (%s) %s", str("tool"), dur.Round(time.Millisecond), ok)
	case "llm_request":
		ev.Summary = fmt.Sprintf("LLM request to %s/%s", str("provider"), str("model"))
	case "error":
		ev.Summary = fmt.Sprintf("Error in %s: %s", str("component"), str("message"))
	default:
		if msg := str("message"); msg != "" {
			ev.Summary = msg
		} else {
			ev.Summary = data
		}
	}

	return ev
}

// Sessions returns a single synthetic session representing the ZeroClaw
// conversation memory (ZeroClaw stores flat memories, not discrete sessions).
func (z *ZeroClawAdapter) Sessions(ctx context.Context) ([]Session, error) {
	body, err := z.getRaw(ctx, "/api/sessions")
	if err != nil {
		return z.sessionsLegacy(ctx)
	}

	var resp struct {
		Sessions []struct {
			ID           string  `json:"id"`
			Name         string  `json:"name"`
			CreatedAt    string  `json:"created_at"`
			Active       bool    `json:"active"`
			MessageCount int     `json:"message_count"`
			LastMessage  *string `json:"last_message"`
		} `json:"sessions"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return z.sessionsLegacy(ctx)
	}

	sessions := make([]Session, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		sess := Session{
			Key:      s.ID,
			Title:    s.Name,
			ReadOnly: false, // ZeroClaw's "active" flag means "currently focused", not "writable"
		}
		if s.LastMessage != nil {
			if t, err := time.Parse(time.RFC3339Nano, *s.LastMessage); err == nil {
				sess.LastMsg = &t
			}
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func (z *ZeroClawAdapter) sessionsLegacy(ctx context.Context) ([]Session, error) {
	entries, err := z.fetchMemoryEntries(ctx)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	var newest time.Time
	for _, e := range entries {
		if e.Timestamp.After(newest) {
			newest = e.Timestamp
		}
	}
	return []Session{{
		Key:     "memory",
		Title:   fmt.Sprintf("Conversations (%d messages)", len(entries)),
		LastMsg: &newest,
	}}, nil
}

// ChatHistory fetches conversation memories from the ZeroClaw REST API.
// Always fetches all conversation entries, then filters client-side.
// The default (active) session claims legacy messages that have no session_id tag.
func (z *ZeroClawAdapter) ChatHistory(ctx context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	all, err := z.fetchMemoryEntriesFrom(ctx, "/api/memory?category=conversation")
	if err != nil {
		return nil, err
	}

	isDefault := z.isDefaultSession(ctx, sessionKey)
	var entries []zcMemoryEntry
	for _, e := range all {
		if sessionKey == "" || sessionKey == "memory" {
			entries = append(entries, e)
		} else if e.SessionID == sessionKey {
			entries = append(entries, e)
		} else if e.SessionID == "" && isDefault {
			entries = append(entries, e)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	messages := make([]ChatMessage, 0, len(entries))
	for _, e := range entries {
		role := "user"
		if strings.HasPrefix(e.Key, "assistant_resp") {
			role = "assistant"
		}
		messages = append(messages, ChatMessage{
			Timestamp: e.Timestamp,
			Role:      role,
			Content:   e.Content,
		})
	}
	return messages, nil
}

// isDefaultSession returns true if the given sessionKey is the "main" session.
// Legacy untagged messages (those with no session_id) are attributed to it.
// We identify it by name rather than the active flag, since the active session
// can change when Eyrie activates a briefing session.
func (z *ZeroClawAdapter) isDefaultSession(ctx context.Context, sessionKey string) bool {
	body, err := z.getRaw(ctx, "/api/sessions")
	if err != nil {
		return true
	}
	var resp struct {
		Sessions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"sessions"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return true
	}
	for _, s := range resp.Sessions {
		if s.Name == "main" && s.ID == sessionKey {
			return true
		}
	}
	if len(resp.Sessions) == 0 {
		return true
	}
	return false
}

type zcMemoryEntry struct {
	Content   string    `json:"content"`
	Timestamp time.Time `json:"-"`
	RawTS     string    `json:"timestamp"`
	Category  string    `json:"category"`
	Key       string    `json:"key"`
	SessionID string    `json:"session_id"`
}

func (z *ZeroClawAdapter) fetchMemoryEntries(ctx context.Context) ([]zcMemoryEntry, error) {
	return z.fetchMemoryEntriesFrom(ctx, "/api/memory")
}

func (z *ZeroClawAdapter) fetchMemoryEntriesFrom(ctx context.Context, path string) ([]zcMemoryEntry, error) {
	body, err := z.getRaw(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetching memory: %w", err)
	}

	var resp struct {
		Entries []zcMemoryEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parsing memory response: %w", err)
	}

	var filtered []zcMemoryEntry
	for i := range resp.Entries {
		e := &resp.Entries[i]
		if e.Category != "conversation" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, e.RawTS); err == nil {
			e.Timestamp = t
		} else if t, err := time.Parse("2006-01-02T15:04:05.999999999-07:00", e.RawTS); err == nil {
			e.Timestamp = t
		}
		filtered = append(filtered, *e)
	}
	return filtered, nil
}

func (z *ZeroClawAdapter) SendMessage(ctx context.Context, message, _ string) (*ChatMessage, error) {
	// Use the CLI agent path for full history, tools, and memory support.
	cmd := exec.CommandContext(ctx, "zeroclaw", "agent", "-m", message)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("zeroclaw agent: %w: %s", err, string(output))
	}

	// Filter out CLI log lines
	var clean strings.Builder
	for _, line := range strings.Split(string(output), "\n") {
		stripped := ansiRe.ReplaceAllString(line, "")
		if isLogLine(stripped) {
			continue
		}
		clean.WriteString(line)
		clean.WriteString("\n")
	}

	return &ChatMessage{
		Timestamp: time.Now(),
		Role:      "assistant",
		Content:   strings.TrimSpace(clean.String()),
	}, nil
}

func (z *ZeroClawAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	// Activate the target session if specified, so the CLI uses it
	if sessionKey != "" {
		if err := z.ActivateSession(ctx, sessionKey); err != nil {
			// Non-fatal: log and continue with whatever session is active
			fmt.Fprintf(os.Stderr, "eyrie: failed to activate session %s: %v\n", sessionKey, err)
		}
	}

	// Use the CLI agent path for full history, tools, and memory support.
	args := []string{"agent", "-m", message}

	cmd := exec.CommandContext(ctx, "zeroclaw", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting zeroclaw agent: %w", err)
	}

	ch := make(chan ChatEvent, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
		var full strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			stripped := ansiRe.ReplaceAllString(line, "")
			if isLogLine(stripped) {
				continue
			}
			full.WriteString(line)
			full.WriteString("\n")
			select {
			case ch <- ChatEvent{Type: "delta", Content: line + "\n"}:
			case <-ctx.Done():
				cmd.Process.Kill()
				return
			}
		}
		if err := cmd.Wait(); err != nil {
			select {
			case ch <- ChatEvent{Type: "error", Error: fmt.Sprintf("zeroclaw agent exited: %v", err)}:
			case <-ctx.Done():
			}
			return
		}
		content := strings.TrimSpace(full.String())
		select {
		case ch <- ChatEvent{Type: "done", Content: content}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}



func (z *ZeroClawAdapter) CreateSession(ctx context.Context, name string) (*Session, error) {
	payload := fmt.Sprintf(`{"name":%q}`, name)
	req, err := http.NewRequestWithContext(ctx, "POST", z.baseURL+"/api/sessions", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create session returned %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Session struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			CreatedAt string `json:"created_at"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding create session response: %w", err)
	}
	return &Session{Key: result.Session.ID, Title: result.Session.Name}, nil
}

func (z *ZeroClawAdapter) ResetSession(ctx context.Context, sessionKey string) error {
	if sessionKey == "" || sessionKey == "memory" {
		return fmt.Errorf("cannot delete the legacy memory session")
	}
	req, err := http.NewRequestWithContext(ctx, "DELETE", z.baseURL+"/api/sessions/"+sessionKey, nil)
	if err != nil {
		return err
	}
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("delete session returned %d", resp.StatusCode)
	}
	return nil
}

func (z *ZeroClawAdapter) DeleteSession(ctx context.Context, sessionKey string) error {
	return z.ResetSession(ctx, sessionKey)
}

func (z *ZeroClawAdapter) DestroySession(ctx context.Context, sessionKey string) error {
	return z.ResetSession(ctx, sessionKey)
}

// ActivateSession sets a session as the active one for CLI agent interactions.
func (z *ZeroClawAdapter) ActivateSession(ctx context.Context, sessionKey string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", z.baseURL+"/api/sessions/"+sessionKey+"/activate", nil)
	if err != nil {
		return err
	}
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("activating session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("activate session returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (z *ZeroClawAdapter) Personality(ctx context.Context) (*Personality, error) {
	// ZeroClaw's personality is embedded in its config (system prompt, agent name).
	// A future version could read dedicated personality files.
	cfg, err := z.Config(ctx)
	if err != nil {
		return nil, err
	}
	return &Personality{
		Name: z.name,
		IdentityFiles: map[string]string{
			"config.toml": cfg.Raw,
		},
	}, nil
}

func (z *ZeroClawAdapter) getJSON(ctx context.Context, path string, target any) error {
	body, err := z.getRaw(ctx, path)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(body), target)
}

func (z *ZeroClawAdapter) getRaw(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", z.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}

	resp, err := z.client.Do(req)
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

func runCLI(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", command, strings.Join(args, " "), err, string(output))
	}
	return nil
}

func (z *ZeroClawAdapter) Capabilities() AgentCapabilities {
	return AgentCapabilities{CommanderCapable: false}
}
