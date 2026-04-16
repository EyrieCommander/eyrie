package commander

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Audacity88/eyrie/internal/embedded"
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

// Registry holds the tools available to the commander. The registry is
// built once at Commander construction time and is read-only thereafter.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds a registry with the skeleton's initial tools.
// Additional tools will be registered here as the commander grows.
func NewRegistry(projectStore *project.Store) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	r.register(listProjectsTool(projectStore))
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

// --- Individual tool implementations ---

// projectSummary is the shape we expose to the LLM — trimmed to the fields
// a human-readable summary actually needs. Avoids flooding the LLM with
// internal-only fields.
type projectSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Goal   string `json:"goal,omitempty"`
	Status string `json:"status"`
}

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
			data, err := json.Marshal(summaries)
			if err != nil {
				return "", fmt.Errorf("marshaling project summaries: %w", err)
			}
			return string(data), nil
		},
	}
}
