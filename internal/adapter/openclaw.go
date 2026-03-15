package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

// OpenClawAdapter communicates with an OpenClaw instance via its WebSocket RPC gateway.
// OpenClaw exposes a single WebSocket endpoint at ws://host:port with RPC methods.
type OpenClawAdapter struct {
	id         string
	name       string
	host       string
	port       int
	token      string
	configPath string
}

func NewOpenClawAdapter(id, name, host string, port int, token, configPath string) *OpenClawAdapter {
	return &OpenClawAdapter{
		id:         id,
		name:       name,
		host:       host,
		port:       port,
		token:      token,
		configPath: configPath,
	}
}

func (o *OpenClawAdapter) ID() string        { return o.id }
func (o *OpenClawAdapter) Name() string      { return o.name }
func (o *OpenClawAdapter) Framework() string  { return "openclaw" }
func (o *OpenClawAdapter) BaseURL() string {
	return fmt.Sprintf("ws://%s:%d", o.host, o.port)
}

func (o *OpenClawAdapter) Health(ctx context.Context) (*HealthStatus, error) {
	result, err := o.rpcCall(ctx, "health", nil)
	if err != nil {
		return &HealthStatus{Alive: false}, err
	}

	hs := &HealthStatus{
		Alive:      true,
		Components: make(map[string]ComponentHealth),
	}

	if raw, ok := result["uptime_seconds"].(float64); ok {
		hs.Uptime = time.Duration(raw) * time.Second
	}
	if raw, ok := result["pid"].(float64); ok {
		hs.PID = int(raw)
	}
	if comps, ok := result["components"].(map[string]any); ok {
		for name, v := range comps {
			if cm, ok := v.(map[string]any); ok {
				ch := ComponentHealth{}
				if s, ok := cm["status"].(string); ok {
					ch.Status = s
				}
				hs.Components[name] = ch
			}
		}
	}

	return hs, nil
}

func (o *OpenClawAdapter) Status(ctx context.Context) (*AgentStatus, error) {
	result, err := o.rpcCall(ctx, "status", nil)
	if err != nil {
		return nil, err
	}

	as := &AgentStatus{
		GatewayPort: o.port,
	}
	if v, ok := result["provider"].(string); ok {
		as.Provider = v
	}
	if v, ok := result["model"].(string); ok {
		as.Model = v
	}
	if v, ok := result["channels"].([]any); ok {
		for _, ch := range v {
			if s, ok := ch.(string); ok {
				as.Channels = append(as.Channels, s)
			}
		}
	}

	return as, nil
}

func (o *OpenClawAdapter) Config(ctx context.Context) (*AgentConfig, error) {
	result, err := o.rpcCall(ctx, "config.get", nil)
	if err != nil {
		return nil, err
	}

	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	return &AgentConfig{Raw: string(raw), Format: "json"}, nil
}

func (o *OpenClawAdapter) Start(ctx context.Context) error {
	return runCLI(ctx, "openclaw", "gateway", "start")
}

func (o *OpenClawAdapter) Stop(ctx context.Context) error {
	return runCLI(ctx, "openclaw", "gateway", "stop")
}

func (o *OpenClawAdapter) Restart(ctx context.Context) error {
	return runCLI(ctx, "openclaw", "gateway", "restart")
}

// TailLogs subscribes to the OpenClaw logs.tail RPC stream.
func (o *OpenClawAdapter) TailLogs(ctx context.Context) (<-chan LogEntry, error) {
	var history []LogEntry
	if o.configPath != "" {
		history = readHistoricalLogs(openclawLogPath(o.configPath), defaultHistoryLines, parseOpenClawLogLine)
	}

	conn, err := o.dial(ctx)
	if err != nil {
		return nil, err
	}

	req := rpcRequest{
		Method: "logs.tail",
		Params: map[string]any{"sinceMs": 60000},
	}
	payload, _ := json.Marshal(req)
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("sending logs.tail: %w", err)
	}

	ch := make(chan LogEntry, 64)
	go func() {
		for _, entry := range history {
			select {
			case ch <- entry:
			case <-ctx.Done():
				conn.Close(websocket.StatusNormalClosure, "")
				close(ch)
				return
			}
		}
		o.readLogStream(ctx, conn, ch)
	}()
	return ch, nil
}

func (o *OpenClawAdapter) readLogStream(ctx context.Context, conn *websocket.Conn, ch chan<- LogEntry) {
	defer close(ch)
	defer conn.Close(websocket.StatusNormalClosure, "")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		entry := LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Fields:    msg,
		}
		if m, ok := msg["message"].(string); ok {
			entry.Message = m
		}
		if l, ok := msg["level"].(string); ok {
			entry.Level = l
		}

		select {
		case ch <- entry:
		case <-ctx.Done():
			return
		}
	}
}

// TailActivity emits recent conversation history as activity events, then
// subscribes to the OpenClaw WebSocket event stream for live events.
func (o *OpenClawAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
	history := o.recentConversationActivity(ctx)

	conn, err := o.dial(ctx)
	if err != nil {
		return nil, err
	}

	ch := make(chan ActivityEvent, 64)
	go func() {
		for _, ev := range history {
			select {
			case ch <- ev:
			case <-ctx.Done():
				conn.Close(websocket.StatusNormalClosure, "")
				close(ch)
				return
			}
		}
		o.readActivityStream(ctx, conn, ch)
	}()
	return ch, nil
}

// recentConversationActivity loads the most recent sessions' chat messages and
// converts them to ActivityEvents with separators between sessions.
func (o *OpenClawAdapter) recentConversationActivity(ctx context.Context) []ActivityEvent {
	sessions, err := o.Sessions(ctx)
	if err != nil || len(sessions) == 0 {
		return nil
	}

	maxSessions := 3
	if len(sessions) < maxSessions {
		maxSessions = len(sessions)
	}

	var events []ActivityEvent
	for i := 0; i < maxSessions; i++ {
		messages, err := o.ChatHistory(ctx, sessions[i].Key, 25)
		if err != nil || len(messages) == 0 {
			continue
		}

		if len(events) > 0 {
			label := sessions[i].Title
			if label == "" {
				label = sessions[i].Key
			}
			ts := messages[0].Timestamp
			if sessions[i].LastMsg != nil {
				ts = *sessions[i].LastMsg
			}
			events = append(events, ActivityEvent{
				Timestamp: ts,
				Type:      "separator",
				Summary:   label + " — " + ts.Format("Jan 2, 3:04 PM"),
			})
		}

		for j, m := range messages {
			if j > 0 {
				gap := m.Timestamp.Sub(messages[j-1].Timestamp)
				if gap >= 30*time.Minute {
					events = append(events, ActivityEvent{
						Timestamp: m.Timestamp,
						Type:      "separator",
						Summary:   m.Timestamp.Format("Jan 2, 3:04 PM"),
					})
				}
			}

			summary := m.Content
			fullContent := ""
			if len(summary) > 100 {
				fullContent = fmt.Sprintf("[%s] %s", m.Role, summary)
				summary = summary[:100] + "..."
			}
			events = append(events, ActivityEvent{
				Timestamp:   m.Timestamp,
				Type:        "chat",
				Summary:     fmt.Sprintf("[%s] %s", m.Role, summary),
				FullContent: fullContent,
			})
		}
	}
	return events
}

func (o *OpenClawAdapter) readActivityStream(ctx context.Context, conn *websocket.Conn, ch chan<- ActivityEvent) {
	defer close(ch)
	defer conn.Close(websocket.StatusNormalClosure, "")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var msg struct {
			Type    string         `json:"type"`
			Event   string         `json:"event"`
			Payload map[string]any `json:"payload,omitempty"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		// Only process server-pushed events
		if msg.Type != "event" {
			continue
		}

		ev := o.parseOpenClawEvent(msg.Event, msg.Payload)
		if ev == nil {
			continue
		}

		select {
		case ch <- *ev:
		case <-ctx.Done():
			return
		}
	}
}

func (o *OpenClawAdapter) parseOpenClawEvent(event string, payload map[string]any) *ActivityEvent {
	if payload == nil {
		payload = map[string]any{}
	}

	str := func(key string) string {
		if v, ok := payload[key].(string); ok {
			return v
		}
		return ""
	}

	ev := &ActivityEvent{
		Timestamp: time.Now(),
		Fields:    payload,
	}

	switch event {
	case "chat":
		state := str("state")
		switch state {
		case "final":
			ev.Type = "chat"
			msg := str("message")
			if len(msg) > 80 {
				msg = msg[:80] + "..."
			}
			ev.Summary = fmt.Sprintf("Response: %s", msg)
		case "error":
			ev.Type = "error"
			ev.Summary = fmt.Sprintf("Chat error: %s", str("errorMessage"))
		case "aborted":
			ev.Type = "chat"
			ev.Summary = "Response aborted"
		default:
			return nil // skip deltas
		}
	case "agent":
		ev.Type = "agent"
		ev.Summary = fmt.Sprintf("Agent event (run %s, seq %s)", str("runId"), str("seq"))
	case "health":
		ev.Type = "health"
		ev.Summary = "Health update"
	case "shutdown":
		ev.Type = "shutdown"
		ev.Summary = "Gateway shutting down"
	default:
		return nil // skip ticks, presence, etc.
	}

	return ev
}

// Sessions reads session metadata directly from OpenClaw's on-disk session
// store rather than using the WebSocket RPC (which requires a full device-auth
// handshake that Eyrie does not implement).
func (o *OpenClawAdapter) Sessions(_ context.Context) ([]Session, error) {
	agentsDir := o.agentsDir()
	if agentsDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("reading agents dir: %w", err)
	}

	var sessions []Session
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessFile := filepath.Join(agentsDir, entry.Name(), "sessions", "sessions.json")
		data, err := os.ReadFile(sessFile)
		if err != nil {
			continue
		}

		var store map[string]json.RawMessage
		if json.Unmarshal(data, &store) != nil {
			continue
		}

		for key, raw := range store {
			var meta struct {
				SessionID   string `json:"sessionId"`
				UpdatedAt   int64  `json:"updatedAt"`
				LastChannel string `json:"lastChannel"`
				ChatType    string `json:"chatType"`
			}
			if json.Unmarshal(raw, &meta) != nil {
				continue
			}

			s := Session{
				Key:     key,
				Title:   key,
				Channel: meta.LastChannel,
			}
			if meta.UpdatedAt > 0 {
				t := time.UnixMilli(meta.UpdatedAt)
				s.LastMsg = &t
			}
			sessions = append(sessions, s)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastMsg == nil {
			return false
		}
		if sessions[j].LastMsg == nil {
			return true
		}
		return sessions[i].LastMsg.After(*sessions[j].LastMsg)
	})

	return sessions, nil
}

// ChatHistory reads the JSONL transcript file for a given session key directly
// from disk. It extracts user and assistant messages, handling OpenClaw's
// content format (array of {type, text} objects).
func (o *OpenClawAdapter) ChatHistory(_ context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	agentsDir := o.agentsDir()
	if agentsDir == "" {
		return nil, nil
	}

	sessionID, sessionFile := o.findSessionFile(agentsDir, sessionKey)
	if sessionFile == "" {
		return nil, fmt.Errorf("session %q not found (id=%s)", sessionKey, sessionID)
	}

	f, err := os.Open(sessionFile)
	if err != nil {
		return nil, fmt.Errorf("opening transcript: %w", err)
	}
	defer f.Close()

	var messages []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 2*1024*1024)

	for scanner.Scan() {
		var entry struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Type != "message" {
			continue
		}
		role := entry.Message.Role
		if role != "user" && role != "assistant" {
			continue
		}

		content := cleanOpenClawContent(role, extractTextContent(entry.Message.Content))
		if content == "" {
			continue
		}

		var ts time.Time
		if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			ts = t
		}

		messages = append(messages, ChatMessage{
			Timestamp: ts,
			Role:      role,
			Content:   content,
		})
	}

	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// agentsDir derives the OpenClaw agents directory from the config path.
// e.g. ~/.openclaw/openclaw.json -> ~/.openclaw/agents/
func (o *OpenClawAdapter) agentsDir() string {
	if o.configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(o.configPath), "agents")
}

// findSessionFile locates the JSONL transcript for a session key by reading
// sessions.json from each agent directory.
func (o *OpenClawAdapter) findSessionFile(agentsDir, sessionKey string) (string, string) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return "", ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessFile := filepath.Join(agentsDir, entry.Name(), "sessions", "sessions.json")
		data, err := os.ReadFile(sessFile)
		if err != nil {
			continue
		}

		var store map[string]struct {
			SessionID   string `json:"sessionId"`
			SessionFile string `json:"sessionFile"`
		}
		if json.Unmarshal(data, &store) != nil {
			continue
		}

		if meta, ok := store[sessionKey]; ok {
			if meta.SessionFile != "" {
				return meta.SessionID, meta.SessionFile
			}
			jsonlPath := filepath.Join(agentsDir, entry.Name(), "sessions", meta.SessionID+".jsonl")
			return meta.SessionID, jsonlPath
		}
	}
	return "", ""
}

// extractTextContent handles OpenClaw's message content, which can be either a
// plain string or an array of {type:"text", text:"..."} content blocks.
func extractTextContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return joinNonEmpty(parts)
	}
	return ""
}

// openclawSenderMetaRe matches the "Sender (untrusted metadata): ```json...```"
// preamble that OpenClaw injects into user messages for channel context.
var openclawSenderMetaRe = regexp.MustCompile(`(?s)^Sender \(untrusted metadata\):\s*` + "```" + `json\s*\{[^}]*\}\s*` + "```" + `\s*`)

// openclawTimestampRe matches the "[Fri 2026-03-13 22:02 GMT+8]" prefix.
var openclawTimestampRe = regexp.MustCompile(`^\[[A-Z][a-z]{2} \d{4}-\d{2}-\d{2} \d{2}:\d{2}[^\]]*\]\s*`)

// cleanOpenClawContent strips OpenClaw-injected metadata from message content.
func cleanOpenClawContent(role, content string) string {
	if role == "user" {
		content = openclawSenderMetaRe.ReplaceAllString(content, "")
		content = openclawTimestampRe.ReplaceAllString(content, "")
	}
	if role == "assistant" {
		content = strings.TrimPrefix(content, "[[reply_to_current]]")
		content = strings.TrimPrefix(content, "[[reply_in_thread]]")
	}
	return strings.TrimSpace(content)
}

func joinNonEmpty(parts []string) string {
	var result []string
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return ""
	}
	return result[0]
}

func (o *OpenClawAdapter) Personality(ctx context.Context) (*Personality, error) {
	cfg, err := o.Config(ctx)
	if err != nil {
		return nil, err
	}
	return &Personality{
		Name: o.name,
		IdentityFiles: map[string]string{
			"openclaw.json": cfg.Raw,
		},
	}, nil
}

type rpcRequest struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (o *OpenClawAdapter) dial(ctx context.Context) (*websocket.Conn, error) {
	url := fmt.Sprintf("ws://%s:%d", o.host, o.port)
	if o.token != "" {
		url += "?token=" + o.token
	}

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to OpenClaw at %s:%d: %w", o.host, o.port, err)
	}
	return conn, nil
}

// rpcCall sends a single RPC request and waits for the response.
func (o *OpenClawAdapter) rpcCall(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := o.dial(callCtx)
	if err != nil {
		return nil, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	req := rpcRequest{Method: method, Params: params}
	payload, _ := json.Marshal(req)

	if err := conn.Write(callCtx, websocket.MessageText, payload); err != nil {
		return nil, fmt.Errorf("sending RPC %s: %w", method, err)
	}

	_, data, err := conn.Read(callCtx)
	if err != nil {
		return nil, fmt.Errorf("reading RPC response for %s: %w", method, err)
	}

	var resp rpcResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		// If it doesn't parse as RPC, try as raw JSON map
		var raw map[string]any
		if json.Unmarshal(data, &raw) == nil {
			return raw, nil
		}
		return nil, fmt.Errorf("parsing RPC response for %s: %w", method, err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC %s error %d: %s", method, resp.Error.Code, resp.Error.Message)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parsing RPC result for %s: %w", method, err)
	}
	return result, nil
}

// QuickHealthCheck does a fast HTTP probe (OpenClaw serves HTTP on the same port for the Control UI).
func (o *OpenClawAdapter) QuickHealthCheck(ctx context.Context) bool {
	url := fmt.Sprintf("http://%s:%d", o.host, o.port)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return false
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return true
}

var (
	_ Agent = (*OpenClawAdapter)(nil)
	_ Agent = (*ZeroClawAdapter)(nil)
)
