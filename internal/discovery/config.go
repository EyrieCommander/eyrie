package discovery

import (
	"encoding/json"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
)

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
		Name:       "zeroclaw",
		Framework:  "zeroclaw",
		Host:       host,
		Port:       port,
		ConfigPath: path,
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
		Name:       "openclaw",
		Framework:  "openclaw",
		Host:       host,
		Port:       port,
		ConfigPath: path,
		Token:      cfg.Gateway.Auth.Token,
	}, nil
}
