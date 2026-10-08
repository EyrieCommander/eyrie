package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// interruptServer is an in-memory Codex app server for one turn.
//   - honour: on turn/interrupt, reply OK and send turn/completed{interrupted}.
//   - ignore: reply OK to turn/interrupt and keep the turn running.
//   - reject: reply with an error to turn/interrupt.
type interruptServer struct {
	mode string

	mu         sync.Mutex
	interrupts []map[string]any
	out        *io.PipeWriter
	enc        *json.Encoder
}

func (s *interruptServer) gotInterrupts() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.interrupts...)
}

// hangUp ends the server's output, as a killed process would.
func (s *interruptServer) hangUp() { _ = s.out.Close() }

func startInterruptServer(t *testing.T, mode string) (*interruptServer, *codexRPCClient) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &interruptServer{mode: mode, out: outW, enc: json.NewEncoder(outW)}
	t.Cleanup(func() { _ = inW.Close(); _ = outW.Close() })
	go func() {
		sc := bufio.NewScanner(inR)
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params map[string]any  `json:"params"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil || req.Method != "turn/interrupt" {
				continue
			}
			s.mu.Lock()
			s.interrupts = append(s.interrupts, req.Params)
			switch s.mode {
			case "reject":
				_ = s.enc.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32000, "message": "no such turn"}})
			case "ignore":
				_ = s.enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
			default:
				_ = s.enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
				_ = s.enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{
					"threadId": req.Params["threadId"],
					"turn":     map[string]any{"id": req.Params["turnId"], "status": "interrupted", "items": []any{}},
				}})
			}
			s.mu.Unlock()
		}
	}()
	c := newCodexRPCClient(inW, outR)
	go c.readLoop()
	return s, c
}

// startTestRun registers a run on a and drives runCodexEventLoop the way
// StreamMessage does, returning the event channel and a kill counter.
func startTestRun(t *testing.T, a *CodexAdapter, key string, srv *interruptServer, c *codexRPCClient, turnID string) (<-chan ChatEvent, *int32) {
	t.Helper()
	var kills int32
	run := &codexRun{done: make(chan struct{})}
	run.setThread(c, "thr_1")
	run.setKill(func() { atomic.AddInt32(&kills, 1); srv.hangUp() })
	run.setTurnID(turnID)
	if err := a.registerRun(key, run); err != nil {
		t.Fatal(err)
	}
	ch := make(chan ChatEvent, 16)
	go runCodexStream(context.Background(), &lockedBuffer{}, c, run, ch,
		func() {}, func() { a.unregisterRun(key, run); close(run.done) }, codexTerminalDeliveryTimeout)
	return ch, &kills
}

func shortInterruptTimers(t *testing.T) {
	t.Helper()
	oldReq, oldGrace := codexInterruptRequestTimeout, codexInterruptGrace
	codexInterruptRequestTimeout, codexInterruptGrace = time.Second, 200*time.Millisecond
	t.Cleanup(func() { codexInterruptRequestTimeout, codexInterruptGrace = oldReq, oldGrace })
}

func drain(ch <-chan ChatEvent) []ChatEvent {
	var out []ChatEvent
	for e := range ch {
		out = append(out, e)
	}
	return out
}

func TestCodexInterruptSendsTurnInterruptAndTurnEndsAsError(t *testing.T) {
	shortInterruptTimers(t)
	a := &CodexAdapter{id: "t-1"}
	srv, c := startInterruptServer(t, "honour")
	ch, kills := startTestRun(t, a, "review", srv, c, "turn_7")

	if err := a.Interrupt(context.Background(), "review"); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	got := srv.gotInterrupts()
	if len(got) != 1 || got[0]["threadId"] != "thr_1" || got[0]["turnId"] != "turn_7" {
		t.Fatalf("turn/interrupt params = %v", got)
	}
	if n := atomic.LoadInt32(kills); n != 0 {
		t.Fatalf("process killed %d times; a turn that ends on interrupt should not be killed", n)
	}
	events := drain(ch)
	last := events[len(events)-1]
	if last.Type != "error" {
		t.Fatalf("last event = %+v; an interrupted turn must not be reported as done", last)
	}
	if a.activeRun("review") != nil {
		t.Fatal("run still registered after the turn ended")
	}
}

func TestCodexInterruptKillsWhenTurnDoesNotEnd(t *testing.T) {
	shortInterruptTimers(t)
	a := &CodexAdapter{id: "t-2"}
	srv, c := startInterruptServer(t, "ignore")
	ch, kills := startTestRun(t, a, "", srv, c, "turn_1")

	start := time.Now()
	if err := a.Interrupt(context.Background(), "default"); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if n := atomic.LoadInt32(kills); n != 1 {
		t.Fatalf("kills = %d, want 1", n)
	}
	if time.Since(start) < codexInterruptGrace {
		t.Fatal("killed before the grace period")
	}
	events := drain(ch)
	if len(events) == 0 || events[len(events)-1].Type != "error" {
		t.Fatalf("events after forced kill = %+v; must end in an error, not look finished", events)
	}
}

func TestCodexInterruptRejectedKillsImmediately(t *testing.T) {
	shortInterruptTimers(t)
	codexInterruptGrace = 5 * time.Second // must not be waited on
	a := &CodexAdapter{id: "t-3"}
	srv, c := startInterruptServer(t, "reject")
	ch, kills := startTestRun(t, a, "x", srv, c, "turn_1")

	start := time.Now()
	if err := a.Interrupt(context.Background(), "x"); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if atomic.LoadInt32(kills) != 1 {
		t.Fatal("rejected interrupt did not kill the process")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("waited out the grace period after a rejected interrupt")
	}
	drain(ch)
}

func TestCodexInterruptLearnsTurnIDFromTurnStarted(t *testing.T) {
	shortInterruptTimers(t)
	a := &CodexAdapter{id: "t-4"}
	srv, c := startInterruptServer(t, "honour")
	ch, _ := startTestRun(t, a, "s", srv, c, "")
	srv.mu.Lock()
	_ = srv.enc.Encode(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "thr_1", "turn": map[string]any{"id": "turn_late"}}})
	srv.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for a.activeRun("s").currentTurnID() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := a.Interrupt(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if got := srv.gotInterrupts(); len(got) != 1 || got[0]["turnId"] != "turn_late" {
		t.Fatalf("turn/interrupt params = %v", got)
	}
	drain(ch)
}

func TestCodexInterruptWithNoActiveTurnIsNoop(t *testing.T) {
	if err := (&CodexAdapter{id: "t-noop"}).Interrupt(context.Background(), "nothing"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexStopInterruptsEveryRun(t *testing.T) {
	shortInterruptTimers(t)
	a := &CodexAdapter{id: "t-5"}
	s1, c1 := startInterruptServer(t, "honour")
	s2, c2 := startInterruptServer(t, "ignore")
	ch1, k1 := startTestRun(t, a, "one", s1, c1, "t1")
	ch2, k2 := startTestRun(t, a, "two", s2, c2, "t2")

	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(s1.gotInterrupts()) != 1 || len(s2.gotInterrupts()) != 1 {
		t.Fatal("stop did not interrupt both runs")
	}
	if atomic.LoadInt32(k1) != 0 || atomic.LoadInt32(k2) != 1 {
		t.Fatalf("kills = %d,%d; want 0,1", *k1, *k2)
	}
	drain(ch1)
	drain(ch2)
}

func TestCodexOneTurnPerSession(t *testing.T) {
	a := &CodexAdapter{id: "t-6"}
	r := &codexRun{done: make(chan struct{})}
	if err := a.registerRun("s", r); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.unregisterRun("s", r) })
	if err := a.registerRun("s", &codexRun{done: make(chan struct{})}); err == nil {
		t.Fatal("second concurrent turn on one session was allowed")
	}
}

func TestCodexTurnErrorTreatsInterruptedAsFailure(t *testing.T) {
	for _, status := range []string{"interrupted", "failed", "cancelled"} {
		if codexTurnError(json.RawMessage(`{"turn":{"id":"t","status":"`+status+`"}}`)) == "" {
			t.Errorf("status %q reported as success", status)
		}
	}
	if msg := codexTurnError(json.RawMessage(`{"turn":{"id":"t","status":"completed"}}`)); msg != "" {
		t.Errorf("completed reported as error %q", msg)
	}
}

func TestCodexStopFromAFreshAdapterReachesTheRun(t *testing.T) {
	// discovery.NewAgent builds a new adapter per lookup; Stop on that new
	// instance must still find the turn the first instance started.
	shortInterruptTimers(t)
	starter := &CodexAdapter{id: "shared-agent"}
	srv, c := startInterruptServer(t, "honour")
	ch, _ := startTestRun(t, starter, "s", srv, c, "turn_9")

	if err := (&CodexAdapter{id: "shared-agent"}).Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(srv.gotInterrupts()) != 1 {
		t.Fatal("Stop on a fresh adapter did not reach the running turn")
	}
	drain(ch)
}

func TestCodexStopLeavesOtherAgentsAlone(t *testing.T) {
	shortInterruptTimers(t)
	mine := &CodexAdapter{id: "agent-a"}
	other := &CodexAdapter{id: "agent-b"}
	srv, c := startInterruptServer(t, "honour")
	ch, _ := startTestRun(t, other, "s", srv, c, "turn_1")
	if err := mine.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(srv.gotInterrupts()) != 0 {
		t.Fatal("Stop interrupted another agent's turn")
	}
	_ = other.Interrupt(context.Background(), "s")
	drain(ch)
}

func TestCodexForcedKillWithNoOutputEndsInError(t *testing.T) {
	shortInterruptTimers(t)
	a := &CodexAdapter{id: "t-forced-quiet"}
	srv, c := startInterruptServer(t, "reject")
	ch, _ := startTestRun(t, a, "s", srv, c, "turn_1")
	_ = a.Interrupt(context.Background(), "s")
	resp, err := (&chanAgent{ch: ch}).collect()
	if err == nil {
		t.Fatalf("forced kill returned success with %q", resp)
	}
}

// chanAgent mirrors SendMessage's reading of the event stream.
type chanAgent struct{ ch <-chan ChatEvent }

func (c *chanAgent) collect() (string, error) {
	var b strings.Builder
	for e := range c.ch {
		switch e.Type {
		case "delta":
			b.WriteString(e.Content)
		case "done":
			return b.String(), nil
		case "error":
			return "", errors.New(e.Error)
		}
	}
	return "", errors.New("closed without a terminal event")
}

func TestCodexStalledConsumerDoesNotPinTheSession(t *testing.T) {
	shortInterruptTimers(t)
	a := &CodexAdapter{id: "t-stalled"}
	srv, c := startInterruptServer(t, "ignore")
	run := &codexRun{done: make(chan struct{})}
	run.setThread(c, "thr_1")
	run.setKill(func() { srv.hangUp() })
	run.setTurnID("turn_1")
	_ = a.registerRun("s", run)
	ch := make(chan ChatEvent) // unbuffered and never read
	go runCodexStream(context.Background(), &lockedBuffer{}, c, run, ch,
		func() {}, func() { a.unregisterRun("s", run); close(run.done) }, codexTerminalDeliveryTimeout)
	srv.mu.Lock()
	for i := 0; i < 5; i++ {
		_ = srv.enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "x"}})
	}
	srv.mu.Unlock()
	time.Sleep(50 * time.Millisecond)

	if err := a.Interrupt(context.Background(), "s"); err != nil {
		t.Fatalf("interrupt with a stalled consumer: %v", err)
	}
	if a.activeRun("s") != nil {
		t.Fatal("session still registered after interrupt with a stalled consumer")
	}
}

// Review finding 1: an undeliverable terminal event (stalled consumer) must
// not let the session look free while the process it owns is still alive,
// and must not keep it busy forever after the process is gone.
func TestCodexSessionHeldUntilReapedEvenIfTerminalUndeliverable(t *testing.T) {
	a := &CodexAdapter{id: "t-full-buffer"}
	srv, c := startInterruptServer(t, "honour")
	run := &codexRun{done: make(chan struct{})}
	run.setThread(c, "thr_1")
	_ = a.registerRun("s", run)
	ch := make(chan ChatEvent) // nobody reads
	reapGate := make(chan struct{})
	var reaped atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	streamDone := make(chan struct{})
	// Registered after the timeout's Cleanup, so it runs first: stop the
	// stream and wait for it before the timeout variable is restored.
	t.Cleanup(func() { cancel(); <-streamDone })
	go func() {
		defer close(streamDone)
		runCodexStream(ctx, &lockedBuffer{}, c, run, ch,
			func() { <-reapGate; reaped.Store(true) }, // slow process exit
			func() { a.unregisterRun("s", run); close(run.done) }, 5*time.Second)
	}()
	srv.mu.Lock()
	_ = srv.enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "t", "status": "completed"}}})
	srv.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	releasedEarly := a.activeRun("s") == nil
	close(reapGate) // let the stream finish either way, so cleanup can't hang
	if releasedEarly {
		t.Fatal("session released before its process was reaped")
	}
	select {
	case <-run.done:
	case <-time.After(2 * time.Second):
		t.Fatal("session not released after reap, though nobody reads the terminal event")
	}
	if !reaped.Load() {
		t.Fatal("released without reaping")
	}
}

func TestCodexSessionFreeWhenDoneIsDelivered(t *testing.T) {
	a := &CodexAdapter{id: "t-done-free"}
	srv, c := startInterruptServer(t, "honour")
	run := &codexRun{done: make(chan struct{})}
	run.setThread(c, "thr_1")
	_ = a.registerRun("s", run)
	ch := make(chan ChatEvent, 4)
	go runCodexStream(context.Background(), &lockedBuffer{}, c, run, ch,
		func() {}, func() { a.unregisterRun("s", run); close(run.done) }, codexTerminalDeliveryTimeout)
	srv.mu.Lock()
	_ = srv.enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "t", "status": "completed"}}})
	srv.mu.Unlock()
	e := <-ch
	if e.Type != "done" {
		t.Fatalf("event = %+v", e)
	}
	if a.activeRun("s") != nil {
		t.Fatal("session still busy when done was delivered; an immediate next turn would be refused")
	}
}

// Review finding 2: the stream ending without turn/completed (no kill mark,
// empty stderr) must be an error, not a partial success.
func TestCodexEOFWithoutCompletionIsAnError(t *testing.T) {
	srv, c := startInterruptServer(t, "honour")
	ch := make(chan ChatEvent, 16)
	go runCodexStream(context.Background(), &lockedBuffer{}, c, nil, ch, func() {}, func() {}, codexTerminalDeliveryTimeout)
	srv.mu.Lock()
	_ = srv.enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "partial"}})
	srv.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	srv.hangUp()
	if resp, err := (&chanAgent{ch: ch}).collect(); err == nil {
		t.Fatalf("EOF without turn/completed returned success %q", resp)
	}
}

func TestCodexCancelRacingEOFIsAnError(t *testing.T) {
	for i := 0; i < 50; i++ {
		srv, c := startInterruptServer(t, "honour")
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan ChatEvent, 16)
		go runCodexStream(ctx, &lockedBuffer{}, c, nil, ch, func() {}, func() {}, codexTerminalDeliveryTimeout)
		cancel()
		srv.hangUp()
		if resp, err := (&chanAgent{ch: ch}).collect(); err == nil {
			t.Fatalf("iteration %d: cancel racing EOF returned success %q", i, resp)
		}
	}
}

func TestCodexInterruptedTerminalSurvivesFullBuffer(t *testing.T) {
	// The buffer is full of deltas when Interrupt kills the server; the
	// interrupted error must still arrive once the consumer reads again.
	shortInterruptTimers(t)
	a := &CodexAdapter{id: "t-full-then-read"}
	srv, c := startInterruptServer(t, "ignore")
	run := &codexRun{done: make(chan struct{})}
	run.setThread(c, "thr_1")
	run.setKill(func() { srv.hangUp() })
	run.setTurnID("turn_1")
	_ = a.registerRun("s", run)
	ch := make(chan ChatEvent, 2)
	go runCodexStream(context.Background(), &lockedBuffer{}, c, run, ch,
		func() {}, func() { a.unregisterRun("s", run); close(run.done) }, codexTerminalDeliveryTimeout)
	srv.mu.Lock()
	for i := 0; i < 6; i++ {
		_ = srv.enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "x"}})
	}
	srv.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	if err := a.Interrupt(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := (&chanAgent{ch: ch}).collect(); err == nil {
		t.Fatal("interrupted turn with a full buffer read as success")
	}
}

func TestSendMessageRuleClosedStreamWithoutTerminalFails(t *testing.T) {
	ch := make(chan ChatEvent, 1)
	ch <- ChatEvent{Type: "delta", Content: "half"}
	close(ch)
	if _, err := (&chanAgent{ch: ch}).collect(); err == nil {
		t.Fatal("closed stream without a terminal event treated as success")
	}
}

// Review finding: EOF can close requests while a successful turn/completed
// still sits in notifications. It must be delivered as done.
func TestCodexCompletionImmediatelyBeforeEOFIsDone(t *testing.T) {
	for i := 0; i < 50; i++ {
		srv, c := startInterruptServer(t, "honour")
		ch := make(chan ChatEvent, 16)
		go runCodexStream(context.Background(), &lockedBuffer{}, c, nil, ch, func() {}, func() {}, codexTerminalDeliveryTimeout)
		srv.mu.Lock()
		_ = srv.enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "answer"}})
		_ = srv.enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "t", "status": "completed"}}})
		srv.mu.Unlock()
		srv.hangUp()
		got, err := (&chanAgent{ch: ch}).collect()
		if err != nil || got != "answer" {
			t.Fatalf("iteration %d: completion before EOF = %q, %v; want done with the answer", i, got, err)
		}
	}
}

// Review finding: success must be explicit.
func TestCodexTurnErrorRequiresExplicitCompleted(t *testing.T) {
	ok := []string{
		`{"turn":{"id":"t","status":"completed"}}`,
		`{"status":"completed"}`,
	}
	bad := []string{
		`{"turn":{"id":"t"}}`,
		`{"turn":{"id":"t","status":""}}`,
		`{"turn":{"id":"t","status":"inProgress"}}`,
		`{"turn":{"id":"t","status":"weird"}}`,
		`{"turn":{"id":"t","status":"completed","error":{}}}`,
		`{"turn":{"id":"t","status":"completed","error":{"message":"boom"}}}`,
		`{}`,
		`not json`,
		`{"turn":{"id":"t","status":7}}`,
	}
	for _, p := range ok {
		if msg := codexTurnError(json.RawMessage(p)); msg != "" {
			t.Errorf("%s: error %q, want success", p, msg)
		}
	}
	for _, p := range bad {
		if codexTurnError(json.RawMessage(p)) == "" {
			t.Errorf("%s: treated as success", p)
		}
	}
}
