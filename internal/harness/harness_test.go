package harness

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
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
			wantReason: "runtime reported failure",
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
			wantReason: "approver returned an error",
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
			t.Cleanup(f.Finish)
			r, err := runCtxWithin(t, 5*time.Second, ctx, f, rec, appr, req)
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

// Review 72495816 #1: after a cancel, a Wait error other than ErrCancelled
// (e.g. transport failure) doesn't confirm exit: unknown, not cancelled.
func TestCancelWithWaitErrorIsUnknown(t *testing.T) {
	rec := &MemoryRecorder{}
	f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true, WaitErr: errors.New("broken pipe")}}
	req := goodRequest("a1")
	req.Limits.Timeout = 30 * time.Millisecond
	r, err := Run(context.Background(), f, rec, nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != StateUnknown || !strings.Contains(r.Reason, "wait failed") {
		t.Fatalf("state = %v reason = %q, want unknown/wait failed", r.State, r.Reason)
	}
}

// runWithin runs Run but fails the test (instead of hanging it) if Run
// doesn't return within d.
func runWithin(t *testing.T, d time.Duration, f *Fake, rec *MemoryRecorder, req Request) (Receipt, error) {
	t.Helper()
	type out struct {
		r   Receipt
		err error
	}
	ch := make(chan out, 1)
	go func() { r, err := Run(context.Background(), f, rec, nil, req); ch <- out{r, err} }()
	select {
	case o := <-ch:
		return o.r, o.err
	case <-time.After(d):
		t.Fatalf("Run did not return within %v (stalled cancel not bounded)", d)
		return Receipt{}, nil
	}
}

// Review #2: a stalled cancellation RPC can't hang Run; the whole cancel
// is bounded by CancelGrace, here and in receipt-failure cleanup.
func TestStalledCancelIsBounded(t *testing.T) {
	t.Run("after timeout", func(t *testing.T) {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true, IgnoreCancel: true, CancelBlocks: true}}
		t.Cleanup(f.Finish)
		req := goodRequest("a1")
		req.Limits.Timeout = 30 * time.Millisecond
		start := time.Now()
		r, err := runWithin(t, 3*time.Second, f, rec, req)
		if err != nil {
			t.Fatal(err)
		}
		if el := time.Since(start); el > 2*time.Second {
			t.Fatalf("Run took %v with a 200ms grace", el)
		}
		if r.State != StateUnknown || !strings.Contains(r.Reason, "cancel") {
			t.Fatalf("state = %v reason = %q", r.State, r.Reason)
		}
	})
	t.Run("started receipt fails", func(t *testing.T) {
		rec := &MemoryRecorder{Fail: func(r Receipt) error {
			if r.State == StateStarted {
				return errors.New("disk full")
			}
			return nil
		}}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true, IgnoreCancel: true, CancelBlocks: true}}
		t.Cleanup(f.Finish)
		start := time.Now()
		r, err := runWithin(t, 3*time.Second, f, rec, goodRequest("a1"))
		if err == nil || r.State != StateUnknown {
			t.Fatalf("state = %v err = %v", r.State, err)
		}
		if el := time.Since(start); el > 2*time.Second {
			t.Fatalf("cleanup cancel took %v", el)
		}
	})
	t.Run("cancel_requested receipt fails", func(t *testing.T) {
		rec := &MemoryRecorder{Fail: func(r Receipt) error {
			if r.State == StateCancelRequested {
				return errors.New("disk full")
			}
			return nil
		}}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true, IgnoreCancel: true, CancelBlocks: true}}
		t.Cleanup(f.Finish)
		req := goodRequest("a1")
		req.Limits.Timeout = 30 * time.Millisecond
		start := time.Now()
		if r, err := runWithin(t, 3*time.Second, f, rec, req); err == nil || r.State != StateUnknown {
			t.Fatalf("state = %v err = %v", r.State, err)
		}
		if el := time.Since(start); el > 2*time.Second {
			t.Fatalf("cleanup cancel took %v", el)
		}
	})
}

// slowApprover ignores ctx, then approves; done closes when it returns.
type slowApprover struct {
	delay time.Duration
	done  chan struct{}
}

func (a *slowApprover) Decide(context.Context, Request, NativePrompt) (string, error) {
	defer close(a.done)
	time.Sleep(a.delay)
	return "accept", nil
}

// Review #3: once cancellation begins, a pending approver decision can't
// reach the runtime, even if the approver ignores its context.
func TestLateApprovalAfterCancelIsNotSent(t *testing.T) {
	rec := &MemoryRecorder{}
	f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Prompts: []NativePrompt{cmdPrompt}, IgnoreCancel: true, KeepEventsOpen: true}}
	t.Cleanup(f.Finish)
	appr := &slowApprover{delay: 300 * time.Millisecond, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	req := goodRequest("a1")
	req.Limits.CancelGrace = time.Second // still cancelling when the approver returns
	r, _ := Run(ctx, f, rec, appr, req)
	<-appr.done
	time.Sleep(50 * time.Millisecond) // let a (wrong) Respond land
	if got := f.Responses(); len(got) != 0 {
		t.Fatalf("decision sent after cancellation began: %v", got)
	}
	if r.State != StateUnknown {
		t.Fatalf("state = %v, want unknown (runtime ignored cancel)", r.State)
	}
}

// Review #4: runtime text (summaries, errors) never reaches receipts; it
// may echo the prompt or a credential.
func TestRuntimeTextNotInReceipts(t *testing.T) {
	const secret = "sk-live-SECRET-123"
	leak := "echo: do the thing " + secret
	scripts := map[string]FakeScript{
		"success summary": {Result: Result{Success: true, Summary: leak}},
		"failure err":     {Result: Result{Success: false, Err: leak, Summary: leak}},
		"start error":     {StartErr: errors.New(leak)},
		"ambiguous start": {StartErr: errors.Join(ErrStartAmbiguous, errors.New(leak))},
		"wait error":      {WaitErr: errors.New(leak)},
		"cancel + wait":   {Hang: true, WaitErr: errors.New(leak)},
		"cancel error":    {Hang: true, IgnoreCancel: true, CancelErr: errors.New(leak)},
	}
	for name, sc := range scripts {
		t.Run(name, func(t *testing.T) {
			rec := &MemoryRecorder{}
			f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: sc}
			t.Cleanup(f.Finish)
			req := goodRequest("a1")
			req.Limits.Timeout = 30 * time.Millisecond
			r, _ := Run(context.Background(), f, rec, nil, req)
			for _, x := range append(rec.Receipts(), r) {
				if strings.Contains(x.Reason, secret) || strings.Contains(x.Reason, req.Prompt) {
					t.Fatalf("receipt leaks runtime text: %q", x.Reason)
				}
			}
		})
	}
}

// Review #5: Run's background workers (Wait, event pump) don't outlive it,
// even when the runtime never confirms exit and keeps events open.
func TestNoWorkerLeakAfterUnconfirmedCancel(t *testing.T) {
	settle := func(max int) int {
		deadline := time.Now().Add(2 * time.Second)
		n := runtime.NumGoroutine()
		for n > max && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			n = runtime.NumGoroutine()
		}
		return n
	}
	before := settle(runtime.NumGoroutine())
	f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true, IgnoreCancel: true, KeepEventsOpen: true}}
	req := goodRequest("a1")
	req.Limits.Timeout = 30 * time.Millisecond
	r, err := Run(context.Background(), f, &MemoryRecorder{}, nil, req)
	if err != nil || r.State != StateUnknown {
		t.Fatalf("state = %v err = %v", r.State, err)
	}
	// The fake's own runner is still blocked (it ignored cancel): +1.
	if n := settle(before + 1); n > before+1 {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines %d -> %d after Run returned; leaked workers:\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
	f.Finish()
	if n := settle(before); n > before {
		t.Fatalf("goroutines %d -> %d after the fake finished", before, n)
	}
}

// Review 1b9573cd #1: approval paths don't leak runtime or approver text
// (prompt fields, Respond errors, approver errors) into receipts.
func TestApprovalTextNotInReceipts(t *testing.T) {
	const secret = "sk-live-SECRET-456"
	leaky := NativePrompt{
		ID: "p-" + secret, Action: "command " + secret, Detail: "curl -H 'Authorization: " + secret + "'",
		Options: []NativeOption{{ID: "accept", Label: "Yes " + secret}},
	}
	cases := map[string]struct {
		script FakeScript
		appr   Approver
	}{
		"no approver":        {FakeScript{Prompts: []NativePrompt{leaky}, KeepEventsOpen: true}, nil},
		"approver error":     {FakeScript{Prompts: []NativePrompt{leaky}, KeepEventsOpen: true}, &fixedApprover{err: errors.New("denied: " + secret)}},
		"option not offered": {FakeScript{Prompts: []NativePrompt{leaky}, KeepEventsOpen: true}, &fixedApprover{option: "approve-" + secret}},
		"respond error":      {FakeScript{Prompts: []NativePrompt{leaky}, KeepEventsOpen: true, RespondErr: errors.New("rpc: " + secret)}, &fixedApprover{option: "accept"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &MemoryRecorder{}
			f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: c.script}
			t.Cleanup(f.Finish)
			r, _ := Run(context.Background(), f, rec, c.appr, goodRequest("a1"))
			if r.State != StateCancelled {
				t.Fatalf("state = %v, want cancelled after the approval failure", r.State)
			}
			for _, x := range append(rec.Receipts(), r) {
				if strings.Contains(x.Reason, secret) {
					t.Fatalf("receipt leaks approval text: %q", x.Reason)
				}
			}
		})
	}
}

// Review #2: caller cancellation reaches startup.
func TestCancellationDuringStart(t *testing.T) {
	t.Run("already cancelled: nothing launches or records", func(t *testing.T) {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Result: Result{Success: true}}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := Run(ctx, f, rec, nil, goodRequest("a1")); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if f.Starts() != 0 || len(rec.Receipts()) != 0 {
			t.Fatalf("starts = %d receipts = %d, want none", f.Starts(), len(rec.Receipts()))
		}
	})
	t.Run("slow start honours cancel", func(t *testing.T) {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{StartDelay: 10 * time.Second}}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		start := time.Now()
		r, err := Run(ctx, f, rec, nil, goodRequest("a1"))
		if err != nil {
			t.Fatal(err)
		}
		if el := time.Since(start); el > 2*time.Second {
			t.Fatalf("Run took %v; cancel didn't reach Start", el)
		}
		if got := rec.States("a1"); !slices.Equal(got, []State{StateDispatching, StateCancelled}) {
			t.Fatalf("states = %v", got)
		}
		if !strings.Contains(r.Reason, "cancelled by caller during start") {
			t.Fatalf("reason = %q", r.Reason)
		}
	})
	t.Run("timeout during slow start", func(t *testing.T) {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{StartDelay: 10 * time.Second}}
		req := goodRequest("a1")
		req.Limits.Timeout = 30 * time.Millisecond
		r, err := Run(context.Background(), f, rec, nil, req)
		if err != nil || r.State != StateCancelled || !strings.Contains(r.Reason, "timeout") {
			t.Fatalf("state = %v reason = %q err = %v", r.State, r.Reason, err)
		}
	})
	t.Run("start ignores cancel: bounded, unknown", func(t *testing.T) {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{StartDelay: 2 * time.Second, StartIgnoresCtx: true, Hang: true}}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		start := time.Now()
		r, err := Run(ctx, f, rec, nil, goodRequest("a1"))
		if err != nil {
			t.Fatal(err)
		}
		if el := time.Since(start); el > time.Second {
			t.Fatalf("Run took %v with a 200ms grace", el)
		}
		if r.State != StateUnknown {
			t.Fatalf("state = %v, want unknown (start may still succeed)", r.State)
		}
		// The handle Start returns later is cancelled, not left running.
		deadline := time.Now().Add(4 * time.Second)
		for !f.Cancelled() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !f.Cancelled() {
			t.Fatal("late handle from a hung Start was never cancelled")
		}
	})
	t.Run("start returns a handle as cancel arrives: cancelled", func(t *testing.T) {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{StartDelay: 100 * time.Millisecond, StartIgnoresCtx: true, Hang: true}}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(20*time.Millisecond, cancel)
		r, err := Run(ctx, f, rec, nil, goodRequest("a1"))
		if err != nil {
			t.Fatal(err)
		}
		if got := rec.States("a1"); !slices.Equal(got, []State{StateDispatching, StateStarted, StateCancelRequested, StateCancelled}) {
			t.Fatalf("states = %v", got)
		}
		if !f.Cancelled() || r.State != StateCancelled {
			t.Fatalf("state = %v cancelled = %v", r.State, f.Cancelled())
		}
	})
}

// ctxApprover blocks until its context ends (an outstanding decision).
type ctxApprover struct{ entered chan struct{} }

func (a *ctxApprover) Decide(ctx context.Context, _ Request, _ NativePrompt) (string, error) {
	close(a.entered)
	<-ctx.Done()
	return "", ctx.Err()
}

// Review #3: shutdown with an outstanding approver decision leaves nothing
// running once Run returns (the approver honours ctx, as Approver requires).
func TestNoLeakWithOutstandingDecision(t *testing.T) {
	settle := func(max int) int {
		deadline := time.Now().Add(2 * time.Second)
		n := runtime.NumGoroutine()
		for n > max && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			n = runtime.NumGoroutine()
		}
		return n
	}
	before := settle(runtime.NumGoroutine())
	f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Prompts: []NativePrompt{cmdPrompt}, IgnoreCancel: true, KeepEventsOpen: true}}
	appr := &ctxApprover{entered: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-appr.entered; cancel() }()
	r, _ := Run(ctx, f, &MemoryRecorder{}, appr, goodRequest("a1"))
	if r.State != StateUnknown {
		t.Fatalf("state = %v", r.State)
	}
	// Only the fake's own runner (which ignored cancel) remains: +1.
	if n := settle(before + 1); n > before+1 {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines %d -> %d after Run returned:\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
	f.Finish()
	if n := settle(before); n > before {
		t.Fatalf("goroutines %d -> %d after the fake finished", before, n)
	}
}

// gatedApprover ignores ctx and returns "accept" only when release closes.
type gatedApprover struct {
	entered, release, done chan struct{}
}

func (a *gatedApprover) Decide(context.Context, Request, NativePrompt) (string, error) {
	defer close(a.done)
	close(a.entered)
	<-a.release
	return "accept", nil
}

// A ctx-ignoring approver can't hold Run's own workers: Run returns, its
// event pump exits while the approver is still stuck, and the decision it
// returns later is not sent.
func TestStuckApproverDoesNotHoldRun(t *testing.T) {
	before := settleGoroutines(runtime.NumGoroutine())
	rec := &MemoryRecorder{}
	f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Prompts: []NativePrompt{cmdPrompt}, KeepEventsOpen: true}}
	t.Cleanup(f.Finish)
	appr := &gatedApprover{entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-appr.release:
		default:
			close(appr.release)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-appr.entered; cancel() }()
	r, err := runCtxWithin(t, 3*time.Second, ctx, f, rec, appr, goodRequest("a1"))
	if err != nil {
		t.Fatal(err)
	}
	if r.State != StateCancelled {
		t.Fatalf("state = %v", r.State)
	}
	// The approver is still blocked (not released). Only its own call may
	// remain: +1. Run's event pump must have exited.
	if n := settleGoroutines(before + 1); n > before+1 {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines %d -> %d while the approver is stuck:\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
	close(appr.release)
	<-appr.done
	time.Sleep(50 * time.Millisecond)
	if got := f.Responses(); len(got) != 0 {
		t.Fatalf("late decision sent: %v", got)
	}
	if n := settleGoroutines(before); n > before {
		t.Fatalf("goroutines %d -> %d after the approver returned", before, n)
	}
}

// runCtxWithin is runWithin with a caller context and approver.
func runCtxWithin(t *testing.T, d time.Duration, ctx context.Context, f *Fake, rec *MemoryRecorder, appr Approver, req Request) (Receipt, error) {
	t.Helper()
	type out struct {
		r   Receipt
		err error
	}
	ch := make(chan out, 1)
	go func() { r, err := Run(ctx, f, rec, appr, req); ch <- out{r, err} }()
	select {
	case o := <-ch:
		return o.r, o.err
	case <-time.After(d):
		t.Fatalf("Run did not return within %v", d)
		return Receipt{}, nil
	}
}

// settleGoroutines waits up to 2s for the goroutine count to drop to max
// and returns the count it reached.
func settleGoroutines(max int) int {
	deadline := time.Now().Add(2 * time.Second)
	n := runtime.NumGoroutine()
	for n > max && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

// countingApprover approves immediately and counts calls (race-safe).
type countingApprover struct{ calls atomic.Int32 }

func (a *countingApprover) Decide(context.Context, Request, NativePrompt) (string, error) {
	a.calls.Add(1)
	return "accept", nil
}

// Review 9dfffaf0 #1: when Start returns a handle after cancellation began,
// a prompt the runtime already buffered is never put to the approver, let
// alone answered.
func TestPromptAfterCancelledStartIsNeverDecided(t *testing.T) {
	for i := 0; i < 50; i++ {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{
			StartDelay: 60 * time.Millisecond, StartIgnoresCtx: true,
			Prompts: []NativePrompt{cmdPrompt}, KeepEventsOpen: true,
		}}
		appr := &countingApprover{}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(10*time.Millisecond, cancel)
		r, err := Run(ctx, f, rec, appr, goodRequest("a1"))
		f.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(r.Reason, "cancelled by caller") {
			t.Fatalf("reason = %q", r.Reason)
		}
		time.Sleep(10 * time.Millisecond)
		if n := appr.calls.Load(); n != 0 {
			t.Fatalf("run %d: approver asked %d times for a cancelled attempt", i, n)
		}
		if got := f.Responses(); len(got) != 0 {
			t.Fatalf("run %d: answered %v for a cancelled attempt", i, got)
		}
	}
}

// stallRecorder blocks (ignoring ctx) on receipts of one state until
// release closes; onStall runs when such a write begins.
type stallRecorder struct {
	MemoryRecorder
	state   State
	release chan struct{}
	onStall func()
}

func (s *stallRecorder) Record(ctx context.Context, r Receipt) error {
	if r.State == s.state {
		if s.onStall != nil {
			s.onStall()
		}
		<-s.release
	}
	return s.MemoryRecorder.Record(ctx, r)
}

// Review #2: a stalled receipt write can neither delay cancellation nor
// hang Run. Every write is bounded by Limits.ReceiptTimeout.
func TestStalledRecorder(t *testing.T) {
	t.Run("cancel is sent before the cancel_requested write", func(t *testing.T) {
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true}}
		var cancelledFirst atomic.Bool
		rec := &stallRecorder{state: StateCancelRequested, release: make(chan struct{})}
		rec.onStall = func() {
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				if f.Cancelled() {
					cancelledFirst.Store(true)
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			close(rec.release)
		}
		req := goodRequest("a1")
		req.Limits.Timeout = 30 * time.Millisecond
		req.Limits.ReceiptTimeout = 2 * time.Second
		r, err := Run(context.Background(), f, rec, nil, req)
		if err != nil {
			t.Fatal(err)
		}
		if !cancelledFirst.Load() {
			t.Fatal("Cancel was not sent while the cancel_requested write was stalled")
		}
		if r.State != StateCancelled {
			t.Fatalf("state = %v", r.State)
		}
	})
	for _, st := range []State{StateDispatching, StateStarted, StateCancelRequested, StateCancelled} {
		t.Run("write stalls forever at "+string(st), func(t *testing.T) {
			rec := &stallRecorder{state: st, release: make(chan struct{})}
			t.Cleanup(func() { close(rec.release) })
			f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{Hang: true}}
			t.Cleanup(f.Finish)
			req := goodRequest("a1")
			req.Limits.Timeout = 30 * time.Millisecond
			req.Limits.ReceiptTimeout = 150 * time.Millisecond
			start := time.Now()
			ch := make(chan error, 1)
			var r Receipt
			go func() { var err error; r, err = Run(context.Background(), f, rec, nil, req); ch <- err }()
			var err error
			select {
			case err = <-ch:
			case <-time.After(3 * time.Second):
				t.Fatalf("Run hung on a stalled %s write", st)
			}
			if err == nil {
				t.Fatalf("stalled %s write: no error (state %v)", st, r.State)
			}
			if el := time.Since(start); el > 2*time.Second {
				t.Fatalf("Run took %v", el)
			}
			if st == StateDispatching {
				if f.Starts() != 0 {
					t.Fatal("launched without a pre-launch receipt")
				}
				return
			}
			if r.State != StateUnknown {
				t.Fatalf("state = %v, want unknown", r.State)
			}
			if !f.Cancelled() {
				t.Fatal("attempt left running after a stalled receipt write")
			}
		})
	}
}

// Review #3: when Start's result and the timeout are ready together,
// cancellation is still recognised. Timeout 1ns has expired before Start
// runs; Start sees its context done and returns at once, and the hook
// holds the select until that result is buffered, so both cases are ready
// and select picks either at random.
func TestStartResultAndTimeoutTogether(t *testing.T) {
	orig := beforeStartSelect
	beforeStartSelect = func() { time.Sleep(5 * time.Millisecond) }
	t.Cleanup(func() { beforeStartSelect = orig })
	for i := 0; i < 100; i++ {
		rec := &MemoryRecorder{}
		f := &Fake{NameValue: "fake", Caps: fullCaps(), Script: FakeScript{StartChecksCtx: true, Result: Result{Success: true}}}
		req := goodRequest("a1")
		req.Limits.Timeout = time.Nanosecond
		r, err := Run(context.Background(), f, rec, nil, req)
		if err != nil {
			t.Fatal(err)
		}
		if r.State != StateCancelled || !strings.Contains(r.Reason, "timeout") {
			t.Fatalf("run %d: state = %v reason = %q, want cancelled by timeout", i, r.State, r.Reason)
		}
	}
}
