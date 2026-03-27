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

	"github.com/Audacity88/eyrie/internal/fileutil"
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

// ProjectStatus represents the lifecycle state of a project.
type ProjectStatus string

const (
	PStatusActive    ProjectStatus = "active"
	PStatusPaused    ProjectStatus = "paused"
	PStatusCompleted ProjectStatus = "completed"
	PStatusArchived  ProjectStatus = "archived"
)

// Valid returns true if s is a recognised project status.
func (s ProjectStatus) Valid() bool {
	switch s {
	case PStatusActive, PStatusPaused, PStatusCompleted, PStatusArchived:
		return true
	}
	return false
}

// CanTransition returns true if transitioning from → to is allowed.
// Allowed: active→paused, paused→active, active→completed, paused→completed, completed→archived.
func CanTransition(from, to ProjectStatus) bool {
	switch {
	case from == PStatusActive && to == PStatusPaused:
		return true
	case from == PStatusPaused && to == PStatusActive:
		return true
	case from == PStatusActive && to == PStatusCompleted:
		return true
	case from == PStatusPaused && to == PStatusCompleted:
		return true
	case from == PStatusCompleted && to == PStatusArchived:
		return true
	}
	return false
}

// Project is the top-level organizational entity for a group of agents
// working toward a shared goal.
type Project struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	Goal           string        `json:"goal,omitempty"`
	OrchestratorID string        `json:"orchestrator_id,omitempty"`
	RoleAgentIDs   []string      `json:"role_agent_ids,omitempty"`
	Status         ProjectStatus `json:"status"`
	Progress       int           `json:"progress,omitempty"`          // 0-100 percentage, set by user or captain
	Deadline       *time.Time    `json:"deadline,omitempty"`          // target completion date
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	CreatedBy      string        `json:"created_by"`            // "user" or coordinator instance ID
	SessionKey     string        `json:"session_key,omitempty"` // human-readable session slug, e.g. "chess-coach"
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
		Status:      PStatusActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		CreatedBy:   req.CreatedBy,
		SessionKey:  s.uniqueSessionKey(req.Name),
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "user"
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := fileutil.AtomicWrite(s.path(p.ID), data, 0o600); err != nil {
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
	data, err := os.ReadFile(s.path(p.ID))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project %q: %w", p.ID, ErrNotFound)
		}
		return err
	}

	// Validate status
	if !p.Status.Valid() {
		return fmt.Errorf("invalid project status %q", p.Status)
	}

	// Check transition if status is changing
	var old Project
	if err := json.Unmarshal(data, &old); err != nil {
		return fmt.Errorf("failed to read existing project: %w", err)
	}
	if old.Status != p.Status {
		if !CanTransition(old.Status, p.Status) {
			return fmt.Errorf("cannot transition project from %q to %q", old.Status, p.Status)
		}
	}

	p.UpdatedAt = time.Now()
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWrite(s.path(p.ID), out, 0o600)
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
	return fileutil.AtomicWrite(s.path(p.ID), out, 0o600)
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
	return fileutil.AtomicWrite(s.path(p.ID), out, 0o600)
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// uniqueSessionKey generates a human-readable session slug from the project
// name (e.g. "chess-coach"), appending "-2", "-3", etc. if needed to avoid
// collisions with existing projects. Must be called under s.mu.
func (s *Store) uniqueSessionKey(name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = nonAlphaNum.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "project"
	}

	// Collect existing session keys
	existing := map[string]bool{}
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var p struct{ SessionKey string `json:"session_key"` }
		if json.Unmarshal(data, &p) == nil && p.SessionKey != "" {
			existing[p.SessionKey] = true
		}
	}

	candidate := base
	for n := 2; existing[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
	return candidate
}
