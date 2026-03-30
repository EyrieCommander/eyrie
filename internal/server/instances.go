package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/manager"
	"github.com/Audacity88/eyrie/internal/persona"
	"github.com/Audacity88/eyrie/internal/project"
)

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	store, err := instance.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	instances, err := store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if instances == nil {
		instances = []instance.Instance{}
	}
	writeJSON(w, http.StatusOK, instances)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store, err := instance.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	inst, err := store.Get(id)
	if err != nil {
		if errors.Is(err, instance.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req instance.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	store, err := instance.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Look up persona if specified
	var pers *persona.Persona
	if req.PersonaID != "" {
		ps, err := persona.NewStore()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open persona store: " + err.Error()})
			return
		}
		pers, err = ps.Get(req.PersonaID)
		if err != nil {
			if errors.Is(err, persona.ErrNotFound) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load persona: " + err.Error()})
			}
			return
		}
	}

	// Populate project context on the request so the provisioner can
	// include project info in identity templates (TemplateContext).
	if req.ProjectID != "" {
		if projStore, pErr := project.NewStore(); pErr == nil {
			if proj, pErr := projStore.Get(req.ProjectID); pErr == nil {
				req.ProjectName = proj.Name
				req.ProjectGoal = proj.Goal
				req.ProjectDescription = proj.Description
			}
		}
	}

	provisioner := instance.NewProvisioner(store)
	inst, err := provisioner.Provision(req, pers)
	if err != nil {
		// Validation errors get 400; others get 500
		if errors.Is(err, instance.ErrNameExists) ||
			errors.Is(err, instance.ErrRequiredField) ||
			errors.Is(err, instance.ErrUnsupportedFramework) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		} else {
			slog.Error("instance provisioning failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to provision instance"})
		}
		return
	}

	// Auto-start if requested
	var autoStartErr error
	if req.AutoStart != nil && *req.AutoStart {
		startCtx, startCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer startCancel()

		if inst.Framework == adapter.FrameworkEmbedded {
			// Embedded agents are started by calling the adapter directly.
			// Trigger a discovery cycle first so the adapter is created and cached.
			agent, findErr := s.findAgentAnyState(startCtx, inst.Name)
			if findErr != nil {
				autoStartErr = fmt.Errorf("finding embedded adapter: %w", findErr)
			} else {
				autoStartErr = agent.Start(startCtx)
			}
		} else {
			autoStartErr = manager.ExecuteWithConfig(startCtx, inst.Framework, inst.ConfigPath, manager.ActionStart)
		}

		if autoStartErr != nil {
			slog.Warn("auto-start failed", "instance", inst.Name, "error", autoStartErr)
			inst.Status = instance.StatusError
		} else if inst.Framework == adapter.FrameworkEmbedded {
			// Embedded Start() is synchronous — agent is running now
			inst.Status = instance.StatusRunning
		} else {
			// External frameworks: set to "starting", discovery will confirm "running"
			inst.Status = instance.StatusStarting
		}
		if err := store.UpdateStatus(inst.ID, inst.Status); err != nil {
			slog.Warn("failed to persist auto-start status", "instance", inst.Name, "error", err)
		}

		// Auto-pair ZeroClaw instances so WebSocket chat works
		if inst.Framework == adapter.FrameworkZeroClaw && autoStartErr == nil {
			go autoPairZeroClaw(inst.Name, inst.Port)
		}
	}

	// Auto-link captain to project: if a captain is created for a project,
	// set the project's orchestrator_id so it appears as the project captain.
	// No separate briefing needed — role instructions are injected inline
	// when the project chat starts.
	if req.HierarchyRole == "captain" && req.ProjectID != "" {
		if projStore, pErr := project.NewStore(); pErr == nil {
			if proj, pErr := projStore.Get(req.ProjectID); pErr == nil {
				proj.OrchestratorID = inst.ID
				if pErr := projStore.Save(*proj); pErr != nil {
					slog.Warn("failed to auto-link captain to project", "project", req.ProjectID, "error", pErr)
				}
			}
		}
	}

	// Auto-add talon to project roster (mirrors the captain auto-link above)
	if req.HierarchyRole == "talon" && req.ProjectID != "" {
		if projStore, pErr := project.NewStore(); pErr == nil {
			if pErr := projStore.AddAgent(req.ProjectID, inst.ID); pErr != nil {
				slog.Warn("failed to auto-add talon to project", "project", req.ProjectID, "error", pErr)
			}
		}
	}

	// Write/refresh PROJECT.md for all project agents and inject a system
	// message so the chat log records the structural change.
	if inst.ProjectID != "" {
		s.refreshProjectContext(inst.ProjectID)
		creator := inst.CreatedBy
		if creator == "" {
			creator = "user"
		}
		injectSystemMessage(inst.ProjectID, fmt.Sprintf("%s created %s (%s, :%d)", creator, inst.Name, inst.Framework, inst.Port))
	}

	// Publish event for real-time UI updates
	if inst.ProjectID != "" {
		s.events.Publish(ProjectEvent{
			Type:      "agent_created",
			ProjectID: inst.ProjectID,
			Agent:     inst.Name,
			AgentRole: string(inst.HierarchyRole),
			Detail:    fmt.Sprintf("%s created (%s, :%d)", inst.DisplayName, inst.Framework, inst.Port),
			Data:      inst,
			Timestamp: time.Now(),
		})
	}

	if req.AutoStart != nil && *req.AutoStart && inst.Status == instance.StatusError {
		writeJSON(w, http.StatusCreated, map[string]any{
			"instance": inst,
			"warning":  fmt.Sprintf("auto-start failed: %v", autoStartErr),
		})
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store, err := instance.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	inst, err := store.Get(id)
	if err != nil {
		if errors.Is(err, instance.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	var update struct {
		Name        string `json:"name,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if update.Name != "" && update.Name != inst.Name {
		inst.Name = update.Name
	}
	if update.DisplayName != "" {
		inst.DisplayName = update.DisplayName
	}

	if err := store.Save(*inst); err != nil {
		if errors.Is(err, instance.ErrNameExists) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance name \"" + inst.Name + "\" already exists"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store, err := instance.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	inst, err := store.Get(id)
	if err != nil {
		if errors.Is(err, instance.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			slog.Error("failed to get instance for deletion", "id", id, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get instance"})
		}
		return
	}

	// Try to stop first — use detached context so stop completes even if
	// the HTTP client disconnects during a slow shutdown.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()
	if stopErr := manager.ExecuteWithConfig(stopCtx, inst.Framework, inst.ConfigPath, manager.ActionStop); stopErr != nil {
		slog.Warn("failed to stop instance before deletion", "instance", inst.Name, "framework", inst.Framework, "error", stopErr)
	}

	// Clear any project references to this instance to avoid dangling refs
	if projStore, pErr := project.NewStore(); pErr == nil {
		if projects, pErr := projStore.List(); pErr == nil {
			for _, proj := range projects {
				if proj.OrchestratorID == id {
					proj.OrchestratorID = ""
					if pErr := projStore.Save(proj); pErr != nil {
						slog.Warn("failed to clear project orchestrator ref", "project", proj.ID, "error", pErr)
					}
				}
			}
		}
		// Remove from project agent roster if this was a talon
		if inst.ProjectID != "" {
			_ = projStore.RemoveAgent(inst.ProjectID, id)
		}
	}

	if err := store.Delete(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Publish events only after successful deletion
	if inst.ProjectID != "" {
		injectSystemMessage(inst.ProjectID, fmt.Sprintf("%s removed from project", inst.Name))
		s.refreshProjectContext(inst.ProjectID)
		s.events.Publish(ProjectEvent{
			Type:      "agent_removed",
			ProjectID: inst.ProjectID,
			Agent:     inst.Name,
			AgentRole: string(inst.HierarchyRole),
			Detail:    fmt.Sprintf("%s removed", inst.Name),
			Timestamp: time.Now(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleInstanceAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	action := r.PathValue("action")

	store, err := instance.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	inst, err := store.Get(id)
	if err != nil {
		if errors.Is(err, instance.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	var mgrAction manager.LifecycleAction
	switch action {
	case "start":
		mgrAction = manager.ActionStart
	case "stop":
		mgrAction = manager.ActionStop
	case "restart":
		mgrAction = manager.ActionRestart
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action: " + action})
		return
	}

	execCtx, execCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer execCancel()

	if inst.Framework == adapter.FrameworkEmbedded {
		// Embedded agents run in-process — delegate to the adapter directly
		// rather than calling the manager (which has no external process to manage).
		agent, findErr := s.findAgentAnyState(execCtx, inst.Name)
		if findErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("finding embedded adapter: %v", findErr)})
			return
		}
		var adapterErr error
		switch action {
		case "start":
			adapterErr = agent.Start(execCtx)
		case "stop":
			adapterErr = agent.Stop(execCtx)
		case "restart":
			adapterErr = agent.Restart(execCtx)
		}
		if adapterErr != nil {
			inst.Status = instance.StatusError
			if updateErr := store.UpdateStatus(inst.ID, instance.StatusError); updateErr != nil {
				slog.Warn("failed to persist error status", "instance", inst.ID, "error", updateErr)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": adapterErr.Error()})
			return
		}
	} else {
		if err := manager.ExecuteWithConfig(execCtx, inst.Framework, inst.ConfigPath, mgrAction); err != nil {
			// Persist the error status so the instance reflects the failure
			inst.Status = instance.StatusError
			if updateErr := store.UpdateStatus(inst.ID, instance.StatusError); updateErr != nil {
				slog.Warn("failed to persist error status", "instance", inst.ID, "error", updateErr)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	// Update status — for embedded agents, Start() is synchronous so we can
	// set "running" directly. For external frameworks, use "starting" and
	// let discovery confirm "running" once the process is alive.
	newStatus := instance.StatusStarting
	if action == "stop" {
		newStatus = instance.StatusStopped
	}
	if inst.Framework == adapter.FrameworkEmbedded && action != "stop" {
		newStatus = instance.StatusRunning
	}
	if err := store.UpdateStatus(id, newStatus); err != nil {
		slog.Warn("failed to persist instance status", "instance", id, "status", newStatus, "error", err)
	}

	// Publish event for real-time UI updates
	if inst.ProjectID != "" {
		eventType := "agent_started"
		if action == "stop" {
			eventType = "agent_stopped"
		}
		s.events.Publish(ProjectEvent{
			Type:      eventType,
			ProjectID: inst.ProjectID,
			Agent:     inst.Name,
			AgentRole: string(inst.HierarchyRole),
			Detail:    fmt.Sprintf("%s %s", inst.Name, action),
			Timestamp: time.Now(),
		})
	}

	// Auto-pair ZeroClaw instances after start so WebSocket chat works.
	// Runs in background — doesn't block the HTTP response.
	if inst.Framework == adapter.FrameworkZeroClaw && action == "start" {
		go autoPairZeroClaw(inst.Name, inst.Port)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": string(newStatus)})
}

// autoPairZeroClaw waits for a ZeroClaw gateway to become ready, fetches its
// pairing code, pairs, and stores the token. This gives provisioned instances
// authenticated WebSocket access for project chat streaming.
func autoPairZeroClaw(name string, port int) {
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// WHY short-timeout client: http.DefaultClient has no timeout. If the
	// gateway hangs (accepts TCP but never responds), the goroutine blocks
	// indefinitely. 5s is generous for localhost health checks.
	client := &http.Client{Timeout: 5 * time.Second}

	// Wait for gateway to be ready (up to 15 seconds)
	var ready bool
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		resp, err := client.Get(baseURL + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 || resp.StatusCode == 401 {
				ready = true
				break
			}
		}
	}
	if !ready {
		slog.Warn("auto-pair: gateway not ready after 15s", "agent", name, "port", port)
		return
	}

	// Open token store once — used for both the "already paired?" check and
	// the final token save, avoiding a redundant second open.
	ts, err := config.NewTokenStore()
	if err != nil {
		slog.Warn("auto-pair: failed to open token store", "agent", name, "error", err)
		return
	}

	// Skip if already paired (another goroutine or manual pair may have beaten us)
	if tok := ts.Get(name); tok != "" {
		slog.Info("auto-pair: already paired, skipping", "agent", name)
		return
	}

	// Fetch pairing code from admin endpoint
	resp, err := client.Get(baseURL + "/admin/paircode")
	if err != nil {
		slog.Warn("auto-pair: failed to fetch paircode", "agent", name, "error", err)
		return
	}
	defer resp.Body.Close()

	var pcResp struct {
		PairingCode     *string `json:"pairing_code"`
		PairingRequired bool    `json:"pairing_required"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pcResp); err != nil {
		slog.Warn("auto-pair: failed to decode paircode response", "agent", name, "error", err)
		return
	}

	if pcResp.PairingCode == nil || *pcResp.PairingCode == "" {
		slog.Info("auto-pair: no pairing code available, skipping", "agent", name)
		return
	}

	// Pair with the gateway
	pairReq, _ := http.NewRequest("POST", baseURL+"/pair", nil)
	pairReq.Header.Set("X-Pairing-Code", *pcResp.PairingCode)
	pairResp, err := client.Do(pairReq)
	if err != nil {
		slog.Warn("auto-pair: pair request failed", "agent", name, "error", err)
		return
	}
	defer pairResp.Body.Close()

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResp.Body).Decode(&tokenResp); err != nil || tokenResp.Token == "" {
		slog.Warn("auto-pair: no token in pair response", "agent", name, "status", pairResp.StatusCode)
		return
	}

	// Store token (reusing the already-opened store)
	if err := ts.Set(name, tokenResp.Token); err != nil {
		slog.Warn("auto-pair: failed to save token", "agent", name, "error", err)
		return
	}

	slog.Info("auto-pair: success", "agent", name, "port", port)
}
