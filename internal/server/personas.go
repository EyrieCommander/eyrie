package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Audacity88/eyrie/internal/persona"
)

func (s *Server) handleListPersonas(w http.ResponseWriter, r *http.Request) {
	catalog, err := persona.NewCatalogClient("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create catalog client"})
		return
	}

	reg, err := catalog.Fetch(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to fetch persona catalog: %v", err)})
		return
	}

	// Merge installed status from store
	store, err := persona.NewStore()
	if err != nil {
		slog.Warn("failed to create persona store", "error", err)
	}
	if err == nil {
		installed, listErr := store.List()
		if listErr != nil {
			slog.Warn("failed to list installed personas", "error", listErr)
		}
		installedMap := make(map[string]persona.Persona, len(installed))
		for _, p := range installed {
			installedMap[p.ID] = p
		}
		for i := range reg.Personas {
			if inst, ok := installedMap[reg.Personas[i].ID]; ok {
				reg.Personas[i].Installed = true
				reg.Personas[i].AgentName = inst.AgentName
			}
		}
	}

	writeJSON(w, http.StatusOK, reg.Personas)
}

func (s *Server) handleGetPersona(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona id is required"})
		return
	}

	// Check store first (installed personas may be customized)
	store, err := persona.NewStore()
	if err != nil {
		slog.Warn("failed to create persona store", "id", id, "error", err)
	}
	if err == nil {
		p, storeErr := store.Get(id)
		if storeErr == nil {
			writeJSON(w, http.StatusOK, p)
			return
		}
		if !errors.Is(storeErr, persona.ErrNotFound) {
			slog.Error("failed to read persona from store", "id", id, "error", storeErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read persona"})
			return
		}
	}

	// Fall back to catalog
	catalog, err := persona.NewCatalogClient("")
	if err != nil {
		slog.Error("failed to initialize persona catalog", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to initialize persona catalog"})
		return
	}
	reg, err := catalog.Fetch(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch catalog"})
		return
	}
	for _, p := range reg.Personas {
		if p.ID == id {
			writeJSON(w, http.StatusOK, p)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("persona %q not found", id)})
}

func (s *Server) handleInstallPersona(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	var body struct {
		PersonaID string `json:"persona_id"`
		Force     bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Debug("malformed persona install request", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed JSON"})
		return
	}
	if body.PersonaID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona_id is required"})
		return
	}

	// Find persona in catalog
	catalog, err := persona.NewCatalogClient("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create catalog client"})
		return
	}
	reg, err := catalog.Fetch(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch catalog"})
		return
	}

	var found *persona.Persona
	for i := range reg.Personas {
		if reg.Personas[i].ID == body.PersonaID {
			found = &reg.Personas[i]
			break
		}
	}
	if found == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("persona %q not found in catalog", body.PersonaID)})
		return
	}

	// Save to store
	store, err := persona.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open persona store"})
		return
	}

	// Check if already installed — return existing persona unless force is set
	existing, existErr := store.Get(found.ID)
	if existErr == nil && existing.Installed && !body.Force {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	if existErr != nil && !errors.Is(existErr, persona.ErrNotFound) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to check installed persona: %v", existErr)})
		return
	}

	found.Installed = true
	if err := store.Save(*found); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save persona: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, found)
}

func (s *Server) handleUpdatePersona(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona id is required"})
		return
	}

	store, err := persona.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open persona store"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	var patch persona.Persona
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid persona data"})
		return
	}

	// Fetch existing persona so partial payloads don't zero-out fields
	existing, err := store.Get(id)
	if err != nil {
		if errors.Is(err, persona.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("persona %q not found", id)})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check persona"})
		}
		return
	}

	// Merge non-zero fields from the patch into the existing persona
	if patch.Name != "" {
		existing.Name = patch.Name
	}
	if patch.Role != "" {
		existing.Role = patch.Role
	}
	if patch.Description != "" {
		existing.Description = patch.Description
	}
	if patch.Icon != "" {
		existing.Icon = patch.Icon
	}
	if patch.Category != "" {
		existing.Category = patch.Category
	}
	if patch.PreferredModel != "" {
		existing.PreferredModel = patch.PreferredModel
	}
	if patch.PreferredFramework != "" {
		existing.PreferredFramework = patch.PreferredFramework
	}
	if patch.SystemPrompt != "" {
		existing.SystemPrompt = patch.SystemPrompt
	}
	if len(patch.IdentityTemplate) > 0 {
		existing.IdentityTemplate = patch.IdentityTemplate
	}
	if len(patch.MemorySeeds) > 0 {
		existing.MemorySeeds = patch.MemorySeeds
	}
	if patch.HierarchyRole != "" {
		existing.HierarchyRole = patch.HierarchyRole
	}
	if patch.AgentName != "" {
		existing.AgentName = patch.AgentName
	}
	existing.ID = id
	existing.Installed = true

	if err := store.Save(*existing); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save persona: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeletePersona(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona id is required"})
		return
	}

	store, err := persona.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open persona store"})
		return
	}

	if err := store.Delete(id); err != nil {
		if errors.Is(err, persona.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("persona %q not found", id)})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to delete persona: %v", err)})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListCategories(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, persona.Categories())
}
