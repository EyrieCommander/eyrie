package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/natalie/eyrie/internal/instance"
	"github.com/natalie/eyrie/internal/manager"
	"github.com/natalie/eyrie/internal/persona"
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
		if strings.Contains(err.Error(), "not found") {
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Auto-start if requested
	if req.AutoStart {
		if err := manager.ExecuteWithConfig(r.Context(), inst.Framework, inst.ConfigPath, manager.ActionStart); err != nil {
			slog.Warn("auto-start failed", "instance", inst.Name, "error", err)
			// Don't fail the creation — just note the error
			inst.Status = "error"
		} else {
			inst.Status = "running"
		}
		if err := store.UpdateStatus(inst.ID, inst.Status); err != nil {
			slog.Warn("failed to persist auto-start status", "instance", inst.Name, "error", err)
		}
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
		if strings.Contains(err.Error(), "not found") {
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

	if update.Name != "" {
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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Try to stop first
	_ = manager.ExecuteWithConfig(r.Context(), inst.Framework, inst.ConfigPath, manager.ActionStop)

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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update status
	newStatus := "running"
	if action == "stop" {
		newStatus = "stopped"
	}
	if err := store.UpdateStatus(id, newStatus); err != nil {
		slog.Warn("failed to persist instance status", "instance", id, "status", newStatus, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}
