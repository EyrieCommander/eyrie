package adapter

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"nhooyr.io/websocket"
)

// ZeroClawAdapter communicates with a ZeroClaw instance via its HTTP REST gateway
// and WebSocket chat endpoint.
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

	if err := z.getJSON(ctx, "/api/status", &resp); err == nil {
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

	// Fall back to parsing config file when agent is offline
	return z.statusFromConfig()
}

// statusFromConfig extracts provider, model, and channels from the TOML config file.
func (z *ZeroClawAdapter) statusFromConfig() (*AgentStatus, error) {
	if z.configPath == "" {
		return nil, fmt.Errorf("no config path available")
	}

	data, err := os.ReadFile(z.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	as := &AgentStatus{}
	content := string(data)

	// Parse default_model and default_provider from the global (pre-section) scope.
	// Only match exact key names to avoid false hits on nested keys like
	// [linkedin.image.flux] model = "fal-ai/flux/schnell".
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break // stop at first section — top-level keys are above all sections
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")
		switch key {
		case "default_model":
			as.Model = val
			if idx := strings.Index(val, "/"); idx > 0 {
				as.Provider = val[:idx]
			}
		case "default_provider":
			as.Provider = val
		}
	}

	// Parse channels (look for [channels.X] sections)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[channels.") {
			name := strings.TrimPrefix(trimmed, "[channels.")
			name = strings.TrimSuffix(name, "]")
			if name != "" {
				as.Channels = append(as.Channels, name)
			}
		}
	}

	return as, nil
}

func (z *ZeroClawAdapter) Config(ctx context.Context) (*AgentConfig, error) {
	// WHY read from disk instead of GET /api/config: The API returns all
	// fields including defaults, expanding a 24-line user config to 700+
	// lines. Reading from disk shows only user overrides, which is what
	// the editor should display and save.
	if z.configPath != "" {
		data, err := os.ReadFile(z.configPath)
		if err == nil {
			return &AgentConfig{Raw: string(data), Format: "toml"}, nil
		}
	}

	// Fall back to API if config file is not accessible
	body, err := z.getRaw(ctx, "/api/config")
	if err == nil {
		return &AgentConfig{Raw: body, Format: "toml"}, nil
	}

	return nil, err
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
// connects to the ZeroClaw SSE event stream for live entries. If the agent
// is not running, historical logs are still returned before closing.
func (z *ZeroClawAdapter) TailLogs(ctx context.Context) (<-chan LogEntry, error) {
	// Pre-read historical entries before creating the channel
	var history []LogEntry
	if z.configPath != "" {
		history = readHistoricalLogs(zeroclawLogPath(z.configPath), defaultHistoryLines, parseZeroClawLogLine)
	}

	// Try to connect to live stream
	req, err := http.NewRequestWithContext(ctx, "GET", z.baseURL+"/api/events", nil)
	if err != nil && len(history) == 0 {
		return nil, fmt.Errorf("creating SSE request: %w", err)
	}

	var resp *http.Response
	if err == nil {
		if z.token != "" {
			req.Header.Set("Authorization", "Bearer "+z.token)
		}
		req.Header.Set("Accept", "text/event-stream")

		client := &http.Client{}
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			resp = nil
		}
	}

	// If we can't connect but have history, serve history only
	if resp == nil && len(history) == 0 {
		return nil, fmt.Errorf("agent is not running and no historical logs found")
	}

	ch := make(chan LogEntry, 64)
	go func() {
		for _, entry := range history {
			select {
			case ch <- entry:
			case <-ctx.Done():
				if resp != nil {
					resp.Body.Close()
				}
				close(ch)
				return
			}
		}
		if resp != nil {
			z.readSSE(ctx, resp.Body, ch)
		} else {
			close(ch)
		}
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
		return nil, wrapConnError(err, "connecting to SSE")
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, httpStatusError(resp.StatusCode, "SSE returned status %d", resp.StatusCode)
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

// Sessions returns sessions from ZeroClaw's session backend (0.5.7+).
// Upstream uses UUID-based session_ids with a gw_ prefix internally.
// Falls back to a synthetic "memory" session for older versions.
func (z *ZeroClawAdapter) Sessions(ctx context.Context) ([]Session, error) {
	body, err := z.getRaw(ctx, "/api/sessions")
	if err != nil {
		// WHY SQLite fallback: When the agent is stopped, the gateway is
		// unreachable but the session database on disk is still readable.
		// This lets the user browse chat history for stopped agents.
		if sessions, dbErr := z.sessionsFromDB(); dbErr == nil && len(sessions) > 0 {
			return sessions, nil
		} else if dbErr != nil {
			slog.Debug("sessionsFromDB fallback failed", "agent", z.name, "error", dbErr)
		}
		return z.sessionsLegacy(ctx)
	}

	// Try upstream 0.5.7+ format: {"sessions": [{"session_id": "...", "name": "...", ...}]}
	var upstreamResp struct {
		Sessions []struct {
			SessionID    string  `json:"session_id"`
			Name         *string `json:"name"`
			CreatedAt    string  `json:"created_at"`
			LastActivity string  `json:"last_activity"`
			MessageCount int     `json:"message_count"`
		} `json:"sessions"`
		Message string `json:"message,omitempty"` // "Session persistence is disabled"
	}
	if json.Unmarshal([]byte(body), &upstreamResp) == nil && upstreamResp.Message == "" {
		sessions := make([]Session, 0, len(upstreamResp.Sessions))
		for _, s := range upstreamResp.Sessions {
			title := s.SessionID
			if s.Name != nil && *s.Name != "" {
				title = *s.Name
			}
			sess := Session{
				Key:   s.SessionID,
				Title: title,
			}
			if t, err := time.Parse(time.RFC3339Nano, s.LastActivity); err == nil {
				sess.LastMsg = &t
			} else if t, err := time.Parse(time.RFC3339, s.LastActivity); err == nil {
				sess.LastMsg = &t
			}
			sessions = append(sessions, sess)
		}
		// WHY merge with SQLite: The gateway API only returns sessions loaded
		// in the current runtime. Older sessions (e.g., briefings from before
		// a restart) are persisted in SQLite but not listed by the API.
		return z.mergeDBSessions(sessions), nil
	}

	// Try older named-session format: {"sessions": [{"id": "...", "name": "...", ...}]}
	var legacyResp struct {
		Sessions []struct {
			ID           string  `json:"id"`
			Name         string  `json:"name"`
			CreatedAt    string  `json:"created_at"`
			MessageCount int     `json:"message_count"`
			LastMessage  *string `json:"last_message"`
		} `json:"sessions"`
	}
	if json.Unmarshal([]byte(body), &legacyResp) == nil && len(legacyResp.Sessions) > 0 {
		sessions := make([]Session, 0, len(legacyResp.Sessions))
		for _, s := range legacyResp.Sessions {
			sess := Session{
				Key:   s.ID,
				Title: s.Name,
			}
			if s.LastMessage != nil {
				if t, err := time.Parse(time.RFC3339Nano, *s.LastMessage); err == nil {
					sess.LastMsg = &t
				}
			}
			sessions = append(sessions, sess)
		}
		return z.mergeDBSessions(sessions), nil
	}

	return z.sessionsLegacy(ctx)
}

// mergeDBSessions supplements gateway sessions with any additional sessions
// found in the SQLite database. Gateway sessions take precedence (fresher data).
func (z *ZeroClawAdapter) mergeDBSessions(gateway []Session) []Session {
	dbSessions, err := z.sessionsFromDB()
	if err != nil || len(dbSessions) == 0 {
		return gateway
	}
	seen := make(map[string]bool, len(gateway))
	for _, s := range gateway {
		seen[s.Key] = true
	}
	for _, s := range dbSessions {
		if !seen[s.Key] {
			gateway = append(gateway, s)
		}
	}
	return gateway
}

// sessionsFromDB reads session metadata directly from ZeroClaw's SQLite
// database on disk. Used when the gateway is unreachable (agent stopped).
func (z *ZeroClawAdapter) sessionsFromDB() ([]Session, error) {
	dbPath := z.sessionDBPath()
	if dbPath == "" {
		return nil, fmt.Errorf("no session DB path")
	}
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT session_key, name, last_activity, message_count FROM session_metadata ORDER BY last_activity DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var key, lastActivity string
		var name sql.NullString
		var count int
		if err := rows.Scan(&key, &name, &lastActivity, &count); err != nil {
			continue
		}
		// WHY strip gw_ prefix: ChatHistory() prepends "gw_" when querying
		// the sessions table. The gateway API returns keys without the prefix,
		// so we must match that convention to avoid double-prefixing.
		displayKey := strings.TrimPrefix(key, "gw_")
		sess := Session{
			Key:   displayKey,
			Title: displayKey,
		}
		if name.Valid && name.String != "" {
			sess.Title = name.String
		}
		if t, err := time.Parse(time.RFC3339Nano, lastActivity); err == nil {
			sess.LastMsg = &t
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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

// ChatHistory returns conversation messages for a session. It reads from
// ZeroClaw's SQLite session database as the source of truth, then enriches
// messages with tool call parts from Eyrie's JSONL store where available.
// Falls back to the legacy memory API if neither is available.
func (z *ZeroClawAdapter) ChatHistory(ctx context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	// Read base messages from SQLite
	dbPath := z.sessionDBPath()
	if dbPath == "" {
		return z.chatHistoryLegacy(ctx, sessionKey, limit)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return z.chatHistoryLegacy(ctx, sessionKey, limit)
	}
	defer db.Close()

	gwKey := "gw_" + sessionKey

	query := `SELECT role, content, created_at FROM sessions WHERE session_key = ? ORDER BY id ASC`
	args := []any{gwKey}
	if limit > 0 {
		query = `SELECT role, content, created_at FROM (
			SELECT role, content, created_at, id FROM sessions WHERE session_key = ? ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`
		args = append(args, limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return z.chatHistoryLegacy(ctx, sessionKey, limit)
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var role, content, createdAt string
		if err := rows.Scan(&role, &content, &createdAt); err != nil {
			continue
		}
		ts, _ := time.Parse(time.RFC3339Nano, createdAt)
		messages = append(messages, ChatMessage{
			Timestamp: ts,
			Role:      role,
			Content:   content,
		})
	}

	// Overlay enriched data (tool call parts) from JSONL store
	enriched := z.loadEnrichedMessages(sessionKey)
	if len(enriched) > 0 {
		// Build a lookup: match enriched messages to SQLite messages by
		// role + content prefix (content is the same, enriched has Parts)
		for i := range messages {
			for _, e := range enriched {
				if e.Role == messages[i].Role && e.Content == messages[i].Content && len(e.Parts) > 0 {
					messages[i].Parts = e.Parts
					break
				}
			}
		}
	}

	return messages, nil
}

// sessionDBPath returns the path to ZeroClaw's session SQLite database,
// derived from the config path: {configDir}/workspace/sessions/sessions.db
func (z *ZeroClawAdapter) sessionDBPath() string {
	if z.configPath == "" {
		return ""
	}
	dir := filepath.Dir(z.configPath)
	dbPath := filepath.Join(dir, "workspace", "sessions", "sessions.db")
	return dbPath
}

// chatHistoryLegacy falls back to the memory API for pre-0.5.7 ZeroClaw.
func (z *ZeroClawAdapter) chatHistoryLegacy(ctx context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	all, err := z.fetchMemoryEntriesFrom(ctx, "/api/memory?category=conversation")
	if err != nil {
		return nil, err
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.Before(all[j].Timestamp)
	})

	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}

	messages := make([]ChatMessage, 0, len(all))
	for _, e := range all {
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

func (z *ZeroClawAdapter) SendMessage(ctx context.Context, message, sessionKey string) (*ChatMessage, error) {
	ch, err := z.StreamMessage(ctx, message, sessionKey)
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

// StreamMessage connects to ZeroClaw's WebSocket chat endpoint with session
// support. The upstream WS handler (0.5.7+) creates a persistent Agent per
// connection, hydrates it from the session backend, and runs multi-turn chat
// with full tool execution.
func (z *ZeroClawAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	wsURL := strings.Replace(z.baseURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws/chat"

	// Build query params with proper URL encoding
	qv := make(url.Values)
	if z.token != "" {
		qv.Set("token", z.token)
	}
	if sessionKey != "" {
		qv.Set("session_id", sessionKey)
		qv.Set("name", sessionKey) // Use session key as the human-readable name
	}
	if encoded := qv.Encode(); encoded != "" {
		wsURL += "?" + encoded
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	conn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	dialCancel()
	if err != nil {
		return nil, wrapConnError(err, "connecting to ZeroClaw WS")
	}
	conn.SetReadLimit(4 * 1024 * 1024) // 4 MB

	ch := make(chan ChatEvent, 64)
	go func() {
		defer close(ch)
		defer conn.CloseNow()

		// Derive a timeout from the parent context for the WS read loop.
		readCtx, readCancel := context.WithTimeout(ctx, 5*time.Minute)
		defer readCancel()

		// Phase 1: Wait for session_start frame with timeout
		startCtx, startCancel := context.WithTimeout(readCtx, 10*time.Second)
		defer startCancel()
		sessionStarted := false
		for !sessionStarted {
			_, data, err := conn.Read(startCtx)
			if err != nil {
				select {
				case ch <- ChatEvent{Type: "error", Error: fmt.Sprintf("waiting for session_start: %v", err)}:
				case <-readCtx.Done():
				}
				return
			}
			var frame map[string]any
			if json.Unmarshal(data, &frame) != nil {
				continue
			}
			if frame["type"] == "session_start" {
				sessionStarted = true
			}
		}
		startCancel() // release the timeout context early

		// Phase 2: Send the user message
		msg := map[string]string{
			"type":    "message",
			"content": message,
		}
		outgoing, _ := json.Marshal(msg)
		if err := conn.Write(readCtx, websocket.MessageText, outgoing); err != nil {
			select {
			case ch <- ChatEvent{Type: "error", Error: fmt.Sprintf("sending message: %v", err)}:
			case <-readCtx.Done():
			}
			return
		}

		// Save the user message to Eyrie's enriched store
		userMsg := ChatMessage{
			Timestamp: time.Now(),
			Role:      "user",
			Content:   message,
		}
		z.saveEnrichedMessage(sessionKey, &userMsg)

		// Phase 3: Read response frames, collecting tool calls
		var toolCalls []ChatPart
		for {
			_, data, err := conn.Read(readCtx)
			if err != nil {
				select {
				case ch <- ChatEvent{Type: "error", Error: fmt.Sprintf("connection read: %v", err)}:
				case <-readCtx.Done():
				}
				return
			}

			var frame map[string]any
			if json.Unmarshal(data, &frame) != nil {
				continue
			}

			frameType, _ := frame["type"].(string)
			var ev ChatEvent

			switch frameType {
			case "chunk":
				content, _ := frame["content"].(string)
				ev = ChatEvent{Type: "delta", Content: content}
			case "tool_call":
				name, _ := frame["name"].(string)
				var args map[string]any
				if a, ok := frame["args"].(map[string]any); ok {
					args = a
				}
				toolID, _ := frame["id"].(string)
				ev = ChatEvent{Type: "tool_start", Tool: name, ToolID: toolID, Args: args}
				// Collect the tool call part
				toolCalls = append(toolCalls, ChatPart{
					Type: "tool_call",
					ID:   toolID,
					Name: name,
					Args: args,
				})
			case "tool_result":
				name, _ := frame["name"].(string)
				output, _ := frame["output"].(string)
				toolID, _ := frame["id"].(string)
				ev = ChatEvent{Type: "tool_result", Tool: name, ToolID: toolID, Output: output}
				// Update the matching tool call part with output
				for i := range toolCalls {
					if toolCalls[i].ID == toolID || (toolID == "" && toolCalls[i].Name == name && toolCalls[i].Output == "") {
						toolCalls[i].Output = output
						break
					}
				}
			case "done":
				content, _ := frame["full_response"].(string)
				ev = ChatEvent{Type: "done", Content: content}
				// Forward usage stats if present in the frame
				if v, ok := frame["input_tokens"].(float64); ok {
					ev.InputTokens = int(v)
				}
				if v, ok := frame["output_tokens"].(float64); ok {
					ev.OutputTokens = int(v)
				}
				if v, ok := frame["cost_usd"].(float64); ok {
					ev.CostUSD = v
				}
				// Save enriched assistant message with tool call parts
				assistantMsg := ChatMessage{
					Timestamp: time.Now(),
					Role:      "assistant",
					Content:   content,
				}
				if len(toolCalls) > 0 {
					// Build parts: tool calls first, then final text
					parts := make([]ChatPart, 0, len(toolCalls)+1)
					parts = append(parts, toolCalls...)
					if content != "" {
						parts = append(parts, ChatPart{Type: "text", Text: content})
					}
					assistantMsg.Parts = parts
				}
				z.saveEnrichedMessage(sessionKey, &assistantMsg)
				select {
				case ch <- ev:
				case <-readCtx.Done():
				}
				return
			case "error":
				msg, _ := frame["message"].(string)
				ev = ChatEvent{Type: "error", Error: msg}
				select {
				case ch <- ev:
				case <-readCtx.Done():
				}
				return
			default:
				continue
			}

			select {
			case ch <- ev:
			case <-readCtx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// safeSessionKey validates that a session key is safe for use in file paths.
// Rejects empty strings, path separators, and traversal sequences.
func safeSessionKey(key string) bool {
	if key == "" || key == "." || key == ".." {
		return false
	}
	if strings.ContainsAny(key, "/\\") || strings.Contains(key, "..") {
		return false
	}
	return key == filepath.Base(key)
}

// saveEnrichedMessage appends a ChatMessage (with tool call parts) to Eyrie's
// session JSONL store at ~/.eyrie/sessions/{agentName}/{sessionKey}.jsonl.
func (z *ZeroClawAdapter) saveEnrichedMessage(sessionKey string, msg *ChatMessage) {
	if !safeSessionKey(sessionKey) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".eyrie", "sessions", z.name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, sessionKey+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if _, err := f.Write(data); err != nil {
		slog.Debug("failed to write enriched message data", "error", err)
		return
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		slog.Debug("failed to write enriched message newline", "error", err)
	}
}

// loadEnrichedMessages reads all enriched messages for a session from Eyrie's
// JSONL store. Returns nil if the file doesn't exist.
func (z *ZeroClawAdapter) loadEnrichedMessages(sessionKey string) []ChatMessage {
	if !safeSessionKey(sessionKey) {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(home, ".eyrie", "sessions", z.name, sessionKey+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var messages []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024) // 4MB max line
	for scanner.Scan() {
		var msg ChatMessage
		if json.Unmarshal(scanner.Bytes(), &msg) == nil {
			messages = append(messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("error reading enriched messages", "session", sessionKey, "error", err)
	}
	return messages
}



// Interrupt asks ZeroClaw to cancel an in-flight response via the abort endpoint.
func (z *ZeroClawAdapter) Interrupt(ctx context.Context, sessionKey string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", z.baseURL+"/api/sessions/"+sessionKey+"/abort", nil)
	if err != nil {
		return err
	}
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return wrapConnError(err, "aborting session")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return httpStatusError(resp.StatusCode, "abort session returned %d", resp.StatusCode)
	}
	return nil
}

// CreateSession generates a new session ID for ZeroClaw. Upstream creates
// sessions implicitly on first WS message, so we just return the ID.
// The session will be created in the backend when StreamMessage connects.
func (z *ZeroClawAdapter) CreateSession(_ context.Context, name string) (*Session, error) {
	// Use the name as the session ID if it looks safe, otherwise generate a UUID.
	// This makes session IDs human-readable in the ZeroClaw backend.
	sessionID := name
	if sessionID == "" || !safeSessionKey(sessionID) {
		sessionID = uuid.New().String()
	}
	return &Session{Key: sessionID, Title: name}, nil
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
		return wrapConnError(err, "deleting session")
	}
	defer resp.Body.Close()
	// 404 is fine — session is already gone (e.g. project reset clears
	// multiple agents and some may not have the session).
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		return httpStatusError(resp.StatusCode, "delete session returned %d", resp.StatusCode)
	}
	return nil
}

func (z *ZeroClawAdapter) DeleteSession(ctx context.Context, sessionKey string) error {
	return z.ResetSession(ctx, sessionKey)
}

func (z *ZeroClawAdapter) DestroySession(ctx context.Context, sessionKey string) error {
	return z.ResetSession(ctx, sessionKey)
}

// ActivateSession is a no-op for upstream ZeroClaw (0.5.7+) since sessions are
// selected per-connection via the ?session_id query param on the WS endpoint.
// Kept for interface compatibility with the hierarchy briefing handler.
func (z *ZeroClawAdapter) ActivateSession(_ context.Context, _ string) error {
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
		return "", wrapConnError(err, "request to %s", path)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", httpStatusError(resp.StatusCode, "%s returned %d: %s", path, resp.StatusCode, string(data))
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
	return AgentCapabilities{CommanderCapable: true} // Requires ZeroClaw 0.5.7+ with session_persistence enabled
}
