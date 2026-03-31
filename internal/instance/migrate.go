package instance

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	econfig "github.com/Audacity88/eyrie/internal/config"
)

// MigrationResult reports what happened to a single instance.
type MigrationResult struct {
	InstanceID   string   `json:"instance_id"`
	InstanceName string   `json:"instance_name"`
	Framework    string   `json:"framework"`
	Applied      []string `json:"applied,omitempty"`  // human-readable descriptions of changes
	Skipped      bool     `json:"skipped,omitempty"`  // true if no changes needed
	Error        string   `json:"error,omitempty"`
}

// MigrateAll applies config migrations to all provisioned instances.
// Each migration is idempotent — safe to run repeatedly.
func MigrateAll() ([]MigrationResult, error) {
	store, err := NewStore()
	if err != nil {
		return nil, fmt.Errorf("opening instance store: %w", err)
	}
	instances, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}

	var results []MigrationResult
	for _, inst := range instances {
		result := migrateInstance(inst)
		results = append(results, result)
	}
	return results, nil
}

func migrateInstance(inst Instance) MigrationResult {
	r := MigrationResult{
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		Framework:    inst.Framework,
	}

	switch inst.Framework {
	case "zeroclaw":
		applied, err := migrateZeroClaw(inst.ConfigPath)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.Applied = applied
		r.Skipped = len(applied) == 0
	default:
		r.Skipped = true
	}
	return r
}

// zeroclaw migration rule: a key path, the required value, and a reason.
type tomlRule struct {
	path   string // dot-separated: "security.sandbox.backend"
	value  any    // required value
	reason string // human-readable why
}

// Current ZeroClaw migration rules. Add new rules here as provisioner
// defaults change. Each rule is applied only if the current value doesn't
// match — existing correct values are left untouched.
var zeroClawRules = []tomlRule{
	{
		path:   "autonomy.level",
		value:  "full",
		reason: "ZeroClaw rejects 'autonomous'; expects readonly/supervised/full",
	},
	{
		path:   "security.sandbox.backend",
		value:  "none",
		reason: "macOS seatbelt blocks basic commands even inside workspace",
	},
	{
		path:   "http_request.enabled",
		value:  true,
		reason: "agents need HTTP access for Eyrie API calls",
	},
}

// zeroClawEnsureCommands is the minimum set of allowed commands.
// Migration adds any missing commands without removing existing ones.
var zeroClawEnsureCommands = []string{
	"git", "npm", "cargo", "make",
	"ls", "cat", "grep", "find", "echo", "pwd",
	"wc", "head", "tail", "date", "curl",
	"sleep", "mkdir", "cp", "mv", "rm", "touch",
	"sed", "awk", "sort", "uniq", "diff",
}

func migrateZeroClaw(configPath string) ([]string, error) {
	configPath = econfig.ExpandHome(configPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg map[string]any
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing TOML: %w", err)
	}

	var applied []string
	changed := false

	// Apply key-value rules
	for _, rule := range zeroClawRules {
		if setNestedValue(cfg, rule.path, rule.value) {
			applied = append(applied, fmt.Sprintf("%s = %v (%s)", rule.path, rule.value, rule.reason))
			changed = true
		}
	}

	// Ensure max_tool_iterations is at least 50
	if current := getNestedInt(cfg, "autonomy.max_tool_iterations"); current < 50 {
		setNestedValue(cfg, "autonomy.max_tool_iterations", int64(50))
		applied = append(applied, fmt.Sprintf("autonomy.max_tool_iterations: %d → 50 (default too low for working agents)", current))
		changed = true
	}

	// Ensure allowed_commands includes all required commands
	if added := ensureAllowedCommands(cfg); len(added) > 0 {
		applied = append(applied, fmt.Sprintf("autonomy.allowed_commands: added %s", strings.Join(added, ", ")))
		changed = true
	}

	// Ensure allowed_private_hosts includes localhost
	if ensurePrivateHosts(cfg) {
		applied = append(applied, "http_request.allowed_private_hosts: added localhost")
		changed = true
	}

	if !changed {
		return nil, nil
	}

	// Write back atomically
	if err := econfig.WriteTOMLAtomic(configPath, cfg); err != nil {
		return applied, fmt.Errorf("writing config: %w", err)
	}

	slog.Info("migrated instance config", "path", configPath, "changes", len(applied))
	return applied, nil
}

// --- Helpers ---

// setNestedValue sets a dot-path key in a nested map. Returns true if the
// value was changed (i.e., it was different or missing).
func setNestedValue(m map[string]any, path string, value any) bool {
	parts := strings.Split(path, ".")
	current := m
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]].(map[string]any)
		if !ok {
			// Create missing intermediate maps
			next = make(map[string]any)
			current[parts[i]] = next
		}
		current = next
	}
	key := parts[len(parts)-1]

	existing := current[key]
	if fmt.Sprintf("%v", existing) == fmt.Sprintf("%v", value) {
		return false // already correct
	}
	current[key] = value
	return true
}

// getNestedInt reads an integer from a dot-path in a nested map.
func getNestedInt(m map[string]any, path string) int {
	parts := strings.Split(path, ".")
	current := m
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]].(map[string]any)
		if !ok {
			return 0
		}
		current = next
	}
	key := parts[len(parts)-1]
	switch v := current[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// ensureAllowedCommands adds missing commands to autonomy.allowed_commands.
// Returns the list of commands that were added.
func ensureAllowedCommands(cfg map[string]any) []string {
	autonomy, ok := cfg["autonomy"].(map[string]any)
	if !ok {
		autonomy = make(map[string]any)
		cfg["autonomy"] = autonomy
	}

	// Read existing commands
	existing := make(map[string]bool)
	if cmds, ok := autonomy["allowed_commands"].([]any); ok {
		for _, c := range cmds {
			if s, ok := c.(string); ok {
				existing[s] = true
			}
		}
	}

	// Add missing ones
	var added []string
	var result []any
	for k := range existing {
		result = append(result, k)
	}
	for _, cmd := range zeroClawEnsureCommands {
		if !existing[cmd] {
			result = append(result, cmd)
			added = append(added, cmd)
		}
	}
	if len(added) > 0 {
		autonomy["allowed_commands"] = result
	}
	return added
}

// ensurePrivateHosts makes sure http_request.allowed_private_hosts includes "localhost".
func ensurePrivateHosts(cfg map[string]any) bool {
	hr, ok := cfg["http_request"].(map[string]any)
	if !ok {
		hr = make(map[string]any)
		cfg["http_request"] = hr
	}

	hosts, _ := hr["allowed_private_hosts"].([]any)
	for _, h := range hosts {
		if s, ok := h.(string); ok && s == "localhost" {
			return false // already present
		}
	}
	hr["allowed_private_hosts"] = append(hosts, "localhost")
	return true
}
