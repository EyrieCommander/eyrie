package bridge

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Review finding 1: a root replaced by a symlink after startup must not
// redirect the bridge. The root descriptor is held from NewFS onward.
func TestRootSwapAfterStartupDoesNotRedirect(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	write(t, filepath.Join(root, "inside.md"), "inside needle\n")
	write(t, filepath.Join(outside, "leak.md"), "outside needle\n")
	fsys, errs := NewFS(map[string]string{"docs": root}, nil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	defer fsys.Close()

	// Swap: move the real root aside, put a symlink to outside in its place.
	if err := os.Rename(root, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}

	hits, _, err := fsys.Search("docs", "", "needle", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if strings.Contains(h.Path, "leak") {
			t.Fatalf("search followed the swapped root: %+v", hits)
		}
	}
	if _, err := fsys.Read("docs", "leak.md", 1, 10); err == nil {
		t.Fatal("read followed the swapped root")
	}
	if r, err := fsys.Read("docs", "inside.md", 1, 10); err != nil || !strings.Contains(r.Content, "inside") {
		t.Fatalf("original root no longer served: %v", err)
	}
}

// A root that is itself a symlink is refused at startup.
func TestRootThatIsSymlinkRefused(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	write(t, filepath.Join(real, "a.md"), "x\n")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	fsys, errs := NewFS(map[string]string{"docs": link}, nil)
	defer fsys.Close()
	if len(errs) != 1 || len(fsys.Aliases()) != 0 {
		t.Fatalf("symlinked root accepted: errs=%v aliases=%v", errs, fsys.Aliases())
	}
}

// Review finding 2: there is no check-then-open gap. Even when the path
// already is an in-root symlink to a denied file at open time, the open
// itself refuses it (O_NOFOLLOW on every component).
func TestOpenRefusesSymlinkAtOpenTime(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	write(t, filepath.Join(root, ".env"), "SECRET=1\n")
	write(t, filepath.Join(root, "ok.md"), "fine\n")
	write(t, filepath.Join(root, "dir", "x.md"), "x\n")
	fsys, _ := NewFS(map[string]string{"docs": root}, nil)
	defer fsys.Close()
	h := fsys.roots["docs"]

	// Swap ok.md for a link to .env, then call the opener directly (no
	// earlier check can save us here: this is the open itself).
	if err := os.Remove(filepath.Join(root, "ok.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".env", filepath.Join(root, "ok.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.openRel(h, "ok.md", wantFile); err != errDenied {
		t.Fatalf("open followed a swapped-in symlink: %v", err)
	}
	// Same for an intermediate directory component.
	if err := os.Rename(filepath.Join(root, "dir"), filepath.Join(base, "dir-real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "dir-real"), filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.openRel(h, "dir/x.md", wantFile); err != errDenied {
		t.Fatalf("open followed a swapped-in directory symlink: %v", err)
	}
}

// Review finding 4: a FIFO must not block. With the old code this test
// hangs until the -timeout; here it must answer promptly with 400.
func TestFIFODoesNotBlock(t *testing.T) {
	f := newFixture(t)
	if err := syscall.Mkfifo(filepath.Join(f.root, "pipe"), 0o600); err != nil {
		t.Skip("mkfifo unsupported:", err)
	}
	done := make(chan int, 1)
	go func() {
		done <- f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=pipe", testToken, nil).Code
	}()
	select {
	case code := <-done:
		if code != 400 {
			t.Fatalf("fifo read: %d, want 400", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
	// Search walks past it without blocking either.
	go func() {
		done <- f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=needle", testToken, nil).Code
	}()
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("search with fifo: %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("search blocked on a FIFO")
	}
	// List hides it.
	rec := f.do(t, "GET", "/bridge/v1/fs/list?root=docs", testToken, nil)
	if strings.Contains(rec.Body.String(), `"pipe"`) {
		t.Fatal("list shows the FIFO")
	}
}

// Review finding 3: a redirect from the wake endpoint must not carry the
// key anywhere. The attempt fails instead.
func TestWakeDoesNotFollowRedirects(t *testing.T) {
	old := wakeRetryDelays
	wakeRetryDelays = nil
	defer func() { wakeRetryDelays = old }()

	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Automation-Key") != "" || r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		w.WriteHeader(200)
	}))
	defer other.Close()
	wake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer wake.Close()

	w := &Waker{URL: wake.URL, Key: "wake-key", Client: &http.Client{}}
	err := w.Deliver(t.Context(), &Message{ConversationID: "chief", MessageID: "m1", Text: "x", TS: time.Now()}, nil)
	if err == nil {
		t.Fatal("redirect counted as delivered")
	}
	if leaked.Load() {
		t.Fatal("wake key forwarded to the redirect target")
	}
	if strings.Contains(err.Error(), "wake-key") || strings.Contains(err.Error(), other.URL) {
		t.Fatalf("error leaks secret or URL: %v", err)
	}
}

// Review finding 5: Listen fails cleanly on a taken port, so the CLI can
// keep the Chief front door off.
func TestListenReportsPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	srv := NewServer(Config{Port: port, BridgeTokenSHA256: HashToken(testToken)}, nil, nil, nil)
	if l2, err := srv.Listen(); err == nil {
		l2.Close()
		t.Fatal("Listen succeeded on a taken port")
	}
}

// Listing reads sizes relative to the directory descriptor, not by path
// from the process cwd.
func TestListSizesAreReal(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/bridge/v1/fs/list?root=docs&path=picker", testToken, nil)
	var out struct {
		Entries []Entry `json:"entries"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Entries) != 1 || out.Entries[0].Size != int64(len("# Picker\nline two\nneedle here\n")) {
		t.Fatalf("entries %+v", out.Entries)
	}
}
