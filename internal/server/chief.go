package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
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
	s.mux.HandleFunc("POST /api/chief/messages", sameOriginJSON(s.handleChiefSend))
	s.mux.HandleFunc("POST /api/chief/messages/{id}/retry", sameOriginJSON(s.handleChiefRetry))
	s.mux.HandleFunc("GET /api/chief/events", s.handleChiefEvents)
}

// sameOriginJSON guards Chief mutations against cross-site requests. A
// prompt POST wakes the chief, so a page on another origin must not be able
// to send one through Dan's browser. Rules:
//   - Host must be a loopback name (blocks DNS rebinding, where an attacker's
//     domain resolves to 127.0.0.1 and Origin matches Host).
//   - If Origin is present it must equal the request's own scheme://Host.
//     The production UI is same-origin; the Vite dev proxy forwards the dev
//     server's Host and Origin unchanged, so they match too. Browsers always
//     send Origin on POST; only non-browser clients (curl) omit it, and those
//     must also not send Sec-Fetch-Site.
//   - Content-Type must be application/json, so a cross-origin request can't
//     be a CORS "simple request" (text/plain form posts are refused).
func sameOriginJSON(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden host"})
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			if origin != "http://"+r.Host && origin != "https://"+r.Host {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
				return
			}
		} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
			return
		}
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if ct != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
			return
		}
		next(w, r)
	}
}

// loopbackHost reports whether a Host header names this machine's loopback.
func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
