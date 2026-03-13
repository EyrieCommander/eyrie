package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

)

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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
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
