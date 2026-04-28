package reviewops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validResult() TaskResult {
	return TaskResult{
		SchemaVersion:       TaskResultSchemaVersion,
		Summary:             "Test summary",
		DraftMarkdown:       "# Draft\nSome content.",
		ProposedActions:     []ProposedAction{},
		Confidence:          0.85,
		Severity:            "medium",
		Notes:               "test note",
		RequiresHumanReview: true,
		GeneratedAt:         time.Now(),
	}
}

func TestTaskResultValidation_Valid(t *testing.T) {
	tr := validResult()
	if err := tr.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestTaskResultValidation_MissingSchemaVersion(t *testing.T) {
	tr := validResult()
	tr.SchemaVersion = ""
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("expected schema_version error, got: %v", err)
	}
}

func TestTaskResultValidation_WrongSchemaVersion(t *testing.T) {
	tr := validResult()
	tr.SchemaVersion = "99.0"
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported version error, got: %v", err)
	}
}

func TestTaskResultValidation_MissingSummary(t *testing.T) {
	tr := validResult()
	tr.Summary = "   "
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("expected summary error, got: %v", err)
	}
}

func TestTaskResultValidation_MissingDraftMarkdown(t *testing.T) {
	tr := validResult()
	tr.DraftMarkdown = ""
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "draft_markdown") {
		t.Fatalf("expected draft_markdown error, got: %v", err)
	}
}

func TestTaskResultValidation_ConfidenceOutOfRange(t *testing.T) {
	for _, c := range []float64{-0.1, 1.5} {
		tr := validResult()
		tr.Confidence = c
		err := tr.Validate()
		if err == nil || !strings.Contains(err.Error(), "confidence") {
			t.Fatalf("expected confidence error for %f, got: %v", c, err)
		}
	}
}

func TestTaskResultValidation_InvalidSeverity(t *testing.T) {
	tr := validResult()
	tr.Severity = "banana"
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "severity") {
		t.Fatalf("expected severity error, got: %v", err)
	}
}

func TestTaskResultValidation_ValidSeverities(t *testing.T) {
	for _, sev := range []string{"low", "medium", "high", "critical", "info"} {
		tr := validResult()
		tr.Severity = sev
		if err := tr.Validate(); err != nil {
			t.Fatalf("severity %q should be valid, got: %v", sev, err)
		}
	}
}

func TestTaskResultValidation_MissingGeneratedAt(t *testing.T) {
	tr := validResult()
	tr.GeneratedAt = time.Time{}
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "generated_at") {
		t.Fatalf("expected generated_at error, got: %v", err)
	}
}

func TestTaskResultValidation_InvalidActionKind(t *testing.T) {
	tr := validResult()
	tr.ProposedActions = []ProposedAction{
		{Kind: "nuke_everything", Description: "bad"},
	}
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported kind") {
		t.Fatalf("expected action kind error, got: %v", err)
	}
}

func TestTaskResultValidation_ActionMissingDescription(t *testing.T) {
	tr := validResult()
	tr.ProposedActions = []ProposedAction{
		{Kind: ActionPostIssueComment, Description: ""},
	}
	err := tr.Validate()
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("expected action description error, got: %v", err)
	}
}

func TestTaskResultValidation_ValidActions(t *testing.T) {
	tr := validResult()
	tr.ProposedActions = []ProposedAction{
		{Kind: ActionPostIssueComment, Description: "Post triage"},
		{Kind: ActionSubmitPRReview, Description: "Submit review"},
		{Kind: ActionApplyLabels, Description: "Add bug label", Labels: []string{"bug"}},
	}
	if err := tr.Validate(); err != nil {
		t.Fatalf("expected valid with actions, got: %v", err)
	}
}

func TestTaskResultMarshalValidated(t *testing.T) {
	tr := validResult()
	tr.ProposedActions = []ProposedAction{
		{Kind: ActionPostIssueComment, Description: "Post comment", Body: "hello"},
	}
	s, err := tr.MarshalValidated()
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	var parsed TaskResult
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.SchemaVersion != TaskResultSchemaVersion {
		t.Fatalf("schema_version mismatch: %q", parsed.SchemaVersion)
	}
	if len(parsed.ProposedActions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(parsed.ProposedActions))
	}
}

func TestTaskResultMarshalValidated_RejectsInvalid(t *testing.T) {
	tr := validResult()
	tr.Summary = ""
	_, err := tr.MarshalValidated()
	if err == nil {
		t.Fatal("expected error for invalid result")
	}
}
