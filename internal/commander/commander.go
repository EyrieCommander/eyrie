package commander

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/embedded"
	"github.com/Audacity88/eyrie/internal/project"
)

// maxTurnIterations bounds the tool-calling loop. Without this, a buggy
// tool or a looping LLM could run indefinitely. Ten iterations is
// generous for legitimate work (each iteration is one LLM call + any
// number of parallel tool executions within it).
const maxTurnIterations = 10

// maxResponseTokens caps each LLM response. OpenRouter (and Anthropic)
// reserve credit up front equal to input+max_tokens regardless of what
// the response actually uses, so leaving this at the model's 64k
// ceiling reserves ~$0.78 per call on Sonnet 4.6 and blocks small
// balances with 402s. Commander replies are short (state queries +
// tool calls + a sentence or two of summary) so 4k is plenty.
const maxResponseTokens = 4096

// baseSystemPrompt shapes the commander's personality and role. The
// effective system prompt is built per turn via buildSystemPrompt so
// the current memory snapshot can be injected. Kept minimal in the
// skeleton — as the tool set grows we'll give it more guidance.
const baseSystemPrompt = `You are Eyrie, a commander that helps the user manage AI agent projects.

You can see the user's projects, captains, and talons. When asked about them, call the appropriate tool to read live data rather than guessing.

When the user asks you to take an action that requires a tool, call the tool directly — do NOT ask the user to confirm in chat first. The Eyrie system handles user approval for sensitive tools automatically; your job is just to decide what tool to call and with what arguments. After calling a tool, react to its result.

You have a persistent memory (the remember/recall/forget tools) that survives across conversations. Actively use it: store user preferences, stakeholders, deadlines, and anything that would help on a later conversation. A snapshot of your current memory appears below — consult it before asking the user to re-state something you should already know.

Be concise. Summarize tool results rather than dumping raw JSON at the user.`

// buildSystemPrompt composes the base prompt with the current memory
// snapshot. If memory is empty the base prompt is returned unchanged so
// we don't advertise an empty section. The snapshot is plain-text so the
// LLM can quote and reason about it without having to parse JSON.
//
// WHY inject every turn (not rely on the recall tool): the commander's
// memories are small in the MVP (expected tens of entries) and recalling
// them costs a whole extra round trip of LLM + tool + LLM. Inlining them
// into the system message is cheaper and, more importantly, makes the
// LLM *aware* that memories exist — without the inline snapshot an LLM
// rarely calls `recall` unprompted.
func buildSystemPrompt(memories []MemoryEntry) string {
	if len(memories) == 0 {
		return baseSystemPrompt
	}
	var sb strings.Builder
	sb.WriteString(baseSystemPrompt)
	sb.WriteString("\n\nStored memories:\n")
	for _, e := range memories {
		sb.WriteString("- ")
		sb.WriteString(e.Key)
		sb.WriteString(": ")
		sb.WriteString(e.Value)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Commander is the orchestrator: it holds the LLM provider, the
// conversation store, and the tool registry. One Commander instance
// serves the whole process — there is a single persistent conversation
// with the user.
type Commander struct {
	provider      embedded.LLMProvider
	model         string
	contextWindow int // model's max context in tokens
	store         *Store
	tools         *Registry
	pending       *PendingStore
	audit         *AuditLog
	memory        *MemoryStore
}

// Config configures a new Commander.
type Config struct {
	Provider      embedded.LLMProvider
	Model         string
	ContextWindow int        // model's max context in tokens; 0 omits from done events
	Store         *Store
	Tools         *Registry
	Pending       *PendingStore // optional; defaults to a fresh in-memory store
	Audit         *AuditLog     // optional; nil disables audit logging
	Memory        *MemoryStore  // optional; nil omits memory injection + tools
}

// New constructs a Commander. Callers supply all dependencies so this
// package has no knowledge of how the provider is obtained (env var,
// config file, vault) — that's the server's responsibility.
func New(cfg Config) *Commander {
	if cfg.Pending == nil {
		cfg.Pending = NewPendingStore()
	}
	return &Commander{
		provider:      cfg.Provider,
		model:         cfg.Model,
		contextWindow: cfg.ContextWindow,
		store:         cfg.Store,
		tools:         cfg.Tools,
		pending:       cfg.Pending,
		audit:         cfg.Audit,
		memory:        cfg.Memory,
	}
}

// Pending returns the pending-action store. Exposed so the server's
// confirm endpoint can look up and resolve pending actions.
func (c *Commander) Pending() *PendingStore {
	return c.pending
}

// Memory returns the memory store, or nil if memory is disabled.
// Exposed for the HTTP read endpoint.
func (c *Commander) Memory() *MemoryStore {
	return c.memory
}

// currentSystemPrompt returns the system prompt for this turn with the
// current memory snapshot baked in. Falls back to the base prompt when
// memory is disabled.
func (c *Commander) currentSystemPrompt() string {
	if c.memory == nil {
		return baseSystemPrompt
	}
	return buildSystemPrompt(c.memory.List())
}

// DefaultConfig bundles everything NewDefault needs from the caller. The
// server populates this with its cached stores and method values for
// server-side callbacks (discovery, project message injection, agent
// restart).
type DefaultConfig struct {
	Projects      *project.Store
	Chat          *project.ChatStore
	Discovery     func(ctx context.Context) discovery.Result
	SendToProject func(ctx context.Context, projectID, message string) error
	RestartAgent  func(ctx context.Context, name string) error
	// Vault is the key vault to read API keys from. When nil,
	// selectProvider falls back to config.GetKeyVault().
	Vault         *config.KeyVault
	// Review task callbacks (function fields to avoid import cycles).
	ListReviewTasks     func(projectID string) ([]map[string]any, error)
	GetReviewTask       func(taskID string) (map[string]any, error)
	CreateReviewTask    func(projectID, domain, kind, repo string, targetNumber int) (map[string]any, error)
	RunReviewTask       func(ctx context.Context, taskID string) (map[string]any, error)
	ListReviewArtifacts func(taskID string) ([]map[string]any, error)
}

// NewDefault builds a Commander with the skeleton defaults: OpenRouter
// as the LLM provider, a Claude model with tool-calling support, the
// standard conversation store, and the full built-in tool registry.
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
func NewDefault(deps DefaultConfig) (*Commander, error) {
	store, err := NewStore()
	if err != nil {
		return nil, fmt.Errorf("commander store: %w", err)
	}
	pc, err := selectProvider(deps.Vault)
	if err != nil {
		return nil, err
	}
	audit, err := NewAuditLog()
	if err != nil {
		// Non-fatal: the commander still works without an audit log,
		// just loses the observability benefit.
		slog.Warn("commander: audit log unavailable", "error", err)
		audit = nil
	}
	memory, err := NewMemoryStore()
	if err != nil {
		// Non-fatal: the commander works without memory, it just can't
		// persist notes across conversations. Surface the reason.
		slog.Warn("commander: memory store unavailable", "error", err)
		memory = nil
	}
	return New(Config{
		Provider:      pc.provider,
		Model:         pc.model,
		ContextWindow: pc.contextWindow,
		Store:         store,
		Pending:  NewPendingStore(),
		Audit:    audit,
		Memory:   memory,
		Tools: NewRegistry(RegistryDeps{
			Projects:            deps.Projects,
			Chat:                deps.Chat,
			Discovery:           deps.Discovery,
			SendToProject:       deps.SendToProject,
			RestartAgent:        deps.RestartAgent,
			Memory:              memory,
			ListReviewTasks:     deps.ListReviewTasks,
			GetReviewTask:       deps.GetReviewTask,
			CreateReviewTask:    deps.CreateReviewTask,
			RunReviewTask:       deps.RunReviewTask,
			ListReviewArtifacts: deps.ListReviewArtifacts,
		}),
	}), nil
}

// selectProvider chooses between OpenRouter (default) and Anthropic
// native based on the EYRIE_COMMANDER_PROVIDER env var. Returns the
// provider, the default model for that provider, and any error.
//
// WHY env var rather than config file: the commander's config story is
// still evolving (model picker in UI is Phase 5b). An env var is a
// stable, easy-to-flip knob that the UI can eventually replace. Default
// preserves existing behavior so upgrading the branch doesn't break
// anyone.
//
// EYRIE_COMMANDER_PROVIDER values:
//   - "" or "openrouter" — OpenAI-compat provider against OpenRouter
//   - "anthropic" — Anthropic native /v1/messages provider
// providerChoice bundles the results of selectProvider.
type providerChoice struct {
	provider      embedded.LLMProvider
	model         string
	contextWindow int
}

func selectProvider(vault *config.KeyVault) (providerChoice, error) {
	choice := strings.ToLower(strings.TrimSpace(os.Getenv("EYRIE_COMMANDER_PROVIDER")))
	if vault == nil {
		vault = config.GetKeyVault()
	}

	switch choice {
	case "anthropic":
		key := vault.Get("anthropic")
		if key == "" {
			return providerChoice{}, fmt.Errorf("EYRIE_COMMANDER_PROVIDER=anthropic but no anthropic key in vault — add one via Settings or ANTHROPIC_API_KEY env var")
		}
		slog.Info("commander: using Anthropic native provider")
		// Sonnet 4.6 has a 200k context window.
		return providerChoice{embedded.NewAnthropicProvider(key, ""), "claude-sonnet-4-6", 200_000}, nil

	case "", "openrouter":
		key := vault.Get("openrouter")
		if key == "" {
			return providerChoice{}, fmt.Errorf("no OpenRouter API key in vault — set one via Settings or OPENROUTER_API_KEY env var")
		}
		slog.Info("commander: using OpenRouter provider")
		return providerChoice{embedded.NewOpenAICompatProvider(key, "https://openrouter.ai/api/v1"), "anthropic/claude-sonnet-4.6", 200_000}, nil

	default:
		return providerChoice{}, fmt.Errorf("unknown EYRIE_COMMANDER_PROVIDER=%q (expected: openrouter, anthropic)", choice)
	}
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

// loadHistory reads the full conversation from the store and prepends
// the current system prompt (with memory snapshot). DRYs the three call
// sites that need a fresh history slice.
func (c *Commander) loadHistory() ([]embedded.Message, error) {
	stored, err := c.store.All()
	if err != nil {
		return nil, err
	}
	history := make([]embedded.Message, 0, len(stored)+2)
	history = append(history, embedded.Message{Role: "system", Content: c.currentSystemPrompt()})
	history = append(history, stored...)
	return history, nil
}

// RunTurn handles one user message: loads history, appends the user
// message, calls the LLM + tools in a bounded loop, and streams events
// to the emitter. Returns nil on success; on error, an error event
// has already been emitted via the emitter.
func (c *Commander) RunTurn(ctx context.Context, userMessage string, emit Emitter) error {
	history, err := c.loadHistory()
	if err != nil {
		c.emitError(emit, fmt.Sprintf("loading conversation: %v", err))
		return err
	}
	userMsg := embedded.Message{Role: "user", Content: userMessage}
	history = append(history, userMsg)
	if err := c.store.Append(userMsg); err != nil {
		c.emitError(emit, fmt.Sprintf("saving user message: %v", err))
		return err
	}

	toolDefs := c.tools.Definitions()
	var inputTokens, outputTokens int
	// contextTokens tracks the last LLM call's input size — i.e. how
	// much of the context window the full conversation currently uses.
	// Differs from inputTokens (sum across all calls = cost).
	var contextTokens int

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
		resp, err := c.provider.ChatStream(ctx, history, toolDefs, c.model, map[string]any{"max_tokens": maxResponseTokens}, onDelta)
		if err != nil {
			c.emitError(emit, fmt.Sprintf("LLM call failed: %v", err))
			return err
		}
		inputTokens += resp.InputTokens
		outputTokens += resp.OutputTokens
		contextTokens = resp.InputTokens

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
			_ = emit.WriteEvent(c.makeDoneEvent(inputTokens, outputTokens, contextTokens))
			return nil
		}

		// Process tool calls serially via processToolCalls. It pauses at
		// the first Confirm tool and leaves later tool_calls unresolved
		// in the assistant message — the resume path picks them up
		// after confirmation. See processToolCalls's doc for why.
		if c.processToolCalls(ctx, resp.ToolCalls, &history, emit) {
			_ = emit.WriteEvent(c.makeDoneEvent(inputTokens, outputTokens, contextTokens))
			return nil
		}
		// Loop back: the LLM may now generate a final response with the
		// tool results in context, or call more tools.
	}

	c.emitError(emit, fmt.Sprintf("turn exceeded %d iterations (possible runaway tool loop)", maxTurnIterations))
	return fmt.Errorf("turn iteration limit reached")
}

// ResumeAfterConfirm processes the result of a user approval or denial
// and either (a) continues processing remaining unresolved tool_calls
// from the same assistant message, or (b) runs the LLM continuation
// once all tool_calls in that batch are resolved. Called by the
// confirm endpoint in the server.
//
// The flow:
// - If approved: execute the tool, append the result (or error) to
//   history as the tool_result for the unresolved tool_call.
// - If denied: append a synthetic tool_result describing the denial.
// - Check the parent assistant message for other tool_calls that still
//   lack tool_results (happens when the LLM batched multiple tool_calls
//   in one reply). If any remain, process them via processToolCalls —
//   auto tools run immediately, the next Confirm triggers another
//   confirm_required and pauses again.
// - Only when all tool_calls in the batch are resolved do we call
//   runContinuation so the LLM can react to the full batch of results
//   (e.g. "I've created all three projects").
//
// Both audit outcomes are logged.
func (c *Commander) ResumeAfterConfirm(ctx context.Context, pa *PendingAction, approved bool, reason string, emit Emitter) error {
	var (
		output string
		isErr  bool
	)

	if approved {
		tool := c.tools.Get(pa.Tool)
		if tool == nil {
			output = fmt.Sprintf("error: tool %q no longer registered", pa.Tool)
			isErr = true
		} else {
			out, err := tool.Execute(ctx, pa.Args)
			if err != nil {
				output = fmt.Sprintf("error: %v", err)
				isErr = true
			} else {
				output = out
			}
		}
		c.auditConfirmedExecution(pa, output, isErr, "")
		_ = emit.WriteEvent(toolResultEvent{
			Type: EventToolResult, ID: pa.ToolCallID, Name: pa.Tool, Output: output, Error: isErr,
		})
	} else {
		output = fmt.Sprintf("user denied this action")
		if reason != "" {
			output += ": " + reason
		}
		c.auditConfirmedExecution(pa, output, false, reason)
		_ = emit.WriteEvent(toolResultEvent{
			Type: EventToolResult, ID: pa.ToolCallID, Name: pa.Tool, Output: output, Error: false,
		})
	}

	// Persist the tool result so the LLM sees it on the continuation turn.
	toolResultMsg := embedded.Message{
		Role:       "tool",
		Content:    output,
		ToolCallID: pa.ToolCallID,
		Name:       pa.Tool,
	}
	if err := c.store.Append(toolResultMsg); err != nil {
		slog.Warn("commander: failed to save tool result on resume", "error", err)
	}

	// Rebuild history from the store (which now includes this tool
	// result) to see if the parent assistant message still has other
	// unresolved tool_calls. Happens when the LLM batched multiple
	// tool_calls in one reply — only one pending action was just
	// resolved; the rest need their own Auto-exec or Confirm pause.
	history, err := c.loadHistory()
	if err != nil {
		c.emitError(emit, fmt.Sprintf("loading conversation: %v", err))
		return err
	}

	if unresolved := findUnresolvedToolCalls(history); len(unresolved) > 0 {
		if c.processToolCalls(ctx, unresolved, &history, emit) {
			_ = emit.WriteEvent(c.makeDoneEvent(0, 0, 0))
			return nil
		}
	}

	// All tool_calls in the batch are resolved. Pass the already-loaded
	// history to avoid a redundant disk read.
	return c.runContinuation(ctx, history, emit)
}

// runContinuation runs the LLM loop without prepending a new user
// message. Used by ResumeAfterConfirm. Accepts pre-built history to
// avoid a redundant store.All() disk read when the caller already has
// it. Pass nil to load from disk (convenience for callers without
// pre-built history).
func (c *Commander) runContinuation(ctx context.Context, history []embedded.Message, emit Emitter) error {
	if history == nil {
		var err error
		history, err = c.loadHistory()
		if err != nil {
			c.emitError(emit, fmt.Sprintf("loading conversation: %v", err))
			return err
		}
	}

	toolDefs := c.tools.Definitions()
	var inputTokens, outputTokens, contextTokens int

	for iter := 0; iter < maxTurnIterations; iter++ {
		if err := ctx.Err(); err != nil {
			c.emitError(emit, fmt.Sprintf("context cancelled: %v", err))
			return err
		}

		onDelta := func(delta string) {
			_ = emit.WriteEvent(deltaEvent{Type: EventDelta, Text: delta})
		}
		resp, err := c.provider.ChatStream(ctx, history, toolDefs, c.model, map[string]any{"max_tokens": maxResponseTokens}, onDelta)
		if err != nil {
			c.emitError(emit, fmt.Sprintf("LLM call failed: %v", err))
			return err
		}
		inputTokens += resp.InputTokens
		outputTokens += resp.OutputTokens
		contextTokens = resp.InputTokens

		assistantMsg := embedded.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		history = append(history, assistantMsg)
		if err := c.store.Append(assistantMsg); err != nil {
			slog.Warn("commander: failed to save assistant message on continuation", "error", err)
		}

		if len(resp.ToolCalls) == 0 {
			_ = emit.WriteEvent(messageEvent{
				Type: EventMessage, Role: "assistant", Content: resp.Content,
			})
			_ = emit.WriteEvent(c.makeDoneEvent(inputTokens, outputTokens, contextTokens))
			return nil
		}

		// Process tool_calls serially; pause at the first Confirm.
		if c.processToolCalls(ctx, resp.ToolCalls, &history, emit) {
			_ = emit.WriteEvent(c.makeDoneEvent(inputTokens, outputTokens, contextTokens))
			return nil
		}
	}

	c.emitError(emit, fmt.Sprintf("continuation exceeded %d iterations", maxTurnIterations))
	return fmt.Errorf("continuation iteration limit reached")
}

// processToolCalls iterates a batch of tool_calls serially. Auto tools
// execute immediately (emitting tool_result and appending to history).
// At the first Confirm tool the helper creates a pending action, emits
// confirm_required, and returns paused=true WITHOUT processing any
// later tool_calls — they remain in the assistant message unresolved
// and are re-examined on resume by findUnresolvedToolCalls.
//
// Why serial: the OpenAI tool-call contract requires N tool_calls be
// followed by N matching tool_results before the next LLM turn. If we
// emit confirm_required for multiple tool_calls in one go, the user
// only resolves one before the LLM re-runs, and the LLM sees a
// partially-resolved group and hallucinates success for the rest.
// Processing serially — one confirm at a time — keeps the contract
// intact across arbitrary interleavings of Auto and Confirm tools.
func (c *Commander) processToolCalls(
	ctx context.Context,
	tcs []embedded.ToolCall,
	history *[]embedded.Message,
	emit Emitter,
) (paused bool) {
	for _, tc := range tcs {
		args := embedded.ParseToolArgs(tc.Function.Arguments)
		_ = emit.WriteEvent(toolCallEvent{
			Type: EventToolCall, ID: tc.ID, Name: tc.Function.Name, Args: args,
		})

		tool := c.tools.Get(tc.Function.Name)
		if tool == nil {
			output := fmt.Sprintf("error: unknown tool %q", tc.Function.Name)
			_ = emit.WriteEvent(toolResultEvent{
				Type: EventToolResult, ID: tc.ID, Name: tc.Function.Name, Output: output, Error: true,
			})
			c.appendToolResult(history, tc.ID, tc.Function.Name, output)
			continue
		}

		if tool.Risk == RiskConfirm {
			summary := summarizeTool(tool, args)
			pa := c.pending.Add(tc.Function.Name, args, summary, tc.ID)
			_ = emit.WriteEvent(confirmRequiredEvent{
				Type:    EventConfirmRequired,
				ID:      pa.ID,
				Tool:    tc.Function.Name,
				Args:    args,
				Summary: summary,
			})
			return true
		}

		// RiskAuto: execute immediately.
		output, toolErr := tool.Execute(ctx, args)
		isErr := toolErr != nil
		if isErr {
			output = fmt.Sprintf("error: %v", toolErr)
		}
		c.auditAutoExecution(tc.Function.Name, args, output, toolErr)
		_ = emit.WriteEvent(toolResultEvent{
			Type: EventToolResult, ID: tc.ID, Name: tc.Function.Name, Output: output, Error: isErr,
		})
		c.appendToolResult(history, tc.ID, tc.Function.Name, output)
	}
	return false
}

// findUnresolvedToolCalls returns the tool_calls from the most recent
// assistant message that still lack a matching tool_result in history.
// Returns nil if all are resolved or there is no such message. Used on
// resume to pick up where RunTurn/runContinuation left off when the
// assistant message batched multiple tool_calls.
func findUnresolvedToolCalls(history []embedded.Message) []embedded.ToolCall {
	lastAsst := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" && len(history[i].ToolCalls) > 0 {
			lastAsst = i
			break
		}
	}
	if lastAsst == -1 {
		return nil
	}
	resolved := make(map[string]bool)
	for i := lastAsst + 1; i < len(history); i++ {
		if history[i].Role == "tool" && history[i].ToolCallID != "" {
			resolved[history[i].ToolCallID] = true
		}
	}
	var unresolved []embedded.ToolCall
	for _, tc := range history[lastAsst].ToolCalls {
		if !resolved[tc.ID] {
			unresolved = append(unresolved, tc)
		}
	}
	return unresolved
}

// appendToolResult writes a tool-result message to both history and
// the conversation store. Called after executing an Auto tool.
func (c *Commander) appendToolResult(history *[]embedded.Message, toolCallID, toolName, output string) {
	msg := embedded.Message{
		Role:       "tool",
		Content:    output,
		ToolCallID: toolCallID,
		Name:       toolName,
	}
	*history = append(*history, msg)
	if err := c.store.Append(msg); err != nil {
		slog.Warn("commander: failed to save tool result", "error", err)
	}
}

// auditAutoExecution logs an auto-executed tool call. Low-cost — skipped
// if no audit log is configured.
func (c *Commander) auditAutoExecution(toolName string, args map[string]any, output string, execErr error) {
	if c.audit == nil {
		return
	}
	entry := AuditEntry{
		Tool: toolName, Args: args, Risk: auditRiskAuto, Decision: auditDecisionAuto, Outcome: auditOutcomeSuccess,
	}
	if execErr != nil {
		entry.Outcome = auditOutcomeError
		entry.Error = execErr.Error()
	}
	if err := c.audit.Append(entry); err != nil {
		slog.Warn("commander: audit append failed", "error", err)
	}
}

// auditConfirmedExecution logs a Confirm-tier tool call after the user
// approves or denies it.
func (c *Commander) auditConfirmedExecution(pa *PendingAction, output string, isErr bool, denialReason string) {
	if c.audit == nil {
		return
	}
	entry := AuditEntry{
		PendingID: pa.ID,
		Tool:      pa.Tool,
		Args:      pa.Args,
		Risk:      auditRiskConfirm,
		Decision:  string(pa.Status),
		Outcome:   auditOutcomeSuccess,
		Reason:    denialReason,
	}
	if isErr {
		entry.Outcome = auditOutcomeError
		entry.Error = output
	}
	if err := c.audit.Append(entry); err != nil {
		slog.Warn("commander: audit append failed", "error", err)
	}
}

// summarizeTool returns a short human-readable description of a tool
// invocation. Falls back to "<name>(<args>)" if the tool has no Summarize.
func summarizeTool(tool *Tool, args map[string]any) string {
	if tool.Summarize != nil {
		return tool.Summarize(args)
	}
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("%s(%s)", tool.Name, string(argsJSON))
}

// makeDoneEvent constructs a done event with context usage info.
// contextTokens is the last LLM call's input size (= how full the
// window is). contextWindow comes from the model's known limit.
func (c *Commander) makeDoneEvent(inputTokens, outputTokens, contextTokens int) doneEvent {
	return doneEvent{
		Type:          EventDone,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		ContextTokens: contextTokens,
		ContextWindow: c.contextWindow,
	}
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
