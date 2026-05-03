package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

var Version = "0.2.1"

type Config struct {
	Dashboard DashboardConfig `toml:"dashboard"`
	Discovery DiscoveryConfig `toml:"discovery"`
	Mesh      MeshConfig      `toml:"mesh"`
	Agents    []ManualAgent   `toml:"agents"`
}

type DashboardConfig struct {
	Port        int    `toml:"port"`
	Host        string `toml:"host"`
	OpenBrowser bool   `toml:"open_browser"`
}

type DiscoveryConfig struct {
	IntervalSeconds int      `toml:"interval_seconds"`
	ConfigPaths     []string `toml:"config_paths"`
}

type MeshConfig struct {
	AgentMeshDir string `toml:"agent_mesh_dir"`
}

type ManualAgent struct {
	Name      string `toml:"name"`
	Framework string `toml:"framework"`
	URL       string `toml:"url"`
	Token     string `toml:"token,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Dashboard: DashboardConfig{
			Port:        7200,
			Host:        "127.0.0.1",
			OpenBrowser: true,
		},
		Discovery: DiscoveryConfig{
			IntervalSeconds: 30,
			ConfigPaths: []string{
				"~/.zeroclaw/config.toml",
				"~/.openclaw/openclaw.json",
				"~/.picoclaw/config.json",
			},
		},
	}
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".eyrie"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func Load() (Config, error) {
	cfg := DefaultConfig()

	path, err := ConfigPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// ClawPath returns the root directory for framework installations.
// When CLAW_PATH is set, all framework config/binary paths (e.g.,
// ~/.zeroclaw/config.toml) resolve under CLAW_PATH instead of ~.
// This enables isolated testing without disturbing real installations.
func ClawPath() string {
	return os.Getenv("CLAW_PATH")
}

// ExpandHome replaces a leading ~ with the user's home directory,
// or with CLAW_PATH if set. This means all framework paths like
// ~/.zeroclaw/config.toml automatically resolve to
// $CLAW_PATH/.zeroclaw/config.toml when the env var is present.
func ExpandHome(path string) string {
	if len(path) < 2 || path[:2] != "~/" {
		return path
	}
	if cp := ClawPath(); cp != "" {
		return filepath.Join(cp, path[2:])
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// LookPathEnriched resolves a command name against the enriched PATH
// (the same dirs added by EnrichedEnv). Use this before exec.Command so
// binaries in ~/go/bin, ~/.cargo/bin, etc. are found even when the Eyrie
// process's own PATH doesn't include them.
func LookPathEnriched(command string) string {
	// If it's already an absolute path, return as-is.
	if filepath.IsAbs(command) {
		return command
	}
	// Check the enriched dirs first, then fall back to the original command
	// (let exec.Command try the system PATH).
	home, err := os.UserHomeDir()
	if err != nil {
		return command
	}
	dirs := []string{
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".local", "bin"),
		"/usr/local/bin",
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin")
	}
	// Find NVM Node.js v22 if available (same logic as EnrichedEnv).
	nvmDir := filepath.Join(home, ".nvm", "versions", "node")
	if entries, err := os.ReadDir(nvmDir); err == nil {
		bestName := ""
		var bestMinor, bestPatch int
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "v22.") {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(name, "v"), ".")
			if len(parts) < 3 {
				continue
			}
			minor, err1 := strconv.Atoi(parts[1])
			patch, err2 := strconv.Atoi(parts[2])
			if err1 != nil || err2 != nil {
				continue
			}
			if bestName == "" || minor > bestMinor || (minor == bestMinor && patch > bestPatch) {
				bestName, bestMinor, bestPatch = name, minor, patch
			}
		}
		if bestName != "" {
			dirs = append(dirs, filepath.Join(nvmDir, bestName, "bin"))
		}
	}
	for _, d := range dirs {
		p := filepath.Join(d, command)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return command
}

// ParseJSONFile reads and unmarshals a JSON file into the given target.
func ParseJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// ParseTOMLFile reads and unmarshals a TOML file into the given target.
func ParseTOMLFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return toml.Unmarshal(data, target)
}

// Save writes the config to the config file.
func Save(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return fmt.Errorf("cannot determine config path: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	// Create temporary file in the same directory
	tmpFile, err := os.CreateTemp(dir, ".config.toml.tmp.*")
	if err != nil {
		return fmt.Errorf("cannot create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Encode to temporary file
	encoder := toml.NewEncoder(tmpFile)
	if err := encoder.Encode(&cfg); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("cannot encode config: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("cannot close temporary file: %w", err)
	}

	// Atomically rename temporary file to config file
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("cannot save config: %w", err)
	}

	return nil
}

// EnrichedEnv returns a copy of the current environment with common tool
// directories prepended to PATH. This ensures exec.Command can find binaries
// like cargo, go, npm, pip, etc. even when the Eyrie server is started from
// a non-interactive shell (e.g., launchd, systemd) that doesn't source
// ~/.bashrc or ~/.zshrc.
func EnrichedEnv() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.Environ()
	}

	extraDirs := []string{
		filepath.Join(home, ".cargo", "bin"), // Rust/cargo
		filepath.Join(home, "go", "bin"),     // Go binaries
		filepath.Join(home, ".local", "bin"), // pip, pipx, user installs
		"/usr/local/bin",                     // Homebrew (Intel Mac), manual installs
	}
	if runtime.GOOS == "darwin" {
		extraDirs = append(extraDirs, "/opt/homebrew/bin") // Homebrew (Apple Silicon)
	}

	// Find NVM Node.js v22 if available. Pick the highest-patched v22.*
	// by numerically comparing minor/patch instead of relying on the
	// directory listing's lexicographic order (which would prefer
	// v22.9.0 over v22.10.0).
	nvmDir := filepath.Join(home, ".nvm", "versions", "node")
	if entries, err := os.ReadDir(nvmDir); err == nil {
		bestName := ""
		var bestMinor, bestPatch int
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "v22.") {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(name, "v"), ".")
			if len(parts) < 3 {
				continue
			}
			minor, err1 := strconv.Atoi(parts[1])
			patch, err2 := strconv.Atoi(parts[2])
			if err1 != nil || err2 != nil {
				continue
			}
			if bestName == "" || minor > bestMinor || (minor == bestMinor && patch > bestPatch) {
				bestName, bestMinor, bestPatch = name, minor, patch
			}
		}
		if bestName != "" {
			extraDirs = append(extraDirs, filepath.Join(nvmDir, bestName, "bin"))
		}
	}

	// Filter to directories that actually exist
	var existing []string
	for _, d := range extraDirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			existing = append(existing, d)
		}
	}
	if len(existing) == 0 {
		return os.Environ()
	}

	extra := strings.Join(existing, string(os.PathListSeparator))
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + extra + string(os.PathListSeparator) + e[5:]
			return env
		}
	}
	// No PATH found — add one
	return append(env, "PATH="+extra)
}
