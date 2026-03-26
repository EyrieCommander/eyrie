package server

import (
	"net/http"
	"sync"
	"time"
)

// ProjectEvent represents a structural change in a project that the UI
// should reflect in real-time. Published by API handlers when agents or
// users make changes; consumed by SSE subscribers on the project workspace.
type ProjectEvent struct {
	Type      string    `json:"type"`                 // agent_created, agent_started, agent_stopped, agent_busy, progress_updated, goal_changed, message_sent, agent_removed
	ProjectID string    `json:"project_id"`
	Agent     string    `json:"agent,omitempty"`       // agent name involved
	AgentRole string    `json:"agent_role,omitempty"`  // commander, captain, talon
	Detail    string    `json:"detail,omitempty"`      // human-readable description
	Data      any       `json:"data,omitempty"`        // arbitrary payload (instance info, etc.)
	Timestamp time.Time `json:"timestamp"`
}

// EventBus is a simple pub/sub hub for project events. API handlers
// publish events; SSE endpoints subscribe to receive them. Thread-safe.
type EventBus struct {
	mu   sync.RWMutex
	subs map[string]map[chan ProjectEvent]struct{} // projectID → set of subscriber channels
}

// NewEventBus creates an empty event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subs: make(map[string]map[chan ProjectEvent]struct{}),
	}
}

// Publish sends an event to all subscribers watching the given project.
// Non-blocking: if a subscriber's channel is full, the event is dropped
// for that subscriber (slow consumers don't block publishers).
func (eb *EventBus) Publish(event ProjectEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	subscribers := eb.subs[event.ProjectID]
	for ch := range subscribers {
		select {
		case ch <- event:
		default:
			// Slow consumer — drop event rather than blocking
		}
	}
}

// Subscribe creates a channel that receives events for the given project.
// The caller must call Unsubscribe when done to avoid leaks.
func (eb *EventBus) Subscribe(projectID string) chan ProjectEvent {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan ProjectEvent, 32)
	if eb.subs[projectID] == nil {
		eb.subs[projectID] = make(map[chan ProjectEvent]struct{})
	}
	eb.subs[projectID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (eb *EventBus) Unsubscribe(projectID string, ch chan ProjectEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if subs, ok := eb.subs[projectID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(eb.subs, projectID)
		}
	}
	close(ch)
}

// handleProjectEvents streams structural events for a project via SSE.
// GET /api/projects/{id}/events
func (s *Server) handleProjectEvents(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	ch := s.events.Subscribe(projectID)
	defer s.events.Unsubscribe(projectID, ch)

	// Send a connected event so the client knows the stream is live
	sse.WriteEvent(ProjectEvent{
		Type:      "connected",
		ProjectID: projectID,
		Timestamp: time.Now(),
	})

	// Stream events until the client disconnects
	ctx := r.Context()
	for {
		select {
		case event := <-ch:
			if err := sse.WriteEvent(event); err != nil {
				return // Client disconnected
			}
		case <-ctx.Done():
			return
		}
	}
}
