package cli

import (
	"reflect"
	"testing"
	"time"
)

func TestReviewGateEnvInjectsOpenRouterKeyWithoutDuplicates(t *testing.T) {
	env := reviewGateEnv(
		[]string{"PATH=/bin", "OPENROUTER_API_KEY=old", "OTHER=1"},
		[]string{"OPENROUTER_API_KEY=vault", "ANTHROPIC_API_KEY=anthropic"},
		"fresh",
		"x-ai/grok-4.3",
	)

	want := map[string]string{
		"PATH":                         "/bin",
		"OTHER":                        "1",
		"ANTHROPIC_API_KEY":            "anthropic",
		"OPENROUTER_API_KEY":           "fresh",
		"OPENROUTER_REVIEW_GATE_MODEL": "x-ai/grok-4.3",
	}
	got := map[string]string{}
	for _, item := range env {
		key, val, ok := splitEnv(item)
		if !ok {
			t.Fatalf("malformed env item %q", item)
		}
		if _, exists := got[key]; exists {
			t.Fatalf("duplicate env key %q in %v", key, env)
		}
		got[key] = val
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func TestReviewGateRunnerArgsIncludesOnlySetOptionalFlags(t *testing.T) {
	flags := reviewGateOptions{
		input:        "gate-input",
		out:          "result.md",
		model:        "x-ai/grok-4.3",
		maxFileBytes: 123,
		timeout:      time.Minute,
	}

	got := reviewGateRunnerArgs(flags)
	want := []string{
		"--input", "gate-input",
		"--out", "result.md",
		"--model", "x-ai/grok-4.3",
		"--max-file-bytes", "123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func splitEnv(item string) (string, string, bool) {
	for i, ch := range item {
		if ch == '=' {
			return item[:i], item[i+1:], true
		}
	}
	return "", "", false
}
