package discovery

import (
	"fmt"
	"os"

	"github.com/natalie/eyrie/internal/adapter"
	"github.com/natalie/eyrie/internal/config"
	"gopkg.in/yaml.v3"
)

// scanYAMLConfig scans a YAML config file (e.g., Hermes)
func scanYAMLConfig(path string) (*adapter.DiscoveredAgent, error) {
	expandedPath := config.ExpandHome(path)

	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// For now, we'll detect Hermes specifically
	// TODO: Make this more generic with framework detection
	return scanHermesConfig(path, raw)
}

// scanHermesConfig extracts Hermes configuration
func scanHermesConfig(path string, raw map[string]interface{}) (*adapter.DiscoveredAgent, error) {
	agent := &adapter.DiscoveredAgent{
		Name:       "hermes",
		Framework:  "hermes",
		Host:       "127.0.0.1",
		Port:       0, // Hermes doesn't have a single gateway port
		ConfigPath: path,
	}

	// Try to extract agent name if configured
	if name, ok := raw["agent_name"].(string); ok && name != "" {
		agent.Name = name
	}

	return agent, nil
}
