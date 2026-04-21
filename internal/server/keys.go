package server

import (
	"encoding/json"
	"net/http"
)

// --- Key vault API handlers ---
// Manage API keys through the centralized vault. Keys are stored in
// ~/.eyrie/keys.json and injected as env vars when spawning agents.

type keyEntry struct {
	Provider  string `json:"provider"`
	MaskedKey string `json:"masked_key"`
	HasKey    bool   `json:"has_key"`
}

type setKeyRequest struct {
	Key            string `json:"key"`
	SkipValidation bool   `json:"skip_validation"`
}

type setKeyResponse struct {
	Provider  string `json:"provider"`
	MaskedKey string `json:"masked_key"`
	Valid     bool   `json:"valid"`
	Verified  bool   `json:"verified"`
}

type validateResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// handleListKeys returns all stored keys with masked values. Never exposes
// raw key material over the API.
// GET /api/keys
func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys := s.vault.List()
	entries := make([]keyEntry, 0, len(keys))
	for provider, key := range keys {
		entries = append(entries, keyEntry{
			Provider:  provider,
			MaskedKey: maskKey(key),
			HasKey:    true,
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleSetKey stores or updates a key for the given provider. Validates
// against the provider API unless skip_validation is true or the provider
// is unknown/local.
// PUT /api/keys/{provider}
func (s *Server) handleSetKey(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if provider == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider is required"})
		return
	}

	var body setKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid 'key' field"})
		return
	}

	verified := false
	if !body.SkipValidation {
		if err := s.vault.ValidateKey(r.Context(), provider, body.Key); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "key validation failed: " + err.Error(),
			})
			return
		}
		verified = true
	}

	if err := s.vault.Set(provider, body.Key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save key: " + err.Error()})
		return
	}

	// Reset the commander so it re-initializes with the new key.
	// Without this, rotating a key leaves the commander using the old one.
	s.commanderMu.Lock()
	s.commander = nil
	s.commanderMu.Unlock()

	// Valid reflects whether validation actually ran and passed. When the
	// client asked to skip validation we haven't proved anything about the
	// key, so don't claim Valid: true.
	writeJSON(w, http.StatusOK, setKeyResponse{
		Provider:  provider,
		MaskedKey: maskKey(body.Key),
		Valid:     verified,
		Verified:  verified,
	})
}

// handleDeleteKey removes a key for the given provider.
// DELETE /api/keys/{provider}
func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if provider == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider is required"})
		return
	}

	if err := s.vault.Delete(provider); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete key: " + err.Error()})
		return
	}

	// Reset the commander so it re-checks the vault on next request.
	// If the deleted key was the one powering the commander, lazy init
	// will fail and the UI shows the setup card again.
	s.commanderMu.Lock()
	s.commander = nil
	s.commanderMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleValidateKey validates a key without saving it.
// POST /api/keys/{provider}/validate
func (s *Server) handleValidateKey(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if provider == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider is required"})
		return
	}

	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid 'key' field"})
		return
	}

	if err := s.vault.ValidateKey(r.Context(), provider, body.Key); err != nil {
		writeJSON(w, http.StatusOK, validateResponse{Valid: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, validateResponse{Valid: true})
}

// maskKey returns first 4 + *** + last 4 characters. For short keys,
// returns all asterisks. Threshold is 12 so that at least 4 characters
// remain hidden (12 - 4 prefix - 4 suffix = 4 masked).
func maskKey(key string) string {
	if len(key) <= 12 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}
