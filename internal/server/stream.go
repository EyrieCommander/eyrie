package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func startSSE(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	return flusher, ok
}

// handleAgentLogs streams log entries as Server-Sent Events.
func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx := r.Context()

	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	agent, err := s.findAgent(discoveryCtx, name)
	cancel()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	logCh, err := agent.TailLogs(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	flusher, ok := startSSE(w)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	for entry := range logCh {
		data, _ := json.Marshal(entry)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// handleAgentActivity streams activity events as Server-Sent Events.
func (s *Server) handleAgentActivity(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx := r.Context()

	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	agent, err := s.findAgent(discoveryCtx, name)
	cancel()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	ch, err := agent.TailActivity(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	flusher, ok := startSSE(w)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	for event := range ch {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// handleAgentSessions returns the list of conversation sessions.
func (s *Server) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	agent, err := s.findAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	sessions, err := agent.Sessions(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if sessions == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"supported": false,
			"sessions":  []any{},
		})
		return
	}

	if s.hidden != nil {
		filtered := sessions[:0]
		for _, sess := range sessions {
			if !s.hidden.IsHidden(name, sess.Key) {
				filtered = append(filtered, sess)
			}
		}
		sessions = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"supported": true,
		"sessions":  sessions,
	})
}

// handleAgentMessages returns chat messages for a given session.
func (s *Server) handleAgentMessages(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sessionKey := r.PathValue("session")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	agent, err := s.findAgent(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	messages, err := agent.ChatHistory(ctx, sessionKey, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, messages)
}
