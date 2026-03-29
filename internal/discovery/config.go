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
			val := strings.TrimSpace(line[idx+len("Name:"):])
			// Strip markdown bold markers (e.g., "** Danya" from "- **Name:** Danya")
			val = strings.Trim(val, "*")
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// WriteIdentityName updates (or creates) the Name field in the workspace's IDENTITY.md.
func WriteIdentityName(configPath, name string) error {
	workspaceDir := filepath.Join(filepath.Dir(configPath), "workspace")
	identityPath := filepath.Join(workspaceDir, "IDENTITY.md")
	data, err := os.ReadFile(identityPath)
	if err != nil {
		// No IDENTITY.md yet — create a minimal one
		if os.IsNotExist(err) {
			if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(identityPath, []byte("# IDENTITY.md — Who Am I?\n\n- **Name:** "+name+"\n"), 0o644)
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, "Name:") {
			lines[i] = "- **Name:** " + name
			found = true
			break
		}
	}
	if !found {
		// Insert after the first heading or at line 2
		insertAt := 1
		for i, line := range lines {
			if strings.HasPrefix(line, "#") {
				insertAt = i + 1
				break
			}
		}
		if insertAt > len(lines) {
			insertAt = len(lines)
		}
		newLine := "- **Name:** " + name
		lines = append(lines[:insertAt], append([]string{newLine}, lines[insertAt:]...)...)
	}

	return os.WriteFile(identityPath, []byte(strings.Join(lines, "\n")), 0o644)
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

// isPicoClawConfig checks whether a JSON config file belongs to PicoClaw by
// looking for the PicoClaw-specific "channels.pico" field as a non-null object.
// This discriminator is called before scanOpenClawConfig in all .json branches.
func isPicoClawConfig(data []byte) bool {
	var cfg struct {
		Channels struct {
			Pico json.RawMessage `json:"pico"`
		} `json:"channels"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	// channels.pico must be present and be a JSON object (not null, not a string)
	if len(cfg.Channels.Pico) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(string(cfg.Channels.Pico))
	return strings.HasPrefix(trimmed, "{")
}

// scanPicoClawConfig reads a PicoClaw config.json and extracts gateway address and token.
func scanPicoClawConfig(path string, preReadData ...[]byte) (*adapter.DiscoveredAgent, error) {
	path = config.ExpandHome(path)
	var data []byte
	if len(preReadData) > 0 && preReadData[0] != nil {
		data = preReadData[0]
	} else {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}

	var cfg struct {
		Gateway struct {
			Port int    `json:"port"`
			Host string `json:"host"`
		} `json:"gateway"`
		Channels struct {
			Pico struct {
				Token string `json:"token"`
			} `json:"pico"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	host := cfg.Gateway.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Gateway.Port
	if port == 0 {
		port = 18790 // PicoClaw default gateway port
	}

	return &adapter.DiscoveredAgent{
		Name:        "picoclaw",
		DisplayName: readIdentityName(path),
		Framework:   "picoclaw",
		Host:        host,
		Port:        port,
		ConfigPath:  path,
		Token:       cfg.Channels.Pico.Token,
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
