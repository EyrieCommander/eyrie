package reviewops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultGitHubBaseURL = "https://api.github.com"
	maxResponseBytes     = 1 << 20 // 1 MiB
	maxComments          = 20
	maxBodyExcerpt       = 2000
	maxCommentExcerpt    = 1000
)

// GitHubClient fetches read-only source context from the GitHub REST API.
// It supports configurable base URL and HTTP client for testing with httptest.
type GitHubClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string // optional; set from GITHUB_TOKEN for higher rate limits
}

// NewGitHubClient returns a client configured for the public GitHub API.
// It reads GITHUB_TOKEN from the environment if available.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		BaseURL:    defaultGitHubBaseURL,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		Token:      os.Getenv("GITHUB_TOKEN"),
	}
}

// SourceContext is the structured snapshot persisted as an artifact.
type SourceContext struct {
	Repo         string           `json:"repo"`
	TargetNumber int              `json:"target_number"`
	TargetType   string           `json:"target_type"` // "issue" or "pull_request"
	Title        string           `json:"title"`
	State        string           `json:"state"`
	Author       string           `json:"author"`
	URL          string           `json:"url"`
	Labels       []string         `json:"labels"`
	BodyExcerpt  string           `json:"body_excerpt"`
	Comments     []ContextComment `json:"comments"`
	FetchedAt    time.Time        `json:"fetched_at"`
}

type ContextComment struct {
	Author      string `json:"author"`
	BodyExcerpt string `json:"body_excerpt"`
	CreatedAt   string `json:"created_at"`
}

// ghIssue is the subset of GitHub's issue/PR response we parse.
type ghIssue struct {
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

type ghComment struct {
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
}

// FetchIssueContext fetches issue metadata and up to maxComments comments.
func (c *GitHubClient) FetchIssueContext(ctx context.Context, repo string, number int) (*SourceContext, error) {
	issue, err := c.getIssue(ctx, repo, number)
	if err != nil {
		return nil, fmt.Errorf("fetch issue %s#%d: %w", repo, number, err)
	}

	targetType := "issue"
	if issue.PullRequest != nil {
		targetType = "pull_request"
	}

	labels := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		labels = append(labels, l.Name)
	}

	sc := &SourceContext{
		Repo:         repo,
		TargetNumber: number,
		TargetType:   targetType,
		Title:        issue.Title,
		State:        issue.State,
		Author:       issue.User.Login,
		URL:          issue.HTMLURL,
		Labels:       labels,
		BodyExcerpt:  truncate(issue.Body, maxBodyExcerpt),
		FetchedAt:    time.Now(),
	}

	comments, err := c.getIssueComments(ctx, repo, number)
	if err != nil {
		// Non-fatal: we have metadata, just no comments.
		sc.Comments = []ContextComment{}
		return sc, nil
	}
	sc.Comments = comments

	return sc, nil
}

// FetchPRContext fetches PR metadata and bounded review comments.
// It first fetches via the issues endpoint for metadata, then tries
// to fetch PR review comments for richer review context.
func (c *GitHubClient) FetchPRContext(ctx context.Context, repo string, number int) (*SourceContext, error) {
	// Use the issues endpoint which works for both issues and PRs.
	sc, err := c.FetchIssueContext(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	sc.TargetType = "pull_request"

	// Try to also fetch PR review comments (inline code comments).
	reviewComments, err := c.getPRReviewComments(ctx, repo, number)
	if err == nil && len(reviewComments) > 0 {
		sc.Comments = append(sc.Comments, reviewComments...)
		// Re-bound if combined comments exceed limit.
		if len(sc.Comments) > maxComments {
			sc.Comments = sc.Comments[:maxComments]
		}
	}

	return sc, nil
}

// RenderMarkdown produces a human-readable markdown representation of the source context.
func (sc *SourceContext) RenderMarkdown() string {
	var b strings.Builder
	b.WriteString("# Source Context\n\n")
	fmt.Fprintf(&b, "- **Repo**: %s\n", sc.Repo)
	fmt.Fprintf(&b, "- **Target**: #%d (%s)\n", sc.TargetNumber, sc.TargetType)
	fmt.Fprintf(&b, "- **Title**: %s\n", sc.Title)
	fmt.Fprintf(&b, "- **State**: %s\n", sc.State)
	fmt.Fprintf(&b, "- **Author**: %s\n", sc.Author)
	fmt.Fprintf(&b, "- **URL**: %s\n", sc.URL)
	if len(sc.Labels) > 0 {
		fmt.Fprintf(&b, "- **Labels**: %s\n", strings.Join(sc.Labels, ", "))
	}
	fmt.Fprintf(&b, "- **Fetched at**: %s\n", sc.FetchedAt.UTC().Format(time.RFC3339))

	b.WriteString("\n## Body excerpt\n\n")
	if sc.BodyExcerpt != "" {
		b.WriteString(sc.BodyExcerpt)
	} else {
		b.WriteString("_(empty)_")
	}
	b.WriteString("\n")

	if len(sc.Comments) > 0 {
		fmt.Fprintf(&b, "\n## Comments (%d)\n\n", len(sc.Comments))
		for i, c := range sc.Comments {
			fmt.Fprintf(&b, "### Comment %d — %s", i+1, c.Author)
			if c.CreatedAt != "" {
				fmt.Fprintf(&b, " (%s)", c.CreatedAt)
			}
			fmt.Fprintf(&b, "\n\n%s\n\n", c.BodyExcerpt)
		}
	}

	return b.String()
}

func (c *GitHubClient) getIssue(ctx context.Context, repo string, number int) (*ghIssue, error) {
	url := fmt.Sprintf("%s/repos/%s/issues/%d", c.BaseURL, repo, number)
	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	var issue ghIssue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("decode issue: %w", err)
	}
	return &issue, nil
}

func (c *GitHubClient) getIssueComments(ctx context.Context, repo string, number int) ([]ContextComment, error) {
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments?per_page=%d", c.BaseURL, repo, number, maxComments)
	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	var raw []ghComment
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode comments: %w", err)
	}
	out := make([]ContextComment, 0, len(raw))
	for _, r := range raw {
		out = append(out, ContextComment{
			Author:      r.User.Login,
			BodyExcerpt: truncate(r.Body, maxCommentExcerpt),
			CreatedAt:   r.CreatedAt,
		})
	}
	if len(out) > maxComments {
		out = out[:maxComments]
	}
	return out, nil
}

func (c *GitHubClient) getPRReviewComments(ctx context.Context, repo string, number int) ([]ContextComment, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/comments?per_page=%d", c.BaseURL, repo, number, maxComments)
	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	var raw []ghComment
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode review comments: %w", err)
	}
	out := make([]ContextComment, 0, len(raw))
	for _, r := range raw {
		out = append(out, ContextComment{
			Author:      r.User.Login,
			BodyExcerpt: truncate(r.Body, maxCommentExcerpt),
			CreatedAt:   r.CreatedAt,
		})
	}
	if len(out) > maxComments {
		out = out[:maxComments]
	}
	return out, nil
}

func (c *GitHubClient) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API %s returned %d: %s", url, resp.StatusCode, string(body))
	}

	return body, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
