package server

import (
	"context"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/manager"
)

// lifecycleViaAdapter reports frameworks whose start/stop/restart must go
// through the adapter because the manager's handlers for them are no-ops.
func lifecycleViaAdapter(framework string) bool {
	return framework == adapter.FrameworkCodex
}

func adapterLifecycle(ctx context.Context, agent adapter.Agent, action manager.LifecycleAction) error {
	switch action {
	case manager.ActionStart:
		return agent.Start(ctx)
	case manager.ActionStop:
		return agent.Stop(ctx)
	case manager.ActionRestart:
		return agent.Restart(ctx)
	}
	return nil
}
