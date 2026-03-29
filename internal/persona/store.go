package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Audacity88/eyrie/internal/fileutil"
)

// ErrNotFound is returned when a persona does not exist on disk.
var ErrNotFound = errors.New("persona not found")

// Store manages persona definitions on disk at ~/.eyrie/personas/.
type Store struct {
	dir string
	mu  sync.RWMutex
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".eyrie", "personas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// List returns all installed personas from disk.
func (s *Store) List() ([]Persona, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var personas []Persona
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			slog.Warn("failed to read persona file", "file", entry.Name(), "error", err)
			continue
		}
		var p Persona
		if err := json.Unmarshal(data, &p); err != nil {
			slog.Warn("failed to unmarshal persona file", "file", entry.Name(), "error", err)
			continue
		}
		// Derive canonical ID from filename so List() returns IDs that Get() can find.
		p.ID = strings.TrimSuffix(entry.Name(), ".json")
		p.Installed = true
		personas = append(personas, p)
	}
	return personas, nil
}

// Get returns a single persona by ID.
func (s *Store) Get(id string) (*Persona, error) {
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." {
		return nil, fmt.Errorf("invalid persona ID %q", id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("persona %q: %w", id, ErrNotFound)
		}
		return nil, err
	}
	var p Persona
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.ID = id
	p.Installed = true
	return &p, nil
}

// Save writes a persona to disk.
func (s *Store) Save(p Persona) error {
	if p.ID == "" || p.ID != filepath.Base(p.ID) || p.ID == "." || p.ID == ".." {
		return fmt.Errorf("invalid persona ID %q", p.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWrite(s.path(p.ID), data, 0o644)
}

// Delete removes a persona from disk.
func (s *Store) Delete(id string) error {
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." {
		return fmt.Errorf("invalid persona ID %q", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.path(id)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("persona %q: %w", id, ErrNotFound)
		}
		return err
	}
	return nil
}

func (s *Store) path(id string) string {
	safe := filepath.Base(id)
	return filepath.Join(s.dir, safe+".json")
}
