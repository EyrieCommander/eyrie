package adapter

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	hs := &HealthStatus{
		Alive:      true,
		Components: make(map[string]ComponentHealth),
	}

	result, err := o.rpcCall(ctx, "health", nil)
	if err == nil {
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
	}

	if hs.PID == 0 {
		hs.PID = pidFromPort(o.port)
	}
	var psUptime time.Duration
	hs.RAM, hs.CPU, psUptime = processStats(hs.PID)
	if hs.Uptime == 0 {
		hs.Uptime = psUptime
	}

	return hs, nil
}

func (o *OpenClawAdapter) Status(ctx context.Context) (*AgentStatus, error) {
	as := &AgentStatus{
		GatewayPort: o.port,
	}

	result, err := o.rpcCall(ctx, "status", nil)
	if err == nil {
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
	}

	// Fill gaps from config file when RPC didn't provide values
	if (as.Provider == "" || as.Model == "" || len(as.Channels) == 0) && o.configPath != "" {
		if data, err := os.ReadFile(o.configPath); err == nil {
			var cfg struct {
				Agents struct {
					Defaults struct {
						Model struct {
							Primary string `json:"primary"`
						} `json:"model"`
					} `json:"defaults"`
				} `json:"agents"`
				Channels map[string]json.RawMessage `json:"channels"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				model := cfg.Agents.Defaults.Model.Primary
				if model != "" && as.Model == "" {
					as.Model = model
				}
				if as.Provider == "" && model != "" {
					if idx := strings.Index(model, "/"); idx > 0 {
						as.Provider = model[:idx]
					}
				}
				if len(as.Channels) == 0 {
					for ch := range cfg.Channels {
						as.Channels = append(as.Channels, ch)
					}
				}
			}
		}
	}

	return as, nil
}

func (o *OpenClawAdapter) Config(ctx context.Context) (*AgentConfig, error) {
	result, err := o.rpcCall(ctx, "config.get", nil)
	if err == nil {
		raw, err := json.MarshalIndent(result, "", "  ")
		if err == nil {
			return &AgentConfig{Raw: string(raw), Format: "json"}, nil
		}
	}

	if o.configPath != "" {
		data, err := os.ReadFile(o.configPath)
		if err == nil {
			return &AgentConfig{Raw: string(data), Format: "json"}, nil
		}
	}

	return nil, fmt.Errorf("unable to retrieve OpenClaw config")
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
		sessDir := filepath.Join(agentsDir, entry.Name(), "sessions")
		sessFile := filepath.Join(sessDir, "sessions.json")
		data, err := os.ReadFile(sessFile)
		if err != nil {
			continue
		}

		var store map[string]json.RawMessage
		if json.Unmarshal(data, &store) != nil {
			continue
		}

		// Track which session IDs are active so we can match archives
		activeSessionIDs := make(map[string]string) // sessionID -> sessionKey

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

			activeSessionIDs[meta.SessionID] = key

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

		// Merge active IDs into the persisted mapping so we remember
		// which session key owned each UUID even after resets.
		knownIDs := loadSessionIDMap()
		for id, key := range activeSessionIDs {
			knownIDs[id] = key
		}
		saveSessionIDMap(knownIDs)

		// Scan for archived .reset. files and surface them as read-only sessions.
		// File pattern: <sessionID>.jsonl.reset.<ISO timestamp>
		sessFiles, _ := os.ReadDir(sessDir)
		for _, sf := range sessFiles {
			name := sf.Name()
			resetIdx := strings.Index(name, ".jsonl.reset.")
			if resetIdx < 0 {
				continue
			}
			archivedID := name[:resetIdx]
			resetTimestamp := name[resetIdx+len(".jsonl.reset."):]

			// Parse the reset timestamp for display and sorting
			resetTime, err := time.Parse("2006-01-02T15-04-05.000Z", resetTimestamp)
			if err != nil {
				resetTime, err = time.Parse("2006-01-02T15-04-05.999Z", resetTimestamp)
			}
			if err != nil {
				// Try without millis
				resetTime, _ = time.Parse("2006-01-02T15-04-05Z", resetTimestamp)
			}

			// Look up the parent session key: first from the persisted mapping
			// (survives resets), then fall back to active sessions.
			parentKey := knownIDs[archivedID]
			if parentKey == "" {
				parentKey = activeSessionIDs[archivedID]
			}
			if parentKey == "" {
				parentKey = "main"
			}

			archiveKey := "archive:" + name
			title := sessionShortName(parentKey)
			if title == "" {
				title = "archived"
			}
			if !resetTime.IsZero() {
				title += " (" + resetTime.Local().Format("1/2 3:04PM") + ")"
			}

			s := Session{
				Key:      archiveKey,
				Title:    title,
				ReadOnly: true,
			}
			if !resetTime.IsZero() {
				s.LastMsg = &resetTime
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
				Role       string `json:"role"`
				Content    any    `json:"content"`
				ToolCallID string `json:"toolCallId"`
				ToolName   string `json:"toolName"`
				IsError    bool   `json:"isError"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Type != "message" {
			continue
		}

		role := entry.Message.Role

		if role == "toolResult" {
			if len(messages) == 0 {
				continue
			}
			last := &messages[len(messages)-1]
			if last.Role != "assistant" {
				continue
			}
			resultText := extractTextContent(entry.Message.Content)
			for i := range last.Parts {
				if last.Parts[i].Type == "tool_call" && last.Parts[i].ID == entry.Message.ToolCallID {
					last.Parts[i].Output = resultText
					last.Parts[i].Error = entry.Message.IsError
					break
				}
			}
			continue
		}

		if role != "user" && role != "assistant" {
			continue
		}

		content := cleanOpenClawContent(role, extractTextContent(entry.Message.Content))

		var toolParts []ChatPart
		if role == "assistant" {
			toolParts = extractToolCallParts(entry.Message.Content)
		}

		if content == "" && len(toolParts) == 0 {
			continue
		}

		// Build ordered parts for this entry: text first (if any), then tool calls.
		var entryParts []ChatPart
		if role == "assistant" {
			if content != "" {
				entryParts = append(entryParts, ChatPart{Type: "text", Text: content})
			}
			entryParts = append(entryParts, toolParts...)
		}

		// Merge consecutive assistant entries into one message so that
		// multi-step tool-call sequences appear as a single grouped message
		// with parts in temporal order.
		if role == "assistant" && len(messages) > 0 {
			last := &messages[len(messages)-1]
			if last.Role == "assistant" {
				last.Parts = append(last.Parts, entryParts...)
				if content != "" {
					if last.Content != "" {
						last.Content += "\n" + content
					} else {
						last.Content = content
					}
				}
				continue
			}
		}

		var ts time.Time
		if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			ts = t
		}

		messages = append(messages, ChatMessage{
			Timestamp: ts,
			Role:      role,
			Content:   content,
			Parts:     entryParts,
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
// sessions.json from each agent directory. Also handles "archive:<filename>"
// keys that point to .reset. archive files.
func (o *OpenClawAdapter) findSessionFile(agentsDir, sessionKey string) (string, string) {
	// Handle archived session keys: "archive:<filename>"
	if strings.HasPrefix(sessionKey, "archive:") {
		archiveFilename := sessionKey[len("archive:"):]
		entries, _ := os.ReadDir(agentsDir)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(agentsDir, entry.Name(), "sessions", archiveFilename)
			if _, err := os.Stat(path); err == nil {
				return archiveFilename, path
			}
		}
		return "", ""
	}

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
func sessionShortName(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return key
}

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

func extractToolCallParts(content any) []ChatPart {
	arr, ok := content.([]any)
	if !ok {
		return nil
	}
	var parts []ChatPart
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] != "toolCall" {
			continue
		}
		p := ChatPart{
			Type: "tool_call",
			Name: strVal(m, "name"),
			ID:   strVal(m, "id"),
		}
		if args, ok := m["arguments"].(map[string]any); ok {
			p.Args = args
		}
		parts = append(parts, p)
	}
	return parts
}

// sessionIDMapPath returns ~/.eyrie/session_ids.json.
func sessionIDMapPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eyrie", "session_ids.json")
}

func loadSessionIDMap() map[string]string {
	p := sessionIDMapPath()
	if p == "" {
		return make(map[string]string)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return make(map[string]string)
	}
	return m
}

func saveSessionIDMap(m map[string]string) {
	p := sessionIDMapPath()
	if p == "" {
		return
	}
	dir := filepath.Dir(p)
	os.MkdirAll(dir, 0700)
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	os.WriteFile(p, data, 0600)
}

func strVal(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
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

func (o *OpenClawAdapter) SendMessage(ctx context.Context, message, sessionKey string) (*ChatMessage, error) {
	chatCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	if sessionKey == "" {
		sessionKey = "agent:main:main"
	}

	conn, err := o.dial(chatCtx)
	if err != nil {
		return nil, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	req := newRPCRequest("chat.send", map[string]any{
		"sessionKey":     sessionKey,
		"message":        message,
		"idempotencyKey": uuidV4(),
	})
	payload, _ := json.Marshal(req)
	if err := conn.Write(chatCtx, websocket.MessageText, payload); err != nil {
		return nil, fmt.Errorf("sending chat.send: %w", err)
	}

	for {
		_, data, err := conn.Read(chatCtx)
		if err != nil {
			return nil, fmt.Errorf("reading chat response: %w", err)
		}

		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}

		frameType, _ := frame["type"].(string)

		if frameType == "event" {
			eventName, _ := frame["event"].(string)
			if eventName != "chat" {
				continue
			}
			pl, _ := frame["payload"].(map[string]any)
			if pl == nil {
				continue
			}
			state, _ := pl["state"].(string)
			if state == "final" {
				content, _ := pl["message"].(string)
				return &ChatMessage{
					Timestamp: time.Now(),
					Role:      "assistant",
					Content:   content,
				}, nil
			}
			if state == "error" {
				errMsg, _ := pl["errorMessage"].(string)
				return nil, fmt.Errorf("agent error: %s", errMsg)
			}
			continue
		}

		if frameType == "res" {
			var resp rpcResponse
			if json.Unmarshal(data, &resp) != nil {
				continue
			}
			if resp.Error != nil {
				return nil, fmt.Errorf("RPC chat.send error %s: %s", resp.Error.Code, resp.Error.Message)
			}
			if resp.Payload != nil {
				var result map[string]any
				json.Unmarshal(resp.Payload, &result)
				if content, ok := result["message"].(string); ok {
					return &ChatMessage{
						Timestamp: time.Now(),
						Role:      "assistant",
						Content:   content,
					}, nil
				}
			}
		}
	}
}

func (o *OpenClawAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	chatCtx, cancel := context.WithTimeout(ctx, 120*time.Second)

	if sessionKey == "" {
		sessionKey = "agent:main:main"
	}

	conn, err := o.dial(chatCtx)
	if err != nil {
		cancel()
		return nil, err
	}

	req := newRPCRequest("chat.send", map[string]any{
		"sessionKey":     sessionKey,
		"message":        message,
		"idempotencyKey": uuidV4(),
	})
	payload, _ := json.Marshal(req)
	if err := conn.Write(chatCtx, websocket.MessageText, payload); err != nil {
		conn.CloseNow()
		cancel()
		return nil, fmt.Errorf("sending chat.send: %w", err)
	}

	ch := make(chan ChatEvent, 64)
	go o.readStreamEvents(chatCtx, cancel, conn, ch)
	return ch, nil
}

func (o *OpenClawAdapter) readStreamEvents(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, ch chan<- ChatEvent) {
	defer close(ch)
	defer cancel()
	defer conn.CloseNow()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var frame map[string]any
		if json.Unmarshal(data, &frame) != nil {
			continue
		}

		frameType, _ := frame["type"].(string)

		if frameType == "event" {
			ev := o.parseChatStreamEvent(frame)
			if ev == nil {
				continue
			}
			select {
			case ch <- *ev:
			case <-ctx.Done():
				return
			}
			if ev.Type == "done" || ev.Type == "error" {
				return
			}
			continue
		}

		if frameType == "res" {
			var resp rpcResponse
			if json.Unmarshal(data, &resp) != nil {
				continue
			}
			if resp.Error != nil {
				select {
				case ch <- ChatEvent{Type: "error", Error: fmt.Sprintf("%s: %s", resp.Error.Code, resp.Error.Message)}:
				case <-ctx.Done():
				}
				return
			}
			if resp.Payload != nil {
				var result map[string]any
				json.Unmarshal(resp.Payload, &result)
				if content, ok := result["message"].(string); ok {
					select {
					case ch <- ChatEvent{Type: "done", Content: content}:
					case <-ctx.Done():
					}
					return
				}
			}
		}
	}
}

func (o *OpenClawAdapter) parseChatStreamEvent(frame map[string]any) *ChatEvent {
	eventName, _ := frame["event"].(string)
	pl, _ := frame["payload"].(map[string]any)
	if pl == nil {
		return nil
	}

	str := func(key string) string {
		if v, ok := pl[key].(string); ok {
			return v
		}
		return ""
	}

	switch eventName {
	case "chat":
		state := str("state")
		switch state {
		case "final":
			return &ChatEvent{Type: "done", Content: str("message")}
		case "error":
			return &ChatEvent{Type: "error", Error: str("errorMessage")}
		case "aborted":
			return &ChatEvent{Type: "error", Error: "response aborted"}
		default:
			delta := str("delta")
			if delta == "" {
				delta = str("message")
			}
			if delta != "" {
				return &ChatEvent{Type: "delta", Content: delta}
			}
			return nil
		}
	case "agent":
		seq := str("seq")
		if toolName := str("tool"); toolName != "" {
			if seq == "tool_start" || seq == "toolCall" {
				var args map[string]any
				if a, ok := pl["args"].(map[string]any); ok {
					args = a
				} else if a, ok := pl["arguments"].(map[string]any); ok {
					args = a
				}
				toolID := str("toolCallId")
				if toolID == "" {
					toolID = str("id")
				}
				return &ChatEvent{Type: "tool_start", Tool: toolName, ToolID: toolID, Args: args}
			}
			if seq == "tool_result" || seq == "toolResult" {
				output := str("output")
				if output == "" {
					output = str("result")
				}
				success := true
				if v, ok := pl["success"].(bool); ok {
					success = v
				}
				return &ChatEvent{Type: "tool_result", Tool: toolName, Output: output, Success: &success}
			}
		}
		return nil
	default:
		return nil
	}
}

func (o *OpenClawAdapter) CreateSession(_ context.Context, name string) (*Session, error) {
	key := "agent:main:" + name
	return &Session{Key: key, Title: name}, nil
}

func (o *OpenClawAdapter) ResetSession(ctx context.Context, sessionKey string) error {
	_, err := o.rpcCall(ctx, "sessions.reset", map[string]any{"key": sessionKey})
	if err != nil {
		return fmt.Errorf("sessions.reset: %w", err)
	}
	return nil
}

// DestroySession completely removes a session — deletes the JSONL transcript
// and removes the entry from sessions.json. Works for both active and archived sessions.
func (o *OpenClawAdapter) DestroySession(_ context.Context, sessionKey string) error {
	// For archived sessions, just delete the file
	if strings.HasPrefix(sessionKey, "archive:") {
		return o.DeleteSession(context.Background(), sessionKey)
	}

	agentsDir := o.agentsDir()
	if agentsDir == "" {
		return fmt.Errorf("cannot determine agents directory")
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return fmt.Errorf("reading agents dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessJSONPath := filepath.Join(agentsDir, entry.Name(), "sessions", "sessions.json")
		data, err := os.ReadFile(sessJSONPath)
		if err != nil {
			continue
		}

		var store map[string]json.RawMessage
		if json.Unmarshal(data, &store) != nil {
			continue
		}

		raw, ok := store[sessionKey]
		if !ok {
			continue
		}

		// Parse to get the session file path
		var meta struct {
			SessionID   string `json:"sessionId"`
			SessionFile string `json:"sessionFile"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return fmt.Errorf("parsing session metadata for %q: %w", sessionKey, err)
		}

		// Delete the JSONL transcript file
		if meta.SessionFile != "" {
			if err := os.Remove(meta.SessionFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing transcript %s: %w", meta.SessionFile, err)
			}
		} else if meta.SessionID != "" {
			jsonlPath := filepath.Join(agentsDir, entry.Name(), "sessions", meta.SessionID+".jsonl")
			if err := os.Remove(jsonlPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing transcript %s: %w", jsonlPath, err)
			}
		}

		// Remove from sessions.json (atomic write via temp file + rename)
		delete(store, sessionKey)
		updated, err := json.MarshalIndent(store, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling sessions.json: %w", err)
		}
		tmpPath := sessJSONPath + ".tmp"
		if err := os.WriteFile(tmpPath, updated, 0o600); err != nil {
			return fmt.Errorf("writing sessions.json tmp: %w", err)
		}
		if err := os.Rename(tmpPath, sessJSONPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("renaming sessions.json: %w", err)
		}
		return nil
	}

	return fmt.Errorf("session %q not found", sessionKey)
}

func (o *OpenClawAdapter) DeleteSession(_ context.Context, sessionKey string) error {
	if !strings.HasPrefix(sessionKey, "archive:") {
		return fmt.Errorf("only archived sessions can be purged")
	}
	archiveFilename := sessionKey[len("archive:"):]
	agentsDir := o.agentsDir()
	if agentsDir == "" {
		return fmt.Errorf("cannot determine agents directory")
	}
	entries, _ := os.ReadDir(agentsDir)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(agentsDir, entry.Name(), "sessions", archiveFilename)
		if _, err := os.Stat(path); err == nil {
			return os.Remove(path)
		}
	}
	return fmt.Errorf("archive file not found: %s", archiveFilename)
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

// OpenClaw gateway protocol: every frame is { type: "req"|"res"|"event", id, ... }.
// The first message on a connection must be a "connect" request with client identity.
type rpcRequest struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Ok      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newRPCRequest(method string, params any) rpcRequest {
	return rpcRequest{
		Type:   "req",
		ID:     uuidV4(),
		Method: method,
		Params: params,
	}
}

// dial opens a WebSocket and performs the mandatory connect handshake.
func (o *OpenClawAdapter) dial(ctx context.Context) (*websocket.Conn, error) {
	url := fmt.Sprintf("ws://%s:%d", o.host, o.port)
	if o.token != "" {
		url += "?token=" + o.token
	}

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to OpenClaw at %s:%d: %w", o.host, o.port, err)
	}
	conn.SetReadLimit(4 * 1024 * 1024) // 4 MB — hello-ok includes a full gateway snapshot

	// Read the connect.challenge event to get the server nonce
	var challengeNonce string
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			conn.Close(websocket.StatusInternalError, "challenge read failed")
			return nil, fmt.Errorf("reading connect challenge: %w", err)
		}
		var ev struct {
			Type    string `json:"type"`
			Event   string `json:"event"`
			Payload struct {
				Nonce string `json:"nonce"`
			} `json:"payload"`
		}
		if json.Unmarshal(data, &ev) == nil && ev.Type == "event" && ev.Event == "connect.challenge" {
			challengeNonce = ev.Payload.Nonce
			break
		}
	}

	scopes := []string{"operator.admin", "operator.read", "operator.write", "operator.approvals", "operator.pairing"}
	connectParams := map[string]any{
		"minProtocol": 3,
		"maxProtocol": 3,
		"client": map[string]any{
			"id":       "gateway-client",
			"version":  "0.1.0",
			"platform": "darwin",
			"mode":     "backend",
		},
		"role":   "operator",
		"scopes": scopes,
	}
	if o.token != "" {
		connectParams["auth"] = map[string]string{"token": o.token}
	}
	if identity := loadOrCreateDeviceIdentity(); identity != nil {
		signedAt := time.Now().UnixMilli()
		connectParams["device"] = identity.buildConnectDevice(o.token, scopes, signedAt, challengeNonce)
	}
	connectReq := newRPCRequest("connect", connectParams)
	payload, _ := json.Marshal(connectReq)
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		conn.Close(websocket.StatusInternalError, "connect write failed")
		return nil, fmt.Errorf("sending connect handshake: %w", err)
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			conn.Close(websocket.StatusInternalError, "connect read failed")
			return nil, fmt.Errorf("reading connect response: %w", err)
		}
		var frame struct {
			Type  string    `json:"type"`
			Ok    bool      `json:"ok"`
			Error *rpcError `json:"error,omitempty"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			conn.Close(websocket.StatusInternalError, "connect failed")
			return nil, fmt.Errorf("connect handshake: unmarshal: %w", err)
		}
		switch frame.Type {
		case "hello-ok":
			goto connected
		case "res":
			if frame.Ok {
				goto connected
			}
			if frame.Error != nil {
				conn.Close(websocket.StatusInternalError, "connect rejected")
				return nil, fmt.Errorf("connect handshake rejected: %s: %s", frame.Error.Code, frame.Error.Message)
			}
			conn.Close(websocket.StatusInternalError, "connect failed")
			return nil, fmt.Errorf("connect handshake: res ok=false with no error")
		case "event":
			continue
		default:
			continue
		}
	}
connected:

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

	req := newRPCRequest(method, params)
	payload, _ := json.Marshal(req)

	if err := conn.Write(callCtx, websocket.MessageText, payload); err != nil {
		return nil, fmt.Errorf("sending RPC %s: %w", method, err)
	}

	for {
		_, data, err := conn.Read(callCtx)
		if err != nil {
			return nil, fmt.Errorf("reading RPC response for %s: %w", method, err)
		}

		var peek struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &peek) == nil && peek.Type == "event" {
			continue // skip event frames interleaved with RPC responses
		}

		var resp rpcResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			var raw map[string]any
			if json.Unmarshal(data, &raw) == nil {
				return raw, nil
			}
			return nil, fmt.Errorf("parsing RPC response for %s: %w", method, err)
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("RPC %s error %s: %s", method, resp.Error.Code, resp.Error.Message)
		}

		var result map[string]any
		if resp.Payload != nil {
			if err := json.Unmarshal(resp.Payload, &result); err != nil {
				return nil, fmt.Errorf("parsing RPC payload for %s: %w", method, err)
			}
		}
		return result, nil
	}
}

// QuickHealthCheck does a fast WebSocket connect + handshake probe.
func (o *OpenClawAdapter) QuickHealthCheck(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := o.dial(probeCtx)
	if err != nil {
		return false
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return true
}

func uuidV4() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Device identity for OpenClaw gateway authentication.
// Without a device identity, the gateway strips all scopes from the connection.
type deviceIdentity struct {
	DeviceID   string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

var eyireDeviceIdentity *deviceIdentity

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func loadOrCreateDeviceIdentity() *deviceIdentity {
	if eyireDeviceIdentity != nil {
		return eyireDeviceIdentity
	}

	identityDir := filepath.Join(os.Getenv("HOME"), ".config", "eyrie")
	identityFile := filepath.Join(identityDir, "device-identity.json")

	// Try loading existing
	if data, err := os.ReadFile(identityFile); err == nil {
		var stored struct {
			Version    int    `json:"version"`
			DeviceID   string `json:"deviceId"`
			PublicKey  string `json:"publicKey"`
			PrivateKey string `json:"privateKey"`
		}
		if json.Unmarshal(data, &stored) == nil && stored.Version == 1 {
			pub, errPub := base64URLEncode_decode(stored.PublicKey)
			priv, errPriv := base64URLEncode_decode(stored.PrivateKey)
			if errPub == nil && errPriv == nil && len(pub) == ed25519.PublicKeySize {
				eyireDeviceIdentity = &deviceIdentity{
					DeviceID:   stored.DeviceID,
					PublicKey:  ed25519.PublicKey(pub),
					PrivateKey: ed25519.PrivateKey(priv),
				}
				return eyireDeviceIdentity
			}
		}
	}

	// Generate new
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil
	}

	hash := sha256.Sum256(pub)
	deviceID := fmt.Sprintf("%x", hash[:])

	eyireDeviceIdentity = &deviceIdentity{
		DeviceID:   deviceID,
		PublicKey:  pub,
		PrivateKey: priv,
	}

	// Persist
	os.MkdirAll(identityDir, 0700)
	stored, _ := json.MarshalIndent(map[string]any{
		"version":    1,
		"deviceId":   deviceID,
		"publicKey":  base64URLEncode(pub),
		"privateKey": base64URLEncode(priv),
	}, "", "  ")
	os.WriteFile(identityFile, stored, 0600)

	return eyireDeviceIdentity
}

func base64URLEncode_decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func (id *deviceIdentity) buildConnectDevice(token string, scopes []string, signedAtMs int64, nonce string) map[string]any {
	scopeStr := strings.Join(scopes, ",")
	payload := strings.Join([]string{
		"v3",
		id.DeviceID,
		"gateway-client",
		"backend",
		"operator",
		scopeStr,
		fmt.Sprintf("%d", signedAtMs),
		token,
		nonce,
		"darwin",
		"",
	}, "|")

	sig := ed25519.Sign(id.PrivateKey, []byte(payload))

	return map[string]any{
		"id":        id.DeviceID,
		"publicKey": base64URLEncode(id.PublicKey),
		"signature": base64URLEncode(sig),
		"signedAt":  signedAtMs,
		"nonce":     nonce,
	}
}

var (
	_ Agent = (*OpenClawAdapter)(nil)
	_ Agent = (*ZeroClawAdapter)(nil)
)
