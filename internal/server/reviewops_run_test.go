package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Audacity88/eyrie/internal/reviewops"
)

func TestRunReviewTaskCreatesSourceContextThenDraft(t *testing.T) {
	store, err := reviewops.NewStoreAt(filepath.Join(t.TempDir(), "review-ops"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(reviewops.CreateTaskRequest{
		ProjectID:    "p1",
		Domain:       reviewops.DomainGitHub,
		Kind:         "triage_issue",
		Repo:         "owner/repo",
		TargetNumber: 42,
	})
	if err != nil {
		t.Fatal(err)
	}

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/issues/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title":    "Bug: review ops context",
				"state":    "open",
				"html_url": "https://github.com/owner/repo/issues/42",
				"body":     "Issue body",
				"user": map[string]string{
					"login": "alice",
				},
				"labels": []map[string]string{
					{"name": "bug"},
				},
			})
		case "/repos/owner/repo/issues/42/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"body":       "I can reproduce this.",
					"created_at": "2026-04-29T00:00:00Z",
					"user": map[string]string{
						"login": "bob",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gh.Close)

	s := &Server{
		reviewStore: store,
		githubClient: &reviewops.GitHubClient{
			BaseURL:    gh.URL,
			HTTPClient: gh.Client(),
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/review-tasks/"+task.ID+"/run", nil)
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()

	s.handleRunReviewTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updated reviewops.Task
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != reviewops.StatusDraftReady {
		t.Fatalf("expected draft_ready, got %s", updated.Status)
	}

	artifacts, err := store.ListArtifactsByTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}
	if artifacts[0].Kind != reviewops.ArtifactKindSourceContext {
		t.Fatalf("first artifact should be source_context, got %s", artifacts[0].Kind)
	}
	if artifacts[1].Kind != reviewops.ArtifactKindMarkdown {
		t.Fatalf("second artifact should be markdown, got %s", artifacts[1].Kind)
	}
	if !strings.Contains(artifacts[0].Content, "Bug: review ops context") {
		t.Fatalf("source context missing issue title: %s", artifacts[0].Content)
	}
	if !strings.Contains(artifacts[1].Content, "No GitHub mutations were performed") {
		t.Fatalf("draft missing safety note: %s", artifacts[1].Content)
	}
}

func TestRunReviewTaskFetchFailureStillCreatesDraft(t *testing.T) {
	store, err := reviewops.NewStoreAt(filepath.Join(t.TempDir(), "review-ops"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(reviewops.CreateTaskRequest{
		ProjectID:    "p1",
		Domain:       reviewops.DomainGitHub,
		Kind:         "triage_issue",
		Repo:         "owner/repo",
		TargetNumber: 404,
	})
	if err != nil {
		t.Fatal(err)
	}

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(gh.Close)

	s := &Server{
		reviewStore: store,
		githubClient: &reviewops.GitHubClient{
			BaseURL:    gh.URL,
			HTTPClient: gh.Client(),
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/review-tasks/"+task.ID+"/run", nil)
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()

	s.handleRunReviewTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updated reviewops.Task
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != reviewops.StatusDraftReady {
		t.Fatalf("expected draft_ready, got %s", updated.Status)
	}

	artifacts, err := store.ListArtifactsByTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 draft artifact, got %d", len(artifacts))
	}
	if artifacts[0].Kind != reviewops.ArtifactKindMarkdown {
		t.Fatalf("expected markdown draft, got %s", artifacts[0].Kind)
	}
	if !strings.Contains(artifacts[0].Content, "Source context fetch failed") {
		t.Fatalf("draft should mention fetch failure: %s", artifacts[0].Content)
	}
}
