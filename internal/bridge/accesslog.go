package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// AccessEntry is one JSONL line. It never carries file contents, prompt or
// reply text, query strings, or tokens.
type AccessEntry struct {
	Time       string `json:"time"`
	Remote     string `json:"remote"`
	Method     string `json:"method"`
	Route      string `json:"route"`
	Root       string `json:"root,omitempty"`
	Path       string `json:"path,omitempty"`
	Status     int    `json:"status"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"duration_ms"`
	AuthFailed bool   `json:"auth_failed,omitempty"`
}

// AccessLog appends entries to a 0600 JSONL file.
type AccessLog struct {
	mu sync.Mutex
	f  *os.File
}

// DefaultAccessLogPath is ~/.eyrie/logs/bridge-access.jsonl.
func DefaultAccessLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".eyrie", "logs", "bridge-access.jsonl"), nil
}

// OpenAccessLog opens path for append (O_APPEND), creating it 0600.
func OpenAccessLog(path string) (*AccessLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &AccessLog{f: f}, nil
}

// Write appends one entry. Errors are dropped: logging must not fail a request.
func (l *AccessLog) Write(e AccessEntry) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.Write(b)
}

// Close closes the file.
func (l *AccessLog) Close() error { return l.f.Close() }
