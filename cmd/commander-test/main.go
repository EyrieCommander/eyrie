// commander-test is a small CLI for exercising the Eyrie commander
// without a UI. Sends a message to POST /api/commander/chat, consumes
// the SSE stream, and pretty-prints each event type. Also supports
// viewing and clearing conversation history.
//
// Usage:
//   commander-test "what projects do I have?"
//   commander-test -history
//   commander-test -clear
//   commander-test -url http://localhost:7200 "hello"
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// ANSI color codes. NO_COLOR env var disables them.
var (
	reset  = "\033[0m"
	dim    = "\033[2m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	gray   = "\033[90m"
)

func init() {
	if os.Getenv("NO_COLOR") != "" {
		reset, dim, bold, red, green, yellow, cyan, gray = "", "", "", "", "", "", "", ""
	}
}

func main() {
	var (
		baseURL = flag.String("url", "http://localhost:7200", "base URL of the Eyrie server")
		history = flag.Bool("history", false, "print conversation history instead of sending a message")
		clear   = flag.Bool("clear", false, "clear conversation history")
	)
	flag.Parse()

	switch {
	case *clear:
		runClear(*baseURL)
	case *history:
		runHistory(*baseURL)
	default:
		runChat(*baseURL)
	}
}

func runChat(baseURL string) {
	msg := strings.Join(flag.Args(), " ")
	if msg == "" {
		// Read from stdin if no args given.
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%serror reading stdin: %v%s\n", red, err, reset)
			os.Exit(1)
		}
		msg = strings.TrimSpace(string(b))
	}
	if msg == "" {
		fmt.Fprintf(os.Stderr, "usage: commander-test [flags] <message>\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	body, _ := json.Marshal(map[string]string{"message": msg})
	req, err := http.NewRequest("POST", baseURL+"/api/commander/chat", bytes.NewReader(body))
	if err != nil {
		fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	fmt.Printf("%s%s> %s%s\n", bold, cyan, msg, reset)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal(fmt.Errorf("request failed (is the server running at %s?): %w", baseURL, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		fatal(fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b)))
	}

	streamEvents(resp.Body)
}

// streamEvents reads the SSE stream and pretty-prints each event. Each
// event type gets a distinct visual treatment so the flow is easy to
// follow at a glance.
func streamEvents(r io.Reader) {
	scanner := bufio.NewScanner(r)
	// Allow large tool results — list_projects can return lots of JSON.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	inDelta := false // track whether we're mid-delta-paragraph for spacing
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")

		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}

		switch ev["type"] {
		case "delta":
			if !inDelta {
				// Start a new assistant reply line. The prefix is shown
				// once; subsequent deltas extend the same line.
				fmt.Printf("%s%s▸ %s", bold, green, reset)
				inDelta = true
			}
			text, _ := ev["text"].(string)
			fmt.Print(text)

		case "tool_call":
			if inDelta {
				fmt.Println()
				inDelta = false
			}
			name, _ := ev["name"].(string)
			args, _ := ev["args"].(map[string]any)
			argsStr := ""
			if len(args) > 0 {
				b, _ := json.Marshal(args)
				argsStr = string(b)
			}
			fmt.Printf("%s%s→ %s(%s)%s\n", bold, cyan, name, argsStr, reset)

		case "tool_result":
			if inDelta {
				fmt.Println()
				inDelta = false
			}
			name, _ := ev["name"].(string)
			output, _ := ev["output"].(string)
			isErr, _ := ev["error"].(bool)
			preview := output
			if len(preview) > 240 {
				preview = preview[:240] + "…"
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			if isErr {
				fmt.Printf("%s%s← %s (error): %s%s\n", bold, red, name, preview, reset)
			} else {
				fmt.Printf("%s%s← %s: %s%s%s\n", dim, gray, name, reset, preview, reset)
			}

		case "message":
			// We've already shown the content via deltas; skip.
			// (If the server ever emits a message without prior deltas,
			// uncomment the block below to display it.)
			// if content, ok := ev["content"].(string); ok && !inDelta {
			// 	fmt.Printf("%s%s▸ %s%s\n", bold, green, reset, content)
			// }

		case "done":
			if inDelta {
				fmt.Println()
				inDelta = false
			}
			in, _ := ev["input_tokens"].(float64)
			out, _ := ev["output_tokens"].(float64)
			if in > 0 || out > 0 {
				fmt.Printf("%s%s✓ done (in=%d out=%d)%s\n", dim, green, int(in), int(out), reset)
			} else {
				fmt.Printf("%s%s✓ done%s\n", dim, green, reset)
			}

		case "error":
			if inDelta {
				fmt.Println()
				inDelta = false
			}
			msg, _ := ev["error"].(string)
			fmt.Printf("%s%s✗ error: %s%s\n", bold, red, msg, reset)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "%sstream read error: %v%s\n", red, err, reset)
	}
}

func runHistory(baseURL string) {
	resp, err := http.Get(baseURL + "/api/commander/history")
	if err != nil {
		fatal(fmt.Errorf("request failed (is the server running at %s?): %w", baseURL, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		fatal(fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b)))
	}

	var messages []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		fatal(err)
	}
	if len(messages) == 0 {
		fmt.Printf("%s(no conversation yet)%s\n", dim, reset)
		return
	}
	for i, m := range messages {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		switch role {
		case "user":
			fmt.Printf("%s%s[%d] > %s%s\n", bold, cyan, i+1, content, reset)
		case "assistant":
			if content == "" {
				// Pure tool-call message (no text content)
				if calls, ok := m["tool_calls"].([]any); ok && len(calls) > 0 {
					fmt.Printf("%s%s[%d] ▸ (calling %d tool(s))%s\n", bold, green, i+1, len(calls), reset)
				}
			} else {
				fmt.Printf("%s%s[%d] ▸%s %s\n", bold, green, i+1, reset, content)
			}
		case "tool":
			name, _ := m["name"].(string)
			preview := content
			if len(preview) > 240 {
				preview = preview[:240] + "…"
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			fmt.Printf("%s%s[%d] ← %s: %s%s\n", dim, gray, i+1, name, preview, reset)
		default:
			fmt.Printf("%s[%d] %s: %s%s\n", dim, i+1, role, content, reset)
		}
	}
}

func runClear(baseURL string) {
	req, err := http.NewRequest("DELETE", baseURL+"/api/commander/history", nil)
	if err != nil {
		fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal(fmt.Errorf("request failed (is the server running at %s?): %w", baseURL, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		fatal(fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b)))
	}
	fmt.Printf("%s✓ conversation cleared%s\n", green, reset)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "%serror: %v%s\n", red, err, reset)
	os.Exit(1)
}
