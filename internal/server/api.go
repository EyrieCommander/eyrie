package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/natalie/eyrie/internal/adapter"
	"github.com/natalie/eyrie/internal/discovery"
	"github.com/natalie/eyrie/internal/manager"
)

type agentJSON struct {
	Name      string                `json:"name"`
	Framework string                `json:"framework"`
	Host      string                `json:"host"`
	Port      int                   `json:"port"`
	Alive     bool                  `json:"alive"`
	Health    *adapter.HealthStatus `json:"health,omitempty"`
	Status    *adapter.AgentStatus  `json:"status,omitempty"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result := s.runDiscovery(ctx)
	agents := make([]agentJSON, 0, len(result.Agents))

	for _, ar := range result.Agents {
		aj := agentJSON{
			Name:      ar.Agent.Name,
			Framework: ar.Agent.Framework,
			Host:      ar.Agent.Host,
			Port:      ar.Agent.Port,
			Alive:     ar.Alive,
		}

		if ar.Alive {
			agent := discovery.NewAgent(ar.Agent)
			if health, err := agent.Health(ctx); err == nil {
				aj.Health = health
			}
			if status, err := agent.Status(ctx); err == nil {
				aj.Status = status
			}
		}

		agents = append(agents, aj)
	}

	writeJSON(w, http.StatusOK, agents)
}

func (s *Server) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	agent, err := s.findAgent(ctx, name)
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

			if err := manager.Execute(ctx, ar.Agent.Framework, la); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

	reply, err := agent.SendMessage(ctx, body.Message, body.SessionKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, reply)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
