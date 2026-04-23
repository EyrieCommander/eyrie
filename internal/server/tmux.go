package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// tmuxConfContent is a minimal tmux config that makes tmux invisible to the user.
// No prefix key, no status bar — the user just sees a normal shell that happens
// to persist across WebSocket reconnections.
const tmuxConfContent = `# Eyrie managed tmux config v6 — do not edit
# Minimal tmux: no prefix key, persistent sessions, clean status bar.

set -g prefix None
set -g prefix2 None
set -g status on
set -g status-position top
set -g status-style "bg=#1a1a1a,fg=#555555"
set -g status-left ""
set -g status-right " #S "
set -g status-right-style "fg=#888888"
set -g window-status-current-format ""
set -g window-status-format ""
set -g history-limit 50000
set -g mouse on
set -g default-terminal "xterm-256color"

# Mouse drag selects text in copy mode. On release, pipe the selection to
# the OS clipboard and exit copy mode. This lets scrollback work (mouse on)
# while still supporting copy via drag-select.
bind-key -T copy-mode MouseDragEnd1Pane send-keys -X copy-pipe-and-cancel "pbcopy"
bind-key -T copy-mode-vi MouseDragEnd1Pane send-keys -X copy-pipe-and-cancel "pbcopy"
`

// eyrieHomeDir returns ~/.eyrie, preferring os.UserHomeDir and falling back
// to $HOME. Returns an error if neither is usable — callers should treat
// that as "tmux not usable for this session" and bail to a plain shell
// rather than silently building paths like "/.eyrie/tmux.conf".
func eyrieHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if env := os.Getenv("HOME"); env != "" {
			home = env
		} else if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		} else {
			return "", fmt.Errorf("cannot resolve home directory: home is empty")
		}
	}
	return filepath.Join(home, ".eyrie"), nil
}

// tmuxConfigPath returns the path to Eyrie's tmux config file, or an error
// if the home directory can't be resolved.
func tmuxConfigPath() (string, error) {
	dir, err := eyrieHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tmux.conf"), nil
}

// ensureTmuxConfig writes the Eyrie tmux config if it doesn't exist or has changed.
// Returns the path and a non-nil error if the directory or file couldn't be
// written; callers should treat an error as "tmux not usable for this session".
func ensureTmuxConfig() (string, error) {
	path, err := tmuxConfigPath()
	if err != nil {
		return "", err
	}

	// Check if it already has the right content
	existing, rerr := os.ReadFile(path)
	if rerr == nil && string(existing) == tmuxConfContent {
		return path, nil
	}

	// Write/overwrite
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return path, fmt.Errorf("creating tmux config dir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(tmuxConfContent), 0644); err != nil {
		return path, fmt.Errorf("writing tmux config %s: %w", path, err)
	}
	return path, nil
}

// tmuxSocketPath returns the path for Eyrie's dedicated tmux socket, or an
// error if the home directory can't be resolved. Using a separate socket
// isolates Eyrie sessions from the user's personal tmux server.
func tmuxSocketPath() (string, error) {
	dir, err := eyrieHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tmux.sock"), nil
}

// hasTmux checks if tmux is available on the system.
func hasTmux() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}
