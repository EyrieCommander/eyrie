package instance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/persona"
)

// TemplateContext is passed to identity file templates when rendering.
type TemplateContext struct {
	Name        string
	DisplayName string
	Role        string
	Description string
	ParentAgent string
	EyrieURL    string
	Framework   string
}

// Provisioner creates new agent instances with full workspace and config.
type Provisioner struct {
	store *Store
}

func NewProvisioner(store *Store) *Provisioner {
	return &Provisioner{store: store}
}

// Provision creates a new agent instance: allocates a port, scaffolds the
// workspace directory with identity files, generates the framework config,
// and saves the instance metadata.
func (p *Provisioner) Provision(req CreateRequest, pers *persona.Persona) (*Instance, error) {
	// Validate
	if req.Name == "" {
		return nil, fmt.Errorf("instance name: %w", ErrRequiredField)
	}
	if req.Framework == "" {
		return nil, fmt.Errorf("framework: %w", ErrRequiredField)
	}
	if req.Framework != "zeroclaw" && req.Framework != "openclaw" && req.Framework != "hermes" {
		return nil, fmt.Errorf("%q: %w", req.Framework, ErrUnsupportedFramework)
	}

	// Reserve name and port under lock; hold until instance.json is persisted
	// to prevent races where another Provision could allocate the same name/port.
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	existing, err := p.store.listLocked()
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}
	for _, inst := range existing {
		if inst.Name == req.Name {
			return nil, fmt.Errorf("instance name %q: %w", req.Name, ErrNameExists)
		}
	}
	port, err := AllocatePort(existing)
	if err != nil {
		return nil, fmt.Errorf("port allocation failed: %w", err)
	}

	// Generate ID and paths
	id := uuid.New().String()
	instDir := filepath.Join(p.store.dir, id)
	workspaceDir := filepath.Join(instDir, "workspace")

	var configExt string
	switch req.Framework {
	case "zeroclaw":
		configExt = "toml"
	case "openclaw":
		configExt = "json"
	case "hermes":
		configExt = "yaml"
	}
	configPath := filepath.Join(instDir, "config."+configExt)

	inst := Instance{
		ID:            id,
		Name:          req.Name,
		DisplayName:   toDisplayName(req.Name),
		Framework:     req.Framework,
		PersonaID:     req.PersonaID,
		HierarchyRole: req.HierarchyRole,
		ProjectID:     req.ProjectID,
		ParentID:      req.ParentID,
		Port:          port,
		ConfigPath:    configPath,
		WorkspacePath: workspaceDir,
		Status:        "created",
		CreatedAt:     time.Now(),
		CreatedBy:     req.CreatedBy,
	}
	if inst.CreatedBy == "" {
		inst.CreatedBy = "user"
	}

	// Deferred cleanup: remove instance directory on any failure.
	// Set success = true just before returning the instance to skip cleanup.
	success := false
	defer func() {
		if !success {
			os.RemoveAll(instDir)
		}
	}()

	// Create directory structure
	dirs := []string{
		instDir,
		workspaceDir,
		filepath.Join(workspaceDir, "memory"),
		filepath.Join(workspaceDir, "sessions"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}

	// Build template context — ProjectName, ProjectGoal, and ParentAgent are
	// populated from the request so identity templates can reference them.
	tc := TemplateContext{
		Name:        inst.Name,
		DisplayName: inst.DisplayName,
		Framework:   inst.Framework,
		EyrieURL:    "http://127.0.0.1:7200",
		ParentAgent: req.ParentID,
	}
	if pers != nil {
		tc.Role = pers.Role
		tc.Description = pers.Description
	}

	// Render identity files
	if err := p.renderIdentityFiles(workspaceDir, req.HierarchyRole, pers, tc); err != nil {
		return nil, fmt.Errorf("failed to render identity files: %w", err)
	}

	// Generate framework config
	if err := p.generateConfig(&inst, pers, req.Model); err != nil {
		return nil, fmt.Errorf("failed to generate config: %w", err)
	}

	// Save instance metadata
	data, err := marshalIndent(inst)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal instance metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(instDir, "instance.json"), data, 0o644); err != nil {
		return nil, fmt.Errorf("failed to save instance metadata: %w", err)
	}

	success = true
	return &inst, nil
}

func (p *Provisioner) renderIdentityFiles(workspaceDir string, role HierarchyRole, pers *persona.Persona, tc TemplateContext) error {
	// If persona has identity templates, use those; otherwise use defaults
	templates := defaultIdentityTemplates(role)
	if pers != nil && len(pers.IdentityTemplate) > 0 {
		for k, v := range pers.IdentityTemplate {
			templates[k] = v
		}
	}

	for filename, tmplStr := range templates {
		// Sanitize filename to prevent path traversal from persona templates
		safe := filepath.Base(filename)
		if safe == "." || safe == ".." || safe != filename {
			return fmt.Errorf("invalid identity template filename %q", filename)
		}
		rendered, err := renderTemplate(safe, tmplStr, tc)
		if err != nil {
			return fmt.Errorf("rendering %s: %w", safe, err)
		}
		path := filepath.Join(workspaceDir, safe)
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", safe, err)
		}
	}
	return nil
}

func (p *Provisioner) generateConfig(inst *Instance, pers *persona.Persona, modelOverride string) error {
	model := "anthropic/claude-sonnet-4-20250514"
	provider := "openrouter"
	if pers != nil && pers.PreferredModel != "" {
		model = pers.PreferredModel
	}
	if modelOverride != "" {
		model = modelOverride
	}

	switch inst.Framework {
	case "zeroclaw":
		return p.generateZeroClawConfig(inst, provider, model)
	case "openclaw":
		return p.generateOpenClawConfig(inst, provider, model)
	case "hermes":
		return p.generateHermesConfig(inst, provider, model)
	default:
		return fmt.Errorf("unsupported framework %q", inst.Framework)
	}
}

func (p *Provisioner) generateZeroClawConfig(inst *Instance, provider, model string) error {
	cfg := map[string]any{
		"default_provider":    provider,
		"default_model":       model,
		"default_temperature": 0.7,
		"gateway": map[string]any{
			"port":                inst.Port,
			"host":                "127.0.0.1",
			"session_persistence": true,
			"require_pairing":     false,
		},
		"autonomy": map[string]any{
			"level":            "supervised",
			"workspace_only":   true,
			"allowed_commands": []string{"git", "npm", "cargo", "ls", "cat", "grep", "find", "echo", "pwd", "wc", "head", "tail", "date", "curl"},
		},
		"memory": map[string]any{
			"backend":   "sqlite",
			"auto_save": true,
		},
	}

	// Copy API key from parent ZeroClaw installation so provisioned instances
	// can use the same provider without manual onboarding.
	parentConfigDir := config.ExpandHome("~/.zeroclaw")
	parentConfigPath := filepath.Join(parentConfigDir, "config.toml")
	if apiKey := readTOMLField(parentConfigPath, "api_key"); apiKey != "" {
		// Copy the secret key so the encrypted api_key can be decrypted.
		// Only set api_key in the config after confirming the secret key was written.
		srcSecret := filepath.Join(parentConfigDir, ".secret_key")
		dstSecret := filepath.Join(filepath.Dir(inst.ConfigPath), ".secret_key")
		secretData, readErr := os.ReadFile(srcSecret)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "eyrie: cannot read parent secret key %s: %v (instance will need manual onboarding)\n", srcSecret, readErr)
		} else if writeErr := os.WriteFile(dstSecret, secretData, 0o600); writeErr != nil {
			fmt.Fprintf(os.Stderr, "eyrie: cannot write secret key %s: %v (instance will need manual onboarding)\n", dstSecret, writeErr)
		} else {
			cfg["api_key"] = apiKey
		}
	}

	return config.WriteTOMLAtomic(inst.ConfigPath, cfg)
}

// readTOMLField reads a single top-level string field from a TOML file.
func readTOMLField(path, field string) string {
	var raw map[string]any
	if err := config.ParseTOMLFile(path, &raw); err != nil {
		return ""
	}
	if val, ok := raw[field]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

func (p *Provisioner) generateOpenClawConfig(inst *Instance, provider, model string) error {
	token := uuid.New().String()
	inst.AuthToken = token
	cfg := map[string]any{
		"provider": provider,
		"model":    model,
		"gateway": map[string]any{
			"port": inst.Port,
			"bind": "loopback",
			"auth": map[string]any{
				"token": token,
			},
		},
	}
	return config.WriteJSONAtomic(inst.ConfigPath, cfg)
}

func (p *Provisioner) generateHermesConfig(inst *Instance, provider, model string) error {
	cfg := map[string]any{
		"provider": provider,
		"model":    model,
		"gateway": map[string]any{
			"port": inst.Port,
			"host": "127.0.0.1",
		},
	}
	return config.WriteYAMLAtomic(inst.ConfigPath, cfg)
}

// --- Default identity templates ---

func defaultIdentityTemplates(role HierarchyRole) map[string]string {
	templates := map[string]string{
		"IDENTITY.md": defaultIdentityMD,
		"SOUL.md":     defaultSoulMD,
		"MEMORY.md":   defaultMemoryMD,
	}

	switch role {
	case RoleCommander:
		templates["TOOLS.md"] = commanderToolsMD
		templates["IDENTITY.md"] = commanderIdentityMD
	case RoleCaptain:
		templates["TOOLS.md"] = captainToolsMD
		templates["IDENTITY.md"] = captainIdentityMD
	case RoleTalon:
		templates["IDENTITY.md"] = talonIdentityMD
	}

	return templates
}

const defaultIdentityMD = `# IDENTITY.md

- **Name:** {{.DisplayName}}
- **Framework:** {{.Framework}}
- **Role:** {{.Role}}
- **Description:** {{.Description}}
`

const defaultSoulMD = `# SOUL.md

You are {{.DisplayName}}.

## Core Principles

- Be genuinely helpful and proactive
- Have opinions and share them when relevant
- Be resourceful — use available tools to find answers
- Be honest about what you don't know
- Remember and build on past conversations
`

const defaultMemoryMD = `# MEMORY.md

*No memories yet. This file will be populated as you work and learn.*
`

const commanderIdentityMD = `# IDENTITY.md

- **Name:** {{.DisplayName}}
- **Framework:** {{.Framework}}
- **Role:** Commander
- **Description:** {{.Description}}

## Responsibilities

You are the Commander of this Eyrie — the master agent overseeing all projects. Your job is to:

1. Talk with the user to understand their goals and help them plan projects
2. Create a Captain for each project to lead its agent team
3. Track progress across all projects and relay status to the user
4. Recommend which agents (Talons) and personas would be most useful
5. Help the user grow their agent team over time

When the user describes a new project, you should:
- Ask clarifying questions about their goals and requirements
- Create the project via the Eyrie API
- Provision a Captain agent for the project
- Brief the Captain on the project's goals

You have access to Eyrie's API to create projects and provision agents.
See TOOLS.md for the API reference.
`

const captainIdentityMD = `# IDENTITY.md

- **Name:** {{.DisplayName}}
- **Framework:** {{.Framework}}
- **Role:** Captain
- **Description:** {{.Description}}

## Responsibilities

You are a Captain — the leader of a specific project's agent team. Your job is to:

1. Understand the project's goals and break them into tasks
2. Coordinate your Talons — assign work, track progress, resolve blockers
3. Create additional Talons when the project needs new capabilities
4. Report project status to the Commander and user
5. Adapt the team composition as the project evolves

See TOOLS.md for the Eyrie API reference.
`

const talonIdentityMD = `# IDENTITY.md

- **Name:** {{.DisplayName}}
- **Framework:** {{.Framework}}
- **Role:** {{.Role}}
- **Description:** {{.Description}}

## Responsibilities

You are a Talon — a specialist agent within a project team. Focus on your
specific expertise and deliver high-quality work in your domain. Report
progress and blockers to your Captain.
`

const commanderToolsMD = `# TOOLS.md — Eyrie API

You can manage projects and agents via Eyrie's REST API.

**Base URL:** {{.EyrieURL}}

## Create a project

` + "```" + `
POST {{.EyrieURL}}/api/projects
Content-Type: application/json

{
  "name": "project name",
  "description": "what this project is about",
  "goal": "the desired outcome"
}
` + "```" + `

## Create a Captain or Talon agent

` + "```" + `
POST {{.EyrieURL}}/api/instances
Content-Type: application/json

{
  "name": "captain-name",
  "framework": "openclaw",
  "persona_id": "exec-strategist",
  "hierarchy_role": "captain",
  "project_id": "project-id-here",
  "auto_start": true
}
` + "```" + `

Hierarchy roles: "commander", "captain", "talon"

## Assign a Captain to a project

` + "```" + `
PUT {{.EyrieURL}}/api/projects/{id}
Content-Type: application/json

{
  "orchestrator_id": "captain-instance-id-or-agent-name"
}
` + "```" + `

## List all instances

` + "```" + `
GET {{.EyrieURL}}/api/instances
` + "```" + `

## List all projects

` + "```" + `
GET {{.EyrieURL}}/api/projects
` + "```" + `

## Start / stop / restart an agent

` + "```" + `
POST {{.EyrieURL}}/api/instances/{id}/start
POST {{.EyrieURL}}/api/instances/{id}/stop
POST {{.EyrieURL}}/api/instances/{id}/restart
` + "```" + `

## Get hierarchy tree

` + "```" + `
GET {{.EyrieURL}}/api/hierarchy
` + "```" + `
`

const captainToolsMD = `# TOOLS.md — Eyrie API

You can manage your project's agents via Eyrie's REST API.

**Base URL:** {{.EyrieURL}}

## Create a Talon (specialist agent)
POST {{.EyrieURL}}/api/instances
Body: {"name": "agent-slug", "framework": "zeroclaw", "persona_id": "...", "hierarchy_role": "talon", "project_id": "your-project-id", "auto_start": true}

## Browse available personas
GET {{.EyrieURL}}/api/personas

## List agents and projects
GET {{.EyrieURL}}/api/instances
GET {{.EyrieURL}}/api/projects

## View full hierarchy
GET {{.EyrieURL}}/api/hierarchy

## Agent lifecycle
POST {{.EyrieURL}}/api/instances/{id}/start
POST {{.EyrieURL}}/api/instances/{id}/stop
`

// --- Helpers ---

func toDisplayName(slug string) string {
	words := strings.Split(slug, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func renderTemplate(name, tmplStr string, data TemplateContext) (string, error) {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func marshalIndent(v any) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
