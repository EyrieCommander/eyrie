package manager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
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
	case "embedded":
		// Embedded agents run in-process as goroutines — lifecycle is managed
		// by the adapter, not by external CLI commands. This is a no-op.
		return nil
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
	// Stop: try service stop first, then always pkill to catch manually started daemons
	svcErr := run(ctx, "zeroclaw", "service", string(action))
	killCmd := exec.CommandContext(ctx, "pkill", "-f", "zeroclaw daemon")
	killErr := killCmd.Run()
	// Return nil if either succeeded; only fail if both failed
	if svcErr != nil && killErr != nil {
		// pkill exit code 1 means "no processes matched" — that's OK
		if exitErr, ok := killErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return svcErr // no processes to kill; report the service error
		}
		return fmt.Errorf("service stop: %v; pkill: %v", svcErr, killErr)
	}
	return nil
}

func executeOpenClaw(ctx context.Context, action LifecycleAction) error {
	return runWithNode22(ctx, "openclaw", "gateway", string(action))
}

// node22BinDir finds the nvm-managed Node.js v22 bin directory.
// OpenClaw requires Node 22 — newer versions crash on older macOS due to
// missing libc++ symbols. Returns "" if no v22 installation is found.
func node22BinDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	nvmDir := filepath.Join(home, ".nvm", "versions", "node")
	entries, err := os.ReadDir(nvmDir)
	if err != nil {
		return ""
	}
	// Collect all v22.x.x directories and pick the latest
	var v22Dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "v22.") {
			v22Dirs = append(v22Dirs, e.Name())
		}
	}
	if len(v22Dirs) == 0 {
		return ""
	}
	// Sort by semantic version (numeric comparison) instead of lexicographic
	// to avoid issues like v22.2.0 sorting after v22.10.0.
	sort.Slice(v22Dirs, func(i, j int) bool {
		return compareNodeVersions(v22Dirs[i], v22Dirs[j]) < 0
	})
	return filepath.Join(nvmDir, v22Dirs[len(v22Dirs)-1], "bin")
}

// compareNodeVersions compares two Node.js version directory names (e.g. "v22.2.0", "v22.10.1")
// by their numeric components. Returns negative if a < b, 0 if equal, positive if a > b.
func compareNodeVersions(a, b string) int {
	partsA := strings.Split(strings.TrimPrefix(a, "v"), ".")
	partsB := strings.Split(strings.TrimPrefix(b, "v"), ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var na, nb int
		if i < len(partsA) {
			na, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			nb, _ = strconv.Atoi(partsB[i])
		}
		if na != nb {
			return na - nb
		}
	}
	return 0
}

// node22Env returns a copy of os.Environ() with Node 22 prepended to PATH,
// or nil if no v22 installation is found.
func node22Env() []string {
	binDir := node22BinDir()
	if binDir == "" {
		return nil
	}
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + e[5:]
			break
		}
	}
	return env
}

// runWithNode22 runs a command with Node.js v22 at the front of PATH.
// Falls back to the default PATH if no v22 installation is found.
func runWithNode22(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	if env := node22Env(); env != nil {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", command, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
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

// runDetachedWithNode22 is like runDetached but prepends Node.js v22 to PATH.
func runDetachedWithNode22(ctx context.Context, logDir string, command string, args ...string) error {
	return runDetachedWithEnv(ctx, logDir, node22Env(), command, args...)
}

// runDetached starts a process in the background (for daemons that don't exit).
// If logDir is non-empty, stdout and stderr are redirected to {logDir}/daemon.stdout.log.
// Returns once the process has started successfully.
func runDetached(ctx context.Context, logDir string, command string, args ...string) error {
	return runDetachedWithEnv(ctx, logDir, nil, command, args...)
}

// runDetachedWithEnv starts a detached daemon process. The context parameter is
// intentionally unused because the process must outlive the caller's context.
func runDetachedWithEnv(_ context.Context, logDir string, env []string, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if env != nil {
		cmd.Env = env
	}

	var logFile *os.File
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "eyrie: failed to create log dir %s: %v\n", logDir, err)
		} else {
			logPath := filepath.Join(logDir, "daemon.stdout.log")
			if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "eyrie: failed to open log file %s: %v\n", logPath, err)
			} else {
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
func killByConfigDir(configDir string) (found bool, err error) {
	// Escape regex metacharacters in configDir to avoid injection
	escaped := regexp.QuoteMeta(configDir)
	cmd := exec.Command("pkill", "-f", fmt.Sprintf("zeroclaw daemon --config-dir %s([[:space:]]|$)", escaped))
	if err := cmd.Run(); err != nil {
		// pkill exit code 1 means "no processes matched" — not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("pkill for config-dir %s: %w", configDir, err)
	}
	return true, nil
}

// processExistsByConfigDir checks if any processes match the config-dir pattern
// without sending a signal. Used in restart loops to wait for process exit.
func processExistsByConfigDir(configDir string) (bool, error) {
	escaped := regexp.QuoteMeta(configDir)
	cmd := exec.Command("pgrep", "-f", fmt.Sprintf("zeroclaw daemon --config-dir %s([[:space:]]|$)", escaped))
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("pgrep for config-dir %s: %w", configDir, err)
	}
	return true, nil
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
			_, _ = killByConfigDir(configDir) // Clean up any stale process first
			return runDetached(ctx, logDir, "zeroclaw", "daemon", "--config-dir", configDir)
		case ActionStop:
			_, err := killByConfigDir(configDir)
			return err
		case ActionRestart:
			if _, stopErr := killByConfigDir(configDir); stopErr != nil {
				fmt.Fprintf(os.Stderr, "eyrie: zeroclaw stop (config-dir %s): %v\n", configDir, stopErr)
			}
			// Wait for old process to exit before starting a new one
			for i := 0; i < 10; i++ {
				time.Sleep(100 * time.Millisecond)
				found, err := processExistsByConfigDir(configDir)
				if err != nil || !found {
					break
				}
			}
			// Verify the old process actually exited before spawning a new one
			if still, _ := processExistsByConfigDir(configDir); still {
				return fmt.Errorf("old process for config-dir %s still running after 1s — not starting duplicate", configDir)
			}
			return runDetached(ctx, logDir, "zeroclaw", "daemon", "--config-dir", configDir)
		default:
			return fmt.Errorf("unknown action %q for zeroclaw", action)
		}
	case "openclaw":
		if action == ActionStart || action == ActionRestart {
			ocLogDir := filepath.Join(filepath.Dir(configPath), "logs")
			return runDetachedWithNode22(ctx, ocLogDir, "openclaw", "gateway", string(action), "--config", configPath)
		}
		return runWithNode22(ctx, "openclaw", "gateway", string(action), "--config", configPath)
	case "hermes":
		if action == ActionStart || action == ActionRestart {
			hLogDir := filepath.Join(filepath.Dir(configPath), "logs")
			return runDetached(ctx, hLogDir, "hermes", "gateway", string(action), "--config", configPath)
		}
		return run(ctx, "hermes", "gateway", string(action), "--config", configPath)
	case "embedded":
		// Embedded agents have no external process — lifecycle is managed
		// by the adapter's Start/Stop/Restart methods directly.
		return nil
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
