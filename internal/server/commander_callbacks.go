// Server-side callbacks the commander invokes for write-tool execution.
// These functions are passed to commander.DefaultConfig as method values
// so the commander package doesn't need to import server internals.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/manager"
	"github.com/Audacity88/eyrie/internal/project"
)

// sendCommanderMessageToProject injects a message from the commander
// into a project's chat and kicks off the captain's response in the
// background. Fire-and-forget from the caller's perspective — the
// commander's tool returns immediately with a "sent" receipt and the
// user reads the captain's reply later via read_project_chat.
//
// WHY fire-and-forget: a blocking wait for the captain's full response
// would stall the commander's turn for many seconds. Asynchronous
// delivery lets the commander stay responsive and the user can decide
// when to check the project chat.
func (s *Server) sendCommanderMessageToProject(ctx context.Context, projectID, message string) error {
	proj, err := s.projectStore.Get(projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return fmt.Errorf("project %q not found", projectID)
		}
		return fmt.Errorf("loading project: %w", err)
	}

	// Launch the orchestrator in a background goroutine. Control returns
	// to the caller immediately — message persistence and the captain's
	// streaming response both happen asynchronously inside the goroutine.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("commander: send_to_project goroutine panicked",
					"project", projectID, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		orch := &ChatOrchestrator{
			cfg:           s.runDiscovery,
			chatStore:     s.chatStore,
			instanceStore: s.instanceStore,
			activeChats:   &s.activeChats,
			// Attribute the trigger message to the commander so the
			// captain and user can see who initiated the exchange.
			triggerSender: "eyrie",
			triggerRole:   "commander",
		}
		// Discard SSE output — there's no client listening. The captain's
		// response is still persisted to chat storage via the orchestrator's
		// normal append logic.
		if err := orch.RunProjectChat(bgCtx, proj, message, NewDiscardSSEWriter()); err != nil {
			slog.Warn("commander: send_to_project orchestrator failed", "project", projectID, "error", err)
		}
	}()

	return nil
}

// restartAgentByName looks up an agent by its discovery name and
// performs a restart. Handles both framework-level agents (via manager)
// and embedded agents (via the adapter's own Restart).
//
// Returns an error if the agent is not found or the restart fails.
// Runs synchronously — completes before the commander records the
// audit entry and tells the user the action succeeded.
func (s *Server) restartAgentByName(ctx context.Context, name string) error {
	// Cap the restart wait. If the underlying manager takes longer
	// than this, the tool returns an error — the actual process may
	// still restart, but from the user's perspective the action is
	// "timed out" rather than "hanging forever".
	restartCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	disc := s.runDiscovery(restartCtx)
	var target *adapter.DiscoveredAgent
	for i := range disc.Agents {
		if disc.Agents[i].Agent.Name == name {
			target = &disc.Agents[i].Agent
			break
		}
	}
	if target == nil {
		return fmt.Errorf("agent %q not found", name)
	}

	// Embedded agents don't have a separate process — their "restart"
	// goes through the adapter directly.
	if target.Framework == adapter.FrameworkEmbedded {
		agent, err := s.findAgentAnyState(restartCtx, name)
		if err != nil {
			return fmt.Errorf("resolving embedded agent: %w", err)
		}
		if restartErr := agent.Restart(restartCtx); restartErr != nil {
			// Mirror the framework-level error-status persistence so both
			// paths behave consistently.
			if target.InstanceID != "" {
				if updateErr := s.instanceStore.UpdateStatus(target.InstanceID, instance.StatusError); updateErr != nil {
					slog.Warn("failed to persist embedded restart error status", "instance", target.InstanceID, "error", updateErr)
				}
			}
			return fmt.Errorf("restarting embedded agent: %w", restartErr)
		}
		return nil
	}

	// Framework-level and provisioned instances go through the manager.
	var env []string
	if s.vault != nil {
		env = s.vault.EnvSlice()
	}
	if err := manager.ExecuteWithConfigEnv(restartCtx, target.Framework, target.ConfigPath, manager.ActionRestart, env); err != nil {
		// Persist error status if this is a tracked instance, matching
		// handleInstanceAction's behavior.
		if target.InstanceID != "" {
			if updateErr := s.instanceStore.UpdateStatus(target.InstanceID, instance.StatusError); updateErr != nil {
				slog.Warn("failed to persist restart error status", "instance", target.InstanceID, "error", updateErr)
			}
		}
		return fmt.Errorf("restarting agent: %w", err)
	}
	return nil
}
