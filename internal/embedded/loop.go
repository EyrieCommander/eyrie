package embedded

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// LoopConfig holds the safety budgets for the agent loop.
type LoopConfig struct {
	// MaxIterations caps the number of LLM round-trips per turn.
	// Default: 20. Prevents infinite tool-call loops.
	MaxIterations int

	// TurnTimeout is the hard deadline for an entire turn.
	// Default: 5 minutes.
	TurnTimeout time.Duration

	// ToolTimeout is the per-tool execution timeout.
	// Default: 60 seconds.
	ToolTimeout time.Duration

	// MaxContextTokens is the approximate token budget. When exceeded,
	// the oldest messages (after system) are dropped. 0 = no limit.
	MaxContextTokens int
}

// DefaultLoopConfig returns conservative defaults.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		MaxIterations:    20,
		TurnTimeout:      5 * time.Minute,
		ToolTimeout:      60 * time.Second,
		MaxContextTokens: 0, // no limit in v1
	}
}

// Event is the embedded agent's internal event type, converted to
// adapter.ChatEvent by the EmbeddedAdapter. This avoids an import cycle
// between embedded and adapter packages.
type Event struct {
	Type    string         // "delta", "tool_start", "tool_result", "done", "error"
	Content string
	Tool    string
	ToolID  string
	Args    map[string]any
	Output  string
	Success *bool
	Error   string
	// Usage stats (populated on "done" events)
	InputTokens  int
	OutputTokens int
}

// LogEntry is the embedded agent's internal log entry type.
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

// AgentLoop orchestrates the send-tool-repeat cycle for an embedded agent.
// It calls the LLM provider, executes tool calls, appends results, and
// repeats until the model produces a text-only response or a budget is hit.
type AgentLoop struct {
	provider LLMProvider
	tools    *ToolRegistry
	config   LoopConfig
	logBuf   *LogBuffer
}

// NewAgentLoop creates a loop with the given provider, tools, and config.
func NewAgentLoop(provider LLMProvider, tools *ToolRegistry, cfg LoopConfig, logBuf *LogBuffer) *AgentLoop {
	return &AgentLoop{
		provider: provider,
		tools:    tools,
		config:   cfg,
		logBuf:   logBuf,
	}
}

// Run executes the agent loop for a single turn, emitting Events to the
// returned channel. The channel is closed after a terminal event ("done" or
// "error").
func (al *AgentLoop) Run(ctx context.Context, systemPrompt string, history []Message, model string) <-chan Event {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)

		// Turn-level timeout
		turnCtx, turnCancel := context.WithTimeout(ctx, al.config.TurnTimeout)
		defer turnCancel()

		// Build initial messages: system prompt + history
		messages := make([]Message, 0, len(history)+1)
		messages = append(messages, Message{Role: "system", Content: systemPrompt})
		messages = append(messages, history...)

		// Truncate if over token budget (approximate: 4 chars per token)
		messages = al.truncateContext(messages)

		opts := map[string]any{
			"temperature": 0.7,
		}

		for iter := 0; iter < al.config.MaxIterations; iter++ {
			resp, err := al.safeIteration(turnCtx, messages, model, opts, ch)
			if err != nil {
				al.logBuf.Add("error", fmt.Sprintf("iteration %d failed: %v", iter, err))
				select {
				case ch <- Event{Type: "error", Error: err.Error()}:
				case <-turnCtx.Done():
				}
				return
			}

			// No tool calls — this is the final response
			if len(resp.ToolCalls) == 0 {
				al.logBuf.Add("info", fmt.Sprintf("turn complete after %d iterations, %d input / %d output tokens",
					iter+1, resp.InputTokens, resp.OutputTokens))
				select {
				case ch <- Event{
					Type:         "done",
					Content:      resp.Content,
					InputTokens:  resp.InputTokens,
					OutputTokens: resp.OutputTokens,
				}:
				case <-turnCtx.Done():
				}
				return
			}

			// Append assistant message with tool calls
			assistantMsg := Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			}
			messages = append(messages, assistantMsg)

			// Execute each tool call and append results
			for _, tc := range resp.ToolCalls {
				args := ParseToolArgs(tc.Function.Arguments)

				// Emit tool_start event
				select {
				case ch <- Event{
					Type:   "tool_start",
					Tool:   tc.Function.Name,
					ToolID: tc.ID,
					Args:   args,
				}:
				case <-turnCtx.Done():
					return
				}

				al.logBuf.Add("info", fmt.Sprintf("tool call: %s", tc.Function.Name))

				// Execute with per-tool timeout
				output, toolErr := al.executeTool(turnCtx, tc.Function.Name, args)

				success := true
				if toolErr != nil {
					output = fmt.Sprintf("error: %v", toolErr)
					success = false
					al.logBuf.Add("warn", fmt.Sprintf("tool %s failed: %v", tc.Function.Name, toolErr))
				}

				// Emit tool_result event
				select {
				case ch <- Event{
					Type:    "tool_result",
					Tool:    tc.Function.Name,
					ToolID:  tc.ID,
					Output:  output,
					Success: &success,
				}:
				case <-turnCtx.Done():
					return
				}

				// Append tool result message for the next LLM call
				messages = append(messages, Message{
					Role:       "tool",
					Content:    output,
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
				})
			}

			// Re-truncate before the next iteration
			messages = al.truncateContext(messages)
		}

		// Hit max iterations without a final response
		al.logBuf.Add("warn", fmt.Sprintf("hit max iterations (%d)", al.config.MaxIterations))
		select {
		case ch <- Event{Type: "error", Error: fmt.Sprintf("reached maximum iterations (%d)", al.config.MaxIterations)}:
		case <-turnCtx.Done():
		}
	}()

	return ch
}

// safeIteration wraps a single LLM call with panic recovery.
func (al *AgentLoop) safeIteration(ctx context.Context, messages []Message, model string, opts map[string]any, ch chan<- Event) (resp *Response, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in agent loop: %v", r)
			al.logBuf.Add("error", fmt.Sprintf("panic recovered: %v", r))
		}
	}()

	toolDefs := al.tools.Definitions()

	// Stream text deltas to the channel while accumulating the full response
	resp, err = al.provider.ChatStream(ctx, messages, toolDefs, model, opts, func(delta string) {
		select {
		case ch <- Event{Type: "delta", Content: delta}:
		case <-ctx.Done():
		}
	})
	return
}

// executeTool runs a registered tool with per-tool timeout and panic recovery.
func (al *AgentLoop) executeTool(ctx context.Context, name string, args map[string]any) (output string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in tool %s: %v", name, r)
			al.logBuf.Add("error", fmt.Sprintf("tool panic recovered: %s: %v", name, r))
		}
	}()

	tool := al.tools.Get(name)
	if tool == nil {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	toolCtx, cancel := context.WithTimeout(ctx, al.config.ToolTimeout)
	defer cancel()

	return tool.Execute(toolCtx, args)
}

// truncateContext drops oldest non-system messages when the approximate token
// count exceeds the budget. Uses a rough 4 chars = 1 token heuristic.
func (al *AgentLoop) truncateContext(messages []Message) []Message {
	if al.config.MaxContextTokens <= 0 || len(messages) <= 1 {
		return messages
	}

	// Approximate total tokens
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
	}

	if total <= al.config.MaxContextTokens {
		return messages
	}

	// Keep system message (index 0) and drop oldest user/assistant messages
	// until we're under budget. Always keep at least the system + last 2 messages.
	minKeep := 3
	if len(messages) <= minKeep {
		return messages
	}

	result := []Message{messages[0]}
	dropped := 0
	for i := 1; i < len(messages); i++ {
		remaining := len(messages) - i
		if total > al.config.MaxContextTokens && remaining >= minKeep {
			total -= len(messages[i].Content) / 4
			dropped++
			continue
		}
		result = append(result, messages[i])
	}

	if dropped > 0 {
		slog.Debug("truncated context", "dropped_messages", dropped, "remaining", len(result))
	}
	return result
}

// LogBuffer is a thread-safe ring buffer for embedded agent log entries.
// Uses a head index and count to avoid O(n) copy-shift on every Add().
type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	head    int // index of the oldest entry
	count   int // number of valid entries
	// 500 entries max — covers typical agent activity between UI refreshes
	maxSize int
}

// NewLogBuffer creates a buffer with the given capacity.
func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		entries: make([]LogEntry, maxSize),
		maxSize: maxSize,
	}
}

// Add appends a log entry with the current timestamp.
func (lb *LogBuffer) Add(level, message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}
	idx := (lb.head + lb.count) % lb.maxSize
	if lb.count < lb.maxSize {
		lb.entries[idx] = entry
		lb.count++
	} else {
		// Buffer full — overwrite oldest and advance head
		lb.entries[lb.head] = entry
		lb.head = (lb.head + 1) % lb.maxSize
	}
}

// Entries returns a copy of all buffered log entries in chronological order.
func (lb *LogBuffer) Entries() []LogEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	cp := make([]LogEntry, lb.count)
	for i := 0; i < lb.count; i++ {
		cp[i] = lb.entries[(lb.head+i)%lb.maxSize]
	}
	return cp
}
