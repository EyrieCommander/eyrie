package adapter

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const defaultHistoryLines = 100

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// zeroclawLogPath derives the daemon log path from the config directory.
func zeroclawLogPath(configPath string) string {
	dir := filepath.Dir(configPath)
	return filepath.Join(dir, "logs", "daemon.stdout.log")
}

// openclawLogPath derives the gateway error log path from the config directory.
func openclawLogPath(configPath string) string {
	dir := filepath.Dir(configPath)
	return filepath.Join(dir, "logs", "gateway.err.log")
}

// readTailLines reads the last n lines from a file.
func readTailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n*2 {
			lines = lines[len(lines)-n:]
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, scanner.Err()
}

// zeroclaw tracing format: "\x1b[2m2026-03-11T14:29:46.728178Z\x1b[0m \x1b[33m WARN\x1b[0m \x1b[2mzeroclaw::...\x1b[0m: message"
var zcTimestampRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z?)\s+(WARN|INFO|ERROR|DEBUG|TRACE)\s+(.*)`)

func parseZeroClawLogLine(raw string) *LogEntry {
	clean := ansiRe.ReplaceAllString(raw, "")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return nil
	}

	m := zcTimestampRe.FindStringSubmatch(clean)
	if m == nil {
		if len(clean) < 3 {
			return nil
		}
		return &LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   clean,
		}
	}

	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z", m[1])
	if err != nil {
		ts = time.Now()
	}

	msg := strings.TrimSpace(m[3])
	// Clean up doubled colons from ANSI stripping (e.g. "zeroclaw::channels::telegram: msg")
	msg = strings.ReplaceAll(msg, "::", ".")

	return &LogEntry{
		Timestamp: ts,
		Level:     strings.ToLower(m[2]),
		Message:   msg,
	}
}

// openclaw format: "2026-03-10T18:52:28.446+08:00 [component] message"
var ocTimestampRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+[+-]\d{2}:\d{2})\s+\[(\S+?)\]\s*(.*)`)

func parseOpenClawLogLine(raw string) *LogEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	m := ocTimestampRe.FindStringSubmatch(raw)
	if m == nil {
		return &LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   raw,
		}
	}

	ts, err := time.Parse("2006-01-02T15:04:05.999-07:00", m[1])
	if err != nil {
		ts = time.Now()
	}

	level := "info"
	component := m[2]
	msg := strings.TrimSpace(m[3])

	// Skip empty log messages (e.g., health probe noise)
	if msg == "" {
		return nil
	}

	if strings.Contains(strings.ToLower(msg), "error") {
		level = "error"
	} else if strings.Contains(strings.ToLower(msg), "warn") {
		level = "warn"
	} else if component == "diagnostic" {
		level = "error"
	}

	return &LogEntry{
		Timestamp: ts,
		Level:     level,
		Message:   "[" + component + "] " + msg,
	}
}

// openclawGatewayLogPath returns the higher-signal gateway.log (vs the noisy
// gateway.err.log which is mostly WebSocket handshake spam).
func openclawGatewayLogPath(configPath string) string {
	dir := filepath.Dir(configPath)
	return filepath.Join(dir, "logs", "gateway.log")
}

// readHistoricalLogs reads the last N entries from a log file using the given parser.
func readHistoricalLogs(path string, n int, parse func(string) *LogEntry) []LogEntry {
	lines, err := readTailLines(path, n*2)
	if err != nil {
		return nil
	}

	var entries []LogEntry
	for _, line := range lines {
		if entry := parse(line); entry != nil {
			entries = append(entries, *entry)
		}
	}

	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return entries
}

// readHistoricalActivity reads log lines and converts them to ActivityEvents,
// filtering for entries that represent meaningful agent activity.
func readHistoricalActivity(path string, n int, parse func(string) *ActivityEvent) []ActivityEvent {
	lines, err := readTailLines(path, n*3)
	if err != nil {
		return nil
	}

	var events []ActivityEvent
	for _, line := range lines {
		if ev := parse(line); ev != nil {
			events = append(events, *ev)
		}
	}

	if len(events) > n {
		events = events[len(events)-n:]
	}
	return events
}

// parseZeroClawActivityLine converts a ZeroClaw log line into an ActivityEvent
// if it represents meaningful activity (errors, connections, pairing, agent starts).
// Returns nil for noise lines (routine polls, etc.).
func parseZeroClawActivityLine(raw string) *ActivityEvent {
	clean := ansiRe.ReplaceAllString(raw, "")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return nil
	}

	m := zcTimestampRe.FindStringSubmatch(clean)
	if m == nil {
		return nil
	}

	ts, err := time.Parse("2006-01-02T15:04:05.999999999Z", m[1])
	if err != nil {
		return nil
	}

	level := strings.ToLower(m[2])
	msg := strings.TrimSpace(m[3])
	msg = strings.ReplaceAll(msg, "::", ".")

	low := strings.ToLower(msg)

	// Skip noisy recurring entries
	if strings.Contains(low, "poll error") {
		return nil
	}

	ev := &ActivityEvent{Timestamp: ts}

	switch {
	case level == "error":
		ev.Type = "error"
		ev.Summary = msg
	case strings.Contains(low, "starting zeroclaw"):
		ev.Type = "agent_start"
		ev.Summary = msg
	case strings.Contains(low, "paired"):
		ev.Type = "agent"
		ev.Summary = msg
	case strings.Contains(low, "connecting to gateway") || strings.Contains(low, "connected and identified"):
		ev.Type = "agent"
		ev.Summary = msg
	case strings.Contains(low, "listening for messages"):
		ev.Type = "agent"
		ev.Summary = msg
	case strings.Contains(low, "config loaded"):
		ev.Type = "agent"
		ev.Summary = msg
	case strings.Contains(low, "warming up"):
		ev.Type = "agent"
		ev.Summary = msg
	case strings.Contains(low, "channel") && strings.Contains(low, "restarting"):
		ev.Type = "error"
		ev.Summary = msg
	case strings.Contains(low, "reconnect"):
		ev.Type = "agent"
		ev.Summary = msg
	default:
		if level == "warn" {
			return nil
		}
		ev.Type = "agent"
		ev.Summary = msg
	}

	return ev
}

// parseOpenClawActivityLine converts an OpenClaw gateway.log line into an
// ActivityEvent for meaningful gateway operations (startup, pairing, config
// reload, RPC calls, channel events).
func parseOpenClawActivityLine(raw string) *ActivityEvent {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	m := ocTimestampRe.FindStringSubmatch(raw)
	if m == nil {
		return nil
	}

	ts, err := time.Parse("2006-01-02T15:04:05.999-07:00", m[1])
	if err != nil {
		return nil
	}

	component := m[2]
	msg := m[3]
	low := strings.ToLower(msg)

	// Skip health check RPC noise
	if strings.Contains(low, "res") && strings.Contains(low, "health") {
		return nil
	}

	ev := &ActivityEvent{Timestamp: ts}

	switch {
	case component == "gateway" && strings.Contains(low, "listening on"):
		ev.Type = "agent_start"
		ev.Summary = msg
	case component == "gateway" && strings.Contains(low, "pairing"):
		ev.Type = "agent"
		ev.Summary = "[pairing] " + msg
	case component == "gateway" && strings.Contains(low, "update available"):
		ev.Type = "agent"
		ev.Summary = msg
	case component == "gateway" && strings.Contains(low, "cron"):
		ev.Type = "agent"
		ev.Summary = "[cron] " + msg
	case component == "reload":
		ev.Type = "agent"
		ev.Summary = "[config] " + msg
	case component == "telegram":
		ev.Type = "agent"
		ev.Summary = "[telegram] " + msg
	case component == "hooks:loader" || component == "hooks":
		ev.Type = "agent"
		ev.Summary = "[hooks] " + msg
	case component == "ws" && strings.Contains(low, "res"):
		// RPC call results (e.g. "⇄ res ✓ sessions.list 84ms")
		ev.Type = "agent"
		ev.Summary = "[rpc] " + msg
	case strings.Contains(low, "error"):
		ev.Type = "error"
		ev.Summary = "[" + component + "] " + msg
	default:
		ev.Type = "agent"
		ev.Summary = "[" + component + "] " + msg
	}

	return ev
}
