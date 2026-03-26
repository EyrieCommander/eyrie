package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/manager"
	"github.com/Audacity88/eyrie/internal/registry"
)

type agentJSON struct {
	Name             string                `json:"name"`
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

	// Set auth header from environment if available. Provider APIs typically
	// require authentication to list models.
	if key := providerAPIKey(st.Provider); key != "" {
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleAgentAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	action := r.PathValue("action")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := s.runDiscovery(ctx)
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			var la manager.LifecycleAction
			switch action {
			case "start":
				la = manager.ActionStart
			case "stop":
				la = manager.ActionStop
			case "restart":
				la = manager.ActionRestart
			default:
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action: " + action})
				return
			}

			// Use instance-specific config if this is a provisioned instance
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

	// Find framework
	for _, fw := range reg.Frameworks {
		if fw.ID == frameworkID {
			writeJSON(w, http.StatusOK, fw)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("framework %q not found", frameworkID)})
}

func (s *Server) handleAgentConfigUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Find agent to get config path and format
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

	// Write config atomically
	if err := config.WriteConfigAtomic(configPath, format, body.Config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to write config: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "configuration saved successfully"})
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

// providerAPIKey returns an API key for the given provider from environment
// variables. Returns "" if no key is configured.
func providerAPIKey(provider string) string {
	switch {
	case strings.HasPrefix(provider, "openrouter"), provider == "openrouter":
		return os.Getenv("OPENROUTER_API_KEY")
	case strings.HasPrefix(provider, "openai"), provider == "openai":
		return os.Getenv("OPENAI_API_KEY")
	default:
		return ""
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
