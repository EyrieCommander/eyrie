package server

import (
	"os"
	"os/exec"
	"path/filepath"
)

// tmuxConfContent is a minimal tmux config that makes tmux invisible to the user.
// No prefix key, no status bar — the user just sees a normal shell that happens
// to persist across WebSocket reconnections.
const tmuxConfContent = `# Eyrie managed tmux config v4 — do not edit
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
`

// tmuxConfigPath returns the path to Eyrie's tmux config file.
func tmuxConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eyrie", "tmux.conf")
}

// ensureTmuxConfig writes the Eyrie tmux config if it doesn't exist or has changed.
func ensureTmuxConfig() string {
	path := tmuxConfigPath()

	// Check if it already has the right content
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == tmuxConfContent {
		return path
	}

	// Write/overwrite
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(tmuxConfContent), 0644)
	return path
}

// tmuxSocketPath returns the path for Eyrie's dedicated tmux socket.
// Using a separate socket isolates Eyrie sessions from the user's personal tmux.
func tmuxSocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eyrie", "tmux.sock")
}

// hasTmux checks if tmux is available on the system.
func hasTmux() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}
