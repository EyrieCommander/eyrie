package persona

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

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
			continue
		}
		var p Persona
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		p.Installed = true
		personas = append(personas, p)
	}
	return personas, nil
}

// Get returns a single persona by ID.
func (s *Store) Get(id string) (*Persona, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("persona %q not found", id)
		}
		return nil, err
	}
	var p Persona
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.Installed = true
	return &p, nil
}

// Save writes a persona to disk.
func (s *Store) Save(p Persona) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(p.ID), data, 0o644)
}

// Delete removes a persona from disk.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
