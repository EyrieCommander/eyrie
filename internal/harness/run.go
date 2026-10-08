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
// receipt for the same attempt ID with ErrDuplicateAttempt, and should
// honour ctx: Run bounds every write by Limits.ReceiptTimeout and treats a
// write that hasn't returned by then as failed.
type Recorder interface {
	Record(ctx context.Context, r Receipt) error
}

// Approver decides a native prompt for an attempt. It returns one of the
// prompt's option IDs. Chat text and agent replies are not approvers.
// Decide must return promptly once ctx is done: Run stops waiting for it
// then, and a decision returned after that is never sent.
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
	ctx = context.WithValue(ctx, receiptTimeoutKey{}, req.Limits.ReceiptTimeout)
	base := Receipt{
		RequestID: req.RequestID, TaskID: req.TaskID, AttemptID: req.AttemptID,
		OfferingID: req.OfferingID, Harness: req.Harness, Model: req.Model,
	}
	if req.Approval != nil {
		base.ApprovalID = req.Approval.ApprovalID
	}

	// A request cancelled before dispatch never launches or records.
	if err := ctx.Err(); err != nil {
		return Receipt{}, fmt.Errorf("not dispatched: %w", err)
	}

	// Persist before launch. If this fails (including a reused attempt
	// ID or a write that stalls past ReceiptTimeout), nothing starts. A
	// stalled write that lands later leaves a dispatching receipt with no
	// start; restart reconciliation (S2-04) marks such attempts unknown.
	if err := record(ctx, rec, base, StateDispatching, ""); err != nil {
		return Receipt{}, fmt.Errorf("not dispatched: %w", err)
	}

	// The attempt is owned here, not by the caller's context: caller
	// cancellation is handled below as an explicit cancel.
	runCtx, cancelRun := context.WithTimeout(context.WithoutCancel(ctx), req.Limits.Timeout)
	defer cancelRun()
	grace := req.Limits.CancelGrace
	if grace <= 0 {
		grace = DefaultCancelGrace
	}

	h, startWhy, err := start(ctx, runCtx, a, req, grace)
	if err != nil {
		state := StateFailed
		reason := "start failed: " + errorClass(err)
		switch {
		case errors.Is(err, ErrStartAmbiguous) || errors.Is(err, errStartHung):
			state = StateUnknown // may be running; reconcile before retry
		case startWhy != "":
			// Start honoured cancellation and nothing is running.
			state, reason = StateCancelled, startWhy+" during start"
		}
		return finish(ctx, rec, base, state, reason)
	}

	// Background workers (event pump, Wait) get a cleanup context that
	// ends when Run returns, so nothing outlives the call even if the
	// runtime never confirms exit. approvalsCtx ends earlier: the moment
	// cancellation begins, no approver decision can reach the runtime.
	workCtx, stopWork := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWork()
	approvalsCtx, stopApprovals := context.WithCancel(runCtx)
	defer stopApprovals()
	if startWhy != "" {
		// Cancellation arrived while Start was returning: no prompt for
		// this attempt may be answered, not even one already buffered.
		// (The pump below isn't started in this case either.)
		stopApprovals()
	}

	base.NativeSession = h.NativeSession()
	if err := record(ctx, rec, base, StateStarted, ""); err != nil {
		// Launched but unrecorded: stop it (bounded) and report unknown.
		stopApprovals()
		boundedCancel(ctx, h, grace)
		return unknown(base, fmt.Errorf("record started: %w", err))
	}

	promptFail := make(chan error, 1)
	pump := func() {
		for {
			select {
			case <-workCtx.Done():
				return
			case ev, ok := <-h.Events():
				if !ok {
					return
				}
				if ev.Kind != EventApproval || ev.Approval == nil {
					continue
				}
				// Decide runs off the pump so a slow approver can't keep
				// Run's workers alive; the pump waits only while work runs.
				res := make(chan error, 1)
				p := *ev.Approval
				go func() { res <- answer(approvalsCtx, h, appr, req, p) }()
				var err error
				select {
				case err = <-res:
				case <-approvalsCtx.Done():
					return
				}
				if err != nil {
					select {
					case promptFail <- err:
					default:
					}
				}
			}
		}
	}
	if startWhy == "" {
		go pump()
	}

	type waited struct {
		res Result
		err error
	}
	done := make(chan waited, 1)
	go func() {
		r, err := h.Wait(workCtx)
		done <- waited{r, err}
	}()

	why := startWhy // set if cancellation arrived while Start was returning
	if why == "" {
		select {
		case w := <-done:
			return finishResult(ctx, rec, base, req, w.res, w.err)
		case <-ctx.Done():
			why = "cancelled by caller"
		case <-runCtx.Done():
			why = fmt.Sprintf("timeout after %s", req.Limits.Timeout)
		case err := <-promptFail:
			why = "approval: " + approvalClass(err)
		}
	}
	stopApprovals()

	// Cancel first, then record: a stalled receipt write must not delay
	// the cancel. The whole cancel (request plus confirmation) is bounded
	// by grace; the attempt shows cancel_requested until exit is
	// confirmed, and the terminal receipt is always written after it.
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	cancelCtx, cancelCancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancelCancel()
	cancelErr := make(chan error, 1)
	go func() { cancelErr <- h.Cancel(cancelCtx) }()

	if err := record(ctx, rec, base, StateCancelRequested, why); err != nil {
		return unknown(base, fmt.Errorf("record cancel_requested: %w", err))
	}

	confirmed := func(w waited) (Receipt, error) {
		// Exit is confirmed only by a clean Wait or ErrCancelled. Any other
		// error (transport failure) leaves the runtime's state unknown.
		if w.err == nil || errors.Is(w.err, ErrCancelled) {
			return finish(ctx, rec, base, StateCancelled, why)
		}
		return finish(ctx, rec, base, StateUnknown, why+"; exit not confirmed: wait failed: "+errorClass(w.err))
	}
	select {
	case w := <-done:
		return confirmed(w)
	case <-deadline.C:
		// If the exit and the deadline are both ready, the exit wins.
		select {
		case w := <-done:
			return confirmed(w)
		default:
		}
		reason := why + "; exit not confirmed within " + grace.String()
		select {
		case err := <-cancelErr:
			if err != nil {
				reason += "; cancel failed: " + errorClass(err)
			}
		default:
			reason += "; cancel request did not return"
		}
		return finish(ctx, rec, base, StateUnknown, reason)
	}
}

// boundedCancel asks the runtime to stop without waiting longer than grace.
func boundedCancel(ctx context.Context, h Handle, grace time.Duration) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = h.Cancel(cctx); close(done) }()
	select {
	case <-done:
	case <-cctx.Done():
	}
}

// errorClass reduces an error to a safe diagnostic for receipts: contract
// sentinels and context errors by name, anything else as "runtime error".
// Runtime error text can echo prompts or credentials, so it isn't stored.
func errorClass(err error) string {
	for _, known := range []error{ErrStartAmbiguous, errStartHung, errReceiptStalled, ErrCancelled, ErrOptionNotOffered, context.DeadlineExceeded, context.Canceled} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "runtime error (detail withheld from receipt)"
}

// Approval failure classes. Receipt reasons use these names only: prompt
// fields and Respond/approver error text come from the runtime or approver
// and may echo the prompt or credentials.
var (
	errNoApprover    = errors.New("runtime asked for approval and no approver is configured")
	errApproverError = errors.New("approver returned an error")
	errLateDecision  = errors.New("decision arrived after cancellation; not sent")
	errRespondFailed = errors.New("sending the decision to the runtime failed")
)

// approvalClass names an answer() error for a receipt.
func approvalClass(err error) string {
	for _, known := range []error{errNoApprover, errApproverError, ErrOptionNotOffered, errLateDecision, errRespondFailed} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "approval failed"
}

// answer forwards an approver decision verbatim, only if the runtime
// offered it. With no approver, no answer is invented: the attempt stops.
// Returned errors wrap a fixed class; detail is not kept.
func answer(ctx context.Context, h Handle, appr Approver, req Request, p NativePrompt) error {
	if appr == nil {
		return errNoApprover
	}
	if ctx.Err() != nil {
		return errLateDecision // cancellation began; don't even ask
	}
	opt, err := appr.Decide(ctx, req, p)
	if err != nil {
		return errApproverError
	}
	if !p.Offers(opt) {
		return ErrOptionNotOffered
	}
	// Cancellation may have begun while the approver was deciding; a late
	// decision must not reach the runtime.
	if ctx.Err() != nil {
		return errLateDecision
	}
	if err := h.Respond(ctx, p.ID, opt); err != nil {
		return errRespondFailed
	}
	return nil
}

// beforeStartSelect is a test hook: tests use it to make Start's result
// and a cancellation ready at the same time. No-op in production.
var beforeStartSelect = func() {}

// cancelReason names the cancellation in effect, or "" if none.
func cancelReason(ctx, runCtx context.Context, req Request) string {
	switch {
	case ctx.Err() != nil:
		return "cancelled by caller"
	case runCtx.Err() != nil:
		return fmt.Sprintf("timeout after %s", req.Limits.Timeout)
	}
	return ""
}

// errStartHung: Start ignored cancellation and didn't return in time.
var errStartHung = errors.New("start did not return after cancellation")

// start runs a.Start so caller cancellation and the timeout reach it.
// why is set if cancellation began during startup. If Start ignores its
// context past grace, start gives up with errStartHung, and cancels any
// handle Start returns later.
func start(ctx, runCtx context.Context, a Adapter, req Request, grace time.Duration) (Handle, string, error) {
	startCtx, cancelStart := context.WithCancel(runCtx)
	type started struct {
		h   Handle
		err error
	}
	ch := make(chan started, 1)
	go func() {
		h, err := a.Start(startCtx, req)
		ch <- started{h, err}
	}()
	beforeStartSelect()
	var why string
	select {
	case s := <-ch:
		cancelStart()
		// Start may have returned because of cancellation that the select
		// didn't pick (both ready at once): check before trusting it.
		return s.h, cancelReason(ctx, runCtx, req), s.err
	case <-ctx.Done():
		why = "cancelled by caller"
	case <-runCtx.Done():
		why = fmt.Sprintf("timeout after %s", req.Limits.Timeout)
	}
	cancelStart()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case s := <-ch:
		return s.h, why, s.err
	case <-timer.C:
		go func() { // late handle: stop it, bounded
			if s := <-ch; s.err == nil && s.h != nil {
				boundedCancel(ctx, s.h, grace)
			}
		}()
		return nil, why, errStartHung
	}
}

func finishResult(ctx context.Context, rec Recorder, base Receipt, req Request, res Result, werr error) (Receipt, error) {
	base.ActualModel = res.Model
	base.Usage = res.Usage
	if base.Usage == nil {
		base.Usage = &Usage{Kind: UsageUnknown}
	}
	// Runtime Summary/Err text is not stored (see Result).
	switch {
	case werr != nil:
		return finish(ctx, rec, base, StateUnknown, "wait failed: "+errorClass(werr))
	case res.Model != "" && res.Model != req.Model:
		// Work by a model nobody selected doesn't count as success.
		return finish(ctx, rec, base, StateFailed, fmt.Sprintf("model mismatch: requested %q, runtime used %q", req.Model, res.Model))
	case res.Success:
		return finish(ctx, rec, base, StateSucceeded, "runtime reported success")
	default:
		return finish(ctx, rec, base, StateFailed, "runtime reported failure")
	}
}

func finish(ctx context.Context, rec Recorder, base Receipt, s State, reason string) (Receipt, error) {
	r := base
	r.State, r.Reason, r.At = s, reason, time.Now().UTC()
	if err := write(ctx, rec, r); err != nil {
		return unknown(base, fmt.Errorf("record %s: %w", s, err))
	}
	return r, nil
}

func record(ctx context.Context, rec Recorder, base Receipt, s State, reason string) error {
	r := base
	r.State, r.Reason, r.At = s, reason, time.Now().UTC()
	return write(ctx, rec, r)
}

// receiptTimeoutKey carries Limits.ReceiptTimeout to write via ctx, so the
// helpers keep their signatures.
type receiptTimeoutKey struct{}

var errReceiptStalled = errors.New("receipt write did not return in time")

// write runs one bounded receipt write. Caller cancellation is stripped
// (a cancelled attempt must still be recorded) but the write gets its own
// deadline, and Run stops waiting at that deadline even if the Recorder
// ignores ctx.
func write(ctx context.Context, rec Recorder, r Receipt) error {
	d, _ := ctx.Value(receiptTimeoutKey{}).(time.Duration)
	if d <= 0 {
		d = DefaultReceiptTimeout
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d)
	defer cancel()
	res := make(chan error, 1)
	go func() { res <- rec.Record(wctx, r) }()
	select {
	case err := <-res:
		return err
	case <-wctx.Done():
		return errReceiptStalled
	}
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
