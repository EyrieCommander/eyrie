package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	run := &codexRun{
		client:   c,
		threadID: "thr_1",
		kill:     func() { atomic.AddInt32(&kills, 1); srv.hangUp() },
		done:     make(chan struct{}),
	}
	run.setTurnID(turnID)
	if err := a.registerRun(key, run); err != nil {
		t.Fatal(err)
	}
	ch := make(chan ChatEvent, 16)
	go func() {
		defer close(run.done)
		defer a.unregisterRun(key, run)
		defer close(ch)
		runCodexEventLoop(context.Background(), &bytes.Buffer{}, c, run, ch)
	}()
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
	a := &CodexAdapter{}
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
	a := &CodexAdapter{}
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
	drain(ch)
}

func TestCodexInterruptRejectedKillsImmediately(t *testing.T) {
	shortInterruptTimers(t)
	codexInterruptGrace = 5 * time.Second // must not be waited on
	a := &CodexAdapter{}
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
	a := &CodexAdapter{}
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
	if err := (&CodexAdapter{}).Interrupt(context.Background(), "nothing"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexStopInterruptsEveryRun(t *testing.T) {
	shortInterruptTimers(t)
	a := &CodexAdapter{}
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
	a := &CodexAdapter{}
	if err := a.registerRun("s", &codexRun{done: make(chan struct{})}); err != nil {
		t.Fatal(err)
	}
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
