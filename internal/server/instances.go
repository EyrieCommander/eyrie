package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

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
	if req.AutoStart {
		if autoStartErr = manager.ExecuteWithConfig(r.Context(), inst.Framework, inst.ConfigPath, manager.ActionStart); autoStartErr != nil {
			slog.Warn("auto-start failed", "instance", inst.Name, "error", autoStartErr)
			inst.Status = "error"
		} else {
			// Set to "starting" — discovery will confirm "running" once the agent is alive
			inst.Status = "starting"
		}
		if err := store.UpdateStatus(inst.ID, inst.Status); err != nil {
			slog.Warn("failed to persist auto-start status", "instance", inst.Name, "error", err)
		}
	}

	// Auto-link captain to project: if a captain is created for a project,
	// set the project's orchestrator_id so it appears as the project captain.
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

	if req.AutoStart && inst.Status == "error" {
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
		// Check uniqueness before allowing rename
		exists, err := store.NameExists(update.Name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if exists {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance name \"" + update.Name + "\" already exists"})
			return
		}
		inst.Name = update.Name
	}
	if update.DisplayName != "" {
		inst.DisplayName = update.DisplayName
	}

	if err := store.Save(*inst); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

	if err := manager.ExecuteWithConfig(r.Context(), inst.Framework, inst.ConfigPath, mgrAction); err != nil {
		// Persist the error status so the instance reflects the failure
		inst.Status = "error"
		if updateErr := store.UpdateStatus(inst.ID, "error"); updateErr != nil {
			slog.Warn("failed to persist error status", "instance", inst.ID, "error", updateErr)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update status — use "starting" for start/restart; discovery will confirm "running"
	newStatus := "starting"
	if action == "stop" {
		newStatus = "stopped"
	}
	if err := store.UpdateStatus(id, newStatus); err != nil {
		slog.Warn("failed to persist instance status", "instance", id, "status", newStatus, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}
