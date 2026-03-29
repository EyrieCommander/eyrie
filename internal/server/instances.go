package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
		if autoStartErr = manager.ExecuteWithConfig(startCtx, inst.Framework, inst.ConfigPath, manager.ActionStart); autoStartErr != nil {
			slog.Warn("auto-start failed", "instance", inst.Name, "error", autoStartErr)
			inst.Status = instance.StatusError
		} else {
			// Set to "starting" — discovery will confirm "running" once the agent is alive
			inst.Status = instance.StatusStarting
		}
		if err := store.UpdateStatus(inst.ID, inst.Status); err != nil {
			slog.Warn("failed to persist auto-start status", "instance", inst.Name, "error", err)
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

	// Try to stop first — log but don't block deletion
	if stopErr := manager.ExecuteWithConfig(r.Context(), inst.Framework, inst.ConfigPath, manager.ActionStop); stopErr != nil {
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

	// Inject system message and refresh project context for remaining agents
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

	if err := store.Delete(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
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
	if err := manager.ExecuteWithConfig(execCtx, inst.Framework, inst.ConfigPath, mgrAction); err != nil {
		// Persist the error status so the instance reflects the failure
		inst.Status = instance.StatusError
		if updateErr := store.UpdateStatus(inst.ID, instance.StatusError); updateErr != nil {
			slog.Warn("failed to persist error status", "instance", inst.ID, "error", updateErr)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update status — use "starting" for start/restart; discovery will confirm "running"
	newStatus := instance.StatusStarting
	if action == "stop" {
		newStatus = instance.StatusStopped
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

	writeJSON(w, http.StatusOK, map[string]string{"status": string(newStatus)})
}
