package commander

// SSE event types emitted by the commander's chat endpoint. These are the
// wire contract between backend and any client (curl, future UI, test
// scripts). Each event is JSON with a `type` field the client discriminates on.
//
// WHY types as constants: Avoids typos, makes grep-ability easy, gives a
// single source of truth when the UI is built on another machine.
const (
	// EventDelta carries an incremental text chunk from the LLM.
	// Field: {"type": "delta", "text": "..."}
	EventDelta = "delta"

	// EventToolCall announces the LLM wants to invoke a tool.
	// Field: {"type": "tool_call", "id": "...", "name": "...", "args": {...}}
	EventToolCall = "tool_call"

	// EventToolResult carries the result of a tool invocation.
	// Field: {"type": "tool_result", "id": "...", "name": "...", "output": "...", "error": false}
	EventToolResult = "tool_result"

	// EventMessage carries a complete, persisted message (assistant-final).
	// Field: {"type": "message", "role": "assistant", "content": "..."}
	EventMessage = "message"

	// EventDone signals the turn is complete. May include token usage.
	// Field: {"type": "done", "input_tokens": N, "output_tokens": M}
	EventDone = "done"

	// EventError signals a terminal error in the turn.
	// Field: {"type": "error", "error": "..."}
	EventError = "error"

	// EventConfirmRequired signals the LLM wants to call a write tool
	// that requires user approval. The turn pauses after this event;
	// the client must POST /api/commander/confirm/{id} with an approval
	// decision before the tool executes. See pending.go.
	// Field: {"type": "confirm_required", "id": "pa_xxx", "tool": "...",
	//         "args": {...}, "summary": "human-readable description"}
	EventConfirmRequired = "confirm_required"
)

// deltaEvent is the payload for EventDelta.
type deltaEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolCallEvent is the payload for EventToolCall.
type toolCallEvent struct {
	Type string         `json:"type"`
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// toolResultEvent is the payload for EventToolResult.
type toolResultEvent struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Output string `json:"output"`
	Error  bool   `json:"error,omitempty"`
}

// messageEvent is the payload for EventMessage (full assistant message).
type messageEvent struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// doneEvent is the payload for EventDone.
type doneEvent struct {
	Type         string `json:"type"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	// ContextTokens is the input_tokens from the last LLM call in the
	// turn — i.e. how many tokens the full conversation currently
	// consumes. Differs from InputTokens (which sums across all LLM
	// calls within the turn and represents cost, not size).
	ContextTokens int `json:"context_tokens,omitempty"`
	// ContextWindow is the model's maximum context size in tokens.
	// Together with ContextTokens, the client can compute a usage
	// percentage to show the user how close to the limit they are.
	ContextWindow int `json:"context_window,omitempty"`
}

// errorEvent is the payload for EventError.
type errorEvent struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// confirmRequiredEvent is the payload for EventConfirmRequired.
type confirmRequiredEvent struct {
	Type    string         `json:"type"`
	ID      string         `json:"id"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Summary string         `json:"summary"`
}
