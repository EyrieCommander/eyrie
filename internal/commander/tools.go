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
	// Parameters is a JSON Schema describing the tool's arguments.
	// Passed to the LLM as the tool's `function.parameters` field.
	Parameters map[string]any
	// Execute runs the tool. `args` is the parsed JSON object the LLM
	// provided. Returns a string the LLM will see as the tool result.
	Execute func(ctx context.Context, args map[string]any) (string, error)
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
// WHY a discovery function (not a store): discovery runs at call time
// to get fresh liveness data. Passing a function lets the server supply
// its own runDiscovery method without creating an import cycle.
type RegistryDeps struct {
	Projects  *project.Store
	Chat      *project.ChatStore
	Discovery func(ctx context.Context) discovery.Result
}

// Registry holds the tools available to the commander. The registry is
// built once at Commander construction time and is read-only thereafter.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds a registry populated with the built-in tool set.
// Additional tools are registered here as the commander grows.
func NewRegistry(deps RegistryDeps) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	r.register(listProjectsTool(deps.Projects))
	r.register(getProjectTool(deps.Projects))
	r.register(listPersonasTool())
	r.register(listAgentsTool(deps.Discovery))
	r.register(readProjectChatTool(deps.Chat))
	return r
}

func (r *Registry) register(t Tool) {
	r.tools[t.Name] = t
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
func (r *Registry) Definitions() []embedded.ToolDef {
	defs := make([]embedded.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
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
