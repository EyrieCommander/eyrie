package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
)

// readIdentityName reads the "Name:" field from the workspace's IDENTITY.md file.
func readIdentityName(configPath string) string {
	workspaceDir := filepath.Join(filepath.Dir(configPath), "workspace")
	data, err := os.ReadFile(filepath.Join(workspaceDir, "IDENTITY.md"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, "Name:"); idx >= 0 {
			return strings.TrimSpace(line[idx+len("Name:"):])
		}
	}
	return ""
}

// scanZeroClawConfig reads a ZeroClaw config.toml and extracts the gateway address.
func scanZeroClawConfig(path string) (*adapter.DiscoveredAgent, error) {
	path = config.ExpandHome(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg struct {
		Gateway struct {
			Port int    `toml:"port"`
			Host string `toml:"host"`
		} `toml:"gateway"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	host := cfg.Gateway.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Gateway.Port
	if port == 0 {
		port = 42617
	}

	return &adapter.DiscoveredAgent{
		Name:        "zeroclaw",
		DisplayName: readIdentityName(path),
		Framework:   "zeroclaw",
		Host:        host,
		Port:        port,
		ConfigPath:  path,
	}, nil
}

// scanOpenClawConfig reads an OpenClaw openclaw.json and extracts the gateway address.
func scanOpenClawConfig(path string) (*adapter.DiscoveredAgent, error) {
	path = config.ExpandHome(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg struct {
		Gateway struct {
			Port int    `json:"port"`
			Bind string `json:"bind"`
			Auth struct {
				Token string `json:"token"`
			} `json:"auth"`
		} `json:"gateway"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	host := "127.0.0.1"
	if cfg.Gateway.Bind != "" && cfg.Gateway.Bind != "loopback" {
		host = cfg.Gateway.Bind
	}
	port := cfg.Gateway.Port
	if port == 0 {
		port = 18789
	}

	return &adapter.DiscoveredAgent{
		Name:        "openclaw",
		DisplayName: readIdentityName(path),
		Framework:   "openclaw",
		Host:        host,
		Port:        port,
		ConfigPath:  path,
		Token:       cfg.Gateway.Auth.Token,
	}, nil
}
