package commander

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/embedded"
	"github.com/Audacity88/eyrie/internal/project"
)

// maxTurnIterations bounds the tool-calling loop. Without this, a buggy
// tool or a looping LLM could run indefinitely. Ten iterations is
// generous for legitimate work (each iteration is one LLM call + any
// number of parallel tool executions within it).
const maxTurnIterations = 10

// systemPrompt shapes the commander's personality and role. Kept minimal
// in the skeleton — as the tool set grows we'll give it more guidance.
const systemPrompt = `You are Eyrie, a commander that helps the user manage AI agent projects.

You can see the user's projects, captains, and talons. When asked about them, call the appropriate tool to read live data rather than guessing.

Be concise. Summarize tool results rather than dumping raw JSON at the user.`

// Commander is the orchestrator: it holds the LLM provider, the
// conversation store, and the tool registry. One Commander instance
// serves the whole process — there is a single persistent conversation
// with the user.
type Commander struct {
	provider embedded.LLMProvider
	model    string
	store    *Store
	tools    *Registry
}

// Config configures a new Commander.
type Config struct {
	Provider embedded.LLMProvider
	Model    string
	Store    *Store
	Tools    *Registry
}

// New constructs a Commander. Callers supply all dependencies so this
// package has no knowledge of how the provider is obtained (env var,
// config file, vault) — that's the server's responsibility.
func New(cfg Config) *Commander {
	return &Commander{
		provider: cfg.Provider,
		model:    cfg.Model,
		store:    cfg.Store,
		tools:    cfg.Tools,
	}
}

// NewDefault builds a Commander with the skeleton defaults: OpenRouter
// as the LLM provider, a Claude model with tool-calling support, the
// standard conversation store, and the project-aware tool registry.
//
// WHY OpenRouter (not the Claude Max proxy): the claude-max-api proxy
// runs Claude Code internally — it ignores custom tool definitions and
// only lets the model use Claude Code's built-in tools. For the
// commander's tool-calling loop to work, we need a real LLM endpoint
// that forwards tool definitions to the model.
//
// WHY baked-in defaults in the skeleton: a proper config file and
// provider switching are Phase 5a follow-up work. OpenRouter key
// retrieval uses the existing vault (env var > keys.json fallback).
func NewDefault(projectStore *project.Store) (*Commander, error) {
	store, err := NewStore()
	if err != nil {
		return nil, fmt.Errorf("commander store: %w", err)
	}
	apiKey := config.GetKeyVault().Get("openrouter")
	if apiKey == "" {
		return nil, fmt.Errorf("no OpenRouter API key in vault — set one via Settings or OPENROUTER_API_KEY env var")
	}
	provider := embedded.NewOpenAICompatProvider(apiKey, "https://openrouter.ai/api/v1")
	return New(Config{
		Provider: provider,
		Model:    "anthropic/claude-sonnet-4.6",
		Store:    store,
		Tools:    NewRegistry(projectStore),
	}), nil
}

// Emitter is the minimal interface the turn loop needs to push events
// to the client. The server's SSEWriter satisfies this.
//
// WHY an interface (not *server.SSEWriter): prevents the commander
// package from importing the server package, which would create an
// import cycle since the server imports the commander.
type Emitter interface {
	WriteEvent(v any) error
}

// RunTurn handles one user message: loads history, appends the user
// message, calls the LLM + tools in a bounded loop, and streams events
// to the emitter. Returns nil on success; on error, an error event
// has already been emitted via the emitter.
func (c *Commander) RunTurn(ctx context.Context, userMessage string, emit Emitter) error {
	// Load prior conversation (system prompt is NOT persisted; it's a
	// code-level configuration prepended in memory every turn).
	stored, err := c.store.All()
	if err != nil {
		c.emitError(emit, fmt.Sprintf("loading conversation: %v", err))
		return err
	}
	history := make([]embedded.Message, 0, len(stored)+2)
	history = append(history, embedded.Message{Role: "system", Content: systemPrompt})
	history = append(history, stored...)
	userMsg := embedded.Message{Role: "user", Content: userMessage}
	history = append(history, userMsg)
	if err := c.store.Append(userMsg); err != nil {
		c.emitError(emit, fmt.Sprintf("saving user message: %v", err))
		return err
	}

	toolDefs := c.tools.Definitions()
	var inputTokens, outputTokens int

	// Turn loop: keep going as long as the LLM wants to call tools.
	for iter := 0; iter < maxTurnIterations; iter++ {
		if err := ctx.Err(); err != nil {
			c.emitError(emit, fmt.Sprintf("context cancelled: %v", err))
			return err
		}

		// Call the LLM with the current history.
		onDelta := func(delta string) {
			// Stream text deltas to the client as they arrive.
			_ = emit.WriteEvent(deltaEvent{Type: EventDelta, Text: delta})
		}
		resp, err := c.provider.ChatStream(ctx, history, toolDefs, c.model, nil, onDelta)
		if err != nil {
			c.emitError(emit, fmt.Sprintf("LLM call failed: %v", err))
			return err
		}
		inputTokens += resp.InputTokens
		outputTokens += resp.OutputTokens

		// Append the assistant message (text + any tool calls) to history.
		assistantMsg := embedded.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		history = append(history, assistantMsg)
		if err := c.store.Append(assistantMsg); err != nil {
			slog.Warn("commander: failed to save assistant message", "error", err)
		}

		// If there are no tool calls, the turn is complete.
		if len(resp.ToolCalls) == 0 {
			_ = emit.WriteEvent(messageEvent{
				Type:    EventMessage,
				Role:    "assistant",
				Content: resp.Content,
			})
			_ = emit.WriteEvent(doneEvent{
				Type:         EventDone,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			})
			return nil
		}

		// Execute each tool call and append results to history.
		for _, tc := range resp.ToolCalls {
			args := parseToolArgs(tc.Function.Arguments)
			_ = emit.WriteEvent(toolCallEvent{
				Type: EventToolCall,
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			})

			output, toolErr := c.executeTool(ctx, tc.Function.Name, args)
			isErr := toolErr != nil
			if isErr {
				output = fmt.Sprintf("error: %v", toolErr)
			}

			_ = emit.WriteEvent(toolResultEvent{
				Type:   EventToolResult,
				ID:     tc.ID,
				Name:   tc.Function.Name,
				Output: output,
				Error:  isErr,
			})

			// Append the tool result to history so the LLM can read it
			// on the next iteration.
			toolResultMsg := embedded.Message{
				Role:       "tool",
				Content:    output,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			}
			history = append(history, toolResultMsg)
			if err := c.store.Append(toolResultMsg); err != nil {
				slog.Warn("commander: failed to save tool result", "error", err)
			}
		}
		// Loop back: the LLM may now generate a final response with the
		// tool results in context, or call more tools.
	}

	c.emitError(emit, fmt.Sprintf("turn exceeded %d iterations (possible runaway tool loop)", maxTurnIterations))
	return fmt.Errorf("turn iteration limit reached")
}

// executeTool dispatches a tool call to the registry. Unknown tool
// names return an error string so the LLM can recover on its next turn.
func (c *Commander) executeTool(ctx context.Context, name string, args map[string]any) (string, error) {
	tool := c.tools.Get(name)
	if tool == nil {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return tool.Execute(ctx, args)
}

// parseToolArgs best-effort parses the LLM's JSON argument string into
// a map. Returns an empty map on malformed JSON — the tool will get an
// empty args and either error or work with defaults.
func parseToolArgs(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return map[string]any{}
	}
	return args
}

// Store returns the underlying conversation store. Exposed for history
// and clear endpoints.
func (c *Commander) Store() *Store {
	return c.store
}

// emitError writes a terminal error event. Uses the method receiver so
// future subclasses or decorators can override.
func (c *Commander) emitError(emit Emitter, msg string) {
	_ = emit.WriteEvent(errorEvent{Type: EventError, Error: msg})
}
