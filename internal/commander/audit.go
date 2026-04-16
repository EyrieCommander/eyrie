package commander

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
)

// AuditEntry records one write-tool attempt. Written as one JSON line
// per entry to ~/.eyrie/commander/audit.jsonl.
//
// WHY append-only JSONL: simple to inspect with tail/grep/jq, tamper-
// evident (any edit breaks the JSONL structure), and the same pattern
// used elsewhere in Eyrie. A real security boundary would need signed
// entries; this level is "integrity through obviousness", appropriate
// for a single-user local tool.
type AuditEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	PendingID string         `json:"pending_id,omitempty"`
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args"`
	Risk      string         `json:"risk"`     // "auto" or "confirm"
	Decision  string         `json:"decision"` // "auto", "approved", "denied", "expired"
	Outcome   string         `json:"outcome"`  // "success", "error"
	Error     string         `json:"error,omitempty"`
	Reason    string         `json:"reason,omitempty"` // denial reason
}

// AuditLog writes audit entries to disk. Thread-safe via a mutex — the
// write rate is low enough (one line per write-tool attempt) that mutex
// contention is not a concern.
type AuditLog struct {
	path string
	mu   sync.Mutex
}

// NewAuditLog creates an audit log backed by ~/.eyrie/commander/audit.jsonl.
func NewAuditLog() (*AuditLog, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return &AuditLog{
		path: filepath.Join(dir, "commander", "audit.jsonl"),
	}, nil
}

// Append writes one audit entry. Non-fatal: on error, returns the error
// but the caller should log it, not abort the user operation. The audit
// log is an observability aid, not a gating mechanism.
func (a *AuditLog) Append(entry AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return fmt.Errorf("creating audit dir: %w", err)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit entry: %w", err)
	}

	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return nil
}
