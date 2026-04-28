package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/commander"
)

// commanderAvailable returns false and writes a 503 if the commander
// cannot be initialised. On first call with a nil commander, it attempts
// lazy construction — so if the user added an API key via the Settings
// page since startup, the commander comes online without a restart.
func (s *Server) commanderAvailable(w http.ResponseWriter) bool {
	s.commanderMu.Lock()
	defer s.commanderMu.Unlock()

	if s.commander != nil {
		return true
	}
	// Attempt lazy init — selectProvider re-reads the vault.
	cmd, err := commander.NewDefault(commander.DefaultConfig{
		Projects:      s.projectStore,
		Chat:          s.chatStore,
		Discovery:     s.runDiscovery,
		SendToProject: s.sendCommanderMessageToProject,
		RestartAgent:  s.restartAgentByName,
		Vault:         s.vault,
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "commander unavailable — add an API key for OpenRouter or Anthropic in the setup below",
		})
		return false
	}
	slog.Info("commander: lazy-initialized after API key was added")
	s.commander = cmd
	return true
}

// handleCommanderChat streams a commander turn via SSE.
// Body: {"message": "..."}
//
// WHY detached context: the LLM call + tool execution can take minutes.
// If the user closes the browser mid-stream, we still want to finish
// persisting the conversation. The request context is used only for the
// SSE writes; agent work runs on a separate context with a generous timeout.
func (s *Server) handleCommanderChat(w http.ResponseWriter, r *http.Request) {
	if !s.commanderAvailable(w) {
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Detached context with a 5-minute ceiling. Long enough for complex
	// multi-tool turns; short enough that a truly stuck turn eventually
	// terminates without leaking goroutines.
	turnCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Run the turn synchronously. RunTurn emits events via sse.WriteEvent
	// including the terminal `done` or `error` event, so the handler
	// does not need to emit an additional done.
	if err := s.commander.RunTurn(turnCtx, body.Message, sse); err != nil {
		slog.Warn("commander turn failed", "error", err)
	}
}

// handleCommanderHistory returns the full conversation as a JSON array.
// GET /api/commander/history
func (s *Server) handleCommanderHistory(w http.ResponseWriter, r *http.Request) {
	if !s.commanderAvailable(w) {
		return
	}
	messages, err := s.commander.Store().All()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

// handleCommanderClear wipes the conversation file. Useful for testing
// a fresh conversation without restarting the server.
// DELETE /api/commander/history
func (s *Server) handleCommanderClear(w http.ResponseWriter, r *http.Request) {
	if !s.commanderAvailable(w) {
		return
	}
	if err := s.commander.Store().Clear(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// handleCommanderMemory returns the commander's stored memory entries.
// With ?key=<k>, returns that one entry (404 if missing). Otherwise
// returns the full list. Read-only for now — writes happen through the
// remember/forget tools during a chat turn.
// GET /api/commander/memory
// GET /api/commander/memory?key=<k>
func (s *Server) handleCommanderMemory(w http.ResponseWriter, r *http.Request) {
	if !s.commanderAvailable(w) {
		return
	}
	mem := s.commander.Memory()
	if mem == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory store unavailable"})
		return
	}
	if key := r.URL.Query().Get("key"); key != "" {
		entry, err := mem.Recall(key)
		if err != nil {
			if errors.Is(err, commander.ErrMemoryNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			return
		}
		writeJSON(w, http.StatusOK, entry)
		return
	}
	writeJSON(w, http.StatusOK, mem.List())
}

// handleCommanderConfirm approves or denies a pending Confirm-tier tool
// call. On approve, the tool is executed; on deny, the denial is
// recorded. Either way, the commander runs a continuation turn so the
// LLM can react to the outcome, and that turn is streamed back as SSE.
// POST /api/commander/confirm/{id}
// Body: {"approved": true|false, "reason": "optional"}
//
// WHY a separate endpoint (not approval events over the chat stream):
// the LLM cannot fake this HTTP request — it is purely out-of-band.
// This is the core of the structured confirmation design: the LLM's
// only way to get a Confirm-tier tool executed is for an external
// client to POST approval here.
func (s *Server) handleCommanderConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.commanderAvailable(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pending id is required"})
		return
	}
	var body struct {
		Approved bool   `json:"approved"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Obtain the SSE writer BEFORE approving/denying. If SSE setup fails
	// we haven't mutated any state yet, so the client can retry cleanly.
	sse, sseErr := NewSSEWriter(w)
	if sseErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": sseErr.Error()})
		return
	}

	var (
		pa  *commander.PendingAction
		err error
	)
	if body.Approved {
		pa, err = s.commander.Pending().Approve(id)
	} else {
		pa, err = s.commander.Pending().Deny(id, body.Reason)
	}
	if err != nil {
		// Distinguish "not found / expired" from "already processed".
		// Both are returned as plain errors by the pending store; check
		// the message to pick the right HTTP status.
		status := http.StatusNotFound
		errMsg := err.Error()
		if strings.Contains(errMsg, "already") {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": errMsg})
		return
	}

	// Detached context so the continuation turn survives the client
	// disconnecting; same 5-minute ceiling as the chat endpoint.
	turnCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := s.commander.ResumeAfterConfirm(turnCtx, pa, body.Approved, body.Reason, sse); err != nil {
		slog.Warn("commander resume failed", "error", err)
	}
}
