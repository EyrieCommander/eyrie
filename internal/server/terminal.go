package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/Audacity88/eyrie/internal/discovery"
	"nhooyr.io/websocket"
)

// validSessionName restricts tmux session names to a safe subset.
// Rejects path separators, shell metacharacters, and anything that
// could be misinterpreted by tmux or the shell.
var validSessionName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// handleShellTerminal spawns a shell terminal session.
// If tmux is available and a ?session= param is provided, the session persists
// across WebSocket reconnections (navigate away, come back, output is still there).
// Falls back to a plain shell if tmux is not installed.
// GET /api/terminal/ws?session=eyrie-zeroclaw
func (s *Server) handleShellTerminal(w http.ResponseWriter, r *http.Request) {
	// Validate session name BEFORE upgrading so we can return a clean 400.
	sessionName := r.URL.Query().Get("session")
	if sessionName != "" && !validSessionName.MatchString(sessionName) {
		http.Error(w, fmt.Sprintf("invalid session name %q: only [a-zA-Z0-9_-]+ allowed", sessionName), http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to upgrade connection: %v", err), http.StatusBadRequest)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "terminal session ended")

	ctx := context.Background()

	useTmux := sessionName != "" && hasTmux()

	var cmd *exec.Cmd
	if useTmux {
		confPath, confErr := ensureTmuxConfig()
		if confErr != nil {
			slog.Warn("tmux config unavailable, falling back to plain shell", "error", confErr)
			useTmux = false
		} else {
			socketPath := tmuxSocketPath()
			// Use a dedicated socket (-L is socket name) so Eyrie's tmux sessions
			// are isolated from the user's personal tmux server and use our config.
			// -A: attach if session exists, create if not
			cmd = exec.CommandContext(ctx, "tmux", "-f", confPath, "-S", socketPath, "new-session", "-A", "-s", sessionName)
			slog.Info("Starting tmux session", "session", sessionName, "socket", socketPath)
		}
	}
	if !useTmux {
		// Prefer $SHELL; otherwise probe a POSIX-portable candidate list.
		// Hard-coding /bin/zsh is macOS-centric — many Linux systems don't
		// ship zsh at all, so fall back to bash, then sh.
		shell := os.Getenv("SHELL")
		if shell == "" {
			for _, candidate := range []string{"/bin/bash", "/bin/sh", "/bin/zsh"} {
				if _, err := os.Stat(candidate); err == nil {
					shell = candidate
					break
				}
			}
		}
		if shell == "" {
			shell = "/bin/sh" // last resort; POSIX guarantees this path
		}
		cmd = exec.CommandContext(ctx, shell, "-l")
		if sessionName != "" {
			slog.Info("tmux not available, falling back to plain shell", "session", sessionName, "shell", shell)
		}
	}

	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		slog.Error("Failed to start shell PTY", "error", err, "tmux", useTmux)
		return
	}
	defer func() {
		ptmx.Close()
		if cmd.Process != nil {
			if useTmux {
				// For tmux: closing the PTY fd detaches the client but the
				// tmux session stays alive in the background. Send SIGHUP
				// which tmux interprets as "client detached" (SIGKILL
				// would kill the wrapping client process harder than
				// needed, though the session itself survives either way).
				cmd.Process.Signal(syscall.SIGHUP)
			} else {
				cmd.Process.Kill()
			}
		}
	}()

	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	ptyDone := make(chan struct{})
	go func() {
		defer close(ptyDone)
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				conn.Close(websocket.StatusNormalClosure, "process exited")
				return
			}
			if n > 0 {
				if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
					return
				}
			}
		}
	}()

	for {
		select {
		case <-ptyDone:
			return
		default:
		}
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if msgType == websocket.MessageText || msgType == websocket.MessageBinary {
			msg := string(data)
			if len(msg) > 7 && msg[:7] == "resize:" {
				var rows, cols uint16
				fmt.Sscanf(msg[7:], "%d:%d", &rows, &cols)
				pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
				continue
			}
			if _, err := ptmx.Write(data); err != nil {
				return
			}
		}
	}
}

// handleTerminal spawns an interactive terminal session for an agent
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	slog.Info("Terminal WebSocket request received", "agent", name)

	// Accept WebSocket connection
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow local connections without origin check
	})
	if err != nil {
		slog.Error("Failed to accept WebSocket", "error", err)
		http.Error(w, fmt.Sprintf("Failed to upgrade connection: %v", err), http.StatusBadRequest)
		return
	}

	slog.Info("WebSocket connection accepted", "agent", name)

	// Ensure connection is closed when handler exits
	defer func() {
		slog.Info("Terminal handler exiting", "agent", name)
		conn.Close(websocket.StatusNormalClosure, "terminal session ended")
	}()

	// Use background context for terminal operations (don't cancel when HTTP request ends)
	ctx := context.Background()

	// Find the agent
	result := s.runDiscovery(ctx)
	var agentResult *discovery.AgentResult
	for _, ar := range result.Agents {
		if ar.Agent.Name == name {
			agentResult = &ar
			break
		}
	}

	if agentResult == nil {
		slog.Error("Agent not found", "agent", name)
		return
	}

	slog.Info("Agent found", "agent", name, "framework", agentResult.Agent.Framework)

	// Determine binary path and args for interactive mode
	var binaryPath string
	var args []string

	switch agentResult.Agent.Framework {
	case "hermes":
		binaryPath = os.ExpandEnv("$HOME/.local/bin/hermes")
		args = []string{} // hermes is interactive by default
	case "zeroclaw":
		binaryPath = os.ExpandEnv("$HOME/.cargo/bin/zeroclaw")
		args = []string{"agent"} // zeroclaw needs 'agent' subcommand for interactive mode
	case "openclaw":
		binaryPath = "/usr/local/bin/openclaw"
		args = []string{"tui"} // openclaw needs 'tui' subcommand for terminal UI
	case "picoclaw":
		binaryPath = os.ExpandEnv("$HOME/go/bin/picoclaw")
		args = []string{"agent"} // picoclaw agent subcommand for interactive chat
	default:
		slog.Error("No terminal support for framework", "agent", name, "framework", agentResult.Agent.Framework)
		return
	}

	// Spawn the agent CLI in interactive mode
	cmd := exec.CommandContext(ctx, binaryPath, args...)

	// Build environment
	env := append(os.Environ(),
		"TERM=xterm-256color",
		"PYTHONUNBUFFERED=1", // Disable Python output buffering for Hermes
	)

	// For OpenClaw, ensure Node 22 is in PATH
	if agentResult.Agent.Framework == "openclaw" {
		home := os.Getenv("HOME")
		if home != "" {
			// Add nvm's Node 22 path
			nvmNodePath := fmt.Sprintf("%s/.nvm/versions/node/v22.22.1/bin", home)
			currentPath := os.Getenv("PATH")
			env = append(env, fmt.Sprintf("PATH=%s:%s", nvmNodePath, currentPath))
		}
	}

	cmd.Env = env

	// Start PTY (pseudo-terminal) for full terminal emulation
	slog.Info("Starting PTY", "agent", name, "binary", binaryPath)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		slog.Error("Failed to start PTY", "agent", name, "error", err)
		return
	}
	defer func() {
		slog.Info("Closing PTY", "agent", name)
		ptmx.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	slog.Info("PTY started successfully", "agent", name)

	// Set initial terminal size (default to 80x24)
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
	})

	// Set PTY to unbuffered mode for immediate output
	// This helps with interactive applications
	_ = ptmx.SetDeadline(time.Time{}) // Clear any deadlines

	slog.Info("Entering WebSocket loop", "agent", name)

	// Bridge PTY <-> WebSocket
	ptyDone := make(chan struct{})
	go func() {
		defer close(ptyDone)
		// PTY stdout -> WebSocket
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					slog.Error("PTY read error", "agent", name, "error", err)
				}
				// Process exited, close WebSocket gracefully
				conn.Close(websocket.StatusNormalClosure, "process exited")
				return
			}
			if n > 0 {
				// Send data to WebSocket
				if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
					slog.Error("WebSocket write error", "agent", name, "error", err)
					return
				}
			}
		}
	}()

	// WebSocket -> PTY stdin
	for {
		select {
		case <-ptyDone:
			// PTY died, stop reading from WebSocket
			slog.Info("PTY closed, stopping WebSocket read loop", "agent", name)
			return
		default:
		}

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			slog.Info("WebSocket read ended", "agent", name, "error", err)
			break
		}

		if msgType == websocket.MessageText || msgType == websocket.MessageBinary {
			// Check for resize messages (format: "resize:rows:cols")
			msg := string(data)
			if len(msg) > 7 && msg[:7] == "resize:" {
				var rows, cols uint16
				fmt.Sscanf(msg[7:], "%d:%d", &rows, &cols)
				slog.Debug("Terminal resize", "agent", name, "rows", rows, "cols", cols)
				pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
				continue
			}

			// Write user input to PTY (check if process is still alive)
			if _, err := ptmx.Write(data); err != nil {
				// PTY closed - process exited
				slog.Info("PTY write failed, process likely exited", "agent", name)
				return
			}
		}
	}
}
