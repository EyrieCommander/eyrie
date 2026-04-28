package reviewops

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreTaskLifecycle(t *testing.T) {
	d := t.TempDir()
	s, err := NewStoreAt(filepath.Join(d, "review-ops"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(CreateTaskRequest{
		ProjectID:    "p1",
		Domain:       DomainGitHub,
		Kind:         "review_pr",
		Repo:         "zeroclaw-labs/zeroclaw",
		TargetNumber: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusQueued {
		t.Fatalf("expected queued, got %s", task.Status)
	}
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "p1" {
		t.Fatalf("unexpected project id: %s", got.ProjectID)
	}
	if _, err := s.UpdateTaskStatus(task.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListTasks("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != StatusRunning {
		t.Fatalf("expected one running task")
	}
}

func TestStoreValidation(t *testing.T) {
	d := t.TempDir()
	s, err := NewStoreAt(filepath.Join(d, "review-ops"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []CreateTaskRequest{
		{ProjectID: "", Domain: DomainGitHub, Kind: "review_pr", Repo: "a/b", TargetNumber: 1},
		{ProjectID: "p", Domain: "jira", Kind: "review_pr", Repo: "a/b", TargetNumber: 1},
		{ProjectID: "p", Domain: DomainGitHub, Kind: "bad", Repo: "a/b", TargetNumber: 1},
		{ProjectID: "p", Domain: DomainGitHub, Kind: "review_pr", Repo: "", TargetNumber: 1},
		{ProjectID: "p", Domain: DomainGitHub, Kind: "review_pr", Repo: "a/b", TargetNumber: 0},
	}
	for _, c := range cases {
		if _, err := s.CreateTask(c); err == nil {
			t.Fatalf("expected validation error for %+v", c)
		}
	}
}

func TestStoreRejectsInvalidIDs(t *testing.T) {
	d := t.TempDir()
	s, err := NewStoreAt(filepath.Join(d, "review-ops"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(CreateTaskRequest{ProjectID: "../p", Domain: DomainGitHub, Kind: "review_pr", Repo: "a/b", TargetNumber: 1}); err == nil {
		t.Fatal("expected invalid project id to fail")
	}
	if _, err := s.GetTask("../task"); err == nil {
		t.Fatal("expected invalid task id to fail")
	}
	if _, err := s.UpdateTaskStatus("../task", StatusRunning); err == nil {
		t.Fatal("expected invalid update task id to fail")
	}
	if _, err := s.CreateArtifact(CreateArtifactRequest{TaskID: "../task", Content: "draft"}); err == nil {
		t.Fatal("expected invalid artifact task id to fail")
	}
	if _, err := s.ListArtifactsByTask("../task"); err == nil {
		t.Fatal("expected invalid list task id to fail")
	}
}

func TestArtifactsByTask(t *testing.T) {
	d := t.TempDir()
	s, err := NewStoreAt(filepath.Join(d, "review-ops"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(CreateTaskRequest{ProjectID: "p", Domain: DomainGitHub, Kind: "triage_issue", Repo: "a/b", TargetNumber: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateArtifact(CreateArtifactRequest{TaskID: task.ID, Content: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateArtifact(CreateArtifactRequest{TaskID: "other", Content: "other"}); err != nil {
		t.Fatal(err)
	}
	arts, err := s.ListArtifactsByTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].Content != "one" {
		t.Fatalf("unexpected artifacts: %+v", arts)
	}
	_, err = s.GetTask("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
