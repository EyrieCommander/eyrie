package harness

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Fake is a scripted in-process adapter for fixtures. Each Start returns a
// handle that follows Script. It runs nothing.
type Fake struct {
	NameValue string
	Caps      Capabilities
	Script    FakeScript

	starts atomic.Int32
	mu     sync.Mutex
	last   *fakeHandle
}

// FakeScript describes what one attempt does.
type FakeScript struct {
	StartErr error          // returned by Start
	Prompts  []NativePrompt // emitted in order; each waits for a Respond
	Result   Result         // returned when the attempt finishes
	WaitErr  error
	// Hang makes the attempt run until cancelled.
	Hang bool
	// IgnoreCancel makes Cancel return without ending the attempt.
	IgnoreCancel bool
	CancelErr    error
	// CancelBlocks makes Cancel block until its ctx is done (a stalled
	// cancellation RPC).
	CancelBlocks bool
	// KeepEventsOpen keeps the event stream open until the attempt ends.
	KeepEventsOpen bool
	// StartDelay makes Start take this long. Start returns ctx.Err() if
	// ctx ends first, unless StartIgnoresCtx.
	StartDelay      time.Duration
	StartIgnoresCtx bool
	RespondErr      error
}

func (f *Fake) Name() string { return f.NameValue }

func (f *Fake) Capabilities(context.Context) (Capabilities, error) { return f.Caps, nil }

// Starts is how many times Start ran.
func (f *Fake) Starts() int { return int(f.starts.Load()) }

// Responses returns the (promptID, optionID) pairs the last attempt got.
func (f *Fake) Responses() [][2]string {
	f.mu.Lock()
	h := f.last
	f.mu.Unlock()
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][2]string(nil), h.responses...)
}

// Cancelled reports whether the last attempt got a Cancel.
func (f *Fake) Cancelled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last != nil && f.last.cancelled.Load()
}

func (f *Fake) Start(ctx context.Context, req Request) (Handle, error) {
	f.starts.Add(1)
	if d := f.Script.StartDelay; d > 0 {
		if f.Script.StartIgnoresCtx {
			time.Sleep(d)
		} else {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	if f.Script.StartErr != nil {
		return nil, f.Script.StartErr
	}
	h := &fakeHandle{
		script:  f.Script,
		events:  make(chan Event, len(f.Script.Prompts)+1),
		ended:   make(chan struct{}),
		answers: make(chan struct{}, len(f.Script.Prompts)),
		session: "fake-session-" + req.AttemptID,
	}
	f.mu.Lock()
	f.last = h
	f.mu.Unlock()
	go h.run()
	return h, nil
}

type fakeHandle struct {
	script    FakeScript
	events    chan Event
	ended     chan struct{}
	endOnce   sync.Once
	answers   chan struct{}
	session   string
	cancelled atomic.Bool

	mu        sync.Mutex
	responses [][2]string
}

func (h *fakeHandle) run() {
	defer close(h.events)
	for i := range h.script.Prompts {
		p := h.script.Prompts[i]
		h.events <- Event{Kind: EventApproval, Approval: &p}
		select {
		case <-h.answers:
		case <-h.ended:
			return
		}
	}
	if !h.script.Hang {
		h.end()
	}
	if h.script.KeepEventsOpen {
		<-h.ended
	}
}

// Finish ends the last attempt regardless of script (test cleanup).
func (f *Fake) Finish() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.last != nil {
		f.last.end()
	}
}

func (h *fakeHandle) end() { h.endOnce.Do(func() { close(h.ended) }) }

func (h *fakeHandle) NativeSession() string { return h.session }
func (h *fakeHandle) Events() <-chan Event  { return h.events }

func (h *fakeHandle) Respond(_ context.Context, promptID, optionID string) error {
	h.mu.Lock()
	h.responses = append(h.responses, [2]string{promptID, optionID})
	h.mu.Unlock()
	if h.script.RespondErr != nil {
		return h.script.RespondErr
	}
	h.answers <- struct{}{}
	return nil
}

func (h *fakeHandle) Cancel(ctx context.Context) error {
	h.cancelled.Store(true)
	if h.script.CancelBlocks {
		<-ctx.Done()
		return ctx.Err()
	}
	if !h.script.IgnoreCancel {
		h.end()
	}
	return h.script.CancelErr
}

func (h *fakeHandle) Wait(ctx context.Context) (Result, error) {
	select {
	case <-h.ended:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	if h.script.WaitErr != nil {
		return Result{}, h.script.WaitErr
	}
	if h.cancelled.Load() {
		return Result{}, ErrCancelled
	}
	return h.script.Result, nil
}
