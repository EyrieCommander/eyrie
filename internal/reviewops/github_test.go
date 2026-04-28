package reviewops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// newTestServer creates an httptest server that serves canned GitHub API responses.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, *GitHubClient) {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, h := range handlers {
		mux.HandleFunc(pattern, h)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	client := &GitHubClient{
		BaseURL:    ts.URL,
		HTTPClient: ts.Client(),
	}
	return ts, client
}

func TestFetchIssueContextSuccess(t *testing.T) {
	issue := ghIssue{
		Title:   "Bug: widget crashes",
		State:   "open",
		HTMLURL: "https://github.com/test/repo/issues/42",
		Body:    "The widget crashes when you click the button.",
	}
	issue.User.Login = "alice"
	issue.Labels = []struct {
		Name string `json:"name"`
	}{{Name: "bug"}, {Name: "priority:high"}}

	comments := []ghComment{
		{Body: "I can reproduce this.", User: struct {
			Login string `json:"login"`
		}{Login: "bob"}, CreatedAt: "2025-01-01T00:00:00Z"},
		{Body: "Fixed in PR #43.", User: struct {
			Login string `json:"login"`
		}{Login: "alice"}, CreatedAt: "2025-01-02T00:00:00Z"},
	}

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/test/repo/issues/42": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(issue)
		},
		"/repos/test/repo/issues/42/comments": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(comments)
		},
	})

	sc, err := client.FetchIssueContext(context.Background(), "test/repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Title != "Bug: widget crashes" {
		t.Fatalf("unexpected title: %s", sc.Title)
	}
	if sc.State != "open" {
		t.Fatalf("unexpected state: %s", sc.State)
	}
	if sc.Author != "alice" {
		t.Fatalf("unexpected author: %s", sc.Author)
	}
	if sc.TargetType != "issue" {
		t.Fatalf("expected issue, got %s", sc.TargetType)
	}
	if len(sc.Labels) != 2 || sc.Labels[0] != "bug" {
		t.Fatalf("unexpected labels: %v", sc.Labels)
	}
	if len(sc.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(sc.Comments))
	}
	if sc.Comments[0].Author != "bob" {
		t.Fatalf("unexpected first comment author: %s", sc.Comments[0].Author)
	}
	if sc.FetchedAt.IsZero() {
		t.Fatal("fetched_at should be set")
	}

	// Verify markdown rendering
	md := sc.RenderMarkdown()
	if !strings.Contains(md, "Bug: widget crashes") {
		t.Fatal("markdown should contain issue title")
	}
	if !strings.Contains(md, "bug, priority:high") {
		t.Fatal("markdown should contain labels")
	}
}

func TestFetchPRContextSuccess(t *testing.T) {
	issue := ghIssue{
		Title:   "feat: add widget",
		State:   "open",
		HTMLURL: "https://github.com/test/repo/pull/10",
		Body:    "Adds the new widget feature.",
	}
	issue.User.Login = "carol"
	issue.PullRequest = &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.com/repos/test/repo/pulls/10"}

	issueComments := []ghComment{
		{Body: "Looks good!", User: struct {
			Login string `json:"login"`
		}{Login: "dave"}, CreatedAt: "2025-02-01T00:00:00Z"},
	}

	reviewComments := []ghComment{
		{Body: "Nit: rename this variable.", User: struct {
			Login string `json:"login"`
		}{Login: "eve"}, CreatedAt: "2025-02-02T00:00:00Z"},
	}

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/test/repo/issues/10": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(issue)
		},
		"/repos/test/repo/issues/10/comments": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(issueComments)
		},
		"/repos/test/repo/pulls/10/comments": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(reviewComments)
		},
	})

	sc, err := client.FetchPRContext(context.Background(), "test/repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if sc.TargetType != "pull_request" {
		t.Fatalf("expected pull_request, got %s", sc.TargetType)
	}
	if sc.Title != "feat: add widget" {
		t.Fatalf("unexpected title: %s", sc.Title)
	}
	// Should have both issue comments and review comments
	if len(sc.Comments) != 2 {
		t.Fatalf("expected 2 combined comments, got %d", len(sc.Comments))
	}
}

func TestBoundedComments(t *testing.T) {
	issue := ghIssue{Title: "lots of comments", State: "open", HTMLURL: "https://github.com/test/repo/issues/1", Body: "test"}
	issue.User.Login = "user"

	// Create 30 comments; should be bounded to maxComments (20).
	comments := make([]ghComment, 30)
	for i := range comments {
		comments[i] = ghComment{
			Body: fmt.Sprintf("comment %d", i),
			User: struct {
				Login string `json:"login"`
			}{Login: fmt.Sprintf("user%d", i)},
			CreatedAt: "2025-01-01T00:00:00Z",
		}
	}

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/test/repo/issues/1": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(issue)
		},
		"/repos/test/repo/issues/1/comments": func(w http.ResponseWriter, r *http.Request) {
			// The server is told per_page=20, but in this test we return all 30
			// to verify the client-side bound works.
			json.NewEncoder(w).Encode(comments)
		},
	})

	sc, err := client.FetchIssueContext(context.Background(), "test/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Comments) > maxComments {
		t.Fatalf("expected at most %d comments, got %d", maxComments, len(sc.Comments))
	}
}

func TestFetchFailureDoesNotPreventDraft(t *testing.T) {
	// This test validates the store/run behavior: if GitHub fetch fails,
	// the draft artifact should still be created and task should be draft_ready.
	d := t.TempDir()
	store, err := NewStoreAt(filepath.Join(d, "review-ops"))
	if err != nil {
		t.Fatal(err)
	}

	task, err := store.CreateTask(CreateTaskRequest{
		ProjectID:    "p1",
		Domain:       DomainGitHub,
		Kind:         "triage_issue",
		Repo:         "test/repo",
		TargetNumber: 999,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a client that always returns 404
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/test/repo/issues/999": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		},
	})

	// Simulate the run flow: try fetch, expect failure
	_, fetchErr := client.FetchIssueContext(context.Background(), "test/repo", 999)
	if fetchErr == nil {
		t.Fatal("expected fetch to fail for non-existent issue")
	}

	// Set running
	if _, err := store.UpdateTaskStatus(task.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}

	// Even though fetch failed, create the draft artifact
	draftContent := fmt.Sprintf("# Local stub draft\n\n- kind: triage_issue\n- repo: test/repo\n\n> Note: Source context fetch failed: %s", fetchErr.Error())
	if _, err := store.CreateArtifact(CreateArtifactRequest{TaskID: task.ID, Kind: ArtifactKindMarkdown, Content: draftContent}); err != nil {
		t.Fatal(err)
	}

	// Mark draft_ready
	updated, err := store.UpdateTaskStatus(task.ID, StatusDraftReady)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusDraftReady {
		t.Fatalf("expected draft_ready, got %s", updated.Status)
	}

	// Verify artifact was persisted
	arts, err := store.ListArtifactsByTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if !strings.Contains(arts[0].Content, "Source context fetch failed") {
		t.Fatal("draft should mention fetch failure")
	}
}

func TestSourceContextArtifactKind(t *testing.T) {
	d := t.TempDir()
	store, err := NewStoreAt(filepath.Join(d, "review-ops"))
	if err != nil {
		t.Fatal(err)
	}

	task, err := store.CreateTask(CreateTaskRequest{
		ProjectID:    "p1",
		Domain:       DomainGitHub,
		Kind:         "review_pr",
		Repo:         "test/repo",
		TargetNumber: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a source_context artifact
	a1, err := store.CreateArtifact(CreateArtifactRequest{
		TaskID:  task.ID,
		Kind:    ArtifactKindSourceContext,
		Content: "# Source Context\n\nTest content",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a1.Kind != ArtifactKindSourceContext {
		t.Fatalf("expected source_context kind, got %s", a1.Kind)
	}

	// Create a markdown draft artifact
	a2, err := store.CreateArtifact(CreateArtifactRequest{
		TaskID:  task.ID,
		Kind:    ArtifactKindMarkdown,
		Content: "# Local stub draft",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Both should be returned in order
	arts, err := store.ListArtifactsByTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}
	if arts[0].Kind != ArtifactKindSourceContext {
		t.Fatalf("first artifact should be source_context, got %s", arts[0].Kind)
	}
	if arts[1].Kind != ArtifactKindMarkdown {
		t.Fatalf("second artifact should be markdown, got %s", arts[1].Kind)
	}
	_ = a2 // used above
}

func TestNoPathTraversalInGitHubClient(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{})

	// Attempt path traversal in repo name — should fail (404 from test server)
	_, err := client.FetchIssueContext(context.Background(), "../../../etc/passwd", 1)
	if err == nil {
		t.Fatal("expected error for path traversal attempt")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Fatalf("unexpected: %s", got)
	}
	long := strings.Repeat("x", 3000)
	got := truncate(long, maxBodyExcerpt)
	if len(got) > maxBodyExcerpt+3 { // +3 for the UTF-8 ellipsis
		t.Fatalf("truncate didn't bound: len=%d", len(got))
	}
}

func TestFetchWithGitHubToken(t *testing.T) {
	var gotAuth string
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/test/repo/issues/1": func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			issue := ghIssue{Title: "test", State: "open", HTMLURL: "https://github.com/test/repo/issues/1"}
			issue.User.Login = "user"
			json.NewEncoder(w).Encode(issue)
		},
		"/repos/test/repo/issues/1/comments": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]ghComment{})
		},
	})
	client.Token = "ghp_testtoken123"

	_, err := client.FetchIssueContext(context.Background(), "test/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer ghp_testtoken123" {
		t.Fatalf("expected Bearer token, got %q", gotAuth)
	}
}
