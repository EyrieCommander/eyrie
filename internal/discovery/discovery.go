package discovery

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/instance"
)

// Result holds the outcome of a discovery run.
type Result struct {
	Agents []AgentResult
}

type AgentResult struct {
	Agent adapter.DiscoveredAgent
	Alive bool
}

// Run performs agent discovery: scans config files, probes health endpoints,
// and returns all discovered agents with their liveness status.
// Stored tokens from ~/.eyrie/tokens.json are applied automatically.
func Run(ctx context.Context, cfg config.Config) Result {
	var result Result

	// Stage 1: Scan config files (legacy agents from standard paths)
	discovered := scanConfigFiles(cfg.Discovery.ConfigPaths)

	// Stage 1b: Scan provisioned instances from ~/.eyrie/instances/
	discovered = append(discovered, scanInstances()...)

	// Include manually configured agents
	for _, m := range cfg.Agents {
		host, port := parseURL(m.URL)
		discovered = append(discovered, adapter.DiscoveredAgent{
			Name:      m.Name,
			Framework: m.Framework,
			Host:      host,
			Port:      port,
			Token:     m.Token,
		})
	}

	// Stage 2: Apply stored tokens for agents that don't have one
	if store, err := config.NewTokenStore(); err == nil {
		for i := range discovered {
			if discovered[i].Token == "" {
				if tok := store.Get(discovered[i].Name); tok != "" {
					discovered[i].Token = tok
				}
			}
		}
	}

	// Stage 3: Probe health endpoints
	instStore, _ := instance.NewStore()
	for _, agent := range discovered {
		alive := probeHealth(ctx, agent.Framework, agent.Host, agent.Port)
		result.Agents = append(result.Agents, AgentResult{
			Agent: agent,
			Alive: alive,
		})
		// Update instance status based on health probe
		if instStore != nil && agent.InstanceID != "" {
			if alive {
				_ = instStore.UpdateStatus(agent.InstanceID, "running")
			} else if inst, err := instStore.Get(agent.InstanceID); err == nil && inst.Status == "starting" {
				// Keep "starting" — don't downgrade to "stopped" yet, give it time
			}
		}
	}

	return result
}

// scanInstances reads all provisioned instances from ~/.eyrie/instances/
// and returns them as discovered agents, using the instance name instead of
// the hardcoded framework name.
func scanInstances() []adapter.DiscoveredAgent {
	store, err := instance.NewStore()
	if err != nil {
		slog.Debug("failed to open instance store", "error", err)
		return nil
	}

	instances, err := store.List()
	if err != nil {
		slog.Debug("failed to list instances", "error", err)
		return nil
	}

	var agents []adapter.DiscoveredAgent
	for _, inst := range instances {
		// Determine framework from config file extension
		expanded := config.ExpandHome(inst.ConfigPath)
		if _, err := os.Stat(expanded); err != nil {
			continue
		}

		ext := filepath.Ext(expanded)
		var agent *adapter.DiscoveredAgent

		switch ext {
		case ".toml":
			agent, err = scanZeroClawConfig(inst.ConfigPath)
		case ".json":
			agent, err = scanOpenClawConfig(inst.ConfigPath)
		case ".yaml", ".yml":
			agent, err = scanYAMLConfig(inst.ConfigPath)
		default:
			continue
		}

		if err != nil {
			slog.Debug("failed to scan instance config", "instance", inst.Name, "error", err)
			continue
		}
		if agent == nil {
			slog.Debug("scan returned nil agent for instance", "instance", inst.Name)
			continue
		}

		// Override the hardcoded name with the instance name and set instance ID
		agent.Name = inst.Name
		agent.InstanceID = inst.ID
		agents = append(agents, *agent)
	}
	return agents
}

func scanConfigFiles(paths []string) []adapter.DiscoveredAgent {
	var agents []adapter.DiscoveredAgent

	for _, path := range paths {
		expanded := config.ExpandHome(path)

		var agent *adapter.DiscoveredAgent
		var err error

		if strings.HasSuffix(expanded, ".toml") {
			agent, err = scanZeroClawConfig(path)
		} else if strings.HasSuffix(expanded, ".json") {
			agent, err = scanOpenClawConfig(path)
		} else if strings.HasSuffix(expanded, ".yaml") || strings.HasSuffix(expanded, ".yml") {
			agent, err = scanYAMLConfig(path)
		} else {
			slog.Debug("skipping unknown config format", "path", path)
			continue
		}

		if err != nil {
			slog.Debug("failed to scan config", "path", path, "error", err)
			continue
		}

		agents = append(agents, *agent)
	}

	return agents
}

func parseURL(rawURL string) (host string, port int) {
	host = "127.0.0.1"
	port = 0

	// Strip scheme
	u := rawURL
	for _, prefix := range []string{"http://", "https://", "ws://", "wss://"} {
		u = strings.TrimPrefix(u, prefix)
	}

	// Split host:port
	if idx := strings.LastIndex(u, ":"); idx >= 0 {
		host = u[:idx]
		for _, c := range u[idx+1:] {
			if c >= '0' && c <= '9' {
				port = port*10 + int(c-'0')
			} else {
				break
			}
		}
	} else {
		host = u
	}

	return host, port
}

// NewAgent creates an adapter.Agent from a discovered agent.
func NewAgent(d adapter.DiscoveredAgent) adapter.Agent {
	switch d.Framework {
	case "zeroclaw":
		return adapter.NewZeroClawAdapter(
			d.Name, d.Name, d.URL(), d.Token, d.ConfigPath,
		)
	case "openclaw":
		return adapter.NewOpenClawAdapter(
			d.Name, d.Name, d.Host, d.Port, d.Token, d.ConfigPath,
		)
	case "hermes":
		binaryPath := config.ExpandHome("~/.local/bin/hermes")
		return adapter.NewHermesAdapter(
			d.Name, d.Name, d.ConfigPath, binaryPath,
		)
	default:
		return adapter.NewZeroClawAdapter(
			d.Name, d.Name, d.URL(), d.Token, d.ConfigPath,
		)
	}
}
