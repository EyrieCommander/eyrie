package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// handleCommanderChat streams a commander turn via SSE.
// Body: {"message": "..."}
//
// WHY detached context: the LLM call + tool execution can take minutes.
// If the user closes the browser mid-stream, we still want to finish
// persisting the conversation. The request context is used only for the
// SSE writes; agent work runs on a separate context with a generous timeout.
func (s *Server) handleCommanderChat(w http.ResponseWriter, r *http.Request) {
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
	messages, err := s.commander.Store().All()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if messages == nil {
		messages = nil // Explicit empty response for new conversation
	}
	writeJSON(w, http.StatusOK, messages)
}

// handleCommanderClear wipes the conversation file. Useful for testing
// a fresh conversation without restarting the server.
// DELETE /api/commander/history
func (s *Server) handleCommanderClear(w http.ResponseWriter, r *http.Request) {
	if err := s.commander.Store().Clear(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
