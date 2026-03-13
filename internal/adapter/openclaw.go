package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"nhooyr.io/websocket"
)

// OpenClawAdapter communicates with an OpenClaw instance via its WebSocket RPC gateway.
// OpenClaw exposes a single WebSocket endpoint at ws://host:port with RPC methods.
type OpenClawAdapter struct {
	id      string
	name    string
	host    string
	port    int
	token   string
}

func NewOpenClawAdapter(id, name, host string, port int, token string) *OpenClawAdapter {
	return &OpenClawAdapter{
		id:    id,
		name:  name,
		host:  host,
		port:  port,
		token: token,
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
	conn, err := o.dial(ctx)
	if err != nil {
		return nil, err
	}

	// Send the logs.tail RPC request
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
	go o.readLogStream(ctx, conn, ch)
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

// TailActivity subscribes to the OpenClaw WebSocket event stream and emits
// structured ActivityEvents for agent and chat events.
func (o *OpenClawAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
	conn, err := o.dial(ctx)
	if err != nil {
		return nil, err
	}

	ch := make(chan ActivityEvent, 64)
	go o.readActivityStream(ctx, conn, ch)
	return ch, nil
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

func (o *OpenClawAdapter) Sessions(ctx context.Context) ([]Session, error) {
	result, err := o.rpcCall(ctx, "sessions.list", map[string]any{
		"includeDerivedTitles": true,
		"includeLastMessage":  true,
	})
	if err != nil {
		return nil, err
	}

	rawSessions, ok := result["sessions"].([]any)
	if !ok {
		return nil, nil
	}

	sessions := make([]Session, 0, len(rawSessions))
	for _, raw := range rawSessions {
		sm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		s := Session{}
		if v, ok := sm["key"].(string); ok {
			s.Key = v
		} else if v, ok := sm["sessionKey"].(string); ok {
			s.Key = v
		}
		if v, ok := sm["title"].(string); ok {
			s.Title = v
		} else if v, ok := sm["derivedTitle"].(string); ok {
			s.Title = v
		}
		if v, ok := sm["channel"].(string); ok {
			s.Channel = v
		}
		if v, ok := sm["lastMessageAt"].(string); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				s.LastMsg = &t
			}
		}
		if s.Key != "" {
			sessions = append(sessions, s)
		}
	}

	return sessions, nil
}

func (o *OpenClawAdapter) ChatHistory(ctx context.Context, sessionKey string, limit int) ([]ChatMessage, error) {
	result, err := o.rpcCall(ctx, "chat.history", map[string]any{
		"sessionKey": sessionKey,
		"limit":      limit,
	})
	if err != nil {
		return nil, err
	}

	rawMessages, ok := result["messages"].([]any)
	if !ok {
		return nil, nil
	}

	messages := make([]ChatMessage, 0, len(rawMessages))
	for _, raw := range rawMessages {
		mm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		m := ChatMessage{}
		if v, ok := mm["role"].(string); ok {
			m.Role = v
		}
		if v, ok := mm["content"].(string); ok {
			m.Content = v
		}
		if v, ok := mm["channel"].(string); ok {
			m.Channel = v
		}
		if v, ok := mm["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				m.Timestamp = t
			}
		}
		if v, ok := mm["createdAt"].(string); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				m.Timestamp = t
			}
		}
		messages = append(messages, m)
	}

	return messages, nil
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
