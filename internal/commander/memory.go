package commander

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/fileutil"
)

// ErrMemoryNotFound is returned when Recall or Forget targets a missing key.
var ErrMemoryNotFound = errors.New("memory key not found")

// MemoryEntry is one note the commander has stored about the user, a
// project, or a preference. Entries are keyed by a normalized string
// (lowercase + trimmed) so minor capitalization differences do not
// create duplicate notes for the same concept.
type MemoryEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryStore is a flat, mutex-guarded key-value store backed by a
// single JSON file at ~/.eyrie/commander/memory.json.
//
// WHY flat JSON (not JSONL): chat is append-only, memory is edit-in-place.
// JSONL would require rewriting the whole file on every Forget anyway, so
// one JSON object keeps the shape matching the semantics. Atomic rewrites
// via fileutil.AtomicWrite prevent partial writes.
//
// WHY in-memory cache: memory contents are injected into the system
// prompt every turn. Re-reading the file each turn would add disk I/O
// to every LLM call for no benefit — the store is authoritative in memory
// and only the disk file is updated on mutation.
type MemoryStore struct {
	path string
	mu   sync.RWMutex
	data map[string]MemoryEntry
}

// NewMemoryStore constructs a MemoryStore backed by the default path.
// Loads existing entries if the file exists; an absent file is treated
// as an empty store. A malformed file logs an error at load time but
// still returns a usable empty store — memory is advisory, not critical.
func NewMemoryStore() (*MemoryStore, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	m := &MemoryStore{
		path: filepath.Join(dir, "commander", "memory.json"),
		data: make(map[string]MemoryEntry),
	}
	if err := m.load(); err != nil {
		return nil, fmt.Errorf("loading memory: %w", err)
	}
	return m, nil
}

// load reads the JSON file into m.data. Safe to call on missing file.
func (m *MemoryStore) load() error {
	f, err := os.Open(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	var entries map[string]MemoryEntry
	if err := json.NewDecoder(f).Decode(&entries); err != nil {
		slog.Warn("commander: malformed memory.json, starting empty", "error", err)
		m.data = make(map[string]MemoryEntry)
		return nil
	}
	if entries == nil {
		entries = make(map[string]MemoryEntry)
	}
	m.data = entries
	return nil
}

// flush rewrites the JSON file. Caller must hold m.mu.
func (m *MemoryStore) flush() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return fmt.Errorf("creating commander dir: %w", err)
	}
	data, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling memory: %w", err)
	}
	return fileutil.AtomicWrite(m.path, data, 0o600)
}

// NormalizeKey returns the canonical form of a memory key: lowercase
// and trimmed. Exposed so tool validation can display the normalized
// form back to the LLM (e.g. confirm_required summary).
func NormalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

// Remember inserts or updates an entry. Returns the stored entry after
// normalization. Empty key or value is rejected.
func (m *MemoryStore) Remember(key, value string) (MemoryEntry, error) {
	norm := NormalizeKey(key)
	if norm == "" {
		return MemoryEntry{}, fmt.Errorf("key is required")
	}
	if value == "" {
		return MemoryEntry{}, fmt.Errorf("value is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	entry, existed := m.data[norm]
	entry.Key = norm
	entry.Value = value
	entry.UpdatedAt = now
	if !existed {
		entry.CreatedAt = now
	}
	m.data[norm] = entry
	if err := m.flush(); err != nil {
		return MemoryEntry{}, err
	}
	return entry, nil
}

// Recall returns the entry for a key. Returns ErrMemoryNotFound if
// the key is not stored.
func (m *MemoryStore) Recall(key string) (MemoryEntry, error) {
	norm := NormalizeKey(key)
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.data[norm]
	if !ok {
		return MemoryEntry{}, ErrMemoryNotFound
	}
	return entry, nil
}

// List returns all entries in deterministic order (sorted by key).
// Safe to call when there are no entries (returns empty slice).
func (m *MemoryStore) List() []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MemoryEntry, 0, len(m.data))
	for _, e := range m.data {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Forget removes an entry. Returns ErrMemoryNotFound if the key is not
// stored — callers can treat this as non-fatal if idempotency matters.
func (m *MemoryStore) Forget(key string) error {
	norm := NormalizeKey(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[norm]; !ok {
		return ErrMemoryNotFound
	}
	delete(m.data, norm)
	return m.flush()
}
