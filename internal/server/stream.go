package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// SSEWriter wraps an http.ResponseWriter for Server-Sent Events.
// It handles JSON marshaling, framing, and flushing in a single call.
type SSEWriter struct {
	w    io.Writer
	f    http.Flusher
	sent bool
}

// NewSSEWriter validates flushing support, sets SSE headers, and returns a writer.
// The flusher check happens before setting headers so callers can safely fall
// back to writeJSON on error without stale SSE headers in the response.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &SSEWriter{w: w, f: flusher}, nil
}

// nopFlusher satisfies http.Flusher as a no-op. Used only by
// NewDiscardSSEWriter so the orchestrator can be invoked without a
// real HTTP response (e.g. fire-and-forget from the commander).
type nopFlusher struct{}

func (nopFlusher) Flush() {}

// NewDiscardSSEWriter returns an SSEWriter that discards all output.
// Used when the orchestrator must run without a client connection —
// for example, when the commander sends a message into a project chat
// via its send_to_project tool. The captain still needs to be invoked,
// but there is no SSE consumer for its stream.
func NewDiscardSSEWriter() *SSEWriter {
	return &SSEWriter{w: io.Discard, f: nopFlusher{}}
}

// WriteEvent marshals v as JSON and writes it as an SSE data frame.
func (s *SSEWriter) WriteEvent(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	s.f.Flush()
	s.sent = true
	return nil
}

// Sent returns true if at least one event has been flushed.
func (s *SSEWriter) Sent() bool {
	return s.sent
}

// WriteDone sends a {"type":"done"} event.
func (s *SSEWriter) WriteDone() error {
	return s.WriteEvent(map[string]string{"type": "done"})
}

// WriteError sends a {"type":"error","error":"..."} event.
func (s *SSEWriter) WriteError(msg string) error {
	return s.WriteEvent(map[string]string{"type": "error", "error": msg})
}

// handleAgentLogs streams log entries as Server-Sent Events.
func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx := r.Context()

	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	agent, err := s.findAgentAnyState(discoveryCtx, name)
	cancel()
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	logCh, err := agent.TailLogs(ctx)
	if err != nil {
		writeAdapterError(w, err)
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for entry := range logCh {
		if err := sse.WriteEvent(entry); err != nil {
			return
		}
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
		writeAdapterError(w, err)
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for event := range ch {
		if err := sse.WriteEvent(event); err != nil {
			return
		}
	}
}

// handleAgentSessions returns the list of conversation sessions.
func (s *Server) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	agent, err := s.findAgentAnyState(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	sessions, err := agent.Sessions(ctx)
	if err != nil {
		writeAdapterError(w, err)
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

	agent, err := s.findAgentAnyState(ctx, name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	messages, err := agent.ChatHistory(ctx, sessionKey, limit)
	if err != nil {
		writeAdapterError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, messages)
}
