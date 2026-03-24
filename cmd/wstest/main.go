package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"nhooyr.io/websocket"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token := os.Getenv("WS_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "error: WS_TOKEN environment variable is required")
		os.Exit(1)
	}
	sessionID := os.Getenv("WS_SESSION_ID")
	if sessionID == "" {
		sessionID = "test-ws"
	}
	wsURL := fmt.Sprintf("ws://127.0.0.1:42617/ws/chat?token=%s&session_id=%s", url.QueryEscape(token), url.QueryEscape(sessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial error:", err)
		os.Exit(1)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 * 1024 * 1024)

	// Read session_start
	_, data, err := conn.Read(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read session_start error:", err)
		os.Exit(1)
	}
	fmt.Printf("< %s\n", truncate(string(data), 200))

	// Send message
	msg, _ := json.Marshal(map[string]string{"type": "message", "content": "ping"})
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		fmt.Fprintln(os.Stderr, "write error:", err)
		os.Exit(1)
	}
	fmt.Println("> sent ping")

	// Read responses
	for i := 0; i < 20; i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read error:", err)
			os.Exit(1)
		}
		fmt.Printf("< %s\n", truncate(string(data), 300))
		var frame map[string]any
		json.Unmarshal(data, &frame)
		if t, _ := frame["type"].(string); t == "done" || t == "error" {
			break
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
