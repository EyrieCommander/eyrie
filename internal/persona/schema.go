package persona

import "fmt"

// Persona defines a named agent personality with a role, system prompt,
// preferred model, and optional framework affinity.
type Persona struct {
	// Identity
	ID          string `json:"id"`
	Name        string `json:"name"`
	Role        string `json:"role"`        // "life-coach", "ceo", "researcher", etc.
	Description string `json:"description"` // Short human-readable summary
	Icon        string `json:"icon"`        // Emoji or icon identifier
	Category    string `json:"category"`    // "personal", "business", "creative", "technical"

	// LLM preferences
	PreferredModel string  `json:"preferred_model"`          // e.g. "claude-opus-4-6"
	Temperature    float64 `json:"temperature,omitempty"`    // 0.0-1.0
	MaxTokens      int     `json:"max_tokens,omitempty"`     // Max response length
	ReasoningLevel string  `json:"reasoning_level,omitempty"` // "low", "medium", "high"

	// Behavior
	SystemPrompt string   `json:"system_prompt"` // The core personality definition
	Tools        []string `json:"tools"`         // Enabled tool names
	Traits       []string `json:"traits"`        // Personality traits for display

	// Framework affinity
	PreferredFramework string `json:"preferred_framework,omitempty"` // "zeroclaw", "openclaw", or "" for any

	// Identity templates: workspace files rendered when instantiating this persona.
	// Keys are filenames (IDENTITY.md, SOUL.md, etc.), values are Go text/template strings.
	IdentityTemplate map[string]string `json:"identity_template,omitempty"`

	// Seed memories written to MEMORY.md on first instantiation
	MemorySeeds []string `json:"memory_seeds,omitempty"`

	// Hierarchy role this persona is designed for ("coordinator", "orchestrator", "implementer", or "")
	HierarchyRole string `json:"hierarchy_role,omitempty"`

	// Status (set at runtime, not in registry)
	Installed   bool   `json:"installed,omitempty"`
	AgentName   string `json:"agent_name,omitempty"`   // Name of the running agent instance
	AgentAlive  bool   `json:"agent_alive,omitempty"`  // Whether the agent is currently running
}

// Validate checks that the Persona's numeric fields are within acceptable ranges.
func (p Persona) Validate() error {
	if p.Temperature != 0 && (p.Temperature < 0.0 || p.Temperature > 1.0) {
		return fmt.Errorf("temperature must be between 0.0 and 1.0, got %g", p.Temperature)
	}
	if p.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be >= 0, got %d", p.MaxTokens)
	}
	return nil
}

// categories holds the immutable category definitions.
var categories = []CategoryInfo{
	{ID: "personal", Name: "personal", Description: "agents that help with your daily life"},
	{ID: "business", Name: "business", Description: "agents that help run your work"},
	{ID: "creative", Name: "creative", Description: "agents for creative and artistic work"},
	{ID: "technical", Name: "technical", Description: "agents for engineering and research"},
}

// Categories returns a copy of the category definitions so callers cannot mutate package state.
func Categories() []CategoryInfo {
	out := make([]CategoryInfo, len(categories))
	copy(out, categories)
	return out
}

type CategoryInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PersonaRegistry holds the full catalog of available personas.
type PersonaRegistry struct {
	Version  string    `json:"version"`
	Personas []Persona `json:"personas"`
}
