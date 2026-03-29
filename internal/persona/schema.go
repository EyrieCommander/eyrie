package persona

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
	"text/template/parse"
)

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

// Validate checks that the Persona's fields are valid.
func (p Persona) Validate() error {
	if p.Temperature != 0 && (p.Temperature < 0.0 || p.Temperature > 1.0) {
		return fmt.Errorf("temperature must be between 0.0 and 1.0, got %g", p.Temperature)
	}
	if p.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be >= 0, got %d", p.MaxTokens)
	}
	for filename, tmplStr := range p.IdentityTemplate {
		if err := validateTemplateEntry(filename, tmplStr); err != nil {
			return err
		}
	}
	return nil
}

// validateTemplateEntry checks that a single identity template entry has a
// safe filename and valid, non-dangerous template content.
func validateTemplateEntry(filename, tmplStr string) error {
	// Filename safety: no path separators (/ or \), no . or ..
	if filename == "" || filepath.Base(filename) != filename || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, "/\\") {
		return fmt.Errorf("invalid identity template filename %q", filename)
	}

	// Parse template syntax
	t, err := template.New(filename).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("identity template %q: %w", filename, err)
	}

	// Walk AST to reject dangerous function calls
	for _, node := range t.Root.Nodes {
		if err := walkTemplateNode(node); err != nil {
			return fmt.Errorf("identity template %q: %w", filename, err)
		}
	}
	return nil
}

// dangerousFuncs are template functions that must not appear in persona templates.
var dangerousFuncs = map[string]bool{
	"call": true, "js": true, "html": true, "urlquery": true,
}

// walkTemplateNode recursively checks template AST nodes for dangerous function calls.
func walkTemplateNode(node parse.Node) error {
	switch n := node.(type) {
	case *parse.ActionNode:
		return walkTemplatePipe(n.Pipe)
	case *parse.IfNode:
		if err := walkTemplatePipe(n.Pipe); err != nil {
			return err
		}
		if n.List != nil {
			for _, child := range n.List.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
		if n.ElseList != nil {
			for _, child := range n.ElseList.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
	case *parse.RangeNode:
		if err := walkTemplatePipe(n.Pipe); err != nil {
			return err
		}
		if n.List != nil {
			for _, child := range n.List.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
		if n.ElseList != nil {
			for _, child := range n.ElseList.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
	case *parse.WithNode:
		if err := walkTemplatePipe(n.Pipe); err != nil {
			return err
		}
		if n.List != nil {
			for _, child := range n.List.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
		if n.ElseList != nil {
			for _, child := range n.ElseList.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
	case *parse.TemplateNode:
		return fmt.Errorf("{{template}} action is not allowed")
	case *parse.BranchNode:
		if err := walkTemplatePipe(n.Pipe); err != nil {
			return err
		}
		if n.List != nil {
			for _, child := range n.List.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
		if n.ElseList != nil {
			for _, child := range n.ElseList.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				if err := walkTemplateNode(child); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// walkTemplatePipe checks pipeline commands for dangerous function identifiers.
func walkTemplatePipe(pipe *parse.PipeNode) error {
	if pipe == nil {
		return nil
	}
	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			if ident, ok := arg.(*parse.IdentifierNode); ok {
				if dangerousFuncs[ident.Ident] {
					return fmt.Errorf("function %q is not allowed in identity templates", ident.Ident)
				}
			}
		}
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
