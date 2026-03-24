package registry

import "time"

// Registry represents the complete Claw frameworks registry
type Registry struct {
	Version    string      `json:"version"`
	UpdatedAt  time.Time   `json:"updated_at"`
	Frameworks []Framework `json:"frameworks"`
}

// Framework describes a single Claw agent framework
type Framework struct {
	// Identity
	ID          string `json:"id"`          // "hermes", "zeroclaw", "openclaw"
	Name        string `json:"name"`        // Display name
	Description string `json:"description"` // Short description
	Language    string `json:"language"`    // "python", "rust", "typescript"
	Repository  string `json:"repository"`  // GitHub URL
	Website     string `json:"website,omitempty"` // Official website URL (optional)

	// Installation
	InstallMethod string   `json:"install_method"` // "script", "cargo", "npm", "pip", "manual"
	InstallCmd    string   `json:"install_cmd"`    // Command or script URL
	Requirements  []string `json:"requirements"`   // ["python>=3.11", "node>=22"]

	// Configuration
	ConfigFormat string        `json:"config_format"` // "toml", "json", "yaml"
	ConfigPath   string        `json:"config_path"`   // "~/.hermes/config.yaml"
	ConfigDir    string        `json:"config_dir"`    // "~/.hermes"
	ConfigSchema *ConfigSchema `json:"config_schema,omitempty"` // Optional config form schema

	// Runtime
	BinaryPath  string `json:"binary_path"`        // "~/.local/bin/hermes"
	AdapterType string `json:"adapter_type"`       // "http", "websocket", "cli", "hybrid"
	DefaultPort int    `json:"default_port,omitempty"` // 0 if not applicable

	// Lifecycle commands
	StartCmd   string `json:"start_cmd"`   // "hermes gateway start"
	StopCmd    string `json:"stop_cmd"`    // "" (means PID-based)
	StatusCmd  string `json:"status_cmd"`  // "hermes status" or ""
	RestartCmd string `json:"restart_cmd"` // Optional explicit restart command

	// Status detection (for adapters without HTTP APIs)
	PIDFile   string `json:"pid_file,omitempty"`   // "~/.hermes/gateway.pid"
	StateFile string `json:"state_file,omitempty"` // "~/.hermes/gateway_state.json"
	HealthURL string `json:"health_url,omitempty"` // "http://localhost:42617/health"

	// Logs and activity
	LogDir    string `json:"log_dir"`    // "~/.hermes/logs"
	LogFormat string `json:"log_format"` // "text", "json"
}

// ConfigSchema defines editable configuration fields for a framework
type ConfigSchema struct {
	CommonFields []ConfigField `json:"common_fields"` // Editable fields for the config form
	APIKeyHint   string        `json:"api_key_hint"`  // Instructions for setting API keys
}

// ConfigField represents a single editable configuration field
type ConfigField struct {
	Key         string   `json:"key"`         // Config key (dot notation for nested: "gateway.port")
	Label       string   `json:"label"`       // Display label
	Type        string   `json:"type"`        // "text", "number", "select", "checkbox", "multiselect"
	Default     any      `json:"default,omitempty"` // Default value
	Required    bool     `json:"required"`    // Whether field is required
	Description string   `json:"description"` // Help text
	Options     []string `json:"options,omitempty"` // For select/multiselect types
	Min         *int     `json:"min,omitempty"` // For number types
	Max         *int     `json:"max,omitempty"` // For number types
}
