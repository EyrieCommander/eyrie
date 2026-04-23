package instance

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
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

// zeroClawEnsureCommands references the shared default list.
// Migration adds any missing commands without removing existing ones.
var zeroClawEnsureCommands = DefaultAllowedCommands()

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

	// Ensure allowed_private_hosts includes localhost and that the legacy
	// field name gets cleaned up. Report which actually changed.
	switch ensurePrivateHosts(cfg) {
	case privateHostsAddedLocalhost:
		applied = append(applied, "http_request.allow_private_hosts: added localhost")
		changed = true
	case privateHostsCleanedLegacy:
		applied = append(applied, "http_request.allow_private_hosts: migrated entries from legacy allowed_private_hosts key")
		changed = true
	case privateHostsBothChanged:
		applied = append(applied, "http_request.allow_private_hosts: added localhost and migrated legacy allowed_private_hosts entries")
		changed = true
	}

	// WHY remove blocking flags: block_high_risk_commands overrides
	// allowed_commands (curl is classified high-risk, gets blocked even
	// though it's in the allowlist). The allowlist itself is the safety
	// boundary — these extra flags just break working agents.
	if removeBlockingFlags(cfg) {
		applied = append(applied, "autonomy: removed blocking flags that override allowed_commands")
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
	// Read existing value for change detection before writing.
	parts := strings.Split(path, ".")
	existing := m
	for _, p := range parts[:len(parts)-1] {
		next, ok := existing[p].(map[string]any)
		if !ok {
			break
		}
		existing = next
	}
	if len(parts) > 0 {
		if old := existing[parts[len(parts)-1]]; fmt.Sprintf("%v", old) == fmt.Sprintf("%v", value) {
			return false
		}
	}
	return econfig.SetNestedValue(m, path, value)
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

	// Add missing ones — sort existing keys for deterministic output
	var added []string
	sorted := make([]string, 0, len(existing))
	for k := range existing {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	result := make([]any, 0, len(sorted))
	for _, k := range sorted {
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

// privateHostsChange reports which kind of change ensurePrivateHosts made.
type privateHostsChange int

const (
	privateHostsNoChange privateHostsChange = iota
	privateHostsAddedLocalhost
	privateHostsCleanedLegacy
	privateHostsBothChanged
)

// ensurePrivateHosts makes sure http_request.allow_private_hosts includes "localhost".
// WHY allow_private_hosts (not allowed_private_hosts): ZeroClaw's http_request
// config struct uses "allow_private_hosts". The web_fetch struct uses
// "allowed_private_hosts" — different field names on different structs.
// Old configs written by earlier provisioners may have entries under the wrong
// name; merge both into allow_private_hosts so user-added hosts aren't lost.
// Returns a status enum so callers can report accurately whether localhost
// was actually added, the legacy key was cleaned up, or both.
func ensurePrivateHosts(cfg map[string]any) privateHostsChange {
	hr, ok := cfg["http_request"].(map[string]any)
	if !ok {
		hr = make(map[string]any)
		cfg["http_request"] = hr
	}

	correct, _ := hr["allow_private_hosts"].([]any)
	legacy, hasLegacy := hr["allowed_private_hosts"].([]any)

	// Merge and dedupe hosts from both keys
	merged := make([]any, 0, len(correct)+len(legacy))
	seen := make(map[string]bool)
	for _, src := range [][]any{correct, legacy} {
		for _, h := range src {
			s, ok := h.(string)
			if !ok || seen[s] {
				continue
			}
			seen[s] = true
			merged = append(merged, s)
		}
	}

	hadLocalhost := seen["localhost"]
	// Already correct: localhost present and no legacy key to clean up
	if hadLocalhost && !hasLegacy {
		return privateHostsNoChange
	}
	addedLocalhost := !hadLocalhost
	if addedLocalhost {
		merged = append(merged, "localhost")
	}
	hr["allow_private_hosts"] = merged
	delete(hr, "allowed_private_hosts")

	switch {
	case addedLocalhost && hasLegacy:
		return privateHostsBothChanged
	case addedLocalhost:
		return privateHostsAddedLocalhost
	default:
		return privateHostsCleanedLegacy
	}
}

// removeBlockingFlags strips autonomy flags that override the allowed_commands
// allowlist. These were added by a previous provisioner version but break shell
// access because ZeroClaw classifies curl as high-risk.
//
// WHY auto_approve is NOT in this list: the provisioner intentionally sets
// auto_approve to a list of unblocked tools (Bash, Read, Write, http_request,
// web_fetch) because headless agents have no terminal to click "approve".
// Deleting it would cause Bash calls to hang waiting for approval. See
// provisioner.go.
func removeBlockingFlags(cfg map[string]any) bool {
	aut, ok := cfg["autonomy"].(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for _, key := range []string{
		"block_high_risk_commands",
		"require_approval_for_medium_risk",
		"require_approval_for_actions",
	} {
		if _, exists := aut[key]; exists {
			delete(aut, key)
			changed = true
		}
	}
	// Ensure claude_code tools are disabled — their Bash tool has its own
	// permission system that blocks commands for headless agents. ZeroClaw's
	// native "shell" tool respects the autonomy config directly.
	for _, key := range []string{"claude_code", "claude_code_runner"} {
		section, ok := cfg[key].(map[string]any)
		if !ok {
			section = make(map[string]any)
			cfg[key] = section
		}
		if enabled, exists := section["enabled"]; !exists || enabled != false {
			section["enabled"] = false
			changed = true
		}
	}
	return changed
}
