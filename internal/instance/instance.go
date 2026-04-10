package instance

import "time"

// HierarchyRole defines the role an instance plays in the agent hierarchy.
type HierarchyRole string

const (
	RoleCommander  HierarchyRole = "commander"
	RoleCaptain    HierarchyRole = "captain"
	RoleTalon      HierarchyRole = "talon"
	RoleStandalone HierarchyRole = "" // not part of a hierarchy
)

// Valid returns true if r is one of the recognised hierarchy roles (including empty/standalone).
func (r HierarchyRole) Valid() bool {
	switch r {
	case RoleCommander, RoleCaptain, RoleTalon, RoleStandalone:
		return true
	}
	return false
}

// defaultAllowedCommands is the minimum set of shell commands for provisioned
// ZeroClaw agents. Unexported to prevent mutation — use DefaultAllowedCommands().
var defaultAllowedCommands = []string{
	"git", "npm", "cargo", "make",
	"ls", "cat", "grep", "find", "echo", "pwd",
	"wc", "head", "tail", "date", "curl",
	"sleep", "mkdir", "cp", "mv", "rm", "touch",
	"sed", "awk", "sort", "uniq", "diff",
}

// DefaultAllowedCommands returns a copy of the default command allowlist.
func DefaultAllowedCommands() []string {
	cp := make([]string, len(defaultAllowedCommands))
	copy(cp, defaultAllowedCommands)
	return cp
}

// InstanceStatus represents the lifecycle state of an instance.
type InstanceStatus string

const (
	StatusCreated  InstanceStatus = "created"
	StatusStarting InstanceStatus = "starting"
	StatusRunning  InstanceStatus = "running"
	StatusStopped  InstanceStatus = "stopped"
	StatusError    InstanceStatus = "error"
)

// Valid returns true if s is a recognised instance status.
func (s InstanceStatus) Valid() bool {
	switch s {
	case StatusCreated, StatusStarting, StatusRunning, StatusStopped, StatusError:
		return true
	}
	return false
}

// Instance is a named, configured agent deployment running on a specific framework.
// Each instance has its own config file, workspace directory, gateway port, and process.
type Instance struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`            // slug: "strategist-sarah"
	DisplayName   string         `json:"display_name"`    // "Strategist Sarah"
	Framework     string         `json:"framework"`       // "zeroclaw", "openclaw", "hermes", "picoclaw", "embedded"
	PersonaID     string         `json:"persona_id,omitempty"`
	HierarchyRole HierarchyRole  `json:"hierarchy_role,omitempty"`
	ProjectID     string         `json:"project_id,omitempty"`
	ParentID      string         `json:"parent_id,omitempty"` // instance that created this one
	Port          int            `json:"port"`
	ConfigPath    string         `json:"config_path"`
	WorkspacePath string         `json:"workspace_path"`
	AuthToken     string         `json:"-"` // gateway auth token — excluded from JSON output for security
	Status        InstanceStatus `json:"status"`
	PID           int            `json:"pid,omitempty"`
	LastSeen      time.Time      `json:"last_seen,omitempty"`
	HealthStatus  string         `json:"health_status,omitempty"` // "healthy", "unhealthy", "unknown"
	RestartCount  int            `json:"restart_count,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	CreatedBy     string         `json:"created_by"` // "user" or parent instance ID
}

// CreateRequest holds the parameters for provisioning a new instance.
type CreateRequest struct {
	Name          string        `json:"name"`
	Framework     string        `json:"framework"`
	PersonaID     string        `json:"persona_id,omitempty"`
	HierarchyRole HierarchyRole `json:"hierarchy_role,omitempty"`
	ProjectID     string        `json:"project_id,omitempty"`
	ParentID      string        `json:"parent_id,omitempty"`
	Model         string        `json:"model,omitempty"`    // override persona default
	AutoStart     *bool         `json:"auto_start,omitempty"`
	CreatedBy     string        `json:"created_by,omitempty"` // "user" or parent instance ID

	// Project context — populated by the server handler (not from API body)
	// so the provisioner can include project info in identity templates.
	ProjectName        string `json:"-"`
	ProjectGoal        string `json:"-"`
	ProjectDescription string `json:"-"`
}
