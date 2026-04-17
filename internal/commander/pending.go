package commander

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// defaultPendingTTL is how long a pending action remains approvable.
// Long enough for a user to come back from a short break, short enough
// that stale actions don't stack up if the user walks away.
//
// WHY 15 minutes: typical user interrupt rhythms (meetings, context
// switches) are under 15 min. Longer than that, the user has likely
// forgotten what they were doing and should start over.
const defaultPendingTTL = 15 * time.Minute

// PendingStatus tracks the lifecycle of a pending action.
type PendingStatus string

const (
	PendingOpen     PendingStatus = "open"     // awaiting user decision
	PendingApproved PendingStatus = "approved" // user approved (tool has run or is running)
	PendingDenied   PendingStatus = "denied"   // user denied
	PendingExpired  PendingStatus = "expired"  // TTL exceeded before decision
)

// PendingAction is a record of a tool call that requires user approval
// before executing. Stored in memory with a TTL.
//
// WHY in-memory only (no persistence): pending actions are short-lived
// and tied to the current commander turn. If the server restarts, the
// user's chat context resets anyway — losing pending actions on restart
// is the expected behavior (equivalent to cancelling all in-flight
// approvals).
type PendingAction struct {
	ID        string         `json:"id"`
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args"`
	Summary   string         `json:"summary"` // human-readable description
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `json:"expires_at"`
	Status    PendingStatus  `json:"status"`

	// ToolCallID is the LLM-assigned id for the unresolved tool_call in
	// the assistant message. We use this when resuming to emit a tool
	// result message with the matching tool_call_id.
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Denial reason (set only when Status == PendingDenied)
	DenialReason string `json:"denial_reason,omitempty"`
}

// PendingStore is an in-memory, goroutine-safe map of pending actions.
// Expired entries are swept lazily on Get/Add (no background goroutine,
// to keep shutdown simple).
type PendingStore struct {
	mu      sync.Mutex
	actions map[string]*PendingAction
	ttl     time.Duration
}

// NewPendingStore creates an empty pending store with the default TTL.
func NewPendingStore() *PendingStore {
	return &PendingStore{
		actions: make(map[string]*PendingAction),
		ttl:     defaultPendingTTL,
	}
}

// Add stores a new pending action and returns it. The caller is
// responsible for setting Tool, Args, Summary, and ToolCallID; Add
// fills in ID, timestamps, and Status.
func (s *PendingStore) Add(tool string, args map[string]any, summary, toolCallID string) *PendingAction {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	id := "pa_" + uuid.New().String()[:8]
	now := time.Now()
	pa := &PendingAction{
		ID:         id,
		Tool:       tool,
		Args:       args,
		Summary:    summary,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.ttl),
		Status:     PendingOpen,
		ToolCallID: toolCallID,
	}
	s.actions[id] = pa
	copy := *pa
	return &copy
}

// Get returns the pending action with the given id, or an error if
// missing. Expired actions are swept first.
func (s *PendingStore) Get(id string) (*PendingAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	pa, ok := s.actions[id]
	if !ok {
		return nil, fmt.Errorf("pending action %q not found or expired", id)
	}
	copy := *pa
	return &copy, nil
}

// Approve marks a pending action as approved and returns a copy for the
// caller to execute. Returns an error if the action is not in open state.
func (s *PendingStore) Approve(id string) (*PendingAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	pa, ok := s.actions[id]
	if !ok {
		return nil, fmt.Errorf("pending action %q not found or expired", id)
	}
	if pa.Status != PendingOpen {
		return nil, fmt.Errorf("pending action %q is already %s", id, pa.Status)
	}
	pa.Status = PendingApproved
	copy := *pa
	return &copy, nil
}

// Deny marks a pending action as denied with an optional reason.
func (s *PendingStore) Deny(id, reason string) (*PendingAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	pa, ok := s.actions[id]
	if !ok {
		return nil, fmt.Errorf("pending action %q not found or expired", id)
	}
	if pa.Status != PendingOpen {
		return nil, fmt.Errorf("pending action %q is already %s", id, pa.Status)
	}
	pa.Status = PendingDenied
	pa.DenialReason = reason
	copy := *pa
	return &copy, nil
}

// sweepLocked removes expired entries. Caller must hold s.mu.
// Cheap enough to run on every access (actions live briefly).
func (s *PendingStore) sweepLocked() {
	now := time.Now()
	for id, pa := range s.actions {
		if pa.Status == PendingOpen && now.After(pa.ExpiresAt) {
			pa.Status = PendingExpired
		}
		// Keep expired entries visible for a grace period so clients
		// that race the sweep get a clear "expired" response rather
		// than "not found". 1 minute after expiry, forget them.
		if now.After(pa.ExpiresAt.Add(time.Minute)) {
			delete(s.actions, id)
		}
	}
}
