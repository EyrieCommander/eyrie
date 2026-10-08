package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/google/uuid"
)

// codexConfig is the on-disk config for Eyrie agents powered by Codex App Server.
// Eyrie owns identity and routing; Codex owns one or more persisted runtime threads.
type codexConfig struct {
	BinaryPath       string            `json:"binary_path,omitempty"`
	CWD              string            `json:"cwd,omitempty"`
	CodexHome        string            `json:"codex_home,omitempty"`
	InstructionsPath string            `json:"instructions_path,omitempty"`
	Model            string            `json:"model,omitempty"`
	Effort           string            `json:"effort,omitempty"`
	Personality      string            `json:"personality,omitempty"`
	ApprovalPolicy   string            `json:"approval_policy,omitempty"`
	Sandbox          string            `json:"sandbox,omitempty"`
	NetworkAccess    bool              `json:"network_access"`
	ThreadID         string            `json:"thread_id,omitempty"`
	Threads          map[string]string `json:"threads,omitempty"`
}

// CodexAdapter implements Agent by launching Codex App Server as the runtime
// behind an Eyrie-owned agent identity.
type CodexAdapter struct {
	id            string
	name          string
	configPath    string
	workspacePath string

	mu  sync.Mutex
	cfg codexConfig
}

// In-flight turns live in a process-wide registry keyed by agent id and
// session, not on the adapter: discovery builds a new CodexAdapter on every
// lookup, so the instance that serves Stop or Interrupt is usually not the
// one that started the turn.
var (
	codexRunsMu sync.Mutex
	codexRuns   = map[codexRunKey]*codexRun{}
)

type codexRunKey struct{ agent, session string }

// codexRun is one in-flight turn: the app-server process serving it and the
// ids turn/interrupt needs. done closes when the stream goroutine exits.
type codexRun struct {
	done      chan struct{}
	startedAt time.Time

	mu       sync.Mutex
	client   *codexRPCClient
	threadID string
	turnID   string
	killFn   func()
	killed   bool // Interrupt had to force-kill; the stream must end in an error
	stopping bool // Interrupt arrived before the process existed
}

func (r *codexRun) setThread(c *codexRPCClient, threadID string) {
	r.mu.Lock()
	r.client, r.threadID = c, threadID
	r.mu.Unlock()
}

// interruptTarget returns what turn/interrupt needs, or ok=false if the turn
// hasn't started far enough to be interrupted politely.
func (r *codexRun) interruptTarget() (c *codexRPCClient, threadID, turnID string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client, r.threadID, r.turnID, r.client != nil && r.turnID != ""
}

func (r *codexRun) setKill(f func()) {
	r.mu.Lock()
	r.killFn = f
	stop := r.stopping
	r.mu.Unlock()
	if stop {
		r.kill()
	}
}

// kill force-ends the run's app server. Before the process exists it marks
// the run so setKill kills it the moment it starts.
func (r *codexRun) kill() {
	r.mu.Lock()
	r.killed = true
	f := r.killFn
	if f == nil {
		r.stopping = true
	}
	r.mu.Unlock()
	if f != nil {
		f()
	}
}

func (r *codexRun) wasKilled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.killed
}

func (r *codexRun) setTurnID(id string) {
	if id == "" {
		return
	}
	r.mu.Lock()
	if r.turnID == "" {
		r.turnID = id
	}
	r.mu.Unlock()
}

func (r *codexRun) currentTurnID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.turnID
}

// How long Interrupt waits for the turn to end after turn/interrupt before it
// kills the app-server process. Variables so tests can shorten them.
var (
	codexInterruptRequestTimeout = 5 * time.Second
	codexInterruptGrace          = 10 * time.Second
)

func NewCodexAdapter(id, name, configPath, workspacePath string) *CodexAdapter {
	a := &CodexAdapter{
		id:            id,
		name:          name,
		configPath:    config.ExpandHome(configPath),
		workspacePath: config.ExpandHome(workspacePath),
	}
	_ = a.loadConfig()
	return a
}

func (a *CodexAdapter) ID() string        { return a.id }
func (a *CodexAdapter) Name() string      { return a.name }
func (a *CodexAdapter) Framework() string { return FrameworkCodex }
func (a *CodexAdapter) BaseURL() string   { return "" }

func (a *CodexAdapter) Health(_ context.Context) (*HealthStatus, error) {
	bin := a.binaryPath()
	if !filepath.IsAbs(bin) {
		resolved := config.LookPathEnriched(bin)
		if resolved != bin {
			return &HealthStatus{Alive: true}, nil
		}
		if _, err := exec.LookPath(bin); err != nil {
			return &HealthStatus{Alive: false}, nil
		}
		return &HealthStatus{Alive: true}, nil
	}
	if filepath.IsAbs(bin) {
		if _, err := os.Stat(bin); err != nil {
			return &HealthStatus{Alive: false}, nil
		}
	}
	return &HealthStatus{Alive: true}, nil
}

func (a *CodexAdapter) Status(_ context.Context) (*AgentStatus, error) {
	cfg := a.currentConfig()
	st := &AgentStatus{
		Provider:       "openai",
		Model:          cfg.Model,
		Channels:       []string{"codex-app-server"},
		ProviderStatus: "ok",
		BusyState:      "idle",
	}
	return st, nil
}

func (a *CodexAdapter) Config(_ context.Context) (*AgentConfig, error) {
	if a.configPath == "" {
		return nil, fmt.Errorf("no config path available")
	}
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		return nil, fmt.Errorf("reading codex config: %w", err)
	}
	return &AgentConfig{Raw: string(data), Format: "json"}, nil
}

func (a *CodexAdapter) Start(context.Context) error { return a.ensureBinary() }

// Stop interrupts every in-flight turn this adapter started, killing any
// app-server process that doesn't end within the grace period. Codex has no
// long-lived process of its own between turns, so there is nothing else to stop.
func (a *CodexAdapter) Stop(ctx context.Context) error {
	codexRunsMu.Lock()
	var keys []string
	for k := range codexRuns {
		if k.agent == a.id {
			keys = append(keys, k.session)
		}
	}
	codexRunsMu.Unlock()
	var errs []error
	for _, k := range keys {
		if err := a.Interrupt(ctx, k); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
func (a *CodexAdapter) Restart(context.Context) error { return a.ensureBinary() }

func (a *CodexAdapter) TailLogs(ctx context.Context) (<-chan LogEntry, error) {
	ch := make(chan LogEntry)
	close(ch)
	return ch, ctx.Err()
}

func (a *CodexAdapter) TailActivity(ctx context.Context) (<-chan ActivityEvent, error) {
	ch := make(chan ActivityEvent)
	close(ch)
	return ch, ctx.Err()
}

func (a *CodexAdapter) Sessions(context.Context) ([]Session, error) {
	cfg := a.currentConfig()
	var sessions []Session
	seen := map[string]bool{}
	add := func(key string) {
		if key == "" {
			key = "default"
		}
		if seen[key] {
			return
		}
		seen[key] = true
		sessions = append(sessions, Session{Key: key, Title: key, Channel: "codex"})
	}
	if cfg.ThreadID != "" {
		add("default")
	}
	for key, threadID := range cfg.Threads {
		if threadID != "" {
			add(key)
		}
	}
	return sessions, nil
}

func (a *CodexAdapter) ChatHistory(context.Context, string, int) ([]ChatMessage, error) {
	return nil, nil
}

func (a *CodexAdapter) SendMessage(ctx context.Context, message, sessionKey string) (*ChatMessage, error) {
	ch, err := a.StreamMessage(ctx, message, sessionKey)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for event := range ch {
		switch event.Type {
		case "delta":
			b.WriteString(event.Content)
		case "done":
			if event.Content != "" {
				return &ChatMessage{Role: "assistant", Content: event.Content, Timestamp: time.Now()}, nil
			}
			return &ChatMessage{Role: "assistant", Content: b.String(), Timestamp: time.Now()}, nil
		case "error":
			return nil, errors.New(event.Error)
		}
	}
	return &ChatMessage{Role: "assistant", Content: b.String(), Timestamp: time.Now()}, nil
}

func (a *CodexAdapter) StreamMessage(ctx context.Context, message, sessionKey string) (<-chan ChatEvent, error) {
	cfg := a.currentConfig()
	if err := a.ensureBinary(); err != nil {
		return nil, err
	}
	if cfg.CWD == "" {
		cfg.CWD = a.workspacePath
	}
	if cfg.CWD == "" {
		cfg.CWD = "."
	}

	// Reserve the session before launching anything, so a second concurrent
	// turn is refused without starting (and leaking) another app server.
	run := &codexRun{done: make(chan struct{}), startedAt: time.Now()}
	if err := a.registerRun(sessionKey, run); err != nil {
		return nil, err
	}
	launched := false
	defer func() {
		if !launched {
			a.unregisterRun(sessionKey, run)
			close(run.done)
		}
	}()

	cmd := exec.CommandContext(ctx, a.commandPath(), "app-server")
	cmd.Dir = cfg.CWD
	if err := a.prepareCodexRuntime(cfg); err != nil {
		return nil, err
	}
	cmd.Env = codexProcessEnv(config.EnrichedEnv(), cfg)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting codex app-server: %w", err)
	}
	run.setKill(func() { _ = cmd.Process.Kill() })

	// abort ends the app server on an early error exit. Wait reaps the child
	// and closes its pipes; Kill alone leaves a zombie per failed attempt.
	abort := func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}

	client := newCodexRPCClient(stdin, stdout)
	go client.readLoop()

	if _, err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "eyrie",
			"version": "0.1",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}); err != nil {
		abort()
		return nil, fmt.Errorf("codex initialize: %w", err)
	}
	_ = client.notify("initialized", map[string]any{})

	threadID, err := a.codexThread(ctx, client, sessionKey, cfg)
	if err != nil {
		abort()
		return nil, err
	}
	run.setThread(client, threadID)
	result, err := client.request(ctx, "turn/start", codexTurnStartParams(threadID, message, cfg))
	if err != nil {
		abort()
		return nil, fmt.Errorf("codex turn/start: %w", err)
	}
	run.setTurnID(codexTurnIDFromResult(result))

	launched = true
	ch := make(chan ChatEvent, 64)
	go func() {
		defer close(run.done)
		defer a.unregisterRun(sessionKey, run)
		a.streamCodexEvents(ctx, cmd, &stderr, client, run, ch)
	}()
	return ch, nil
}

// Interrupt cancels the session's in-flight turn: it sends turn/interrupt,
// waits up to codexInterruptGrace for the turn to end, and kills the
// app-server process if it hasn't. No active turn is not an error.
func (a *CodexAdapter) Interrupt(ctx context.Context, sessionKey string) error {
	run := a.activeRun(sessionKey)
	if run == nil {
		return nil
	}
	var reqErr error
	if client, threadID, turnID, ok := run.interruptTarget(); ok {
		rctx, cancel := context.WithTimeout(ctx, codexInterruptRequestTimeout)
		_, reqErr = client.request(rctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID})
		cancel()
	} else {
		reqErr = errors.New("turn id not known yet")
	}
	wait := codexInterruptGrace
	if reqErr != nil {
		// The app server didn't take the interrupt; don't wait for a turn end
		// that may never come.
		wait = 0
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-run.done:
		return nil
	case <-timer.C:
	case <-ctx.Done():
	}
	run.kill()
	select {
	case <-run.done:
	case <-time.After(codexInterruptGrace):
		return fmt.Errorf("codex session %q: process killed but stream did not end", codexSessionKey(sessionKey))
	}
	return nil
}

func codexSessionKey(k string) string {
	if k == "" {
		return "default"
	}
	return k
}

func (a *CodexAdapter) runKey(sessionKey string) codexRunKey {
	return codexRunKey{agent: a.id, session: codexSessionKey(sessionKey)}
}

func (a *CodexAdapter) registerRun(sessionKey string, run *codexRun) error {
	k := a.runKey(sessionKey)
	codexRunsMu.Lock()
	defer codexRunsMu.Unlock()
	if codexRuns[k] != nil {
		return fmt.Errorf("codex session %q already has a turn in flight", k.session)
	}
	codexRuns[k] = run
	return nil
}

func (a *CodexAdapter) unregisterRun(sessionKey string, run *codexRun) {
	k := a.runKey(sessionKey)
	codexRunsMu.Lock()
	defer codexRunsMu.Unlock()
	if codexRuns[k] == run {
		delete(codexRuns, k)
	}
}

func (a *CodexAdapter) activeRun(sessionKey string) *codexRun {
	codexRunsMu.Lock()
	defer codexRunsMu.Unlock()
	return codexRuns[a.runKey(sessionKey)]
}

func (a *CodexAdapter) CreateSession(_ context.Context, name string) (*Session, error) {
	key := strings.TrimSpace(name)
	if key == "" {
		key = "codex-" + uuid.NewString()
	}
	return &Session{Key: key, Title: key, Channel: "codex"}, nil
}

func (a *CodexAdapter) ResetSession(_ context.Context, sessionKey string) error {
	return a.updateConfig(func(cfg *codexConfig) {
		if sessionKey == "" || sessionKey == "default" {
			cfg.ThreadID = ""
		}
		if cfg.Threads != nil {
			delete(cfg.Threads, sessionKey)
		}
	})
}

func (a *CodexAdapter) DeleteSession(ctx context.Context, sessionKey string) error {
	return a.ResetSession(ctx, sessionKey)
}

func (a *CodexAdapter) Personality(context.Context) (*Personality, error) {
	files := map[string]string{}
	for _, name := range identityFiles {
		path := filepath.Join(a.workspacePath, name)
		if data, err := os.ReadFile(path); err == nil {
			files[name] = string(data)
		}
	}
	return &Personality{Name: a.name, IdentityFiles: files}, nil
}

func (a *CodexAdapter) Capabilities() AgentCapabilities {
	return AgentCapabilities{CommanderCapable: true}
}

func (a *CodexAdapter) binaryPath() string {
	cfg := a.currentConfig()
	if cfg.BinaryPath != "" {
		return config.ExpandHome(cfg.BinaryPath)
	}
	return "codex"
}

func (a *CodexAdapter) ensureBinary() error {
	bin := a.binaryPath()
	if filepath.IsAbs(bin) {
		if _, err := os.Stat(bin); err != nil {
			return fmt.Errorf("codex binary not found at %s: %w", bin, err)
		}
		return nil
	}
	if config.LookPathEnriched(bin) == bin {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("codex binary not found in PATH: %w", err)
		}
	}
	return nil
}

func (a *CodexAdapter) commandPath() string {
	bin := a.binaryPath()
	if filepath.IsAbs(bin) {
		return bin
	}
	resolved := config.LookPathEnriched(bin)
	if resolved != bin {
		return resolved
	}
	return bin
}

func (a *CodexAdapter) currentConfig() codexConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.cfg
	if cfg.Threads != nil {
		cfg.Threads = cloneStringMap(cfg.Threads)
	}
	applyCodexConfigDefaults(&cfg, a.workspacePath, a.configPath)
	return cfg
}

func (a *CodexAdapter) loadConfig() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loadConfigLocked()
}

func (a *CodexAdapter) loadConfigLocked() error {
	if a.configPath == "" {
		applyCodexConfigDefaults(&a.cfg, a.workspacePath, a.configPath)
		return nil
	}
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		applyCodexConfigDefaults(&a.cfg, a.workspacePath, a.configPath)
		return err
	}
	var cfg codexConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		applyCodexConfigDefaults(&a.cfg, a.workspacePath, a.configPath)
		return err
	}
	applyCodexConfigDefaults(&cfg, a.workspacePath, a.configPath)
	a.cfg = cfg
	return nil
}

func (a *CodexAdapter) updateConfig(mut func(*codexConfig)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.loadConfigLocked(); err != nil && !os.IsNotExist(err) {
		return err
	}
	mut(&a.cfg)
	applyCodexConfigDefaults(&a.cfg, a.workspacePath, a.configPath)
	if a.configPath == "" {
		return nil
	}
	data, err := json.MarshalIndent(a.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.configPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.configPath, append(data, '\n'), 0o600)
}

func (a *CodexAdapter) codexThread(ctx context.Context, client *codexRPCClient, sessionKey string, cfg codexConfig) (string, error) {
	if sessionKey == "" {
		sessionKey = "default"
	}
	var threadID string
	if sessionKey == "default" {
		threadID = cfg.ThreadID
	}
	if cfg.Threads != nil && cfg.Threads[sessionKey] != "" {
		threadID = cfg.Threads[sessionKey]
	}
	if threadID != "" {
		// A saved thread that can't be resumed is an error, not a cue to start
		// over: silently minting a new thread drops the conversation's context
		// and leaves the caller believing it continued. Starting fresh is an
		// explicit choice, made through ResetSession.
		if _, err := client.request(ctx, "thread/resume", map[string]any{"threadId": threadID}); err != nil {
			return "", &CodexResumeError{SessionKey: sessionKey, ThreadID: threadID, Err: err}
		}
		return threadID, nil
	}

	result, err := client.request(ctx, "thread/start", codexThreadStartParams(cfg))
	if err != nil {
		return "", fmt.Errorf("codex thread/start: %w", err)
	}
	threadID = codexThreadIDFromResult(result)
	if threadID == "" {
		return "", fmt.Errorf("codex thread/start response did not include thread id")
	}
	_ = a.updateConfig(func(next *codexConfig) {
		if next.Threads == nil {
			next.Threads = map[string]string{}
		}
		next.Threads[sessionKey] = threadID
		if sessionKey == "default" {
			next.ThreadID = threadID
		}
	})
	return threadID, nil
}

// CodexResumeError reports that a session's saved Codex thread could not be
// resumed. The saved mapping is left in place; ResetSession clears it so the
// next message starts a new thread.
type CodexResumeError struct {
	SessionKey string
	ThreadID   string
	Err        error
}

func (e *CodexResumeError) Error() string {
	return fmt.Sprintf("codex thread/resume failed for session %q (thread %s): %v; reset the session to start a new thread", e.SessionKey, e.ThreadID, e.Err)
}

func (e *CodexResumeError) Unwrap() error { return e.Err }

func (a *CodexAdapter) streamCodexEvents(ctx context.Context, cmd *exec.Cmd, stderr *bytes.Buffer, client *codexRPCClient, run *codexRun, ch chan<- ChatEvent) {
	defer close(ch)
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	runCodexEventLoop(ctx, stderr, client, run, ch)
}

func runCodexEventLoop(ctx context.Context, stderr *bytes.Buffer, client *codexRPCClient, run *codexRun, ch chan<- ChatEvent) {
	// A stream that ends because Interrupt killed the app server must not
	// look like a finished reply to SendMessage.
	sentTerminal := false
	defer func() {
		if !sentTerminal && run != nil && run.wasKilled() {
			ch <- ChatEvent{Type: "error", Error: "Codex turn interrupted"}
		}
	}()

	var full strings.Builder
	var inputTokens, outputTokens int
	for {
		select {
		case <-ctx.Done():
			sentTerminal = true
			ch <- ChatEvent{Type: "error", Error: ctx.Err().Error()}
			return
		case req, ok := <-client.requests:
			if !ok {
				if stderr.Len() > 0 {
					sentTerminal = true
					ch <- ChatEvent{Type: "error", Error: strings.TrimSpace(stderr.String())}
				}
				return
			}
			event, response := codexChatEventFromServerRequest(req.Method, req.Params)
			if event.Type != "" {
				ch <- event
			}
			_ = client.respond(req.ID, response)
		case note, ok := <-client.notifications:
			if !ok {
				return
			}
			if note.Method == "thread/tokenUsage/updated" {
				if input, output := codexUsageFromParams(note.Params); input > 0 || output > 0 {
					inputTokens = input
					outputTokens = output
				}
				continue
			}
			if note.Method == "turn/started" && run != nil {
				run.setTurnID(codexTurnIDFromResult(note.Params))
				continue
			}
			if note.Method == "turn/completed" {
				if errMsg := codexTurnError(note.Params); errMsg != "" {
					sentTerminal = true
					ch <- ChatEvent{Type: "error", Error: errMsg}
					return
				}
				sentTerminal = true
				ch <- ChatEvent{Type: "done", Content: full.String(), InputTokens: inputTokens, OutputTokens: outputTokens}
				return
			}
			event, ok := codexChatEventFromNotification(note.Method, note.Params)
			if !ok {
				continue
			}
			if event.Type == "delta" {
				full.WriteString(event.Content)
			}
			ch <- event
		}
	}
}

func applyCodexConfigDefaults(cfg *codexConfig, workspacePath, configPath string) {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "codex"
	}
	if cfg.CWD == "" {
		cfg.CWD = workspacePath
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.4"
	}
	if cfg.Effort == "" {
		cfg.Effort = "medium"
	}
	if cfg.ApprovalPolicy == "" {
		cfg.ApprovalPolicy = "untrusted"
	}
	if cfg.Sandbox == "" {
		cfg.Sandbox = "workspaceWrite"
	}
	if cfg.CodexHome == "" {
		if configPath != "" {
			cfg.CodexHome = filepath.Join(filepath.Dir(configPath), "codex-home")
		} else if workspacePath != "" {
			cfg.CodexHome = filepath.Join(filepath.Dir(workspacePath), "codex-home")
		}
	}
	if cfg.InstructionsPath == "" && workspacePath != "" {
		cfg.InstructionsPath = filepath.Join(workspacePath, "AGENTS.md")
	}
}

func (a *CodexAdapter) prepareCodexRuntime(cfg codexConfig) error {
	if cfg.CodexHome == "" {
		return nil
	}
	if err := os.MkdirAll(cfg.CodexHome, 0o700); err != nil {
		return fmt.Errorf("creating codex home %s: %w", cfg.CodexHome, err)
	}
	if err := seedCodexAuth(cfg.CodexHome); err != nil {
		return fmt.Errorf("seeding codex auth: %w", err)
	}
	return nil
}

func codexProcessEnv(base []string, cfg codexConfig) []string {
	if cfg.CodexHome == "" {
		return base
	}
	entry := "CODEX_HOME=" + config.ExpandHome(cfg.CodexHome)
	out := make([]string, 0, len(base)+1)
	replaced := false
	for _, item := range base {
		if strings.HasPrefix(item, "CODEX_HOME=") {
			if !replaced {
				out = append(out, entry)
				replaced = true
			}
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, entry)
	}
	return out
}

func seedCodexAuth(codexHome string) error {
	sourceHome := sourceCodexHome()
	if sourceHome == "" || sourceHome == codexHome {
		return nil
	}
	return copyFileIfMissing(
		filepath.Join(sourceHome, "auth.json"),
		filepath.Join(codexHome, "auth.json"),
		0o600,
	)
}

func sourceCodexHome() string {
	if env := os.Getenv("CODEX_HOME"); env != "" {
		return config.ExpandHome(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func copyFileIfMissing(src, dst string, perm os.FileMode) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type codexRPCClient struct {
	in            io.WriteCloser
	out           io.Reader
	writeMu       sync.Mutex
	nextID        int
	pendingMu     sync.Mutex
	pending       map[string]chan codexRPCMessage
	closed        bool // output ended; guarded by pendingMu
	notifications chan codexRPCMessage
	requests      chan codexRPCMessage
}

type codexRPCMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

func newCodexRPCClient(in io.WriteCloser, out io.Reader) *codexRPCClient {
	return &codexRPCClient{
		in:            in,
		out:           out,
		pending:       map[string]chan codexRPCMessage{},
		notifications: make(chan codexRPCMessage, 128),
		requests:      make(chan codexRPCMessage, 16),
	}
}

func (c *codexRPCClient) readLoop() {
	defer close(c.notifications)
	defer close(c.requests)
	// When the app server's output ends (exit or kill), no response will ever
	// arrive: fail every pending request instead of leaving it to its context.
	defer c.failPending()
	scanner := bufio.NewScanner(c.out)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg codexRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if len(msg.ID) > 0 && (len(msg.Result) > 0 || msg.Error != nil) {
			c.pendingMu.Lock()
			ch := c.pending[string(msg.ID)]
			delete(c.pending, string(msg.ID))
			c.pendingMu.Unlock()
			if ch != nil {
				ch <- msg
				close(ch)
			}
			continue
		}
		if len(msg.ID) > 0 && msg.Method != "" {
			c.requests <- msg
			continue
		}
		if msg.Method != "" {
			c.notifications <- msg
		}
	}
}

func (c *codexRPCClient) failPending() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.closed = true
	for k, ch := range c.pending {
		close(ch)
		delete(c.pending, k)
	}
}

func (c *codexRPCClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.writeMu.Lock()
	c.nextID++
	id := c.nextID
	key := strconv.Itoa(id)
	ch := make(chan codexRPCMessage, 1)
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		c.writeMu.Unlock()
		return nil, io.ErrUnexpectedEOF
	}
	c.pending[key] = ch
	c.pendingMu.Unlock()
	err := c.writeLocked(map[string]any{"id": id, "method": method, "params": params})
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-ch:
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		if msg.Error != nil {
			return nil, errors.New(msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func (c *codexRPCClient) notify(method string, params any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeLocked(map[string]any{"method": method, "params": params})
}

func (c *codexRPCClient) respond(id json.RawMessage, result any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeLocked(map[string]any{"id": json.RawMessage(id), "result": result})
}

func (c *codexRPCClient) writeLocked(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = c.in.Write(data)
	return err
}

func codexThreadStartParams(cfg codexConfig) map[string]any {
	params := map[string]any{}
	if cfg.CWD != "" {
		params["cwd"] = cfg.CWD
	}
	if cfg.Model != "" {
		params["model"] = cfg.Model
	}
	if cfg.Personality != "" {
		params["personality"] = cfg.Personality
	}
	if cfg.ApprovalPolicy != "" {
		params["approvalPolicy"] = cfg.ApprovalPolicy
	}
	if cfg.Sandbox != "" {
		params["sandbox"] = codexThreadSandboxMode(cfg.Sandbox)
	}
	return params
}

func codexThreadSandboxMode(sandbox string) string {
	switch sandbox {
	case "workspaceWrite":
		return "workspace-write"
	case "readOnly":
		return "read-only"
	case "dangerFullAccess":
		return "danger-full-access"
	default:
		return sandbox
	}
}

func codexTurnStartParams(threadID, message string, cfg codexConfig) map[string]any {
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]string{
			{"type": "text", "text": message},
		},
	}
	if cfg.CWD != "" {
		params["cwd"] = cfg.CWD
	}
	if cfg.Model != "" {
		params["model"] = cfg.Model
	}
	if cfg.Effort != "" {
		params["effort"] = cfg.Effort
	}
	if cfg.Personality != "" {
		params["personality"] = cfg.Personality
	}
	if cfg.ApprovalPolicy != "" {
		params["approvalPolicy"] = cfg.ApprovalPolicy
	}
	if policy := codexSandboxPolicy(cfg); policy != nil {
		params["sandboxPolicy"] = policy
	}
	return params
}

func codexSandboxPolicy(cfg codexConfig) map[string]any {
	switch cfg.Sandbox {
	case "workspaceWrite":
		policy := map[string]any{
			"type":          "workspaceWrite",
			"networkAccess": cfg.NetworkAccess,
		}
		if cfg.CWD != "" {
			policy["writableRoots"] = []string{cfg.CWD}
		}
		return policy
	case "readOnly":
		return map[string]any{"type": "readOnly"}
	case "":
		return nil
	default:
		return map[string]any{"type": cfg.Sandbox}
	}
}

func codexThreadIDFromResult(result json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"threadId", "id"} {
		if s, _ := payload[key].(string); s != "" {
			return s
		}
	}
	if thread, _ := payload["thread"].(map[string]any); thread != nil {
		if s, _ := thread["id"].(string); s != "" {
			return s
		}
	}
	return ""
}

func codexChatEventFromNotification(method string, params json.RawMessage) (ChatEvent, bool) {
	switch method {
	case "error":
		if msg := codexErrorMessage(params); msg != "" {
			return ChatEvent{Type: "error", Error: msg}, true
		}
	case "item/agentMessage/delta":
		if delta := codexString(params, "delta", "text", "content"); delta != "" {
			return ChatEvent{Type: "delta", Content: delta}, true
		}
	case "item/started":
		item := codexItem(params)
		itemType := codexMapString(item, "type")
		if codexVisibleItemType(itemType) {
			return ChatEvent{
				Type:   "tool_start",
				Tool:   itemType,
				ToolID: codexMapString(item, "id"),
				Args:   codexToolArgs(item),
			}, true
		}
	case "item/completed":
		item := codexItem(params)
		itemType := codexMapString(item, "type")
		if codexVisibleItemType(itemType) {
			success := codexItemSuccess(item)
			return ChatEvent{
				Type:    "tool_result",
				Tool:    itemType,
				ToolID:  codexMapString(item, "id"),
				Output:  codexItemOutput(item),
				Success: &success,
			}, true
		}
	}
	return ChatEvent{}, false
}

func codexVisibleItemType(itemType string) bool {
	if itemType == "" {
		return false
	}
	switch itemType {
	case "commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall", "collabToolCall", "webSearch", "imageView", "plan", "reasoning", "enteredReviewMode", "exitedReviewMode", "contextCompaction":
		return true
	default:
		return strings.Contains(strings.ToLower(itemType), "tool")
	}
}

func codexItemSuccess(item map[string]any) bool {
	switch strings.ToLower(codexMapString(item, "status")) {
	case "failed", "error", "declined", "cancelled", "canceled":
		return false
	default:
		return true
	}
}

func codexChatEventFromServerRequest(method string, params json.RawMessage) (ChatEvent, map[string]any) {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return ChatEvent{
			Type:  "error",
			Error: "Codex requested approval; Eyrie declined automatically because this runtime slice does not auto-approve actions.",
		}, map[string]any{"decision": "decline"}
	case "item/permissions/requestApproval":
		return ChatEvent{
			Type:  "error",
			Error: "Codex requested additional permissions; Eyrie returned an empty grant because this runtime slice does not auto-approve actions.",
		}, map[string]any{"permissions": map[string]any{}, "scope": "turn"}
	case "item/tool/requestUserInput":
		return ChatEvent{
			Type:  "error",
			Error: "Codex requested user input; Eyrie returned no answers because interactive tool prompts are not wired in this runtime slice.",
		}, map[string]any{"answers": map[string]any{}}
	case "execCommandApproval", "applyPatchApproval":
		return ChatEvent{
			Type:  "error",
			Error: "Codex requested approval; Eyrie denied automatically because this runtime slice does not auto-approve actions.",
		}, map[string]any{"decision": "denied"}
	default:
		return ChatEvent{}, map[string]any{}
	}
}

// codexTurnStatusIsFailure: the app-server protocol's TurnStatus is
// completed | interrupted | failed | inProgress. "cancelled" is kept for older
// servers. An interrupted turn must not be reported as done.
func codexTurnStatusIsFailure(status string) bool {
	return status == "failed" || status == "interrupted" || status == "cancelled"
}

// codexTurnIDFromResult reads turn.id from a turn/start response or a
// turn/started notification.
func codexTurnIDFromResult(raw json.RawMessage) string {
	var payload struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
		TurnID string `json:"turnId"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if payload.Turn.ID != "" {
		return payload.Turn.ID
	}
	return payload.TurnID
}

func codexTurnError(params json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return ""
	}
	if turn, _ := payload["turn"].(map[string]any); turn != nil {
		if errMap, _ := turn["error"].(map[string]any); errMap != nil {
			if msg, _ := errMap["message"].(string); msg != "" {
				return msg
			}
		}
		if status, _ := turn["status"].(string); codexTurnStatusIsFailure(status) {
			return "Codex turn " + status
		}
	}
	if status, _ := payload["status"].(string); codexTurnStatusIsFailure(status) {
		return "Codex turn " + status
	}
	return ""
}

func codexItem(params json.RawMessage) map[string]any {
	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil
	}
	if item, _ := payload["item"].(map[string]any); item != nil {
		return item
	}
	return payload
}

func codexToolArgs(item map[string]any) map[string]any {
	args := map[string]any{}
	for _, key := range []string{"command", "cwd", "tool", "arguments", "query", "action", "path", "status", "changes"} {
		if val, ok := item[key]; ok {
			args[key] = val
		}
	}
	return args
}

func codexItemOutput(item map[string]any) string {
	if output := codexFileChangesOutput(item); output != "" {
		return output
	}
	for _, key := range []string{"aggregatedOutput", "output", "content", "text", "review", "summary"} {
		if s, _ := item[key].(string); s != "" {
			return s
		}
	}
	if contentItems, ok := item["contentItems"]; ok {
		if data, err := json.MarshalIndent(contentItems, "", "  "); err == nil {
			return string(data)
		}
	}
	return ""
}

func codexFileChangesOutput(item map[string]any) string {
	changes, _ := item["changes"].([]any)
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	for i, raw := range changes {
		change, _ := raw.(map[string]any)
		if change == nil {
			continue
		}
		if i > 0 && b.Len() > 0 {
			b.WriteString("\n\n")
		}
		path := codexMapString(change, "path")
		kind := codexMapString(change, "kind")
		switch {
		case path != "" && kind != "":
			b.WriteString(path)
			b.WriteByte(' ')
			b.WriteString(kind)
		case path != "":
			b.WriteString(path)
		case kind != "":
			b.WriteString(kind)
		}
		if diff := codexMapString(change, "diff"); diff != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(diff)
		}
	}
	return b.String()
}

func codexString(raw json.RawMessage, keys ...string) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	for _, key := range keys {
		if s, _ := payload[key].(string); s != "" {
			return s
		}
	}
	if item, _ := payload["item"].(map[string]any); item != nil {
		for _, key := range keys {
			if s, _ := item[key].(string); s != "" {
				return s
			}
		}
	}
	return ""
}

func codexErrorMessage(raw json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if errMap, _ := payload["error"].(map[string]any); errMap != nil {
		if msg, _ := errMap["message"].(string); msg != "" {
			return msg
		}
	}
	if msg, _ := payload["message"].(string); msg != "" {
		return msg
	}
	return ""
}

func codexUsageFromParams(raw json.RawMessage) (int, int) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, 0
	}
	for _, candidate := range []map[string]any{
		payload,
		codexMap(payload, "usage"),
		codexMap(payload, "tokenUsage"),
		codexMap(payload, "tokens"),
	} {
		if candidate == nil {
			continue
		}
		input := codexMapInt(candidate, "inputTokens", "input_tokens", "input")
		output := codexMapInt(candidate, "outputTokens", "output_tokens", "output")
		if input > 0 || output > 0 {
			return input, output
		}
	}
	return 0, 0
}

func codexMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	child, _ := m[key].(map[string]any)
	return child
}

func codexMapInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return int(n)
			}
		}
	}
	return 0
}

func codexMapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}
