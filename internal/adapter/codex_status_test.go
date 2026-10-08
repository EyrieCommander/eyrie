package adapter

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCodexStatusIdleWithNoTurn(t *testing.T) {
	a := &CodexAdapter{id: "status-idle"}
	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st.InferBusyState()
	if st.BusyState != "idle" || st.CurrentTask != "" || st.LastTask != nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestCodexStatusBusyWhileTurnRunsAcrossAdapterInstances(t *testing.T) {
	starter := &CodexAdapter{id: "status-busy"}
	started := time.Now().Add(-5 * time.Minute) // longer than InferBusyState's 60s window
	run := &codexRun{done: make(chan struct{}), startedAt: started, task: codexTaskPreview("Review the bridge branch\nfull details below")}
	if err := starter.registerRun("review", run); err != nil {
		t.Fatal(err)
	}

	st, err := (&CodexAdapter{id: "status-busy"}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st.InferBusyState() // the server always calls this; it must not override
	if st.BusyState != "busy" {
		t.Fatalf("busy_state = %q, want busy", st.BusyState)
	}
	if !strings.Contains(st.CurrentTask, "session review: Review the bridge branch") || strings.Contains(st.CurrentTask, "full details") {
		t.Fatalf("current_task = %q", st.CurrentTask)
	}
	if st.LastTask == nil || !st.LastTask.Equal(started) {
		t.Fatalf("last_task = %v, want %v", st.LastTask, started)
	}

	starter.unregisterRun("review", run)
	st, _ = starter.Status(context.Background())
	st.InferBusyState()
	if st.BusyState != "idle" || st.CurrentTask != "" {
		t.Fatalf("after turn ended: %+v", st)
	}
	if st.LastTask == nil || st.LastTask.Before(started) {
		t.Fatalf("last_task after end = %v", st.LastTask)
	}
}

func TestCodexStatusCountsConcurrentSessions(t *testing.T) {
	a := &CodexAdapter{id: "status-multi"}
	t0 := time.Now()
	r1 := &codexRun{done: make(chan struct{}), startedAt: t0, task: "first"}
	r2 := &codexRun{done: make(chan struct{}), startedAt: t0.Add(time.Second), task: "second"}
	_ = a.registerRun("a", r1)
	_ = a.registerRun("b", r2)
	defer a.unregisterRun("a", r1)
	defer a.unregisterRun("b", r2)

	st, _ := a.Status(context.Background())
	if !strings.HasPrefix(st.CurrentTask, "session a: first") || !strings.HasSuffix(st.CurrentTask, "+1 more") {
		t.Fatalf("current_task = %q", st.CurrentTask)
	}
}

func TestCodexStatusIgnoresOtherAgentsTurns(t *testing.T) {
	other := &CodexAdapter{id: "status-other"}
	r := &codexRun{done: make(chan struct{}), startedAt: time.Now()}
	_ = other.registerRun("s", r)
	defer other.unregisterRun("s", r)
	st, _ := (&CodexAdapter{id: "status-mine"}).Status(context.Background())
	if st.BusyState != "idle" {
		t.Fatalf("busy from another agent's turn: %+v", st)
	}
}

func TestCodexTaskPreview(t *testing.T) {
	long := strings.Repeat("é", 200)
	if got := []rune(codexTaskPreview(long)); len(got) != codexTaskPreviewMax {
		t.Fatalf("preview runes = %d, want %d", len(got), codexTaskPreviewMax)
	}
	if got := codexTaskPreview("  \n first line \nsecond"); got != "first line" {
		t.Fatalf("preview = %q", got)
	}
}

func TestInferBusyStateKeepsAdapterValue(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	st := &AgentStatus{BusyState: "busy", LastTask: &old}
	st.InferBusyState()
	if st.BusyState != "busy" {
		t.Fatalf("adapter-set busy overwritten to %q", st.BusyState)
	}
	recent := time.Now()
	st = &AgentStatus{LastTask: &recent}
	st.InferBusyState()
	if st.BusyState != "busy" {
		t.Fatalf("inference without adapter value = %q, want busy", st.BusyState)
	}
}
