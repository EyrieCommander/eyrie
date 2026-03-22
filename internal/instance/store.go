package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store manages instance metadata on disk at ~/.eyrie/instances/.
// Each instance gets its own subdirectory; the registry.json file
// stores the metadata index for fast listing.
type Store struct {
	dir string // ~/.eyrie/instances/
	mu  sync.RWMutex
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".eyrie", "instances")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// List returns all instances from disk.
func (s *Store) List() ([]Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) listLocked() ([]Instance, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var instances []Instance
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.dir, entry.Name(), "instance.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var inst Instance
		if err := json.Unmarshal(data, &inst); err != nil {
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// Get returns a single instance by ID.
func (s *Store) Get(id string) (*Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("instance %q not found", id)
		}
		return nil, err
	}
	var inst Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Save writes instance metadata to disk.
func (s *Store) Save(inst Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.dir, inst.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(inst.ID), data, 0o644)
}

// UpdateStatus updates just the status field for an instance.
func (s *Store) UpdateStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return err
	}
	var inst Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return err
	}
	inst.Status = status
	out, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(id), out, 0o644)
}

// Delete removes an instance directory and all its contents.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.dir, id)
	return os.RemoveAll(dir)
}

// Dir returns the base directory for a given instance ID.
func (s *Store) Dir(id string) string {
	return filepath.Join(s.dir, id)
}

// NameExists checks whether an instance with the given name already exists.
func (s *Store) NameExists(name string) (bool, error) {
	instances, err := s.listLocked()
	if err != nil {
		return false, err
	}
	for _, inst := range instances {
		if inst.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) metaPath(id string) string {
	return filepath.Join(s.dir, id, "instance.json")
}
