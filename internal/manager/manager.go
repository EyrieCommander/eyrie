package manager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	// stop: try service stop, fall back to pkill
	err := run(ctx, "zeroclaw", "service", string(action))
	if err != nil {
		// Service stop failed — kill all non-instance zeroclaw daemons
		killCmd := exec.Command("pkill", "-f", "zeroclaw daemon$")
		_ = killCmd.Run()
	}
	return err
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

// runDetached starts a process in the background (for daemons that don't exit).
// If logDir is non-empty, stdout and stderr are redirected to {logDir}/daemon.stdout.log.
// Returns once the process has started successfully.
func runDetached(_ context.Context, logDir string, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var logFile *os.File
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0o755); err == nil {
			logPath := filepath.Join(logDir, "daemon.stdout.log")
			if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				cmd.Stdout = f
				cmd.Stderr = f
				logFile = f
			}
		}
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}

	// Reap the process in the background and close the log file when done
	go func() {
		_ = cmd.Wait()
		if logFile != nil {
			logFile.Close()
		}
	}()
	return nil
}

// killByConfigDir finds and kills all processes that were started with --config-dir pointing
// to the given directory. This is more reliable than "zeroclaw service stop" for processes
// started via runDetached (which don't register with launchd/systemd).
func killByConfigDir(configDir string) error {
	// Use pkill to find processes with the config-dir argument
	cmd := exec.Command("pkill", "-f", fmt.Sprintf("zeroclaw daemon --config-dir %s", configDir))
	_ = cmd.Run() // pkill returns non-zero if no processes found, which is fine
	return nil
}

// ExecuteWithConfig runs a lifecycle action for a framework using a specific config path.
// This is used for provisioned instances that have their own config files.
func ExecuteWithConfig(ctx context.Context, framework, configPath string, action LifecycleAction) error {
	switch framework {
	case "zeroclaw":
		// ZeroClaw uses --config-dir (directory), not --config (file).
		// configPath points to the config file; we pass its parent directory.
		configDir := filepath.Dir(configPath)
		logDir := filepath.Join(configDir, "logs")
		switch action {
		case ActionStart:
			_ = killByConfigDir(configDir) // Clean up any stale process first
			return runDetached(ctx, logDir, "zeroclaw", "daemon", "--config-dir", configDir)
		case ActionStop:
			return killByConfigDir(configDir)
		case ActionRestart:
			if stopErr := killByConfigDir(configDir); stopErr != nil {
				fmt.Fprintf(os.Stderr, "eyrie: zeroclaw stop (config-dir %s): %v\n", configDir, stopErr)
			}
			return runDetached(ctx, logDir, "zeroclaw", "daemon", "--config-dir", configDir)
		default:
			return fmt.Errorf("unknown action %q for zeroclaw", action)
		}
	case "openclaw":
		if action == ActionStart || action == ActionRestart {
			ocLogDir := filepath.Join(filepath.Dir(configPath), "logs")
			return runDetached(ctx, ocLogDir, "openclaw", "gateway", string(action), "--config", configPath)
		}
		return run(ctx, "openclaw", "gateway", string(action), "--config", configPath)
	case "hermes":
		if action == ActionStart || action == ActionRestart {
			hLogDir := filepath.Join(filepath.Dir(configPath), "logs")
			return runDetached(ctx, hLogDir, "hermes", "gateway", string(action), "--config", configPath)
		}
		return run(ctx, "hermes", "gateway", string(action), "--config", configPath)
	default:
		return fmt.Errorf("unknown framework %q", framework)
	}
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
