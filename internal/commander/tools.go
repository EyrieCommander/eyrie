package commander

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/embedded"
	"github.com/Audacity88/eyrie/internal/persona"
	"github.com/Audacity88/eyrie/internal/project"
)

// Risk classifies a tool's blast radius. The turn loop gates execution
// based on this: Auto tools run immediately; Confirm tools pause for
// out-of-band user approval before executing.
//
// WHY a typed enum, not a bool: leaves room for a future Dangerous tier
// (e.g. "confirm + require typing target id") without another round of
// API changes. For MVP only Auto and Confirm are implemented.
type Risk int

const (
	// RiskAuto executes immediately. Use for read-only and trivially
	// reversible tools. Every Auto tool call still goes in the audit log.
	RiskAuto Risk = iota
	// RiskConfirm requires out-of-band user approval before execution.
	// The turn emits a confirm_required event, stores a pending action,
	// and ends the turn with an unresolved tool_call. Execution happens
	// only when /api/commander/confirm/{id} is POSTed with approved=true.
	RiskConfirm
)

// Tool is an action the commander can invoke. Each tool is a plain Go
// function — no HTTP, no subprocess, no sandbox. The tool executes
// directly against Eyrie's in-process stores.
//
// WHY no subprocess: The commander orchestrates; it does not do workspace
// work. Captains and talons are the ones that need sandboxing. Direct
// function calls eliminate a whole class of failures (serialization,
// streaming parser mismatches, subprocess lifecycle).
type Tool struct {
	Name        string
	Description string
	// Risk controls whether the tool requires user confirmation.
	// Zero value (RiskAuto) is safe for read-only tools.
	Risk Risk
	// Parameters is a JSON Schema describing the tool's arguments.
	// Passed to the LLM as the tool's `function.parameters` field.
	Parameters map[string]any
	// Execute runs the tool. `args` is the parsed JSON object the LLM
	// provided. Returns a string the LLM will see as the tool result.
	Execute func(ctx context.Context, args map[string]any) (string, error)
	// Summarize produces a one-line human-readable description of what
	// this tool call would do with these args. Used in confirm_required
	// events so the user sees a clear summary before approving. Optional
	// — defaults to "<tool_name>(<args>)".
	Summarize func(args map[string]any) string
}

// Definition converts the Tool to the OpenAI-format ToolDef expected by
// the LLM provider.
func (t Tool) Definition() embedded.ToolDef {
	return embedded.ToolDef{
		Type: "function",
		Function: embedded.ToolFunction{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		},
	}
}

// RegistryDeps bundles the stores and callbacks the built-in tools need.
// Passed to NewRegistry so tool implementations can close over exactly
// what they need without the registry package importing server internals.
//
// WHY function fields (not pointers to server types): discovery, message
// injection, and agent lifecycle live in the server package. Passing
// functions lets the server supply method values without the commander
// package importing server — avoids an import cycle.
type RegistryDeps struct {
	// Projects is the project store for read + write tools.
	Projects *project.Store
	// Chat is the project chat store, used by read_project_chat.
	Chat *project.ChatStore
	// Discovery runs agent discovery on demand; used by list_agents.
	Discovery func(ctx context.Context) discovery.Result
	// SendToProject injects a commander message into a project's chat
	// and kicks off the project orchestrator in the background (captain
	// responds asynchronously). Returns an error only if the injection
	// itself fails (project not found, etc.).
	SendToProject func(ctx context.Context, projectID, message string) error
	// RestartAgent stops then starts an agent by name. Best-effort;
	// returns an error if either step fails.
	RestartAgent func(ctx context.Context, name string) error
	// Memory is the commander's persistent key-value note store, exposed
	// through the remember/recall/forget tools.
	Memory *MemoryStore
}

// Registry holds the tools available to the commander. The registry is
// built once at Commander construction time and is read-only thereafter.
type Registry struct {
	tools map[string]Tool
	defs  []embedded.ToolDef // cached; built once after all tools registered
}

// NewRegistry builds a registry populated with the built-in tool set.
// Additional tools are registered here as the commander grows.
func NewRegistry(deps RegistryDeps) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	// Read-only (Auto risk) — guard on deps so a nil pointer doesn't
	// cause a panic when the LLM invokes the tool. Missing deps just
	// mean the tool isn't offered; the LLM picks from what's available.
	if deps.Projects != nil {
		r.register(listProjectsTool(deps.Projects))
		r.register(getProjectTool(deps.Projects))
	}
	r.register(listPersonasTool())
	if deps.Discovery != nil {
		r.register(listAgentsTool(deps.Discovery))
	}
	if deps.Chat != nil {
		r.register(readProjectChatTool(deps.Chat))
	}
	// Write (Confirm risk)
	if deps.Projects != nil {
		r.register(createProjectTool(deps.Projects))
	}
	if deps.SendToProject != nil {
		r.register(sendToProjectTool(deps.SendToProject, deps.Projects))
	}
	if deps.RestartAgent != nil {
		r.register(restartAgentTool(deps.RestartAgent))
	}
	// Memory tools — Auto risk. These update the commander's own notes,
	// not user data of record, so they don't require out-of-band approval.
	if deps.Memory != nil {
		r.register(rememberTool(deps.Memory))
		r.register(recallTool(deps.Memory))
		r.register(forgetTool(deps.Memory))
	}
	r.buildDefs()
	return r
}

func (r *Registry) register(t Tool) {
	r.tools[t.Name] = t
}

// buildDefs caches the tool definitions slice. Called once after all
// tools are registered. The registry is immutable after construction
// so this never needs invalidation.
func (r *Registry) buildDefs() {
	r.defs = make([]embedded.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		r.defs = append(r.defs, t.Definition())
	}
}

// Get returns the tool with the given name, or nil if not registered.
func (r *Registry) Get(name string) *Tool {
	t, ok := r.tools[name]
	if !ok {
		return nil
	}
	return &t
}

// Definitions returns all registered tools as LLM-ready ToolDefs.
// Returns the cached slice built at construction time.
func (r *Registry) Definitions() []embedded.ToolDef {
	return r.defs
}

// --- Shared shapes ----------------------------------------------------
// Trimmed projections of internal types so the LLM sees only the fields
// it needs. Keeps tool results compact and avoids leaking internals.

type projectSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Goal   string `json:"goal,omitempty"`
	Status string `json:"status"`
}

type projectDetail struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	Goal           string   `json:"goal,omitempty"`
	Status         string   `json:"status"`
	OrchestratorID string   `json:"captain_id,omitempty"`
	RoleAgentIDs   []string `json:"talon_ids,omitempty"`
	Progress       int      `json:"progress"`
}

type personaSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	HierarchyRole string `json:"hierarchy_role,omitempty"`
	Installed     bool   `json:"installed,omitempty"`
}

type agentSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Framework   string `json:"framework"`
	Port        int    `json:"port,omitempty"`
	Alive       bool   `json:"alive"`
	InstanceID  string `json:"instance_id,omitempty"`
}

type chatMessageSummary struct {
	Role    string `json:"role"`
	Sender  string `json:"sender,omitempty"`
	Content string `json:"content"`
	Time    string `json:"time,omitempty"`
}

// --- Tool implementations ---------------------------------------------

func listProjectsTool(store *project.Store) Tool {
	return Tool{
		Name:        "list_projects",
		Description: "List all projects in the Eyrie workspace. Returns a JSON array of {id, name, goal, status} for each project. Use this to answer 'what are my projects' or 'what am I working on'.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			projects, err := store.List()
			if err != nil {
				return "", fmt.Errorf("listing projects: %w", err)
			}
			summaries := make([]projectSummary, 0, len(projects))
			for _, p := range projects {
				summaries = append(summaries, projectSummary{
					ID:     p.ID,
					Name:   p.Name,
					Goal:   p.Goal,
					Status: string(p.Status),
				})
			}
			return marshalJSON(summaries)
		},
	}
}

func getProjectTool(store *project.Store) Tool {
	return Tool{
		Name:        "get_project",
		Description: "Fetch full details for a single project by id. Returns {id, name, description, goal, status, captain_id, talon_ids, progress}. Use when the user asks about a specific project by name — look up the id via list_projects first if needed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The project's id (UUID or short slug from list_projects).",
				},
			},
			"required": []string{"id"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			id, _ := args["id"].(string)
			if id == "" {
				return "", fmt.Errorf("id is required")
			}
			p, err := store.Get(id)
			if err != nil {
				if errors.Is(err, project.ErrNotFound) {
					return "", fmt.Errorf("no project with id %q", id)
				}
				return "", fmt.Errorf("fetching project: %w", err)
			}
			detail := projectDetail{
				ID:             p.ID,
				Name:           p.Name,
				Description:    p.Description,
				Goal:           p.Goal,
				Status:         string(p.Status),
				OrchestratorID: p.OrchestratorID,
				RoleAgentIDs:   p.RoleAgentIDs,
				Progress:       p.Progress,
			}
			return marshalJSON(detail)
		},
	}
}

func listPersonasTool() Tool {
	return Tool{
		Name:        "list_personas",
		Description: "List all available personas from the catalog. Personas are templates for captains and talons (a mix of name, description, preferred model, and hierarchy role). Use when the user asks 'what kinds of captains can I assign' or wants to pick a persona for a new agent.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			client, err := persona.NewCatalogClient("")
			if err != nil {
				return "", fmt.Errorf("creating persona catalog client: %w", err)
			}
			reg, err := client.Fetch(ctx)
			if err != nil {
				return "", fmt.Errorf("fetching persona catalog: %w", err)
			}
			// Merge installed status from the local store so the LLM
			// knows which personas are already on disk.
			installedIDs := map[string]bool{}
			if store, err := persona.NewStore(); err == nil {
				if installed, err := store.List(); err == nil {
					for _, p := range installed {
						installedIDs[p.ID] = true
					}
				}
			}
			summaries := make([]personaSummary, 0, len(reg.Personas))
			for _, p := range reg.Personas {
				summaries = append(summaries, personaSummary{
					ID:            p.ID,
					Name:          p.Name,
					Description:   p.Description,
					HierarchyRole: p.HierarchyRole,
					Installed:     installedIDs[p.ID],
				})
			}
			return marshalJSON(summaries)
		},
	}
}

func listAgentsTool(discover func(ctx context.Context) discovery.Result) Tool {
	return Tool{
		Name:        "list_agents",
		Description: "List all discovered agents (frameworks and provisioned instances), with their running/stopped status. Returns a JSON array of {name, display_name, framework, port, alive, instance_id}. Use to answer 'what agents are running' or before any agent-targeted action.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			result := discover(ctx)
			summaries := make([]agentSummary, 0, len(result.Agents))
			for _, ar := range result.Agents {
				summaries = append(summaries, agentSummary{
					Name:        ar.Agent.Name,
					DisplayName: ar.Agent.DisplayName,
					Framework:   ar.Agent.Framework,
					Port:        ar.Agent.Port,
					Alive:       ar.Alive,
					InstanceID:  ar.Agent.InstanceID,
				})
			}
			return marshalJSON(summaries)
		},
	}
}

func readProjectChatTool(store *project.ChatStore) Tool {
	return Tool{
		Name:        "read_project_chat",
		Description: "Read the recent chat history of a specific project. Returns a JSON array of {role, sender, content, time} messages in order. Use to answer 'what's happening in project X' or to catch up before sending a message into a project.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{
					"type":        "string",
					"description": "The project's id.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of recent messages to return. Defaults to 20.",
				},
			},
			"required": []string{"project_id"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			projectID, _ := args["project_id"].(string)
			if projectID == "" {
				return "", fmt.Errorf("project_id is required")
			}
			limit := 20
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			messages, err := store.Messages(projectID, limit)
			if err != nil {
				return "", fmt.Errorf("reading chat: %w", err)
			}
			summaries := make([]chatMessageSummary, 0, len(messages))
			for _, m := range messages {
				summaries = append(summaries, chatMessageSummary{
					Role:    m.Role,
					Sender:  m.Sender,
					Content: m.Content,
					Time:    m.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
				})
			}
			return marshalJSON(summaries)
		},
	}
}

// marshalJSON is a small helper to reduce boilerplate in Execute bodies.
func marshalJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling tool result: %w", err)
	}
	return string(data), nil
}

// --- Write tools ------------------------------------------------------

// Validation limits for write tools. Reject inputs over these sizes at
// the Go layer so a prompt injection can't use tools to flood storage.
const (
	maxProjectNameLen  = 200
	maxProjectGoalLen  = 500
	maxProjectDescLen  = 2000
	maxSendMessageLen  = 10000
	maxAgentNameLen    = 200
)

func createProjectTool(store *project.Store) Tool {
	return Tool{
		Name:        "create_project",
		Risk:        RiskConfirm,
		Description: "Create a new project with the given name, optional goal, and optional description. Returns the new project's id.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Short project name (required).",
				},
				"goal": map[string]any{
					"type":        "string",
					"description": "Optional project goal (one sentence).",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Optional longer description.",
				},
			},
			"required": []string{"name"},
		},
		Summarize: func(args map[string]any) string {
			name, _ := args["name"].(string)
			goal, _ := args["goal"].(string)
			if goal != "" {
				return fmt.Sprintf("Create project %q (goal: %s)", name, goal)
			}
			return fmt.Sprintf("Create project %q", name)
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			name, _ := args["name"].(string)
			goal, _ := args["goal"].(string)
			description, _ := args["description"].(string)
			if err := validateString("name", name, 1, maxProjectNameLen); err != nil {
				return "", err
			}
			if err := validateString("goal", goal, 0, maxProjectGoalLen); err != nil {
				return "", err
			}
			if err := validateString("description", description, 0, maxProjectDescLen); err != nil {
				return "", err
			}
			saved, err := store.Create(project.CreateRequest{
				Name:        name,
				Goal:        goal,
				Description: description,
				CreatedBy:   "commander",
			})
			if err != nil {
				return "", fmt.Errorf("creating project: %w", err)
			}
			return fmt.Sprintf(`{"id":%q,"name":%q,"status":"active"}`, saved.ID, saved.Name), nil
		},
	}
}

func sendToProjectTool(send func(ctx context.Context, projectID, message string) error, store *project.Store) Tool {
	return Tool{
		Name:        "send_to_project",
		Risk:        RiskConfirm,
		Description: "Send a message into a project's chat as the commander. The project's captain will see the message and may respond. The captain's response can be read later via read_project_chat.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{
					"type":        "string",
					"description": "The project's id.",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "The message to send. Will appear in the project chat with role=commander.",
				},
			},
			"required": []string{"project_id", "message"},
		},
		Summarize: func(args map[string]any) string {
			projectID, _ := args["project_id"].(string)
			message, _ := args["message"].(string)
			name := projectID
			if p, err := store.Get(projectID); err == nil {
				name = p.Name
			}
			preview := message
			if len(preview) > 80 {
				preview = preview[:80] + "…"
			}
			return fmt.Sprintf("Send to %s: %q", name, preview)
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			projectID, _ := args["project_id"].(string)
			message, _ := args["message"].(string)
			if err := validateString("project_id", projectID, 1, 200); err != nil {
				return "", err
			}
			if err := validateString("message", message, 1, maxSendMessageLen); err != nil {
				return "", err
			}
			if err := send(ctx, projectID, message); err != nil {
				return "", fmt.Errorf("sending message: %w", err)
			}
			return `{"status":"sent","note":"Captain will respond asynchronously. Use read_project_chat to see the reply once it arrives."}`, nil
		},
	}
}

func restartAgentTool(restart func(ctx context.Context, name string) error) Tool {
	return Tool{
		Name:        "restart_agent",
		Risk:        RiskConfirm,
		Description: "Stop and restart an agent by name. Disrupts any in-flight work on that agent. Returns confirmation when the restart completes.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Agent name (from list_agents).",
				},
			},
			"required": []string{"name"},
		},
		Summarize: func(args map[string]any) string {
			name, _ := args["name"].(string)
			return fmt.Sprintf("Restart agent %q", name)
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			name, _ := args["name"].(string)
			if err := validateString("name", name, 1, maxAgentNameLen); err != nil {
				return "", err
			}
			if err := restart(ctx, name); err != nil {
				return "", fmt.Errorf("restarting agent: %w", err)
			}
			return fmt.Sprintf(`{"status":"restarted","name":%q}`, name), nil
		},
	}
}

// --- Memory tools -----------------------------------------------------

const (
	maxMemoryKeyLen   = 200
	maxMemoryValueLen = 4000
)

func rememberTool(mem *MemoryStore) Tool {
	return Tool{
		Name:        "remember",
		Description: "Store or update a note in the commander's long-term memory. Use for user preferences, project context, or anything that should survive across conversations. Keys are normalized (lowercase, trimmed); re-using a key overwrites the previous value. Prefer stable, descriptive keys (e.g. 'user-prefers-evening-syncs', 'project-x-stakeholder').",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "Short descriptive key. Normalized to lowercase.",
				},
				"value": map[string]any{
					"type":        "string",
					"description": "The note contents. Free-form text.",
				},
			},
			"required": []string{"key", "value"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			key, _ := args["key"].(string)
			value, _ := args["value"].(string)
			if err := validateString("key", key, 1, maxMemoryKeyLen); err != nil {
				return "", err
			}
			if err := validateString("value", value, 1, maxMemoryValueLen); err != nil {
				return "", err
			}
			entry, err := mem.Remember(key, value)
			if err != nil {
				return "", fmt.Errorf("storing memory: %w", err)
			}
			return marshalJSON(entry)
		},
	}
}

func recallTool(mem *MemoryStore) Tool {
	return Tool{
		Name:        "recall",
		Description: "Look up a stored memory entry by key, or list all entries when no key is given. The LLM sees a compact snapshot of all memories in its system prompt every turn, so prefer recalling a specific key only when you need full detail or timestamps.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "Optional key to look up. If omitted, returns all stored entries.",
				},
			},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			key, _ := args["key"].(string)
			if key == "" {
				return marshalJSON(mem.List())
			}
			entry, err := mem.Recall(key)
			if err != nil {
				if errors.Is(err, ErrMemoryNotFound) {
					return "", fmt.Errorf("no memory stored for key %q", NormalizeKey(key))
				}
				return "", fmt.Errorf("recalling memory: %w", err)
			}
			return marshalJSON(entry)
		},
	}
}

func forgetTool(mem *MemoryStore) Tool {
	return Tool{
		Name:        "forget",
		Description: "Delete a stored memory entry by key. Use when the user asks you to forget something or when a note is no longer accurate.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "Key to forget (normalized to lowercase).",
				},
			},
			"required": []string{"key"},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			key, _ := args["key"].(string)
			if err := validateString("key", key, 1, maxMemoryKeyLen); err != nil {
				return "", err
			}
			if err := mem.Forget(key); err != nil {
				if errors.Is(err, ErrMemoryNotFound) {
					return "", fmt.Errorf("no memory stored for key %q", NormalizeKey(key))
				}
				return "", fmt.Errorf("forgetting memory: %w", err)
			}
			return fmt.Sprintf(`{"status":"forgotten","key":%q}`, NormalizeKey(key)), nil
		},
	}
}

// validateString rejects inputs that are missing (when minLen>0) or
// exceed maxLen. Centralizes the boilerplate for write-tool arg checks.
func validateString(field, value string, minLen, maxLen int) error {
	if len(value) < minLen {
		if minLen == 1 {
			return fmt.Errorf("%s is required", field)
		}
		return fmt.Errorf("%s must be at least %d characters", field, minLen)
	}
	if len(value) > maxLen {
		return fmt.Errorf("%s exceeds max length of %d characters", field, maxLen)
	}
	return nil
}
