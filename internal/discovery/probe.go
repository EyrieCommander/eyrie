package discovery

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nhooyr.io/websocket"
)

// probeHealth checks whether an agent's gateway is actually responding.
func probeHealth(ctx context.Context, framework, host string, port int) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	switch framework {
	case "zeroclaw":
		return probeHTTP(probeCtx, host, port)
	case "openclaw":
		return probeWebSocket(probeCtx, host, port)
	default:
		return probeHTTP(probeCtx, host, port)
	}
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
