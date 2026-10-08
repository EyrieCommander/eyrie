package harness

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State is a receipt's state. The terminal ones are attempt outcomes
// (eyrie-v1-prd.md: succeeded, failed, cancelled, interrupted, unknown).
// "succeeded" means the runtime reported success; verification is separate.
type State string

const (
	StateDispatching     State = "dispatching" // recorded before launch
	StateStarted         State = "started"
	StateCancelRequested State = "cancel_requested"
	StateSucceeded       State = "succeeded"
	StateFailed          State = "failed"
	StateCancelled       State = "cancelled"
	StateUnknown         State = "unknown" // can't establish; block for Dan
)

func (s State) Terminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled, StateUnknown:
		return true
	}
	return false
}

// Receipt is one recorded step of an attempt. No prompt or transcript text.
type Receipt struct {
	RequestID     string
	TaskID        string
	AttemptID     string
	OfferingID    string
	Harness       string
	Model         string // requested
	ActualModel   string // as reported by the runtime, if any
	ApprovalID    string
	State         State
	Reason        string
	NativeSession string
	Usage         *Usage
	At            time.Time
}

// Recorder persists receipts. It must refuse a second StateDispatching
// receipt for the same attempt ID with ErrDuplicateAttempt.
type Recorder interface {
	Record(ctx context.Context, r Receipt) error
}

// Approver decides a native prompt for an attempt. It returns one of the
// prompt's option IDs. Chat text and agent replies are not approvers.
type Approver interface {
	Decide(ctx context.Context, req Request, p NativePrompt) (optionID string, err error)
}

// Run drives one attempt through the contract and returns its terminal
// receipt. The returned receipt is what was recorded; if recording fails
// after launch, it is StateUnknown and err is non-nil.
//
// ctx cancellation is a user cancel. Limits.Timeout ends the attempt too.
func Run(ctx context.Context, a Adapter, rec Recorder, appr Approver, req Request) (Receipt, error) {
	if err := req.Validate(); err != nil {
		return Receipt{}, err
	}
	caps, err := a.Capabilities(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: capabilities: %v", ErrUnsupported, err)
	}
	if err := Check(a.Name(), caps, req); err != nil {
		return Receipt{}, err
	}
	base := Receipt{
		RequestID: req.RequestID, TaskID: req.TaskID, AttemptID: req.AttemptID,
		OfferingID: req.OfferingID, Harness: req.Harness, Model: req.Model,
	}
	if req.Approval != nil {
		base.ApprovalID = req.Approval.ApprovalID
	}

	// Persist before launch. If this fails (including a reused attempt
	// ID), nothing starts.
	if err := record(ctx, rec, base, StateDispatching, ""); err != nil {
		return Receipt{}, fmt.Errorf("not dispatched: %w", err)
	}

	// The attempt is owned here, not by the caller's context: caller
	// cancellation is handled below as an explicit cancel.
	runCtx, cancelRun := context.WithTimeout(context.WithoutCancel(ctx), req.Limits.Timeout)
	defer cancelRun()

	h, err := a.Start(runCtx, req)
	if err != nil {
		state := StateFailed
		if errors.Is(err, ErrStartAmbiguous) {
			state = StateUnknown // may be running; reconcile before retry
		}
		return finish(ctx, rec, base, state, "start: "+err.Error())
	}
	base.NativeSession = h.NativeSession()
	if err := record(ctx, rec, base, StateStarted, ""); err != nil {
		// Launched but unrecorded: stop it and report unknown.
		_ = h.Cancel(context.WithoutCancel(ctx))
		return unknown(base, fmt.Errorf("record started: %w", err))
	}

	// Answer native prompts through the approver as they arrive.
	promptFail := make(chan error, 1)
	go func() {
		for ev := range h.Events() {
			if ev.Kind != EventApproval || ev.Approval == nil {
				continue
			}
			if err := answer(runCtx, h, appr, req, *ev.Approval); err != nil {
				select {
				case promptFail <- err:
				default:
				}
			}
		}
	}()

	type waited struct {
		res Result
		err error
	}
	done := make(chan waited, 1)
	go func() {
		r, err := h.Wait(context.WithoutCancel(ctx))
		done <- waited{r, err}
	}()

	var why string
	select {
	case w := <-done:
		return finishResult(ctx, rec, base, req, w.res, w.err)
	case <-ctx.Done():
		why = "cancelled by caller"
	case <-runCtx.Done():
		why = fmt.Sprintf("timeout after %s", req.Limits.Timeout)
	case err := <-promptFail:
		why = "approval: " + err.Error()
	}

	// Cancel shows "requested" until the runtime confirms exit.
	if err := record(ctx, rec, base, StateCancelRequested, why); err != nil {
		_ = h.Cancel(context.WithoutCancel(ctx))
		return unknown(base, fmt.Errorf("record cancel_requested: %w", err))
	}
	cerr := h.Cancel(context.WithoutCancel(ctx))
	grace := req.Limits.CancelGrace
	if grace <= 0 {
		grace = DefaultCancelGrace
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		if cerr != nil {
			why += "; cancel error: " + cerr.Error()
		}
		return finish(ctx, rec, base, StateCancelled, why)
	case <-timer.C:
		return finish(ctx, rec, base, StateUnknown, why+"; exit not confirmed within "+grace.String())
	}
}

// answer forwards an approver decision verbatim, only if the runtime
// offered it. With no approver, no answer is invented: the attempt stops.
func answer(ctx context.Context, h Handle, appr Approver, req Request, p NativePrompt) error {
	if appr == nil {
		return fmt.Errorf("runtime asked %q (%s) and no approver is configured", p.ID, p.Action)
	}
	opt, err := appr.Decide(ctx, req, p)
	if err != nil {
		return fmt.Errorf("prompt %q: %w", p.ID, err)
	}
	if !p.Offers(opt) {
		return fmt.Errorf("prompt %q: %w: %q", p.ID, ErrOptionNotOffered, opt)
	}
	return h.Respond(ctx, p.ID, opt)
}

func finishResult(ctx context.Context, rec Recorder, base Receipt, req Request, res Result, werr error) (Receipt, error) {
	base.ActualModel = res.Model
	base.Usage = res.Usage
	if base.Usage == nil {
		base.Usage = &Usage{Kind: UsageUnknown}
	}
	switch {
	case werr != nil:
		return finish(ctx, rec, base, StateUnknown, "wait: "+werr.Error())
	case res.Model != "" && res.Model != req.Model:
		// Work by a model nobody selected doesn't count as success.
		return finish(ctx, rec, base, StateFailed, fmt.Sprintf("model mismatch: requested %q, runtime used %q", req.Model, res.Model))
	case res.Success:
		return finish(ctx, rec, base, StateSucceeded, res.Summary)
	default:
		reason := res.Err
		if reason == "" {
			reason = res.Summary
		}
		return finish(ctx, rec, base, StateFailed, reason)
	}
}

func finish(ctx context.Context, rec Recorder, base Receipt, s State, reason string) (Receipt, error) {
	r := base
	r.State, r.Reason, r.At = s, reason, time.Now().UTC()
	if err := rec.Record(context.WithoutCancel(ctx), r); err != nil {
		return unknown(base, fmt.Errorf("record %s: %w", s, err))
	}
	return r, nil
}

func record(ctx context.Context, rec Recorder, base Receipt, s State, reason string) error {
	r := base
	r.State, r.Reason, r.At = s, reason, time.Now().UTC()
	return rec.Record(context.WithoutCancel(ctx), r)
}

// unknown is returned when Eyrie couldn't record the truth. The adapter's
// claim is not trusted in its place.
func unknown(base Receipt, err error) (Receipt, error) {
	r := base
	r.State, r.Reason, r.At = StateUnknown, err.Error(), time.Now().UTC()
	return r, err
}

// MemoryRecorder is an in-process Recorder for tests and fixtures. It is
// not durable; S2-04 supplies the durable one.
type MemoryRecorder struct {
	mu       sync.Mutex
	receipts []Receipt
	Fail     func(Receipt) error // inject write failures
}

func (m *MemoryRecorder) Record(_ context.Context, r Receipt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		if err := m.Fail(r); err != nil {
			return err
		}
	}
	if r.State == StateDispatching {
		for _, x := range m.receipts {
			if x.AttemptID == r.AttemptID {
				return fmt.Errorf("%w: %s", ErrDuplicateAttempt, r.AttemptID)
			}
		}
	}
	m.receipts = append(m.receipts, r)
	return nil
}

// Receipts returns a copy of everything recorded, in order.
func (m *MemoryRecorder) Receipts() []Receipt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Receipt(nil), m.receipts...)
}

// States returns the recorded states for one attempt, in order.
func (m *MemoryRecorder) States(attemptID string) []State {
	var out []State
	for _, r := range m.Receipts() {
		if r.AttemptID == attemptID {
			out = append(out, r.State)
		}
	}
	return out
}
