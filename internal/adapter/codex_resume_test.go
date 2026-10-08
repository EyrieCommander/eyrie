package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
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
