package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/manager"
	"github.com/Audacity88/eyrie/internal/registry"
)

type agentJSON struct {
	Name             string                `json:"name"`
	DisplayName      string                `json:"display_name,omitempty"`
	Framework        string                `json:"framework"`
	Host             string                `json:"host"`
	Port             int                   `json:"port"`
	Alive            bool                  `json:"alive"`
	Health           *adapter.HealthStatus `json:"health,omitempty"`
	Status           *adapter.AgentStatus  `json:"status,omitempty"`
	CommanderCapable bool                  `json:"commander_capable"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result := s.runDiscovery(ctx)
	agents := make([]agentJSON, 0, len(result.Agents))

	for _, ar := range result.Agents {
		aj := agentJSON{
			Name:             ar.Agent.Name,
			DisplayName:      ar.Agent.DisplayName,
			Framework:        ar.Agent.Framework,
			Host:             ar.Agent.Host,
			Port:             ar.Agent.Port,
			Alive:            ar.Alive,
			CommanderCapable: discovery.NewAgent(ar.Agent).Capabilities().CommanderCapable,
		}

		agent := discovery.NewAgent(ar.Agent)
		if ar.Alive {
			if health, err := agent.Health(ctx); err == nil {
				aj.Health = health
			}
		}
		if status, err := agent.Status(ctx); err == nil {
			if ar.Alive && status.Provider != "" {
				status.ProviderStatus = adapter.ProbeProvider(ctx, status.Provider)
				// Override to error if the vault doesn't have a key for this
				// provider. The provider endpoint may be reachable (probe says
				// "ok") but the agent can't use it without credentials.
				if status.ProviderStatus == "ok" && s.vault != nil {
					if s.vault.Get(status.Provider) == "" {
						status.ProviderStatus = "error"
					}
				}
			}
			status.InferBusyState()
			aj.Status = status
		}

		agents = append(agents, aj)
	}

	writeJSON(w, http.StatusOK, agents)
}

// handleAgentModels returns available models from the agent's LLM provider.
// It reads the provider URL from the agent's config and queries its /v1/models endpoint.
func (s *Server) handleAgentModels(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	agent, err := s.findAgentAnyState(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	st, err := agent.Status(ctx)
	if err != nil || st == nil || st.Provider == "" {
		writeJSON(w, http.StatusOK, []string{})
		return
	}

	// Extract base URL from provider string.
	// Formats: "custom:http://host:port/v1" or "openrouter" (named provider).
	providerURL := ""
	if after, ok := strings.CutPrefix(st.Provider, "custom:"); ok {
		providerURL = strings.TrimRight(after, "/")
		// Remove trailing /v1 if present — we'll add /v1/models
		providerURL = strings.TrimSuffix(providerURL, "/v1")
	} else {
		// Named providers — map to their known API base
		switch st.Provider {
		case "openrouter":
			providerURL = "https://openrouter.ai/api"
		case "openai":
			providerURL = "https://api.openai.com"
		case "anthropic":
			// Anthropic doesn't have a /v1/models endpoint
			writeJSON(w, http.StatusOK, []string{})
			return
		default:
			writeJSON(w, http.StatusOK, []string{})
			return
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", providerURL+"/v1/models", nil)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}

	// Set auth header from vault if available. Provider APIs typically
	// require authentication to list models.
	if key := s.providerAPIKey(st.Provider); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	agent, err := s.findAgentAnyState(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	cfg, err := agent.Config(ctx)
	if err != nil {
		writeAdapterError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

func parseLifecycleAction(action string) (manager.LifecycleAction, bool) {
	switch action {
	case "start":
		return manager.ActionStart, true
	case "stop":
		return manager.ActionStop, true
	case "restart":
		return manager.ActionRestart, true
	default:
		return "", false
	}
}

func (s *Server) handleAgentAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	action := r.PathValue("action")

	la, ok := parseLifecycleAction(action)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action: " + action})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := s.runDiscovery(ctx)
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			var execErr error
			if ar.Agent.ConfigPath != "" && ar.Agent.InstanceID != "" {
				execErr = manager.ExecuteWithConfig(ctx, ar.Agent.Framework, ar.Agent.ConfigPath, la)
			} else {
				execErr = manager.Execute(ctx, ar.Agent.Framework, la)
			}
			if execErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": execErr.Error()})
				return
			}

			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
	}

	// Agent not in discovery — might be a crashed provisioned instance.
	// Fall back to the instance store so the user can restart it.
	if inst := s.findInstanceByName(name); inst != nil {
		var execErr error
		if inst.Framework == adapter.FrameworkEmbedded {
			agent, findErr := s.findAgentAnyState(ctx, inst.Name)
			if findErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": findErr.Error()})
				return
			}
			switch action {
			case "start":
				execErr = agent.Start(ctx)
			case "stop":
				execErr = agent.Stop(ctx)
			case "restart":
				execErr = agent.Restart(ctx)
			}
		} else {
			var env []string
			if s.vault != nil {
				env = s.vault.EnvSlice()
			}
			execErr = manager.ExecuteWithConfigEnv(ctx, inst.Framework, inst.ConfigPath, la, env)
		}
		if execErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": execErr.Error()})
			return
		}
		newStatus := instance.StatusStarting
		if action == "stop" {
			newStatus = instance.StatusStopped
		}
		_ = s.instanceStore.UpdateStatus(inst.ID, newStatus)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("agent %q not found", name)})
}

func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	agent, err := s.findAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	var body struct {
		Message    string `json:"message"`
		SessionKey string `json:"session_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid 'message' field"})
		return
	}

	eventCh, err := agent.StreamMessage(ctx, body.Message, body.SessionKey)
	if err != nil {
		writeAdapterError(w, err)
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for ev := range eventCh {
		sse.WriteEvent(ev)
	}

	// If the client aborted (stop button), ask the framework to discard
	// the in-flight response so the agent doesn't have stale context.
	if ctx.Err() != nil {
		intCtx, intCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = agent.Interrupt(intCtx, body.SessionKey)
		intCancel()
	}
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	agent, err := s.findAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	sess, err := agent.CreateSession(ctx, body.Name)
	if err != nil {
		writeAdapterError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleResetSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sessionKey := r.PathValue("session")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	agent, err := s.findAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if err := agent.ResetSession(ctx, sessionKey); err != nil {
		writeAdapterError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sessionKey := r.PathValue("session")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	agent, err := s.findAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if err := agent.DeleteSession(ctx, sessionKey); err != nil {
		writeAdapterError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SessionDestroyer is optionally implemented by adapters that support
// fully removing a session (transcript + registry entry).
type SessionDestroyer interface {
	DestroySession(ctx context.Context, sessionKey string) error
}

func (s *Server) handleDestroySession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sessionKey := r.PathValue("session")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	agent, err := s.findAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	destroyer, ok := agent.(SessionDestroyer)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "this agent does not support session destruction"})
		return
	}

	if err := destroyer.DestroySession(ctx, sessionKey); err != nil {
		writeAdapterError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleHideSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sessionKey := r.PathValue("session")

	if s.hidden == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hidden store not available"})
		return
	}

	if err := s.hidden.Hide(name, sessionKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUnhideSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sessionKey := r.PathValue("session")

	if s.hidden == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hidden store not available"})
		return
	}

	if err := s.hidden.Unhide(name, sessionKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleFrameworkDetail(w http.ResponseWriter, r *http.Request) {
	frameworkID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Fetch registry (uses default URL from registry package)
	client, err := registry.NewClient("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create registry client"})
		return
	}

	reg, err := client.Fetch(ctx, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch registry"})
		return
	}

	// Find framework and include install status + version
	for _, fw := range reg.Frameworks {
		if fw.ID == frameworkID {
			installed, configured := frameworkStatus(fw)
			var version string
			if installed {
				version = frameworkVersion(fw)
			}
			writeJSON(w, http.StatusOK, frameworkWithStatus{
				Framework:     fw,
				Installed:     installed,
				Configured:    configured,
				Version:       version,
				VersionStatus: registry.ComputeVersionStatus(version, fw),
			})
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("framework %q not found", frameworkID)})
}

func (s *Server) handleAgentConfigUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Find agent to get config path, format, and liveness
	result := s.runDiscovery(ctx)
	var discoveredAgent *adapter.DiscoveredAgent
	var agentAlive bool
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			discoveredAgent = &ar.Agent
			agentAlive = ar.Alive
			break
		}
	}

	if discoveredAgent == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("agent %q not found", name)})
		return
	}

	// Parse request body (could be raw string or structured data)
	var body struct {
		Config interface{} `json:"config"` // Can be string (raw) or object (structured)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if body.Config == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'config' field"})
		return
	}

	// Get config format from discovered agent
	configPath := config.ExpandHome(discoveredAgent.ConfigPath)

	// Determine format from file extension if not provided
	format := discoveredAgent.Framework
	if discoveredAgent.Framework == "zeroclaw" {
		format = "toml"
	} else if discoveredAgent.Framework == "openclaw" {
		format = "json"
	} else if discoveredAgent.Framework == "hermes" {
		format = "yaml"
	}

	// If config is a raw string (from the text editor), validate format
	// before writing to prevent saving malformed config that could break
	// the agent on next start.
	if rawStr, ok := body.Config.(string); ok {
		if valErr := config.ValidateRawFormat(format, rawStr); valErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid %s config: %v", format, valErr)})
			return
		}

		// When the agent is online, its GET /api/config response masks
		// sensitive fields (api_key becomes "***MASKED***"). Proxy saves
		// through the agent's own PUT /api/config so it can restore the
		// real values from memory. Writing masked values directly to disk
		// would replace actual secrets with the literal mask string.
		if agentAlive && discoveredAgent.Framework == "zeroclaw" {
			if err := proxyConfigSave(ctx, discoveredAgent, rawStr); err == nil {
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "configuration saved successfully"})
				return
			}
			// Proxy failed (agent went offline between discovery and save).
			// Fall through to direct disk write with safety check below.
		}

		// Safety net: reject direct disk writes that contain masked
		// secret placeholders. These come from the agent's API response
		// and would corrupt the config if written to disk.
		if containsMaskedSecrets(rawStr) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "config contains masked secret placeholders (***MASKED***) that would overwrite real API keys — restart the agent and try again",
			})
			return
		}

		if err := config.WriteRawAtomic(configPath, rawStr); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to write config: %v", err)})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "configuration saved successfully"})
		return
	}

	// Config is a parsed object (from inline field editors).
	// JSON decoding converts all numbers to float64, which corrupts
	// integer fields when re-encoded to TOML (e.g., port = 42617.0).
	// Fix by converting whole-number floats back to int64.
	config.CoerceJSONNumbers(body.Config)

	// Safety net: check the structured config for masked secret placeholders
	// before writing to disk.
	if configJSON, err := json.Marshal(body.Config); err == nil && containsMaskedSecrets(string(configJSON)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "config contains masked secret placeholders (***MASKED***) that would overwrite real API keys — restart the agent and try again",
		})
		return
	}

	// Write config atomically
	if err := config.WriteConfigAtomic(configPath, format, body.Config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to write config: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "configuration saved successfully"})
}

// proxyConfigSave forwards a config save to the agent's own PUT /api/config
// endpoint. This lets the agent restore masked secret placeholders
// (***MASKED***) with the real values from its in-memory config before
// writing to disk.
func proxyConfigSave(ctx context.Context, agent *adapter.DiscoveredAgent, rawConfig string) error {
	apiURL := agent.URL() + "/api/config"
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, bytes.NewReader([]byte(rawConfig)))
	if err != nil {
		return fmt.Errorf("creating proxy request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if agent.Token != "" {
		req.Header.Set("Authorization", "Bearer "+agent.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxy request to %s: %w", apiURL, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent returned %d", resp.StatusCode)
	}
	return nil
}

// containsMaskedSecrets checks whether a config string contains masked secret
// placeholders that would corrupt the config if saved to disk. These come from
// agent API responses that mask sensitive fields for display.
func containsMaskedSecrets(s string) bool {
	return strings.Contains(s, "***MASKED***")
}

func (s *Server) handleAgentConfigValidate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Find agent
	result := s.runDiscovery(ctx)
	var discoveredAgent *adapter.DiscoveredAgent
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			discoveredAgent = &ar.Agent
			break
		}
	}

	if discoveredAgent == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("agent %q not found", name)})
		return
	}

	// Parse request body
	var body struct {
		Config interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if body.Config == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'config' field"})
		return
	}

	// Determine format
	format := "toml"
	if discoveredAgent.Framework == "openclaw" {
		format = "json"
	} else if discoveredAgent.Framework == "hermes" {
		format = "yaml"
	}

	// Create temp file for validation
	tempFile, err := os.CreateTemp("", fmt.Sprintf("eyrie-validate-%s-*.%s", name, format))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
		return
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	// Write config to temp file
	if err := config.WriteConfigAtomic(tempPath, format, body.Config); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"valid": false,
			"error": fmt.Sprintf("invalid config format: %v", err),
		})
		return
	}

	// For now, just return success if format is valid
	// TODO: Actually test-start the agent with temp config
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":   true,
		"message": "configuration is valid",
	})
}

func (s *Server) findAgent(ctx context.Context, name string) (adapter.Agent, error) {
	result := s.runDiscovery(ctx)
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			if !ar.Alive {
				return nil, fmt.Errorf("agent %q is not running", name)
			}
			return discovery.NewAgent(ar.Agent), nil
		}
	}
	return nil, fmt.Errorf("agent %q not found", name)
}

// findAgentAnyState returns an adapter for the named agent regardless of
// whether it is currently running. This is used by endpoints that can serve
// data from persistent sources (log files, config files, chat history).
func (s *Server) findAgentAnyState(ctx context.Context, name string) (adapter.Agent, error) {
	result := s.runDiscovery(ctx)
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			return discovery.NewAgent(ar.Agent), nil
		}
	}
	return nil, fmt.Errorf("agent %q not found", name)
}

// findDiscoveredAgent returns the raw DiscoveredAgent for the named agent.
func (s *Server) findDiscoveredAgent(ctx context.Context, name string) (*adapter.DiscoveredAgent, error) {
	result := s.runDiscovery(ctx)
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			return &ar.Agent, nil
		}
	}
	return nil, fmt.Errorf("agent %q not found", name)
}

// findInstanceByName looks up a provisioned instance by its name (slug).
// Returns nil if no match is found. Used as a fallback when discovery
// doesn't find the agent (e.g., it crashed and isn't responding to health probes).
func (s *Server) findInstanceByName(name string) *instance.Instance {
	if s.instanceStore == nil {
		return nil
	}
	instances, err := s.instanceStore.List()
	if err != nil {
		return nil
	}
	for i := range instances {
		if instances[i].Name == name {
			return &instances[i]
		}
	}
	return nil
}

func (s *Server) handleUpdateDisplayName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	// Sanitize: keep only alphanumeric, spaces, hyphens, and underscores
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, body.DisplayName)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "display name must contain at least one alphanumeric character"})
		return
	}

	agent, err := s.findDiscoveredAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Update IDENTITY.md in the agent's workspace
	if err := discovery.WriteIdentityName(agent.ConfigPath, cleaned); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"display_name": cleaned})
}

// providerAPIKey returns an API key for the given provider. The vault checks
// env vars first, then the on-disk store. Normalizes composite provider
// strings like "custom:http://..." to just the prefix.
func (s *Server) providerAPIKey(provider string) string {
	// Normalize: "custom:http://..." → "" (no key for custom endpoints)
	// "openrouter" or "openrouter:something" → "openrouter"
	normalized := provider
	if idx := strings.Index(provider, ":"); idx > 0 {
		prefix := provider[:idx]
		if prefix == "custom" {
			return ""
		}
		normalized = prefix
	}
	return s.vault.Get(normalized)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
