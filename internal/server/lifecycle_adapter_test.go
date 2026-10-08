package server

import (
	"context"
	"testing"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/manager"
)

type lifecycleSpy struct {
	adapter.Agent
	calls []string
}

func (l *lifecycleSpy) Start(context.Context) error { l.calls = append(l.calls, "start"); return nil }
func (l *lifecycleSpy) Stop(context.Context) error  { l.calls = append(l.calls, "stop"); return nil }
func (l *lifecycleSpy) Restart(context.Context) error {
	l.calls = append(l.calls, "restart")
	return nil
}

func TestCodexLifecycleGoesThroughAdapter(t *testing.T) {
	if !lifecycleViaAdapter(adapter.FrameworkCodex) {
		t.Fatal("codex lifecycle would go to the manager, whose codex handlers are no-ops")
	}
	for _, fw := range []string{adapter.FrameworkZeroClaw, adapter.FrameworkOpenClaw, adapter.FrameworkHermes} {
		if lifecycleViaAdapter(fw) {
			t.Fatalf("%s rerouted; only codex should change", fw)
		}
	}
	spy := &lifecycleSpy{}
	for _, a := range []manager.LifecycleAction{manager.ActionStop, manager.ActionStart, manager.ActionRestart} {
		if err := adapterLifecycle(context.Background(), spy, a); err != nil {
			t.Fatal(err)
		}
	}
	if len(spy.calls) != 3 || spy.calls[0] != "stop" || spy.calls[1] != "start" || spy.calls[2] != "restart" {
		t.Fatalf("calls = %v", spy.calls)
	}
}
