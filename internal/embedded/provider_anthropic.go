package embedded

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// AnthropicProvider implements LLMProvider against Anthropic's native
// /v1/messages API. It translates between our OpenAI-shaped internal
// types (Message, ToolDef, ToolCall) and Anthropic's content-block
// format on the way in and out.
//
// WHY translate (not switch our internal types): the commander and
// EyrieClaw are already built around OpenAI-shaped Message/ToolDef
// types. Changing those would force a refactor across both consumers.
// Translating at the provider boundary is the minimum-disruption path
// and means either provider can be swapped in behind the same interface.
//
// Differences from OpenAICompatProvider that this provider handles:
//   - system prompt is a top-level request parameter, not a message
//   - tool definitions are flatter: {name, description, input_schema}
//   - assistant content is an array of typed blocks (text | tool_use)
//   - tool results live inside user messages as tool_result blocks
//   - streaming is event-typed (content_block_start/delta/stop) rather
//     than a single delta stream
//   - auth uses x-api-key + anthropic-version header, not Bearer
type AnthropicProvider struct {
	apiKey  string
	apiBase string // e.g. "https://api.anthropic.com"
	version string // anthropic-version header value
	client  *http.Client
}

// NewAnthropicProvider creates a provider for the given base URL and
// API key. apiBase should be the URL without the /v1/messages suffix.
// Defaults to https://api.anthropic.com when empty.
func NewAnthropicProvider(apiKey, apiBase string) *AnthropicProvider {
	if apiBase == "" {
		apiBase = "https://api.anthropic.com"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		apiBase: strings.TrimRight(apiBase, "/"),
		// 2023-06-01 is the stable release of the Messages API and what
		// the official SDKs default to. Bump when we opt into newer
		// features (e.g. extended thinking) behind a feature flag.
		version: "2023-06-01",
		client: &http.Client{
			// Same 3-minute ceiling as the OpenAI-compat provider. The
			// streaming path uses a separate timeout-less client so long
			// responses aren't killed by the overall timeout.
			Timeout: 3 * time.Minute,
		},
	}
}

// --- Anthropic wire types ---------------------------------------------
// Kept unexported — they exist only to drive JSON encode/decode and are
// not part of the LLMProvider contract.

// anthropicMessage matches {role: "user"|"assistant", content: [...blocks]}.
// Anthropic requires content to always be a list of blocks on the wire
// (the string-content shorthand is equivalent to a single text block).
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

// anthropicBlock is one content block. The Type field determines which
// of the other fields are populated. A custom MarshalJSON emits only
// the fields Anthropic expects for each type — Anthropic validates
// strictly and rejects extra fields (even null ones) on block types
// that don't use them.
type anthropicBlock struct {
	Type string // "text" | "tool_use" | "tool_result"

	// text
	Text string

	// tool_use (assistant-produced)
	ID    string
	Name  string
	Input map[string]any

	// tool_result (user-produced, feeds back into the next turn)
	ToolUseID  string
	ContentStr string
	IsError    bool
}

// MarshalJSON emits only the fields relevant to each block type.
//
// WHY custom marshal: Anthropic's strict validation rejects unknown
// fields on each block type. A polymorphic struct with omitempty can't
// satisfy both "input is required on tool_use" (even when empty) and
// "input is prohibited on text blocks". Marshaling per-type is the
// only clean solution without splitting into three separate structs.
func (b anthropicBlock) MarshalJSON() ([]byte, error) {
	switch b.Type {
	case "text":
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{b.Type, b.Text})

	case "tool_use":
		input := b.Input
		if input == nil {
			input = map[string]any{}
		}
		return json.Marshal(struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}{b.Type, b.ID, b.Name, input})

	case "tool_result":
		v := struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   string `json:"content,omitempty"`
			IsError   bool   `json:"is_error,omitempty"`
		}{b.Type, b.ToolUseID, b.ContentStr, b.IsError}
		return json.Marshal(v)

	default:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{b.Type})
	}
}

// anthropicTool matches Anthropic's flatter tool-definition shape.
// Note: input_schema, not parameters; no outer {type: "function"} wrapper.
type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicRequest is the JSON body for POST /v1/messages.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"` // required by Anthropic
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
}

// --- Translation: our types → Anthropic wire types --------------------

// translateMessages converts our OpenAI-shaped history into Anthropic's
// (system, messages) pair. Three non-trivial transforms happen here:
//
//  1. The first system message is extracted into the top-level `system`
//     field. Anthropic rejects system messages in the messages array.
//     Later system messages (rare) are folded into an assistant text
//     block so we don't drop information.
//  2. An assistant message with both text content and tool_calls
//     becomes one assistant message with multiple blocks: a text block
//     (if non-empty) followed by one tool_use block per tool call. The
//     tool_use block's `input` is the tool_call's JSON-string arguments
//     parsed into an object.
//  3. A tool result message (role="tool") becomes a tool_result block
//     inside a USER message. Consecutive tool results get merged into
//     the same user message so Anthropic sees them as a batch. If a
//     tool result arrives between assistant messages with no interleaved
//     user text, we still wrap it as a fresh user message.
func translateMessages(msgs []Message) (system string, out []anthropicMessage) {
	// Track the pending user message so we can append additional
	// tool_result blocks to it when tool results come in a batch.
	var pendingUser *anthropicMessage

	for _, m := range msgs {
		switch m.Role {
		case "system":
			if system == "" {
				system = m.Content
				continue
			}
			// A second system message is unusual. Fold it into the last
			// assistant message as a text block so the content isn't lost.
			// In practice the commander only emits one system message.
			if len(out) > 0 && out[len(out)-1].Role == "assistant" {
				out[len(out)-1].Content = append(out[len(out)-1].Content,
					anthropicBlock{Type: "text", Text: m.Content})
			}

		case "user":
			pendingUser = nil
			out = append(out, anthropicMessage{
				Role:    "user",
				Content: []anthropicBlock{{Type: "text", Text: m.Content}},
			})

		case "assistant":
			pendingUser = nil
			blocks := make([]anthropicBlock, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				// Parse the OpenAI JSON-string arguments into an object.
				// Anthropic's `input` is a real JSON object, not a string.
				var input map[string]any
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}
				if input == nil {
					input = map[string]any{}
				}
				blocks = append(blocks, anthropicBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			// Anthropic rejects empty-content messages. Skip any
			// assistant message that came in with neither text nor
			// tool calls (shouldn't happen, but defensive).
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropicMessage{Role: "assistant", Content: blocks})

		case "tool":
			block := anthropicBlock{
				Type:       "tool_result",
				ToolUseID:  m.ToolCallID,
				ContentStr: m.Content,
			}
			// Merge into the prior pending user message if the last
			// message we emitted was a user message carrying tool results.
			// This keeps batched tool results in a single user turn.
			if pendingUser != nil {
				pendingUser.Content = append(pendingUser.Content, block)
				continue
			}
			out = append(out, anthropicMessage{
				Role:    "user",
				Content: []anthropicBlock{block},
			})
			pendingUser = &out[len(out)-1]
		}
	}
	return system, out
}

// buildRequest assembles the Anthropic request body from our call-site
// inputs. max_tokens is required, so we supply a conservative default
// when the caller omitted it from opts. Temperature is optional.
func (p *AnthropicProvider) buildRequest(messages []Message, tools []ToolDef, model string, opts map[string]any, stream bool) anthropicRequest {
	system, translated := translateMessages(messages)
	req := anthropicRequest{
		Model:    model,
		System:   system,
		Messages: translated,
		Tools:    translateTools(tools),
		Stream:   stream,
	}
	// 4096 matches commander.go's default and is plenty for most replies.
	// Callers (like the commander) pass max_tokens explicitly; this
	// fallback exists only for direct API consumers.
	req.MaxTokens = 4096
	if mt, ok := opts["max_tokens"]; ok {
		switch v := mt.(type) {
		case int:
			req.MaxTokens = v
		case float64:
			req.MaxTokens = int(v)
		}
	}
	return req
}

// Chat sends a non-streaming request to /v1/messages. Returns the
// response translated back into our OpenAI-shaped Response type.
func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, model string, opts map[string]any) (*Response, error) {
	body, err := json.Marshal(p.buildRequest(messages, tools, model, opts, false))
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	p.setAuthHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return parseAnthropicResponse(respBody)
}

// setAuthHeaders applies the x-api-key + anthropic-version headers
// Anthropic requires. Unlike OpenAI's Bearer token, the version header
// is mandatory; Anthropic returns 400 without it.
func (p *AnthropicProvider) setAuthHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("x-api-key", p.apiKey)
	}
	req.Header.Set("anthropic-version", p.version)
}

// parseAnthropicResponse translates a /v1/messages response body into
// our Response type. Key shape differences from OpenAI:
//   - No `choices` array. The response IS the single message.
//   - `content` is a list of blocks; we concatenate text blocks into
//     Response.Content and convert tool_use blocks to ToolCalls.
//   - `input` on a tool_use block is a JSON object; we re-marshal it
//     into a string to fit ToolCall.Function.Arguments.
//   - Token counts live in `usage.input_tokens` / `usage.output_tokens`
//     (not prompt_tokens/completion_tokens).
func parseAnthropicResponse(data []byte) (*Response, error) {
	var raw struct {
		Content    []anthropicBlock `json:"content"`
		StopReason string           `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	resp := &Response{
		FinishReason: raw.StopReason,
		InputTokens:  raw.Usage.InputTokens,
		OutputTokens: raw.Usage.OutputTokens,
	}
	var textBuf strings.Builder
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			textBuf.WriteString(block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			tc := ToolCall{ID: block.ID, Type: "function"}
			tc.Function.Name = block.Name
			tc.Function.Arguments = string(argsJSON)
			resp.ToolCalls = append(resp.ToolCalls, tc)
		}
	}
	resp.Content = textBuf.String()
	return resp, nil
}

// ChatStream sends a streaming request to /v1/messages and calls
// onDelta for each text chunk. Returns the accumulated Response once
// the stream completes.
//
// Anthropic's streaming format is fundamentally different from OpenAI's.
// OpenAI emits one kind of event (a chat.completion.chunk) whose delta
// may contain either text or tool-call fragments, with tool_calls
// addressed by a numeric index. Anthropic emits a small state machine
// of typed events:
//
//   message_start         — usage/metadata; we capture input_tokens here
//   content_block_start   — "block N is going to be text" or "tool_use id=...
//                           name=..." — tool_use IDs arrive here, not on delta
//   content_block_delta   — the actual chunk: text_delta.text or
//                           input_json_delta.partial_json
//   content_block_stop    — block N is finished
//   message_delta         — stop_reason + final output_tokens
//   message_stop          — terminator
//
// We accumulate per-block state in a map keyed by block index, then
// assemble the final Response when message_stop arrives.
func (p *AnthropicProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDef, model string, opts map[string]any, onDelta func(delta string)) (*Response, error) {
	body, err := json.Marshal(p.buildRequest(messages, tools, model, opts, true))
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	p.setAuthHeaders(req)
	req.Header.Set("Accept", "text/event-stream")

	// Streaming client with no overall Timeout — the http.Client.Timeout
	// aborts body reads mid-stream. Cancellation rides on ctx instead.
	streamClient := &http.Client{Transport: p.client.Transport}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return parseAnthropicStream(ctx, resp.Body, onDelta)
}

// parseAnthropicStream reads an Anthropic SSE stream and builds a
// Response. See ChatStream's comment for the event sequence.
func parseAnthropicStream(ctx context.Context, reader io.Reader, onDelta func(delta string)) (*Response, error) {
	// Per-block accumulators keyed by block index. For text blocks we
	// build up the concatenated string; for tool_use blocks we capture
	// id+name at content_block_start and append input_json_delta chunks
	// into a JSON string that we parse at block_stop.
	type blockAccum struct {
		typ      string // "text" | "tool_use"
		text     strings.Builder
		toolID   string
		toolName string
		argsJSON strings.Builder
	}
	blocks := map[int]*blockAccum{}

	var (
		stopReason   string
		inputTokens  int
		outputTokens int
		textAll      strings.Builder
	)

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	// Anthropic's SSE messages have both `event:` and `data:` lines.
	// We only need data — the `type` field inside the JSON payload is
	// authoritative and matches the event header. Skipping event lines
	// keeps the parser compatible if Anthropic ever elides them.
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var envelope struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			// message_start / message_delta payloads
			Message *struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message,omitempty"`
			// content_block_start payload
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block,omitempty"`
			// content_block_delta payload
			Delta *struct {
				Type        string `json:"type"` // "text_delta" | "input_json_delta"
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				// message_delta piggybacks stop_reason on the same Delta
				// field name. Harmless because the outer Type tells us
				// how to interpret it.
				StopReason string `json:"stop_reason"`
			} `json:"delta,omitempty"`
			// message_delta carries final output_tokens here (not in Message)
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			continue // skip malformed chunks
		}

		switch envelope.Type {
		case "message_start":
			if envelope.Message != nil {
				inputTokens = envelope.Message.Usage.InputTokens
				outputTokens = envelope.Message.Usage.OutputTokens
			}

		case "content_block_start":
			if envelope.ContentBlock == nil {
				continue
			}
			acc := &blockAccum{typ: envelope.ContentBlock.Type}
			if acc.typ == "tool_use" {
				acc.toolID = envelope.ContentBlock.ID
				acc.toolName = envelope.ContentBlock.Name
			}
			blocks[envelope.Index] = acc

		case "content_block_delta":
			acc, ok := blocks[envelope.Index]
			if !ok || envelope.Delta == nil {
				continue
			}
			switch envelope.Delta.Type {
			case "text_delta":
				acc.text.WriteString(envelope.Delta.Text)
				textAll.WriteString(envelope.Delta.Text)
				if onDelta != nil && envelope.Delta.Text != "" {
					onDelta(envelope.Delta.Text)
				}
			case "input_json_delta":
				acc.argsJSON.WriteString(envelope.Delta.PartialJSON)
			}

		case "content_block_stop":
			// Nothing to do — the accumulator already has everything.
			// Kept explicit so future handling (e.g. structured outputs)
			// has an obvious hook point.

		case "message_delta":
			if envelope.Delta != nil && envelope.Delta.StopReason != "" {
				stopReason = envelope.Delta.StopReason
			}
			if envelope.Usage != nil {
				outputTokens = envelope.Usage.OutputTokens
			}

		case "message_stop":
			// Terminator — break out. `break` alone only exits the
			// switch; we need a labeled break or fallthrough via flag.
			goto done

		case "ping":
			// Anthropic sends periodic pings to keep the connection
			// alive. Ignore.
		}
	}
done:

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("streaming read error: %w", err)
	}

	// Assemble final tool calls in deterministic index order so resume
	// paths (findUnresolvedToolCalls) see a stable ordering.
	indices := make([]int, 0, len(blocks))
	for i := range blocks {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	var toolCalls []ToolCall
	for _, i := range indices {
		acc := blocks[i]
		if acc.typ != "tool_use" {
			continue
		}
		tc := ToolCall{ID: acc.toolID, Type: "function"}
		tc.Function.Name = acc.toolName
		// Anthropic streams input as JSON fragments that concatenate
		// into a valid JSON object. Empty string (no input) becomes "{}".
		args := acc.argsJSON.String()
		if args == "" {
			args = "{}"
		}
		tc.Function.Arguments = args
		toolCalls = append(toolCalls, tc)
	}

	if stopReason == "" {
		stopReason = "end_turn"
	}

	return &Response{
		Content:      textAll.String(),
		ToolCalls:    toolCalls,
		FinishReason: stopReason,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// translateTools converts our ToolDef list into Anthropic's flatter
// tool definitions. Our Parameters map becomes Anthropic's InputSchema
// verbatim — both sides speak JSON Schema.
func translateTools(tools []ToolDef) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return out
}
