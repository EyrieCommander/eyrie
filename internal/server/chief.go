package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Audacity88/eyrie/internal/bridge"
)

// Chief front door, management side (loopback only). Dan's UI uses these
// routes to send prompts to the chief and read the durable thread. The
// public bridge listener (internal/bridge) serves the chief's replies; it
// shares the bridge.Service with these handlers but no mux or routes.
//
//   GET  /api/chief/status                 -> {enabled, wake_configured}
//   GET  /api/chief/messages?conversation= -> {messages:[...]}
//   POST /api/chief/messages               {conversation_id?, text}
//   POST /api/chief/messages/{id}/retry
//   GET  /api/chief/events                 SSE refresh hints only

const chiefEventsProject = "__chief__"

// AttachChief connects the Chief front door. Call before Start. svc may be
// nil, in which case the routes report the bridge as disabled.
func (s *Server) AttachChief(svc *bridge.Service) {
	s.chief = svc
	if svc != nil {
		svc.Notify = func(conv string) {
			s.events.Publish(ProjectEvent{Type: "chief_updated", ProjectID: chiefEventsProject, Detail: conv, Timestamp: time.Now()})
		}
	}
}

func (s *Server) registerChiefRoutes() {
	s.mux.HandleFunc("GET /api/chief/status", s.handleChiefStatus)
	s.mux.HandleFunc("GET /api/chief/messages", s.handleChiefMessages)
	s.mux.HandleFunc("POST /api/chief/messages", s.handleChiefSend)
	s.mux.HandleFunc("POST /api/chief/messages/{id}/retry", s.handleChiefRetry)
	s.mux.HandleFunc("GET /api/chief/events", s.handleChiefEvents)
}

func (s *Server) handleChiefStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"enabled":         s.chief != nil,
		"wake_configured": s.chief != nil && s.chief.WakeConfigured(),
	})
}

func (s *Server) chiefAvailable(w http.ResponseWriter) bool {
	if s.chief == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bridge not configured (see docs/bridge.md)"})
		return false
	}
	return true
}

func (s *Server) handleChiefMessages(w http.ResponseWriter, r *http.Request) {
	if !s.chiefAvailable(w) {
		return
	}
	conv := r.URL.Query().Get("conversation")
	if conv == "" {
		conv = "chief"
	}
	msgs, err := s.chief.Store.Conversation(conv, 200)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store read failed"})
		return
	}
	if msgs == nil {
		msgs = []bridge.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) handleChiefSend(w http.ResponseWriter, r *http.Request) {
	if !s.chiefAvailable(w) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bridge.MaxPromptBytes+1024))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "prompt too large"})
		return
	}
	var req struct {
		ConversationID string `json:"conversation_id"`
		Text           string `json:"text"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if len(req.Text) > bridge.MaxPromptBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "prompt too large"})
		return
	}
	m, err := s.chief.Send(req.ConversationID, req.Text)
	switch {
	case errors.Is(err, bridge.ErrEmptyPrompt):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty prompt"})
	case errors.Is(err, bridge.ErrWakeNotConfigured):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chief wake not configured (set chief_wake_url and chief_wake_key)"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save prompt"})
	default:
		writeJSON(w, http.StatusAccepted, m)
	}
}

func (s *Server) handleChiefRetry(w http.ResponseWriter, r *http.Request) {
	if !s.chiefAvailable(w) {
		return
	}
	m, err := s.chief.Retry(r.PathValue("id"))
	switch {
	case errors.Is(err, bridge.ErrUnknownMessage):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown message"})
	case errors.Is(err, bridge.ErrWakeNotConfigured):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chief wake not configured"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "retry failed"})
	default:
		writeJSON(w, http.StatusAccepted, m)
	}
}

// handleChiefEvents streams refresh hints. The UI re-reads
// /api/chief/messages on each; no message data travels on this stream.
func (s *Server) handleChiefEvents(w http.ResponseWriter, r *http.Request) {
	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ch := s.events.Subscribe(chiefEventsProject)
	defer s.events.Unsubscribe(chiefEventsProject, ch)
	_ = sse.WriteEvent(ProjectEvent{Type: "connected", ProjectID: chiefEventsProject, Timestamp: time.Now()})
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case ev := <-ch:
			if err := sse.WriteEvent(ev); err != nil {
				return
			}
		case <-ping.C:
			if err := sse.WriteEvent(ProjectEvent{Type: "ping", ProjectID: chiefEventsProject, Timestamp: time.Now()}); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
