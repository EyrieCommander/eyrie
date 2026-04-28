package reviewops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/fileutil"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("review task not found")

var validIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const (
	DomainGitHub = "github"
)

type TaskStatus string

const (
	StatusQueued     TaskStatus = "queued"
	StatusRunning    TaskStatus = "running"
	StatusDraftReady TaskStatus = "draft_ready"
	StatusPosted     TaskStatus = "posted"
	StatusFailed     TaskStatus = "failed"
)

type ArtifactKind string

const (
	ArtifactKindMarkdown      ArtifactKind = "markdown"
	ArtifactKindSourceContext ArtifactKind = "source_context"
)

type Task struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	Domain       string     `json:"domain"`
	Kind         string     `json:"kind"`
	Repo         string     `json:"repo"`
	TargetNumber int        `json:"target_number"`
	RunnerKind   string     `json:"runner_kind,omitempty"`
	Status       TaskStatus `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Artifact struct {
	ID        string       `json:"id"`
	TaskID    string       `json:"task_id"`
	Kind      ArtifactKind `json:"kind"`
	Content   string       `json:"content"`
	CreatedAt time.Time    `json:"created_at"`
}

type CreateTaskRequest struct {
	ProjectID    string `json:"project_id"`
	Domain       string `json:"domain"`
	Kind         string `json:"kind"`
	Repo         string `json:"repo"`
	TargetNumber int    `json:"target_number"`
	RunnerKind   string `json:"runner_kind,omitempty"`
}

type CreateArtifactRequest struct {
	TaskID   string       `json:"task_id"`
	Kind     ArtifactKind `json:"kind"`
	Content  string       `json:"content"`
	CreateAt time.Time
}

type Store struct {
	baseDir      string
	tasksDir     string
	artifactsDir string
	mu           sync.RWMutex
}

func NewStore() (*Store, error) {
	base, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return NewStoreAt(filepath.Join(base, "review-ops"))
}

func NewStoreAt(dir string) (*Store, error) {
	s := &Store{baseDir: dir, tasksDir: filepath.Join(dir, "tasks"), artifactsDir: filepath.Join(dir, "artifacts")}
	if err := os.MkdirAll(s.tasksDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.artifactsDir, 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

func validKind(kind string) bool {
	switch kind {
	case "triage_issue", "review_pr", "rereview_pr", "respond_reviewer":
		return true
	default:
		return false
	}
}

func validateTaskCreate(req CreateTaskRequest) error {
	if strings.TrimSpace(req.ProjectID) == "" {
		return fmt.Errorf("project_id is required")
	}
	if err := validateID(req.ProjectID, "project_id"); err != nil {
		return err
	}
	if req.Domain != DomainGitHub {
		return fmt.Errorf("unsupported domain %q", req.Domain)
	}
	if !validKind(req.Kind) {
		return fmt.Errorf("unsupported kind %q", req.Kind)
	}
	if strings.TrimSpace(req.Repo) == "" {
		return fmt.Errorf("repo is required")
	}
	if req.TargetNumber <= 0 {
		return fmt.Errorf("target_number must be positive")
	}
	return nil
}

func validateID(id, field string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !validIDRe.MatchString(id) {
		return fmt.Errorf("invalid %s %q", field, id)
	}
	return nil
}

func (s *Store) CreateTask(req CreateTaskRequest) (*Task, error) {
	if err := validateTaskCreate(req); err != nil {
		return nil, err
	}
	now := time.Now()
	t := Task{
		ID:           "rt_" + uuid.New().String(),
		ProjectID:    strings.TrimSpace(req.ProjectID),
		Domain:       req.Domain,
		Kind:         req.Kind,
		Repo:         strings.TrimSpace(req.Repo),
		TargetNumber: req.TargetNumber,
		RunnerKind:   strings.TrimSpace(req.RunnerKind),
		Status:       StatusQueued,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveTaskLocked(t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTasks(projectID string) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ents, err := os.ReadDir(s.tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, err
	}
	out := make([]Task, 0)
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.tasksDir, e.Name()))
		if err != nil {
			continue
		}
		var t Task
		if err := json.Unmarshal(b, &t); err != nil {
			continue
		}
		if projectID != "" && t.ProjectID != projectID {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) GetTask(id string) (*Task, error) {
	if err := validateID(id, "task id"); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := os.ReadFile(filepath.Join(s.tasksDir, id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var t Task
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) UpdateTaskStatus(id string, status TaskStatus) (*Task, error) {
	if err := validateID(id, "task id"); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.tasksDir, id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var t Task
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	if err := s.saveTaskLocked(t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) saveTaskLocked(t Task) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWrite(filepath.Join(s.tasksDir, t.ID+".json"), b, 0o600)
}

func (s *Store) CreateArtifact(req CreateArtifactRequest) (*Artifact, error) {
	if err := validateID(req.TaskID, "task_id"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}
	if req.Kind == "" {
		req.Kind = ArtifactKindMarkdown
	}
	now := req.CreateAt
	if now.IsZero() {
		now = time.Now()
	}
	a := Artifact{ID: "ra_" + uuid.New().String(), TaskID: req.TaskID, Kind: req.Kind, Content: req.Content, CreatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := fileutil.AtomicWrite(filepath.Join(s.artifactsDir, a.ID+".json"), b, 0o600); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) ListArtifactsByTask(taskID string) ([]Artifact, error) {
	if err := validateID(taskID, "task id"); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ents, err := os.ReadDir(s.artifactsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Artifact{}, nil
		}
		return nil, err
	}
	out := make([]Artifact, 0)
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.artifactsDir, e.Name()))
		if err != nil {
			continue
		}
		var a Artifact
		if err := json.Unmarshal(b, &a); err != nil {
			continue
		}
		if a.TaskID == taskID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
