package harness

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func goodRequest(attempt string) Request {
	return Request{
		RequestID: "req-1", TaskID: "task-1", AttemptID: attempt,
		OfferingID: "codex-gpt-5", Harness: "fake", Model: "gpt-5",
		Workspace: "/work/repo", Prompt: "do the thing",
		Limits: Limits{Timeout: 5 * time.Second, CancelGrace: 200 * time.Millisecond},
	}
}

func fullCaps() Capabilities {
	return Capabilities{Launch: true, Resume: true, Cancel: true, Usage: true, NativeApprovals: true, Models: []string{"gpt-5"}}
}

type fixedApprover struct {
	option string
	err    error
	calls  int
}

func (a *fixedApprover) Decide(_ context.Context, _ Request, _ NativePrompt) (string, error) {
	a.calls++
	return a.option, a.err
}

var cmdPrompt = NativePrompt{
	ID: "p1", Action: "command", Detail: "go test ./...",
	Options: []NativeOption{{ID: "accept", Label: "Yes"}, {ID: "acceptForSession", Label: "Yes, this session"}, {ID: "decline", Label: "No"}},
}

// TestLifecycle is the shared lifecycle fixture: each case is one attempt
// through Run, checked by recorded receipt states and adapter effects.
func TestLifecycle(t *testing.T) {
	cases := []struct {
		name       string
		caps       Capabilities
		script     FakeScript
		approver   *fixedApprover
		mutate     func(*Request)
		cancelCtx  time.Duration // cancel caller ctx after this
		wantErr    error
		wantStarts int
		wantStates []State
		wantReason string
		check      func(t *testing.T, f *Fake, r Receipt)
	}{
		{
			name:       "success with reported usage",
			caps:       fullCaps(),
			script:     FakeScript{Result: Result{Success: true, Summary: "done", Model: "gpt-5", Usage: &Usage{Kind: UsageReported, InputTokens: 10, OutputTokens: 5}}},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateSucceeded},
			check: func(t *testing.T, f *Fake, r Receipt) {
				if r.ActualModel != "gpt-5" || r.Usage.Kind != UsageReported || r.NativeSession != "fake-session-a1" {
					t.Fatalf("receipt = %+v", r)
				}
			},
		},
		{
			name:       "missing usage is unknown, never zero",
			caps:       fullCaps(),
			script:     FakeScript{Result: Result{Success: true}},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateSucceeded},
			check: func(t *testing.T, f *Fake, r Receipt) {
				if r.Usage == nil || r.Usage.Kind != UsageUnknown {
					t.Fatalf("usage = %+v, want unknown", r.Usage)
				}
			},
		},
		{
			name:       "runtime failure",
			caps:       fullCaps(),
			script:     FakeScript{Result: Result{Success: false, Err: "tests failed"}},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateFailed},
			wantReason: "tests failed",
		},
		{
			name:       "runtime used another model: failed, not success",
			caps:       fullCaps(),
			script:     FakeScript{Result: Result{Success: true, Model: "gpt-4o-mini"}},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateFailed},
			wantReason: "model mismatch",
		},
		{
			name:       "start fails cleanly",
			caps:       fullCaps(),
			script:     FakeScript{StartErr: errors.New("binary not found")},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateFailed},
		},
		{
			name:       "ambiguous start is unknown (reconcile before retry)",
			caps:       fullCaps(),
			script:     FakeScript{StartErr: errors.Join(ErrStartAmbiguous, errors.New("connection reset after send"))},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateUnknown},
		},
		{
			name:       "timeout cancels and is confirmed",
			caps:       fullCaps(),
			script:     FakeScript{Hang: true},
			mutate:     func(r *Request) { r.Limits.Timeout = 50 * time.Millisecond },
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateCancelRequested, StateCancelled},
			wantReason: "timeout",
			check: func(t *testing.T, f *Fake, r Receipt) {
				if !f.Cancelled() {
					t.Fatal("adapter never got Cancel")
				}
			},
		},
		{
			name:       "caller cancel",
			caps:       fullCaps(),
			script:     FakeScript{Hang: true},
			cancelCtx:  30 * time.Millisecond,
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateCancelRequested, StateCancelled},
			wantReason: "cancelled by caller",
		},
		{
			name:       "cancel not confirmed is unknown, not cancelled",
			caps:       fullCaps(),
			script:     FakeScript{Hang: true, IgnoreCancel: true},
			mutate:     func(r *Request) { r.Limits.Timeout = 30 * time.Millisecond },
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateCancelRequested, StateUnknown},
			wantReason: "exit not confirmed",
		},
		{
			name:       "native prompt answered with a native option, verbatim",
			caps:       fullCaps(),
			script:     FakeScript{Prompts: []NativePrompt{cmdPrompt}, Result: Result{Success: true}},
			approver:   &fixedApprover{option: "acceptForSession"},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateSucceeded},
			check: func(t *testing.T, f *Fake, r Receipt) {
				if got := f.Responses(); len(got) != 1 || got[0] != [2]string{"p1", "acceptForSession"} {
					t.Fatalf("responses = %v", got)
				}
			},
		},
		{
			name:       "option the runtime didn't offer is never sent",
			caps:       fullCaps(),
			script:     FakeScript{Prompts: []NativePrompt{cmdPrompt}, Result: Result{Success: true}},
			approver:   &fixedApprover{option: "approve"},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateCancelRequested, StateCancelled},
			wantReason: "not offered",
			check: func(t *testing.T, f *Fake, r Receipt) {
				if got := f.Responses(); len(got) != 0 {
					t.Fatalf("responses = %v, want none", got)
				}
			},
		},
		{
			name:       "no approver: nothing invented, attempt stops",
			caps:       fullCaps(),
			script:     FakeScript{Prompts: []NativePrompt{cmdPrompt}, Result: Result{Success: true}},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateCancelRequested, StateCancelled},
			wantReason: "no approver",
			check: func(t *testing.T, f *Fake, r Receipt) {
				if got := f.Responses(); len(got) != 0 {
					t.Fatalf("responses = %v, want none", got)
				}
			},
		},
		{
			name:       "approver error stops the attempt",
			caps:       fullCaps(),
			script:     FakeScript{Prompts: []NativePrompt{cmdPrompt}, Result: Result{Success: true}},
			approver:   &fixedApprover{err: errors.New("approval expired")},
			wantStarts: 1,
			wantStates: []State{StateDispatching, StateStarted, StateCancelRequested, StateCancelled},
			wantReason: "approval expired",
		},
		// Refused before dispatch: no receipt, no Start.
		{
			name:    "unsupported: no cancel",
			caps:    Capabilities{Launch: true, Models: []string{"gpt-5"}},
			wantErr: ErrUnsupported, wantReason: "cancel",
		},
		{
			name:    "unsupported: no launch",
			caps:    Capabilities{Cancel: true},
			wantErr: ErrUnsupported, wantReason: "launch",
		},
		{
			name:    "unsupported: model not offered (no silent fallback)",
			caps:    fullCaps(),
			mutate:  func(r *Request) { r.Model = "claude-opus" },
			wantErr: ErrUnsupported, wantReason: `model "claude-opus"`,
		},
		{
			name:    "unsupported: resume",
			caps:    Capabilities{Launch: true, Cancel: true},
			mutate:  func(r *Request) { r.ResumeSession = "thread-9" },
			wantErr: ErrUnsupported, wantReason: "resume",
		},
		{
			name:    "unsupported: usage required",
			caps:    Capabilities{Launch: true, Cancel: true},
			mutate:  func(r *Request) { r.RequireUsage = true },
			wantErr: ErrUnsupported, wantReason: "usage",
		},
		{
			name:    "unsupported: wrong harness",
			caps:    fullCaps(),
			mutate:  func(r *Request) { r.Harness = "claude-code" },
			wantErr: ErrUnsupported, wantReason: "claude-code",
		},
		{
			name:    "invalid: no timeout",
			caps:    fullCaps(),
			mutate:  func(r *Request) { r.Limits.Timeout = 0 },
			wantErr: ErrInvalidRequest, wantReason: "timeout",
		},
		{
			name:    "invalid: relative workspace",
			caps:    fullCaps(),
			mutate:  func(r *Request) { r.Workspace = "repo" },
			wantErr: ErrInvalidRequest, wantReason: "absolute",
		},
		{
			name:    "invalid: missing ids",
			caps:    fullCaps(),
			mutate:  func(r *Request) { r.AttemptID, r.OfferingID = "", "" },
			wantErr: ErrInvalidRequest, wantReason: "attempt_id, offering_id",
		},
		{
			name:    "invalid: half an approval binding",
			caps:    fullCaps(),
			mutate:  func(r *Request) { r.Approval = &ApprovalBinding{ApprovalID: "apv_1"} },
			wantErr: ErrInvalidRequest, wantReason: "payload_hash",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &Fake{NameValue: "fake", Caps: c.caps, Script: c.script}
			rec := &MemoryRecorder{}
			req := goodRequest("a1")
			if c.mutate != nil {
				c.mutate(&req)
			}
			ctx := context.Background()
			if c.cancelCtx > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				time.AfterFunc(c.cancelCtx, cancel)
			}
			var appr Approver
			if c.approver != nil {
				appr = c.approver
			}
			r, err := Run(ctx, f, rec, appr, req)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("err = %v, want %v", err, c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantReason) {
					t.Fatalf("err = %v, want it to mention %q", err, c.wantReason)
				}
				if f.Starts() != 0 || len(rec.Receipts()) != 0 {
					t.Fatalf("refused request still dispatched: starts=%d receipts=%d", f.Starts(), len(rec.Receipts()))
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if f.Starts() != c.wantStarts {
				t.Fatalf("starts = %d, want %d", f.Starts(), c.wantStarts)
			}
			if got := rec.States(req.AttemptID); !slices.Equal(got, c.wantStates) {
				t.Fatalf("states = %v, want %v", got, c.wantStates)
			}
			last := rec.Receipts()[len(rec.Receipts())-1]
			if last.State != r.State || !r.State.Terminal() {
				t.Fatalf("returned %v, last recorded %v", r.State, last.State)
			}
			if c.wantReason != "" && !strings.Contains(r.Reason, c.wantReason) {
				t.Fatalf("reason = %q, want it to mention %q", r.Reason, c.wantReason)
			}
			for _, x := range rec.Receipts() {
				if strings.Contains(x.Reason, req.Prompt) {
					t.Fatalf("receipt carries prompt text: %+v", x)
				}
			}
			if c.check != nil {
				c.check(t, f, r)
			}
		})
	}
}

// Reusing an attempt ID is refused before launch; a retry needs a new
// attempt ID under the same request ID, and the first outcome is kept.
func TestRetryNeedsNewAttempt(t *testing.T) {
	rec := &MemoryRecorder{}
	f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Result: Result{Success: false, Err: "flaky"}}}
	if _, err := Run(context.Background(), f, rec, nil, goodRequest("a1")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), f, rec, nil, goodRequest("a1")); !errors.Is(err, ErrDuplicateAttempt) {
		t.Fatalf("replayed attempt: err = %v, want ErrDuplicateAttempt", err)
	}
	if f.Starts() != 1 {
		t.Fatalf("starts = %d after a replayed attempt, want 1", f.Starts())
	}
	f.Script = FakeScript{Result: Result{Success: true}}
	r, err := Run(context.Background(), f, rec, nil, goodRequest("a2"))
	if err != nil || r.State != StateSucceeded {
		t.Fatalf("retry: %v %v", r.State, err)
	}
	if got := rec.States("a1"); got[len(got)-1] != StateFailed {
		t.Fatalf("first attempt outcome lost: %v", got)
	}
	for _, x := range rec.Receipts() {
		if x.RequestID != "req-1" {
			t.Fatalf("request id changed across attempts: %+v", x)
		}
	}
}

// Receipts are local truth: if the pre-launch receipt can't be written,
// nothing starts; if the terminal one can't, the outcome is unknown even
// when the adapter reported success.
func TestReceiptFailures(t *testing.T) {
	t.Run("before launch", func(t *testing.T) {
		rec := &MemoryRecorder{Fail: func(Receipt) error { return errors.New("disk full") }}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Result: Result{Success: true}}}
		if _, err := Run(context.Background(), f, rec, nil, goodRequest("a1")); err == nil || !strings.Contains(err.Error(), "not dispatched") {
			t.Fatalf("err = %v", err)
		}
		if f.Starts() != 0 {
			t.Fatal("started without a pre-launch receipt")
		}
	})
	t.Run("terminal", func(t *testing.T) {
		rec := &MemoryRecorder{Fail: func(r Receipt) error {
			if r.State.Terminal() {
				return errors.New("disk full")
			}
			return nil
		}}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Result: Result{Success: true}}}
		r, err := Run(context.Background(), f, rec, nil, goodRequest("a1"))
		if err == nil || r.State != StateUnknown {
			t.Fatalf("state = %v err = %v, want unknown + error", r.State, err)
		}
	})
	t.Run("started", func(t *testing.T) {
		rec := &MemoryRecorder{Fail: func(r Receipt) error {
			if r.State == StateStarted {
				return errors.New("disk full")
			}
			return nil
		}}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true}}
		r, err := Run(context.Background(), f, rec, nil, goodRequest("a1"))
		if err == nil || r.State != StateUnknown || !f.Cancelled() {
			t.Fatalf("state = %v err = %v cancelled = %v; want unknown, error, and the unrecorded attempt cancelled", r.State, err, f.Cancelled())
		}
	})
}

// Approval binding travels onto every receipt.
func TestApprovalBindingOnReceipts(t *testing.T) {
	rec := &MemoryRecorder{}
	f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Result: Result{Success: true}}}
	req := goodRequest("a1")
	req.Approval = &ApprovalBinding{ApprovalID: "apv_1", PayloadHash: "sha256:abc"}
	if _, err := Run(context.Background(), f, rec, nil, req); err != nil {
		t.Fatal(err)
	}
	for _, x := range rec.Receipts() {
		if x.ApprovalID != "apv_1" {
			t.Fatalf("receipt without approval id: %+v", x)
		}
	}
}

// An adapter that can't list models passes the exact model through; it is
// never swapped for a default.
func TestUnlistedModelsPassThrough(t *testing.T) {
	c := fullCaps()
	c.Models = nil
	if err := Check("fake", c, goodRequest("a1")); err != nil {
		t.Fatal(err)
	}
}
