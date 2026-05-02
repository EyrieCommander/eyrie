package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/config"
	"nhooyr.io/websocket"
)

// probeHealth checks whether an agent's gateway is actually responding.
func probeHealth(ctx context.Context, framework, host string, port int) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	switch framework {
	case adapter.FrameworkHermes:
		return probeHermesPID()
	case adapter.FrameworkEmbedded:
		// Embedded agents have no HTTP endpoint. Liveness is determined by
		// the adapter's running flag, checked via probeEmbeddedByName().
		// The caller passes the agent name for embedded lookups.
		return probeEmbeddedByName(host)
	default:
		return probeHTTP(probeCtx, host, port)
	}
}

// probeHermesPID checks if Hermes is running by reading the PID file
func probeHermesPID() bool {
	// Check ~/.hermes/gateway.pid
	pidFile := config.ExpandHome("~/.hermes/gateway.pid")

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	// Parse JSON format (Hermes uses {"pid": 12345, ...})
	var pidInfo struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(pidData, &pidInfo); err != nil {
		// Try plain text as fallback
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if parseErr != nil {
			return false
		}
		pidInfo.PID = pid
	}

	pid := pidInfo.PID
	if pid <= 0 {
		return false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check existence. EPERM means the process exists but
	// this sandboxed caller cannot signal it.
	if err := process.Signal(syscall.Signal(0)); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}

	return true
}

// probeEmbeddedByName checks whether an embedded agent is running by looking
// up its cached adapter singleton.
func probeEmbeddedByName(name string) bool {
	embeddedAdaptersMu.RLock()
	a := embeddedAdapters[name]
	embeddedAdaptersMu.RUnlock()
	if a == nil {
		return false
	}
	return a.IsRunning()
}

// probeHTTP does a quick GET /health against an HTTP gateway.
func probeHTTP(ctx context.Context, host string, port int) bool {
	url := fmt.Sprintf("http://%s:%d/health", host, port)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// probeWebSocket attempts a WebSocket dial to confirm the gateway is up.
func probeWebSocket(ctx context.Context, host string, port int) bool {
	url := fmt.Sprintf("ws://%s:%d", host, port)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		// Fall back to HTTP probe (OpenClaw serves HTTP on the same port)
		return probeHTTP(ctx, host, port)
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return true
}
