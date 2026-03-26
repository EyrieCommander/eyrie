package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/Audacity88/eyrie/internal/fileutil"
)

// ErrNotFound is returned when an instance does not exist on disk.
var ErrNotFound = errors.New("instance not found")

// ErrNameExists is returned when an instance name is already taken.
var ErrNameExists = errors.New("instance name already exists")

// ErrRequiredField is returned when a required field is missing.
var ErrRequiredField = errors.New("required field is missing")

// ErrUnsupportedFramework is returned for unknown framework names.
var ErrUnsupportedFramework = errors.New("unsupported framework")

// Store manages instance metadata on disk at ~/.eyrie/instances/.
// Each instance gets its own subdirectory containing an instance.json
// file with the metadata.
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
			slog.Warn("skipping instance: failed to read metadata", "id", entry.Name(), "error", err)
			continue
		}
		var inst Instance
		if err := json.Unmarshal(data, &inst); err != nil {
			slog.Warn("skipping instance: failed to parse metadata", "id", entry.Name(), "error", err)
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// Get returns a single instance by ID.
func (s *Store) Get(id string) (*Instance, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("instance %q: %w", id, ErrNotFound)
		}
		return nil, err
	}
	var inst Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Save writes instance metadata to disk atomically.
func (s *Store) Save(inst Instance) error {
	if err := validateID(inst.ID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check name uniqueness: another instance with the same name but a
	// different ID is a conflict. Same-ID updates are always allowed.
	instances, err := s.listLocked()
	if err != nil {
		return fmt.Errorf("failed to check name uniqueness: %w", err)
	}
	for _, existing := range instances {
		if existing.Name == inst.Name && existing.ID != inst.ID {
			return ErrNameExists
		}
	}

	dir := filepath.Join(s.dir, inst.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWrite(s.metaPath(inst.ID), data, 0o600)
}

// UpdateStatus updates just the status field for an instance.
// Returns an error if status is not a valid InstanceStatus.
func (s *Store) UpdateStatus(id string, status InstanceStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid instance status %q", status)
	}
	if err := validateID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("instance %q: %w", id, ErrNotFound)
		}
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
	return fileutil.AtomicWrite(s.metaPath(id), out, 0o600)
}

// Delete removes an instance directory and all its contents.
// Returns ErrNotFound if the instance does not exist.
func (s *Store) Delete(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.dir, id)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("instance %q: %w", id, ErrNotFound)
		}
		return err
	}
	return os.RemoveAll(dir)
}

// Dir returns the base directory for a given instance ID.
func (s *Store) Dir(id string) string {
	if validateID(id) != nil {
		return ""
	}
	return filepath.Join(s.dir, id)
}

// NameExists checks whether an instance with the given name already exists.
func (s *Store) NameExists(name string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

// validIDRe matches safe instance IDs: alphanumerics, hyphens, and underscores only.
var validIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateID ensures the id is safe for use in file paths (no traversal).
func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("instance ID is empty")
	}
	if !validIDRe.MatchString(id) {
		return fmt.Errorf("invalid instance ID %q: must contain only alphanumerics, hyphens, and underscores", id)
	}
	return nil
}

func (s *Store) metaPath(id string) string {
	return filepath.Join(s.dir, id, "instance.json")
}

