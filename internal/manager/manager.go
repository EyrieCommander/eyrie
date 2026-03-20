package manager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type LifecycleAction string

const (
	ActionStart   LifecycleAction = "start"
	ActionStop    LifecycleAction = "stop"
	ActionRestart LifecycleAction = "restart"
)

// Execute runs a lifecycle action for the given framework.
// It checks whether the OS service is installed first and adapts the command accordingly.
func Execute(ctx context.Context, framework string, action LifecycleAction) error {
	switch framework {
	case "zeroclaw":
		return executeZeroClaw(ctx, action)
	case "openclaw":
		return executeOpenClaw(ctx, action)
	case "hermes":
		return executeHermes(ctx, action)
	default:
		return fmt.Errorf("unknown framework %q: cannot determine lifecycle command", framework)
	}
}

func executeZeroClaw(ctx context.Context, action LifecycleAction) error {
	if action == ActionStart || action == ActionRestart {
		// Check if the launchd service is installed
		if serviceInstalled(ctx, "zeroclaw") {
			return run(ctx, "zeroclaw", "service", string(action))
		}
		// Service not installed -- install it first, then start
		if err := run(ctx, "zeroclaw", "service", "install"); err != nil {
			return fmt.Errorf("service not installed and auto-install failed: %w\nYou can also start manually with: zeroclaw daemon", err)
		}
		return run(ctx, "zeroclaw", "service", string(action))
	}
	// stop: try service stop, fall back gracefully
	return run(ctx, "zeroclaw", "service", string(action))
}

func executeOpenClaw(ctx context.Context, action LifecycleAction) error {
	return run(ctx, "openclaw", "gateway", string(action))
}

func executeHermes(ctx context.Context, action LifecycleAction) error {
	switch action {
	case ActionStart:
		// Check if launchd service is installed, install if needed
		if !hermesServiceInstalled(ctx) {
			if err := run(ctx, "hermes", "gateway", "install"); err != nil {
				return fmt.Errorf("failed to install hermes service: %w", err)
			}
		}
		return run(ctx, "hermes", "gateway", "start")
	case ActionStop:
		return run(ctx, "hermes", "gateway", "stop")
	case ActionRestart:
		return run(ctx, "hermes", "gateway", "restart")
	default:
		return fmt.Errorf("unsupported action %q for Hermes", action)
	}
}

// hermesServiceInstalled checks if the Hermes launchd service is installed
func hermesServiceInstalled(ctx context.Context) bool {
	home := os.Getenv("HOME")
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.hermes.gateway.plist")
	_, err := os.Stat(plistPath)
	return err == nil
}

func serviceInstalled(ctx context.Context, framework string) bool {
	switch framework {
	case "zeroclaw":
		out, err := exec.CommandContext(ctx, "zeroclaw", "service", "status").CombinedOutput()
		if err != nil {
			return false
		}
		// If the output contains "not loaded" or "not installed", the service isn't set up
		s := string(out)
		return !strings.Contains(s, "not loaded") && !strings.Contains(s, "not installed")
	default:
		return true
	}
}

func run(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", command, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// CommandString returns a human-readable version of the command that would run.
func CommandString(framework string, action LifecycleAction) string {
	switch framework {
	case "zeroclaw":
		return "zeroclaw service " + string(action)
	case "openclaw":
		return "openclaw gateway " + string(action)
	case "hermes":
		if action == ActionStart {
			return "hermes gateway start"
		}
		return fmt.Sprintf("adapter.%s() (PID-based)", strings.Title(string(action)))
	default:
		return fmt.Sprintf("<unknown framework %q> %s", framework, action)
	}
}
