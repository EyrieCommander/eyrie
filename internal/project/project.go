package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var validProjectIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func validateProjectID(id string) error {
	if id == "" {
		return fmt.Errorf("project ID is empty")
	}
	if !validProjectIDRe.MatchString(id) {
		return fmt.Errorf("invalid project ID %q", id)
	}
	return nil
}

// ErrNotFound is returned when a project does not exist.
var ErrNotFound = errors.New("project not found")

// Project is the top-level organizational entity for a group of agents
// working toward a shared goal.
type Project struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Goal           string            `json:"goal,omitempty"`
	OrchestratorID string            `json:"orchestrator_id,omitempty"`
	RoleAgentIDs   []string          `json:"role_agent_ids,omitempty"`
	Status         string            `json:"status"` // "active", "paused", "completed", "archived"
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	CreatedBy      string            `json:"created_by"` // "user" or coordinator instance ID
}

// CreateRequest holds parameters for creating a new project.
type CreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Goal        string `json:"goal,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
}

// Store manages project definitions on disk at ~/.eyrie/projects/.
type Store struct {
	dir string
	mu  sync.RWMutex
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".eyrie", "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) List() ([]Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var projects []Project
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			slog.Warn("failed to read project file", "file", entry.Name(), "error", err)
			continue
		}
		var p Project
		if err := json.Unmarshal(data, &p); err != nil {
			slog.Warn("failed to unmarshal project file", "file", entry.Name(), "error", err)
			continue
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (s *Store) Get(id string) (*Project, error) {
	if err := validateProjectID(id); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("project %q: %w", id, ErrNotFound)
		}
		return nil, err
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) Create(req CreateRequest) (*Project, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("project Name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p := Project{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Goal:        req.Goal,
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		CreatedBy:   req.CreatedBy,
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "user"
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.path(p.ID), data, 0o644); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) Save(p Project) error {
	if err := validateProjectID(p.ID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prevent implicit creation — the project file must already exist.
	if _, err := os.Stat(s.path(p.ID)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project %q: %w", p.ID, ErrNotFound)
		}
		return err
	}

	p.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(p.ID), data, 0o644)
}

func (s *Store) Delete(id string) error {
	if err := validateProjectID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AddAgent adds a role agent to a project.
func (s *Store) AddAgent(projectID, instanceID string) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("instanceID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path(projectID))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project %q: %w", projectID, ErrNotFound)
		}
		return err
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}

	// Avoid duplicates
	for _, id := range p.RoleAgentIDs {
		if id == instanceID {
			return nil
		}
	}
	p.RoleAgentIDs = append(p.RoleAgentIDs, instanceID)
	p.UpdatedAt = time.Now()

	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(p.ID), out, 0o644)
}

// RemoveAgent removes a role agent from a project.
func (s *Store) RemoveAgent(projectID, instanceID string) error {
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("instanceID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path(projectID))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project %q: %w", projectID, ErrNotFound)
		}
		return err
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}

	filtered := p.RoleAgentIDs[:0]
	for _, id := range p.RoleAgentIDs {
		if id != instanceID {
			filtered = append(filtered, id)
		}
	}
	p.RoleAgentIDs = filtered
	p.UpdatedAt = time.Now()

	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(p.ID), out, 0o644)
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
