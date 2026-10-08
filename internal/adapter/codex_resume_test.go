package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeCodexServer answers codexRPCClient requests over in-memory pipes.
// resumeErr non-empty makes thread/resume fail with that message.
type fakeCodexServer struct {
	resumeErr string
	mu        sync.Mutex
	methods   []string
}

func (f *fakeCodexServer) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.methods...)
}

func startFakeCodex(t *testing.T, f *fakeCodexServer) *codexRPCClient {
	t.Helper()
	clientToServerR, clientToServerW := io.Pipe()
	serverToClientR, serverToClientW := io.Pipe()
	t.Cleanup(func() {
		_ = clientToServerW.Close()
		_ = serverToClientW.Close()
	})
	go func() {
		sc := bufio.NewScanner(clientToServerR)
		enc := json.NewEncoder(serverToClientW)
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.ID) == 0 {
				continue
			}
			f.mu.Lock()
			f.methods = append(f.methods, req.Method)
			f.mu.Unlock()
			switch {
			case req.Method == "thread/resume" && f.resumeErr != "":
				_ = enc.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32000, "message": f.resumeErr}})
			case req.Method == "thread/start":
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{"thread": map[string]any{"id": "thr_new"}}})
			default:
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
			}
		}
	}()
	c := newCodexRPCClient(clientToServerW, serverToClientR)
	go c.readLoop()
	return c
}

func newTestCodexAdapter(t *testing.T, cfg codexConfig) *CodexAdapter {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return NewCodexAdapter("codex-test", "Codex Test", path, filepath.Join(dir, "workspace"))
}

func TestCodexThreadFailedResumeIsAnErrorNotANewThread(t *testing.T) {
	a := newTestCodexAdapter(t, codexConfig{Threads: map[string]string{"review": "thr_saved"}})
	f := &fakeCodexServer{resumeErr: "thread not found"}
	c := startFakeCodex(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := a.codexThread(ctx, c, "review", a.currentConfig())
	if err == nil {
		t.Fatalf("codexThread returned thread %q with no error; a failed resume must not fall through to a new thread", got)
	}
	var re *CodexResumeError
	if !errors.As(err, &re) {
		t.Fatalf("error = %v (%T), want *CodexResumeError", err, err)
	}
	if re.SessionKey != "review" || re.ThreadID != "thr_saved" {
		t.Fatalf("resume error fields = %+v", re)
	}
	for _, m := range f.seen() {
		if m == "thread/start" {
			t.Fatalf("thread/start was called after a failed resume: %v", f.seen())
		}
	}
	if a.currentConfig().Threads["review"] != "thr_saved" {
		t.Fatalf("saved thread mapping changed: %v", a.currentConfig().Threads)
	}
}

func TestCodexThreadFailedResumeOnDefaultSessionKeepsThreadID(t *testing.T) {
	a := newTestCodexAdapter(t, codexConfig{ThreadID: "thr_default"})
	c := startFakeCodex(t, &fakeCodexServer{resumeErr: "gone"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := a.codexThread(ctx, c, "", a.currentConfig()); err == nil {
		t.Fatal("expected a resume error for the default session")
	}
	if a.currentConfig().ThreadID != "thr_default" {
		t.Fatalf("default ThreadID overwritten: %q", a.currentConfig().ThreadID)
	}
}

func TestCodexThreadResumeSuccessReusesSavedThread(t *testing.T) {
	a := newTestCodexAdapter(t, codexConfig{Threads: map[string]string{"review": "thr_saved"}})
	f := &fakeCodexServer{}
	c := startFakeCodex(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := a.codexThread(ctx, c, "review", a.currentConfig())
	if err != nil || got != "thr_saved" {
		t.Fatalf("codexThread = %q, %v; want thr_saved", got, err)
	}
	if s := f.seen(); len(s) != 1 || s[0] != "thread/resume" {
		t.Fatalf("methods = %v, want only thread/resume", s)
	}
}

func TestCodexThreadAfterResetStartsAndSavesNewThread(t *testing.T) {
	a := newTestCodexAdapter(t, codexConfig{Threads: map[string]string{"review": "thr_saved"}})
	if err := a.ResetSession(context.Background(), "review"); err != nil {
		t.Fatal(err)
	}
	f := &fakeCodexServer{resumeErr: "should not be asked"}
	c := startFakeCodex(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := a.codexThread(ctx, c, "review", a.currentConfig())
	if err != nil || got != "thr_new" {
		t.Fatalf("codexThread = %q, %v; want thr_new", got, err)
	}
	if s := f.seen(); len(s) != 1 || s[0] != "thread/start" {
		t.Fatalf("methods = %v, want only thread/start", s)
	}
	if a.currentConfig().Threads["review"] != "thr_new" {
		t.Fatalf("new thread not saved: %v", a.currentConfig().Threads)
	}
}

// TestMain lets the test binary stand in for `codex app-server`: when
// EYRIE_FAKE_CODEX_RESUME_FAIL is set it serves a minimal app server whose
// thread/resume always fails, after writing its pid to that path.
func TestMain(m *testing.M) {
	if os.Getenv("EYRIE_FAKE_CODEX_EOF_MIDTURN") != "" {
		// Answers setup, starts the turn, sends one delta, then exits with
		// no turn/completed and nothing on stderr.
		sc := bufio.NewScanner(os.Stdin)
		enc := json.NewEncoder(os.Stdout)
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.ID) == 0 {
				continue
			}
			switch req.Method {
			case "thread/start":
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{"thread": map[string]any{"id": "thr_eof"}}})
			case "turn/start":
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{"turn": map[string]any{"id": "turn_eof"}}})
				_ = enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "half an answ"}})
				os.Exit(0)
			default:
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
			}
		}
		os.Exit(0)
	}
	if pidFile := os.Getenv("EYRIE_FAKE_CODEX_CHILD"); pidFile != "" {
		// Spawns a long-lived grandchild (as a Codex tool call would), writes
		// its pid, then hangs on turn/start like the HANG_TURN server. With
		// EYRIE_FAKE_CODEX_CHILD_SETSID the grandchild leaves the process
		// group by starting its own session, which a group kill misses.
		child := exec.Command("sleep", "300")
		if os.Getenv("EYRIE_FAKE_CODEX_CHILD_SETSID") != "" {
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		}
		if err := child.Start(); err == nil {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600)
		}
		os.Setenv("EYRIE_FAKE_CODEX_HANG_TURN", pidFile+".server")
	}
	if pidFile := os.Getenv("EYRIE_FAKE_CODEX_HANG_TURN"); pidFile != "" {
		// Answers everything except turn/start, which it never answers.
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
		sc := bufio.NewScanner(os.Stdin)
		enc := json.NewEncoder(os.Stdout)
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.ID) == 0 {
				continue
			}
			if req.Method == "turn/start" {
				// EYRIE_FAKE_CODEX_FLOOD: flood notifications past the client's
				// 128-slot buffer before (never) answering, so its read loop
				// is stuck on a full channel during startup.
				if os.Getenv("EYRIE_FAKE_CODEX_FLOOD") != "" {
					for i := 0; i < 300; i++ {
						_ = enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "x"}})
					}
				}
				continue
			}
			if req.Method == "thread/start" {
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{"thread": map[string]any{"id": "thr_hang"}}})
				continue
			}
			_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
		}
		os.Exit(0)
	}
	if pidFile := os.Getenv("EYRIE_FAKE_CODEX_RESUME_FAIL"); pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
		sc := bufio.NewScanner(os.Stdin)
		enc := json.NewEncoder(os.Stdout)
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.ID) == 0 {
				continue
			}
			if req.Method == "thread/resume" {
				_ = enc.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32000, "message": "thread not found"}})
				continue
			}
			_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCodexStreamMessageFailedResumeReapsAppServer(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "fake.pid")
	t.Setenv("EYRIE_FAKE_CODEX_RESUME_FAIL", pidFile)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "source-codex-home")) // keep seedCodexAuth off ~/.codex
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a := newTestCodexAdapter(t, codexConfig{BinaryPath: self, CWD: dir, Threads: map[string]string{"review": "thr_saved"}})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := a.StreamMessage(ctx, "hello", "review")
	if err == nil {
		for range ch {
		}
		t.Fatal("StreamMessage succeeded after a failed resume")
	}
	var re *CodexResumeError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want *CodexResumeError", err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("fake app server never ran: %v", err)
	}
	pid, _ := strconv.Atoi(string(raw))
	// A killed-but-unwaited child is a zombie and still answers signal 0.
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("app server pid %d not reaped after StreamMessage returned (kill 0: %v)", pid, err)
	}
}

func TestCodexInterruptDuringTurnStartUnblocksAndReaps(t *testing.T) {
	testCodexInterruptDuringTurnStart(t, false)
}

// Review r5 P2: with the notification buffer full during startup, readLoop
// is blocked, so a kill alone never fails the pending turn/start. The kill
// path must shut the client down too.
func TestCodexInterruptDuringTurnStartWithFullBuffer(t *testing.T) {
	testCodexInterruptDuringTurnStart(t, true)
}

func testCodexInterruptDuringTurnStart(t *testing.T, flood bool) {
	if flood {
		t.Setenv("EYRIE_FAKE_CODEX_FLOOD", "1")
	}
	shortInterruptTimers(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "hang.pid")
	t.Setenv("EYRIE_FAKE_CODEX_HANG_TURN", pidFile)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "src"))
	self, _ := os.Executable()
	a := newTestCodexAdapter(t, codexConfig{BinaryPath: self, CWD: dir})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type res struct {
		ch  <-chan ChatEvent
		err error
	}
	out := make(chan res, 1)
	go func() {
		ch, err := a.StreamMessage(ctx, "hi", "s")
		out <- res{ch, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil && a.activeRun("s") != nil && runHasClient(a.activeRun("s")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake app server never reached turn/start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // let turn/start go out

	if err := a.Interrupt(context.Background(), "s"); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	select {
	case r := <-out:
		if r.err == nil {
			t.Fatal("StreamMessage succeeded after being interrupted during turn/start")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("StreamMessage still blocked on turn/start after the app server was killed")
	}
	if a.activeRun("s") != nil {
		t.Fatal("session still marked busy after interrupted startup")
	}
	raw, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(string(raw))
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("app server %d not reaped: %v", pid, err)
	}
}

func TestCodexConcurrentTurnRefusedBeforeLaunch(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "should-not-exist.pid")
	t.Setenv("EYRIE_FAKE_CODEX_RESUME_FAIL", pidFile)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "src"))
	self, _ := os.Executable()
	a := newTestCodexAdapter(t, codexConfig{BinaryPath: self, CWD: dir})
	busy := &codexRun{done: make(chan struct{})}
	if err := a.registerRun("s", busy); err != nil {
		t.Fatal(err)
	}
	defer a.unregisterRun("s", busy)

	if _, err := a.StreamMessage(context.Background(), "hi", "s"); err == nil {
		t.Fatal("second concurrent turn accepted")
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(pidFile); err == nil {
		t.Fatal("an app server was launched for a turn that was refused")
	}
	if a.activeRun("s") != busy {
		t.Fatal("refused turn disturbed the running turn's registration")
	}
}

func runHasClient(r *codexRun) bool {
	c, _, _, _ := r.interruptTarget()
	return c != nil
}

func testCodexKillReachesTool(t *testing.T, setsid bool) {
	if setsid {
		t.Setenv("EYRIE_FAKE_CODEX_CHILD_SETSID", "1")
	}
	shortInterruptTimers(t)
	dir := t.TempDir()
	childPid := filepath.Join(dir, "child.pid")
	t.Setenv("EYRIE_FAKE_CODEX_CHILD", childPid)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "src"))
	self, _ := os.Executable()
	a := newTestCodexAdapter(t, codexConfig{BinaryPath: self, CWD: dir})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go func() { _, _ = a.StreamMessage(ctx, "hi", "s") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(childPid); err == nil && a.activeRun("s") != nil && runHasClient(a.activeRun("s")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake app server never spawned its child")
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := os.ReadFile(childPid)
	pid, _ := strconv.Atoi(string(raw))
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	if err := a.Interrupt(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	gone := false
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			gone = true
			break
		}
		// The grandchild is reparented to init once the app server dies;
		// init reaps it, so ESRCH arrives shortly after the kill.
		time.Sleep(20 * time.Millisecond)
	}
	if !gone {
		t.Fatalf("tool process %d survived Stop", pid)
	}
}

func TestCodexKillReachesSpawnedToolProcesses(t *testing.T) { testCodexKillReachesTool(t, false) }

// Review finding: a tool that calls setsid leaves the process group, and a
// group kill alone misses it.
func TestCodexKillReachesToolThatLeftTheGroup(t *testing.T) { testCodexKillReachesTool(t, true) }

func TestCodexSendMessageFailsWhenServerExitsMidTurn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EYRIE_FAKE_CODEX_EOF_MIDTURN", "1")
	t.Setenv("CODEX_HOME", filepath.Join(dir, "src"))
	self, _ := os.Executable()
	a := newTestCodexAdapter(t, codexConfig{BinaryPath: self, CWD: dir})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msg, err := a.SendMessage(ctx, "hi", "s")
	if err == nil {
		t.Fatalf("SendMessage returned a partial reply as success: %q", msg.Content)
	}
}

func TestCodexReadLoopUnblocksWhenNobodyReads(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer inW.Close()
	go func() { _, _ = io.Copy(io.Discard, inR) }() // the "server" reads requests and never answers
	c := newCodexRPCClient(inW, outR)
	done := make(chan struct{})
	go func() { c.readLoop(); close(done) }()
	go func() { // flood well past the 128-entry notification buffer
		enc := json.NewEncoder(outW)
		for i := 0; i < 400; i++ {
			if enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "x"}}) != nil {
				return
			}
		}
	}()
	time.Sleep(100 * time.Millisecond) // buffer full, readLoop blocked on send
	// A request waiting on a response must be released once we shut down.
	reqDone := make(chan error, 1)
	go func() {
		_, err := c.request(context.Background(), "turn/start", map[string]any{})
		reqDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	c.shutdown()
	_ = outW.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop stayed blocked on a full buffer after shutdown")
	}
	select {
	case err := <-reqDone:
		if err == nil {
			t.Fatal("pending request succeeded with no response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending request not released after readLoop exit")
	}
}

// forkerScript builds a detached tree slowly enough that a teardown can
// land mid-growth: the root starts children one at a time; each child calls
// setsid (leaving the group), records its pid, and starts one detached
// grandchild after a short delay. Hard caps: 40 children, each with one
// grandchild, so at most 81 processes, and every process exits on its own
// after 60 s even if the test fails to kill it.
const forkerScript = `/usr/bin/python3 - "$1" <<'PY'
import os, sys, time, signal
log = sys.argv[1]
signal.alarm(60)
def note():
    with open(log, "a") as f:
        f.write(str(os.getpid()) + chr(10))
def child(depth):
    os.setsid()
    signal.alarm(60)
    note()
    if depth == 0:
        time.sleep(0.01)
        if os.fork() == 0:
            child(1)
    time.sleep(60)
    os._exit(0)
for _ in range(40):
    if os.fork() == 0:
        child(0)
    time.sleep(0.005)
time.sleep(60)
PY`

// Review r5 P1: descendants found by a later scan must be stopped too, and
// pids found earlier must still be killed after they're reparented.
func TestKillCodexGroupFreezesAForkingTree(t *testing.T) {
	for round := 0; round < 3; round++ {
		pids := filepath.Join(t.TempDir(), "pids")
		cmd := exec.Command("/bin/sh", "-c", forkerScript, "sh", pids)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		// Kill mid-growth: once some children exist, while more are coming.
		deadline := time.Now().Add(5 * time.Second)
		for {
			raw, _ := os.ReadFile(pids)
			if n := len(strings.Fields(string(raw))); n >= 10 || time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := killCodexGroup(cmd); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Wait()
		time.Sleep(200 * time.Millisecond) // anything that escaped would have recorded itself by now
		raw, _ := os.ReadFile(pids)
		var alive []string
		total := 0
		for _, f := range strings.Fields(string(raw)) {
			pid, err := strconv.Atoi(f)
			if err != nil {
				continue
			}
			total++
			if syscall.Kill(pid, 0) == nil {
				alive = append(alive, f)
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		if total == 0 {
			t.Fatalf("the forker recorded no children; the test proves nothing (pids file %d bytes)", len(raw))
		}
		if len(alive) > 0 {
			t.Fatalf("round %d: %d of %d detached children survived: %v", round, len(alive), total, alive)
		}
	}
}

// Review r5 P1: Interrupt's timeout must not be held by a write the server
// never reads. A pipe nobody drains blocks Write; request() must still
// return when its context ends.
func TestRequestReturnsOnContextEvenIfWriteBlocks(t *testing.T) {
	_, inW := io.Pipe() // nobody reads: every Write blocks
	outR, outW := io.Pipe()
	defer outW.Close()
	defer inW.Close()
	c := newCodexRPCClient(inW, outR)
	go c.readLoop()
	defer c.shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	done := make(chan error, 1)
	go func() { _, err := c.request(ctx, "turn/interrupt", map[string]any{}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("request succeeded with a stuck pipe")
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("request took %v to honour a 200ms context", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request stayed blocked in Write past its context")
	}
	// A second caller isn't blocked behind the stuck one either.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	go func() { _, err := c.request(ctx2, "turn/interrupt", map[string]any{}); done <- err }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("second request blocked behind the stuck writer")
	}
}
