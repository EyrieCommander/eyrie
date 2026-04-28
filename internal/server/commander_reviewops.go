// Commander callback wrappers for review task operations.
// These functions adapt the reviewops store and server run logic
// into the map[string]any signatures the commander RegistryDeps
// expects (avoiding import cycles).
package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Audacity88/eyrie/internal/reviewops"
)

func taskToMap(t *reviewops.Task) map[string]any {
	b, _ := json.Marshal(t)
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

func artifactToMap(a reviewops.Artifact) map[string]any {
	b, _ := json.Marshal(a)
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

func (s *Server) cmdListReviewTasks(projectID string) ([]map[string]any, error) {
	tasks, err := s.reviewStore.ListTasks(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(tasks))
	for i := range tasks {
		out = append(out, taskToMap(&tasks[i]))
	}
	return out, nil
}

func (s *Server) cmdGetReviewTask(taskID string) (map[string]any, error) {
	t, err := s.reviewStore.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	return taskToMap(t), nil
}

func (s *Server) cmdCreateReviewTask(projectID, domain, kind, repo string, targetNumber int) (map[string]any, error) {
	t, err := s.reviewStore.CreateTask(reviewops.CreateTaskRequest{
		ProjectID:    projectID,
		Domain:       domain,
		Kind:         kind,
		Repo:         repo,
		TargetNumber: targetNumber,
	})
	if err != nil {
		return nil, err
	}
	return taskToMap(t), nil
}

// cmdRunReviewTask executes the full run flow (source context fetch +
// task_result + draft artifacts) and returns the updated task.
// Reuses the same logic as handleRunReviewTask but returns a map
// instead of writing HTTP response.
func (s *Server) cmdRunReviewTask(ctx context.Context, taskID string) (map[string]any, error) {
	t, err := s.reviewStore.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if t.Status == reviewops.StatusRunning {
		return nil, fmt.Errorf("task already running")
	}
	if t.Status == reviewops.StatusPosted {
		return nil, fmt.Errorf("task already posted")
	}
	if t.Status == reviewops.StatusFailed {
		return nil, fmt.Errorf("task failed; create a new task")
	}

	if _, err := s.reviewStore.UpdateTaskStatus(taskID, reviewops.StatusRunning); err != nil {
		return nil, err
	}

	// Delegate to the shared run logic.
	s.executeReviewTaskRun(ctx, t)

	// Re-read the task to get final status.
	updated, err := s.reviewStore.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	return taskToMap(updated), nil
}

func (s *Server) cmdListReviewArtifacts(taskID string) ([]map[string]any, error) {
	arts, err := s.reviewStore.ListArtifactsByTask(taskID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(arts))
	for _, a := range arts {
		out = append(out, artifactToMap(a))
	}
	return out, nil
}
