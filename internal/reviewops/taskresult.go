package reviewops

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Schema version for the TaskResult contract. Bump when fields are
// added or semantics change so consumers can migrate gracefully.
const TaskResultSchemaVersion = "1.0"

// ProposedActionKind identifies a structured write intent. These are
// inert descriptors only — no action executes without a future
// explicit approval + post step.
type ProposedActionKind string

const (
	ActionPostIssueComment ProposedActionKind = "post_issue_comment"
	ActionSubmitPRReview   ProposedActionKind = "submit_pr_review"
	ActionApplyLabels      ProposedActionKind = "apply_labels"
)

func validActionKind(k ProposedActionKind) bool {
	switch k {
	case ActionPostIssueComment, ActionSubmitPRReview, ActionApplyLabels:
		return true
	default:
		return false
	}
}

// ProposedAction is a structured intent that an agent/runner produces.
// It describes what should happen, not whether it will happen — Eyrie's
// approval layer decides that later.
type ProposedAction struct {
	Kind        ProposedActionKind `json:"kind"`
	Description string             `json:"description"`
	// Body holds the content for the action (e.g. comment text,
	// review body). Empty for label-only actions.
	Body string `json:"body,omitempty"`
	// Labels is populated only for apply_labels actions.
	Labels []string `json:"labels,omitempty"`
}

// TaskResult is the versioned, validated contract for agent-run task
// output. It is persisted as a task_result artifact (JSON) alongside
// the human-readable markdown draft.
type TaskResult struct {
	SchemaVersion      string           `json:"schema_version"`
	Summary            string           `json:"summary"`
	DraftMarkdown      string           `json:"draft_markdown"`
	ProposedActions    []ProposedAction `json:"proposed_actions"`
	Confidence         float64          `json:"confidence"`
	Severity           string           `json:"severity"`
	Notes              string           `json:"notes,omitempty"`
	RequiresHumanReview bool            `json:"requires_human_review"`
	GeneratedAt        time.Time        `json:"generated_at"`
}

// Validate checks the TaskResult for structural correctness.
// Returns nil if valid; an error describing the first violation otherwise.
func (tr *TaskResult) Validate() error {
	if tr.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}
	if tr.SchemaVersion != TaskResultSchemaVersion {
		return fmt.Errorf("unsupported schema_version %q (expected %q)", tr.SchemaVersion, TaskResultSchemaVersion)
	}
	if strings.TrimSpace(tr.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	if strings.TrimSpace(tr.DraftMarkdown) == "" {
		return fmt.Errorf("draft_markdown is required")
	}
	if tr.Confidence < 0 || tr.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1, got %f", tr.Confidence)
	}
	if strings.TrimSpace(tr.Severity) == "" {
		return fmt.Errorf("severity is required")
	}
	validSeverities := map[string]bool{"low": true, "medium": true, "high": true, "critical": true, "info": true}
	if !validSeverities[strings.ToLower(tr.Severity)] {
		return fmt.Errorf("invalid severity %q (expected low, medium, high, critical, or info)", tr.Severity)
	}
	if tr.GeneratedAt.IsZero() {
		return fmt.Errorf("generated_at is required")
	}
	for i, a := range tr.ProposedActions {
		if !validActionKind(a.Kind) {
			return fmt.Errorf("proposed_actions[%d]: unsupported kind %q", i, a.Kind)
		}
		if strings.TrimSpace(a.Description) == "" {
			return fmt.Errorf("proposed_actions[%d]: description is required", i)
		}
	}
	return nil
}

// MarshalJSON produces the JSON representation after validation.
// Returns an error if the result is invalid.
func (tr *TaskResult) MarshalValidated() (string, error) {
	if err := tr.Validate(); err != nil {
		return "", fmt.Errorf("invalid task result: %w", err)
	}
	b, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
