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

// stdinReader is a single bufio.Reader over os.Stdin shared across all
// approval prompts. Creating a fresh bufio.Reader per prompt drops any
// buffered read-ahead when the old reader goes out of scope — and
// bufio reads in large chunks, so with piped input the first prompt
// consumes the entire pipe and later prompts see EOF. Sharing one
// reader keeps buffered bytes available across prompts.
var stdinReader = bufio.NewReader(os.Stdin)

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
		b, err := io.ReadAll(stdinReader)
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

	streamEvents(resp.Body, baseURL)
}

// streamEvents reads the SSE stream and pretty-prints each event.
// On confirm_required, pauses to prompt the user, POSTs the decision
// to /api/commander/confirm/{id}, and recursively streams the
// continuation response.
func streamEvents(r io.Reader, baseURL string) {
	scanner := bufio.NewScanner(r)
	// Allow large tool results — list_projects can return lots of JSON.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	inDelta := false // track whether we're mid-delta-paragraph for spacing
	endDelta := func() {
		if inDelta {
			fmt.Println()
			inDelta = false
		}
	}
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
			endDelta()
			name, _ := ev["name"].(string)
			args, _ := ev["args"].(map[string]any)
			argsStr := ""
			if len(args) > 0 {
				b, _ := json.Marshal(args)
				argsStr = string(b)
			}
			fmt.Printf("%s%s→ %s(%s)%s\n", bold, cyan, name, argsStr, reset)

		case "tool_result":
			endDelta()
			name, _ := ev["name"].(string)
			output, _ := ev["output"].(string)
			isErr, _ := ev["error"].(bool)
			preview := output
			if r := []rune(preview); len(r) > 240 {
				preview = string(r[:240]) + "…"
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			if isErr {
				fmt.Printf("%s%s← %s (error): %s%s\n", bold, red, name, preview, reset)
			} else {
				fmt.Printf("%s%s← %s: %s%s%s\n", dim, gray, name, reset, preview, reset)
			}

		case "message":
			// We've already shown the content via deltas; skip.

		case "confirm_required":
			endDelta()
			id, _ := ev["id"].(string)
			summary, _ := ev["summary"].(string)
			tool, _ := ev["tool"].(string)
			args, _ := ev["args"].(map[string]any)
			argsJSON, _ := json.MarshalIndent(args, "    ", "  ")
			fmt.Printf("\n%s%s⚠ approval required: %s%s\n", bold, yellow, tool, reset)
			fmt.Printf("    %s%s%s\n", bold, summary, reset)
			fmt.Printf("    args:\n    %s\n", string(argsJSON))
			approved, reason := promptDecision()
			if err := postConfirm(baseURL, id, approved, reason); err != nil {
				fmt.Fprintf(os.Stderr, "%sconfirm POST failed: %v%s\n", red, err, reset)
				return
			}
			// The confirm endpoint streams the continuation as SSE.
			// We already drained this response; recursively stream the
			// new one.
			return

		case "done":
			endDelta()
			in, _ := ev["input_tokens"].(float64)
			out, _ := ev["output_tokens"].(float64)
			ctxTokens, _ := ev["context_tokens"].(float64)
			ctxWindow, _ := ev["context_window"].(float64)
			if in > 0 || out > 0 {
				usage := ""
				if ctxWindow > 0 && ctxTokens > 0 {
					pct := ctxTokens / ctxWindow * 100
					usage = fmt.Sprintf(" | ctx %dk/%dk %.0f%%",
						int(ctxTokens)/1000, int(ctxWindow)/1000, pct)
				}
				fmt.Printf("%s%s✓ done (in=%d out=%d%s)%s\n", dim, green, int(in), int(out), usage, reset)
			} else {
				fmt.Printf("%s%s✓ done%s\n", dim, green, reset)
			}

		case "error":
			endDelta()
			msg, _ := ev["error"].(string)
			fmt.Printf("%s%s✗ error: %s%s\n", bold, red, msg, reset)

		default:
			// Unknown event type. Surface it loudly so a server/client
			// version mismatch doesn't silently swallow events (the
			// bug we hit when confirm_required shipped before the CLI
			// knew how to render it).
			endDelta()
			t, _ := ev["type"].(string)
			fmt.Printf("%s%s? unknown event type %q: %s%s\n", dim, yellow, t, payload, reset)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "%sstream read error: %v%s\n", red, err, reset)
	}
}

// promptDecision reads a y/n decision from the shared stdinReader.
// Any other input is treated as denial with an empty reason. Returns
// (approved, reason).
func promptDecision() (bool, string) {
	fmt.Printf("%s    approve? [y/N] %s", bold, reset)
	line, err := stdinReader.ReadString('\n')
	if err != nil {
		return false, "stdin read error"
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans == "y" || ans == "yes" {
		return true, ""
	}
	// Offer to collect a reason on "n" / anything else. Empty reason is fine.
	fmt.Printf("%s    denial reason (optional, press enter to skip): %s", dim, reset)
	reasonLine, _ := stdinReader.ReadString('\n')
	return false, strings.TrimSpace(reasonLine)
}

// postConfirm sends the approval decision and streams the continuation.
// Prints the same event flow via streamEvents, which may recursively
// trigger further confirmations if the LLM calls another write tool.
func postConfirm(baseURL, id string, approved bool, reason string) error {
	body, _ := json.Marshal(map[string]any{
		"approved": approved,
		"reason":   reason,
	})
	req, err := http.NewRequest("POST", baseURL+"/api/commander/confirm/"+id, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	decision := "denied"
	if approved {
		decision = "approved"
	}
	fmt.Printf("%s    → %s%s\n\n", dim, decision, reset)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}
	streamEvents(resp.Body, baseURL)
	return nil
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
			if r := []rune(preview); len(r) > 240 {
				preview = string(r[:240]) + "…"
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
