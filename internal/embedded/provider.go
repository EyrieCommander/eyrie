// Package embedded implements the EyrieClaw embedded agent — an agent that
// runs inside the Eyrie process as a goroutine rather than as a separate
// framework process. It calls LLM APIs directly and executes tools as Go
// functions, streaming responses through the standard adapter.ChatEvent model.
//
// The provider layer is pluggable: LLMProvider is the interface, and
// OpenAICompatProvider is the first implementation (covers OpenRouter,
// OpenAI, Ollama, and any OpenAI-compatible endpoint).
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

// Message represents a chat message in the OpenAI chat completions format.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set when Role == "tool"
	Name       string     `json:"name,omitempty"`
}

// ToolDef defines a tool the model can invoke, matching the OpenAI tool format.
type ToolDef struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction describes the function within a ToolDef.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall represents a tool invocation returned by the model.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // raw JSON string
	} `json:"function"`
}

// Response is the parsed result of an LLM chat completion.
type Response struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
	InputTokens  int        `json:"input_tokens,omitempty"`
	OutputTokens int        `json:"output_tokens,omitempty"`
}

// LLMProvider is the interface for calling an LLM chat completions API.
// Implementations handle serialization, HTTP transport, and response parsing.
type LLMProvider interface {
	// Chat sends messages to the LLM and returns the complete response.
	Chat(ctx context.Context, messages []Message, tools []ToolDef, model string, opts map[string]any) (*Response, error)

	// ChatStream sends messages and calls onDelta for each text chunk.
	// Tool calls are accumulated and returned in the final Response.
	ChatStream(ctx context.Context, messages []Message, tools []ToolDef, model string, opts map[string]any, onDelta func(delta string)) (*Response, error)
}

// OpenAICompatProvider implements LLMProvider using raw HTTP against any
// OpenAI-compatible chat completions endpoint. This avoids pulling in the
// openai-go SDK (which adds significant dependency weight) in favor of a
// lean implementation following PicoClaw's proven pattern.
type OpenAICompatProvider struct {
	apiKey string
	apiBase string // e.g. "https://openrouter.ai/api/v1"
	client *http.Client
}

// NewOpenAICompatProvider creates a provider for the given base URL and key.
func NewOpenAICompatProvider(apiKey, apiBase string) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		apiKey:  apiKey,
		apiBase: strings.TrimRight(apiBase, "/"),
		client: &http.Client{
			// 3-minute timeout for non-streaming requests. Streaming uses a
			// separate client without Timeout so long responses aren't killed.
			Timeout: 3 * time.Minute,
		},
	}
}

func (p *OpenAICompatProvider) buildBody(messages []Message, tools []ToolDef, model string, opts map[string]any) map[string]any {
	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	if temp, ok := opts["temperature"]; ok {
		body["temperature"] = temp
	}
	if maxTok, ok := opts["max_tokens"]; ok {
		body["max_tokens"] = maxTok
	}
	return body
}

// Chat sends a non-streaming chat completion request.
func (p *OpenAICompatProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, model string, opts map[string]any) (*Response, error) {
	body := p.buildBody(messages, tools, model, opts)

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

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
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return parseNonStreamResponse(respBody)
}

// ChatStream sends a streaming chat completion request and calls onDelta
// for each text content chunk. Returns the accumulated response with all
// tool calls assembled.
func (p *OpenAICompatProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDef, model string, opts map[string]any, onDelta func(delta string)) (*Response, error) {
	body := p.buildBody(messages, tools, model, opts)
	body["stream"] = true
	// Request usage in the final stream chunk (OpenAI / OpenRouter support this)
	body["stream_options"] = map[string]any{"include_usage": true}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Streaming client without Timeout — the http.Client.Timeout covers the
	// entire lifecycle including body reads, which kills long streams.
	// Context cancellation provides the safety net instead.
	streamClient := &http.Client{Transport: p.client.Transport}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return parseStreamResponse(ctx, resp.Body, onDelta)
}

// parseNonStreamResponse parses a non-streaming chat completion response.
func parseNonStreamResponse(data []byte) (*Response, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}
	r := &Response{
		Content:      raw.Choices[0].Message.Content,
		ToolCalls:    raw.Choices[0].Message.ToolCalls,
		FinishReason: raw.Choices[0].FinishReason,
	}
	if raw.Usage != nil {
		r.InputTokens = raw.Usage.PromptTokens
		r.OutputTokens = raw.Usage.CompletionTokens
	}
	return r, nil
}

// parseStreamResponse parses an OpenAI-compatible SSE stream, following
// PicoClaw's proven streaming parser pattern.
func parseStreamResponse(ctx context.Context, reader io.Reader, onDelta func(delta string)) (*Response, error) {
	var textContent strings.Builder
	var finishReason string
	var inputTokens, outputTokens int

	// Tool call assembly: OpenAI streams tool calls as incremental deltas
	// keyed by index within the choices[0].delta.tool_calls array.
	type toolAccum struct {
		id       string
		name     string
		argsJSON strings.Builder
	}
	activeTools := map[int]*toolAccum{}

	scanner := bufio.NewScanner(reader)
	// 1 MB initial buffer, 10 MB max — matches PicoClaw's buffer sizing
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function *struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		if choice.Delta.Content != "" {
			textContent.WriteString(choice.Delta.Content)
			if onDelta != nil {
				onDelta(choice.Delta.Content)
			}
		}

		for _, tc := range choice.Delta.ToolCalls {
			acc, ok := activeTools[tc.Index]
			if !ok {
				acc = &toolAccum{}
				activeTools[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function != nil {
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.argsJSON.WriteString(tc.Function.Arguments)
				}
			}
		}

		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("streaming read error: %w", err)
	}

	// Assemble tool calls from accumulated deltas. Collect and sort
	// the map keys to handle non-contiguous indices deterministically.
	var toolCalls []ToolCall
	sortedIndices := make([]int, 0, len(activeTools))
	for idx := range activeTools {
		sortedIndices = append(sortedIndices, idx)
	}
	sort.Ints(sortedIndices)
	for _, idx := range sortedIndices {
		acc := activeTools[idx]
		tc := ToolCall{
			ID:   acc.id,
			Type: "function",
		}
		tc.Function.Name = acc.name
		tc.Function.Arguments = acc.argsJSON.String()
		toolCalls = append(toolCalls, tc)
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	return &Response{
		Content:      textContent.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}
