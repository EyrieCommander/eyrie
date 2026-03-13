package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ZeroClawAdapter communicates with a ZeroClaw instance via its HTTP REST gateway.
// ZeroClaw exposes: GET /health, GET /api/status, GET /api/config, GET /api/events (SSE).
type ZeroClawAdapter struct {
	id      string
	name    string
	baseURL string
	token   string
	client  *http.Client
}

func NewZeroClawAdapter(id, name, baseURL, token string) *ZeroClawAdapter {
	return &ZeroClawAdapter{
		id:      id,
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
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
		Provider    string   `json:"provider"`
		Model       string   `json:"model"`
		Channels    []string `json:"channels"`
		GatewayPort int      `json:"gateway_port"`
		Uptime      int      `json:"uptime_seconds"`
	}

	if err := z.getJSON(ctx, "/api/status", &resp); err != nil {
		return nil, err
	}

	return &AgentStatus{
		Provider:    resp.Provider,
		Model:       resp.Model,
		Channels:    resp.Channels,
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

// TailLogs connects to the ZeroClaw SSE event stream at GET /api/events.
func (z *ZeroClawAdapter) TailLogs(ctx context.Context) (<-chan LogEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", z.baseURL+"/api/events", nil)
	if err != nil {
		return nil, fmt.Errorf("creating SSE request: %w", err)
	}
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a client without timeout for streaming
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
	go z.readSSE(ctx, resp.Body, ch)
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

// TailActivity connects to the ZeroClaw SSE event stream and returns typed
// activity events (agent_start, tool_call, llm_request, etc.) instead of raw logs.
func (z *ZeroClawAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
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
	go z.readActivitySSE(ctx, resp.Body, ch)
	return ch, nil
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

func (z *ZeroClawAdapter) Sessions(_ context.Context) ([]Session, error) {
	return nil, nil
}

func (z *ZeroClawAdapter) ChatHistory(_ context.Context, _ string, _ int) ([]ChatMessage, error) {
	return nil, nil
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
