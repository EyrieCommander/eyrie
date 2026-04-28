package reviewops

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
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

func TestTaskResultArtifactKind(t *testing.T) {
	d := t.TempDir()
	s, err := NewStoreAt(filepath.Join(d, "review-ops"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(CreateTaskRequest{ProjectID: "p", Domain: DomainGitHub, Kind: "triage_issue", Repo: "a/b", TargetNumber: 5})
	if err != nil {
		t.Fatal(err)
	}
	// Persist all three artifact kinds in the expected order.
	if _, err := s.CreateArtifact(CreateArtifactRequest{TaskID: task.ID, Kind: ArtifactKindSourceContext, Content: "## Source Context\ntest"}); err != nil {
		t.Fatal(err)
	}
	tr := TaskResult{
		SchemaVersion:       TaskResultSchemaVersion,
		Summary:             "Test",
		DraftMarkdown:       "# Draft",
		ProposedActions:     []ProposedAction{{Kind: ActionPostIssueComment, Description: "Post"}},
		Confidence:          0.9,
		Severity:            "low",
		RequiresHumanReview: true,
		GeneratedAt:         time.Now(),
	}
	resultJSON, err := tr.MarshalValidated()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateArtifact(CreateArtifactRequest{TaskID: task.ID, Kind: ArtifactKindTaskResult, Content: resultJSON}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateArtifact(CreateArtifactRequest{TaskID: task.ID, Kind: ArtifactKindMarkdown, Content: "# Draft"}); err != nil {
		t.Fatal(err)
	}
	arts, err := s.ListArtifactsByTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(arts))
	}
	kinds := make([]ArtifactKind, len(arts))
	for i, a := range arts {
		kinds[i] = a.Kind
	}
	if kinds[0] != ArtifactKindSourceContext {
		t.Fatalf("expected first artifact to be source_context, got %s", kinds[0])
	}
	if kinds[1] != ArtifactKindTaskResult {
		t.Fatalf("expected second artifact to be task_result, got %s", kinds[1])
	}
	if kinds[2] != ArtifactKindMarkdown {
		t.Fatalf("expected third artifact to be markdown, got %s", kinds[2])
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
