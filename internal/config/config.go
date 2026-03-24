package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

var Version = "dev"

type Config struct {
	Dashboard DashboardConfig `toml:"dashboard"`
	Discovery DiscoveryConfig `toml:"discovery"`
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

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(path string) string {
	if len(path) < 2 || path[:2] != "~/" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
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
