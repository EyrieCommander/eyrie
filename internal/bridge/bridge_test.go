package bridge

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "eyb_test-token-not-a-secret"

// fixture builds a root with ordinary files, denied files, a binary, an
// in-root and an out-of-root symlink, and an outside dir holding a target.
type fixture struct {
	root, outside, logPath string
	srv                    *Server
	store                  *Store
	svc                    *Service
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{root: filepath.Join(base, "root"), outside: filepath.Join(base, "outside"), logPath: filepath.Join(base, "logs", "access.jsonl")}
	write(t, filepath.Join(f.root, "picker", "doc.md"), "# Picker\nline two\nneedle here\n")
	write(t, filepath.Join(f.root, "notes.txt"), "alpha\nbeta\n")
	write(t, filepath.Join(f.root, ".env"), "SECRET=1\n")
	write(t, filepath.Join(f.root, "id_ed25519"), "key\n")
	write(t, filepath.Join(f.root, "server.PEM"), "pem\n")
	write(t, filepath.Join(f.root, "my-credentials.txt"), "needle\n")
	write(t, filepath.Join(f.root, "sub", ".git", "config"), "needle\n")
	write(t, filepath.Join(f.root, "prod.env"), "needle\n")
	if err := os.WriteFile(filepath.Join(f.root, "blob.bin"), []byte{0x7f, 'E', 'L', 'F', 0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "latin1.txt"), []byte{'c', 'a', 'f', 0xe9, '\n'}, 0o600); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.outside, "target.txt"), "outside needle\n")
	if err := os.Symlink(filepath.Join(f.outside, "target.txt"), filepath.Join(f.root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.outside, filepath.Join(f.root, "escape-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("notes.txt", filepath.Join(f.root, "inside-link.txt")); err != nil {
		t.Fatal(err)
	}

	fsys, errs := NewFS(map[string]string{"docs": f.root}, nil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	store, err := OpenStore(filepath.Join(base, "chief.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	log, err := OpenAccessLog(f.logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	cfg := Config{Port: DefaultPort, BridgeTokenSHA256: HashToken(testToken)}
	f.store = store
	f.svc = NewService(store, cfg)
	f.srv = NewServer(cfg, fsys, f.svc, log)
	return f
}

func (f *fixture) do(t *testing.T, method, target, token string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = "192.0.2.10:5555"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func (f *fixture) logLines(t *testing.T) []AccessEntry {
	t.Helper()
	fh, err := os.Open(f.logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	var out []AccessEntry
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		var e AccessEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad log line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

// TestPathSafety is the table-driven path-safety test (acceptance check c).
func TestPathSafety(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name, method, target, token string
		want                        int
		wantErr                     string
	}{
		{"ok read", "GET", "/bridge/v1/fs/read?root=docs&path=picker/doc.md", testToken, 200, ""},
		{"ok list root", "GET", "/bridge/v1/fs/list?root=docs", testToken, 200, ""},
		{"dotdot traversal", "GET", "/bridge/v1/fs/read?root=docs&path=../outside/target.txt", testToken, 400, "bad_path"},
		{"dotdot mid path", "GET", "/bridge/v1/fs/read?root=docs&path=picker/../notes.txt", testToken, 400, "bad_path"},
		{"bare dotdot", "GET", "/bridge/v1/fs/list?root=docs&path=..", testToken, 400, "bad_path"},
		{"absolute path", "GET", "/bridge/v1/fs/read?root=docs&path=/etc/passwd", testToken, 400, "bad_path"},
		{"encoded traversal", "GET", "/bridge/v1/fs/read?root=docs&path=%2e%2e%2foutside%2ftarget.txt", testToken, 400, "bad_path"},
		{"double encoded stays literal", "GET", "/bridge/v1/fs/read?root=docs&path=%252e%252e%252fx", testToken, 404, "not_found"},
		{"encoded absolute", "GET", "/bridge/v1/fs/read?root=docs&path=%2fetc%2fpasswd", testToken, 400, "bad_path"},
		{"backslash", "GET", "/bridge/v1/fs/read?root=docs&path=..%5cnotes.txt", testToken, 400, "bad_path"},
		{"nul byte", "GET", "/bridge/v1/fs/read?root=docs&path=notes.txt%00.md", testToken, 400, "bad_path"},
		{"symlink outside root", "GET", "/bridge/v1/fs/read?root=docs&path=escape.txt", testToken, 403, "denied"},
		{"symlink dir outside root", "GET", "/bridge/v1/fs/read?root=docs&path=escape-dir/target.txt", testToken, 403, "denied"},
		{"symlink inside root refused too", "GET", "/bridge/v1/fs/read?root=docs&path=inside-link.txt", testToken, 403, "denied"},
		{"denied .env", "GET", "/bridge/v1/fs/read?root=docs&path=.env", testToken, 403, "denied"},
		{"denied id_ed25519", "GET", "/bridge/v1/fs/read?root=docs&path=id_ed25519", testToken, 403, "denied"},
		{"denied .git component", "GET", "/bridge/v1/fs/read?root=docs&path=sub/.git/config", testToken, 403, "denied"},
		{"denied case-insensitive pem", "GET", "/bridge/v1/fs/read?root=docs&path=server.PEM", testToken, 403, "denied"},
		{"denied credential name", "GET", "/bridge/v1/fs/read?root=docs&path=my-credentials.txt", testToken, 403, "denied"},
		{"denied *.env", "GET", "/bridge/v1/fs/read?root=docs&path=prod.env", testToken, 403, "denied"},
		{"denied list of .git", "GET", "/bridge/v1/fs/list?root=docs&path=sub/.git", testToken, 403, "denied"},
		{"binary NUL", "GET", "/bridge/v1/fs/read?root=docs&path=blob.bin", testToken, 415, "binary"},
		{"binary invalid utf8", "GET", "/bridge/v1/fs/read?root=docs&path=latin1.txt", testToken, 415, "binary"},
		{"unknown root", "GET", "/bridge/v1/fs/read?root=nope&path=notes.txt", testToken, 404, "not_found"},
		{"root alias is not a path", "GET", "/bridge/v1/fs/list?root=" + f.root, testToken, 404, "not_found"},
		{"missing file", "GET", "/bridge/v1/fs/read?root=docs&path=missing.md", testToken, 404, "not_found"},
		{"missing token", "GET", "/bridge/v1/fs/read?root=docs&path=notes.txt", "", 401, ""},
		{"wrong token", "GET", "/bridge/v1/fs/read?root=docs&path=notes.txt", "eyb_wrong", 401, ""},
		{"PUT on fs/read", "PUT", "/bridge/v1/fs/read?root=docs&path=notes.txt", testToken, 405, "method_not_allowed"},
		{"POST on fs/list", "POST", "/bridge/v1/fs/list?root=docs", testToken, 405, "method_not_allowed"},
		{"DELETE on fs/read", "DELETE", "/bridge/v1/fs/read?root=docs&path=notes.txt", testToken, 405, "method_not_allowed"},
		{"GET on reply", "GET", "/bridge/v1/reply", testToken, 405, "method_not_allowed"},
		{"no write route", "POST", "/bridge/v1/fs/write?root=docs&path=x", testToken, 404, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.srv.limits = newLimiter() // rate limits are tested separately
			rec := f.do(t, tc.method, tc.target, tc.token, nil)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == 401 && strings.TrimSpace(rec.Body.String()) != "" {
				t.Fatalf("401 must carry no detail, got %q", rec.Body.String())
			}
			if tc.wantErr != "" {
				var e map[string]string
				_ = json.Unmarshal(rec.Body.Bytes(), &e)
				if e["error"] != tc.wantErr {
					t.Fatalf("error %q, want %q", e["error"], tc.wantErr)
				}
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing Cache-Control: no-store")
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("bridge must not send CORS headers")
			}
		})
	}
	// Every refusal above is logged, with status and no secrets.
	lines := f.logLines(t)
	if len(lines) != len(cases) {
		t.Fatalf("access log has %d lines, want %d", len(lines), len(cases))
	}
	raw, _ := os.ReadFile(f.logPath)
	for _, forbidden := range []string{testToken, "eyb_wrong", "SECRET=1", "line two", "Bearer"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("access log leaked %q", forbidden)
		}
	}
	for i, tc := range cases {
		if lines[i].Status != tc.want {
			t.Fatalf("log line %d (%s) status %d, want %d", i, tc.name, lines[i].Status, tc.want)
		}
		if tc.want == 401 && !lines[i].AuthFailed {
			t.Fatalf("log line %d (%s) should mark auth_failed", i, tc.name)
		}
	}
}

func TestCleanRel(t *testing.T) {
	ok := map[string]string{"": ".", ".": ".", "a": "a", "a/b": "a/b", "a//b/": "a/b", "./a": "a"}
	for in, want := range ok {
		got, err := cleanRel(in)
		if err != nil || got != want {
			t.Errorf("cleanRel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"..", "../a", "a/..", "a/../b", "/a", "a\x00b", `a\b`, `..\a`} {
		if _, err := cleanRel(bad); err == nil {
			t.Errorf("cleanRel(%q) accepted", bad)
		}
	}
}

func TestListHidesDenied(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/bridge/v1/fs/list?root=docs", testToken, nil)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var out struct {
		Entries []Entry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range out.Entries {
		names[e.Name] = true
	}
	for _, want := range []string{"notes.txt", "picker", "sub", "blob.bin"} {
		if !names[want] {
			t.Errorf("list missing %s", want)
		}
	}
	for _, hidden := range []string{".env", "id_ed25519", "server.PEM", "my-credentials.txt", "prod.env", "escape.txt", "escape-dir", "inside-link.txt"} {
		if names[hidden] {
			t.Errorf("list exposed %s", hidden)
		}
	}
}

func TestRootsAliasesOnly(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/bridge/v1/fs/roots", testToken, nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), f.root) || !strings.Contains(rec.Body.String(), `"docs"`) {
		t.Fatalf("roots: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNoRootsConfiguredAll404(t *testing.T) {
	fsys, _ := NewFS(nil, nil)
	cfg := Config{Port: DefaultPort, BridgeTokenSHA256: HashToken(testToken)}
	srv := NewServer(cfg, fsys, nil, nil)
	for _, p := range []string{"/bridge/v1/fs/roots", "/bridge/v1/fs/list?root=docs", "/bridge/v1/fs/read?root=docs&path=a", "/bridge/v1/fs/search?root=docs&q=a"} {
		req := httptest.NewRequest("GET", p, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Errorf("%s: %d, want 404", p, rec.Code)
		}
	}
}

func TestSearchSkipsDeniedAndBinary(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=NEEDLE", testToken, nil)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var out struct {
		Hits []Hit `json:"hits"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Hits) != 1 || out.Hits[0].Path != "picker/doc.md" || out.Hits[0].Line != 3 {
		t.Fatalf("hits = %+v; want only picker/doc.md:3", out.Hits)
	}
	// Regex metacharacters are literal.
	rec = f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=.*", testToken, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || len(out.Hits) != 0 {
		t.Fatalf("literal search for .* got %d hits", len(out.Hits))
	}
}

func TestReadPaging(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=picker/doc.md&offset=2&limit=1", testToken, nil)
	var r ReadResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if rec.Code != 200 || r.Content != "line two\n" || r.LinesReturned != 1 || !r.Truncated || r.Offset != 2 {
		t.Fatalf("read: %d %+v", rec.Code, r)
	}
	rec = f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=picker/doc.md&offset=0", testToken, nil)
	if rec.Code != 400 {
		t.Fatalf("offset=0 should be 400, got %d", rec.Code)
	}
}

func TestReadTooLarge(t *testing.T) {
	f := newFixture(t)
	big := filepath.Join(f.root, "big.txt")
	fh, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := fh.Truncate(maxReadFileSize + 1); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	rec := f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=big.txt", testToken, nil)
	if rec.Code != 413 {
		t.Fatalf("got %d, want 413", rec.Code)
	}
}

func TestLooksBinaryAllowsCutRune(t *testing.T) {
	// "é" is 0xC3 0xA9; a buffer cut after 0xC3 is not binary.
	if looksBinary([]byte("abc\xc3"), false) {
		t.Fatal("partial trailing rune before EOF treated as binary")
	}
	if !looksBinary([]byte("abc\xc3"), true) {
		t.Fatal("truncated rune at EOF should be binary")
	}
	if !looksBinary([]byte("ab\xffcd"), false) {
		t.Fatal("invalid byte mid-buffer should be binary")
	}
}

func TestAuthRateLimitAndFailedAuthThrottle(t *testing.T) {
	f := newFixture(t)
	now := time.Unix(1_800_000_000, 0)
	f.srv.limits.now = func() time.Time { return now }
	// Burst of 20 succeeds, the 21st is 429 with Retry-After.
	for i := 0; i < rateBurst; i++ {
		if rec := f.do(t, "GET", "/bridge/v1/fs/roots", testToken, nil); rec.Code != 200 {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	rec := f.do(t, "GET", "/bridge/v1/fs/roots", testToken, nil)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("21st: %d retry-after %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	now = now.Add(2 * time.Second) // refills 2 tokens at 1/s
	if rec := f.do(t, "GET", "/bridge/v1/fs/roots", testToken, nil); rec.Code != 200 {
		t.Fatalf("after refill: %d", rec.Code)
	}

	// >10 failed auths in a minute from one address -> 429, even with a good token.
	for i := 0; i < authFailPerMin+1; i++ {
		f.do(t, "GET", "/bridge/v1/fs/roots", "eyb_wrong", nil)
	}
	rec = f.do(t, "GET", "/bridge/v1/fs/roots", testToken, nil)
	if rec.Code != 429 {
		t.Fatalf("after auth failures: %d, want 429", rec.Code)
	}
	now = now.Add(61 * time.Second)
	if rec := f.do(t, "GET", "/bridge/v1/fs/roots", testToken, nil); rec.Code != 200 {
		t.Fatalf("after window: %d", rec.Code)
	}
}

func TestConcurrencyCap(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < maxConcurrent; i++ {
		f.srv.sem <- struct{}{}
	}
	rec := f.do(t, "GET", "/bridge/v1/fs/roots", testToken, nil)
	if rec.Code != 429 {
		t.Fatalf("got %d, want 429 when %d requests are in flight", rec.Code, maxConcurrent)
	}
}

func TestBodyCap(t *testing.T) {
	f := newFixture(t)
	body := `{"conversation_id":"c","in_reply_to":"m","reply_id":"r","text":"` + strings.Repeat("x", maxBodyBytes) + `"}`
	rec := f.do(t, "POST", "/bridge/v1/reply", testToken, strings.NewReader(body))
	if rec.Code != 413 {
		t.Fatalf("got %d, want 413", rec.Code)
	}
}

// TestIsolation covers acceptance check (d) at the handler level: the
// bridge has no management routes, and its listener binds loopback only.
func TestIsolation(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{
		"/api/agents", "/api/commander/chat", "/api/instances", "/api/terminal/ws",
		"/api/chief/messages", "/", "/index.html", "/bridge", "/bridge/v1", "/bridge/v1/",
		"/bridge/v1/fs", "/bridge/v1/fs/read/", "/bridge/v1/prompts/a/b",
		"/bridge/v1/../api/agents", "/%2e%2e/api/agents",
	} {
		for _, m := range []string{"GET", "POST", "PUT", "DELETE"} {
			f.srv.limits = newLimiter() // isolate from the per-token rate limit
			rec := f.do(t, m, p, testToken, nil)
			if rec.Code != 404 {
				t.Errorf("%s %s through bridge: %d, want 404", m, p, rec.Code)
			}
		}
	}
	if got := f.srv.Addr(); !strings.HasPrefix(got, "127.0.0.1:") {
		t.Fatalf("bridge binds %s, want 127.0.0.1", got)
	}
}

// Real listener round trip: bind 127.0.0.1, confirm an /api path 404s over
// the wire too (no mux fallthrough), and confirm the server is not reachable
// on a non-loopback address of this host.
func TestIsolationOverTheWire(t *testing.T) {
	f := newFixture(t)
	f.srv.cfg.Port = freePort(t)
	done := make(chan error, 1)
	go func() { done <- f.srv.Serve() }()
	t.Cleanup(func() { _ = f.srv.Shutdown(t.Context()); <-done })
	waitUp(t, "http://"+f.srv.Addr())

	req, _ := http.NewRequest("GET", "http://"+f.srv.Addr()+"/api/agents", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("/api/agents over bridge: %d", resp.StatusCode)
	}
	for _, ip := range nonLoopbackIPs(t) {
		c := http.Client{Timeout: 500 * time.Millisecond}
		if r, err := c.Get("http://" + ip + ":" + portOf(f.srv.Addr()) + "/bridge/v1/fs/roots"); err == nil {
			r.Body.Close()
			t.Fatalf("bridge reachable on non-loopback %s", ip)
		}
	}
}

// ── Chief front door ────────────────────────────────────────────────

func TestReplyFlowIdempotentAndDurable(t *testing.T) {
	base := t.TempDir()
	dbPath := filepath.Join(base, "chief.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.CreateMessage("chief", "hello chief")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetState(m.MessageID, StateWaiting, 1, ""); err != nil {
		t.Fatal(err)
	}
	store.Close()

	// "Restart": reopen the store; the waiting message survives.
	store, err = OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := Config{Port: DefaultPort, BridgeTokenSHA256: HashToken(testToken)}
	fsys, _ := NewFS(nil, nil)
	svc := NewService(store, cfg)
	var notified atomic.Int32
	svc.Notify = func(string) { notified.Add(1) }
	srv := NewServer(cfg, fsys, svc, nil)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/bridge/v1/reply", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	got, _ := store.GetMessage(m.MessageID)
	if got.State != StateWaiting {
		t.Fatalf("after restart state %s", got.State)
	}

	interim := `{"conversation_id":"chief","in_reply_to":"` + m.MessageID + `","reply_id":"r0","text":"on it","final":false}`
	if rec := post(interim); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	got, _ = store.GetMessage(m.MessageID)
	if got.State != StateWaiting {
		t.Fatalf("interim reply moved state to %s", got.State)
	}

	final := `{"conversation_id":"chief","in_reply_to":"` + m.MessageID + `","reply_id":"r1","text":"**hi** <script>x</script>","final":true}`
	rec := post(final)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), `"duplicate":true`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec = post(final)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"duplicate":true`) {
		t.Fatalf("repeat: %d %s", rec.Code, rec.Body.String())
	}
	got, _ = store.GetMessage(m.MessageID)
	if got.State != StateAnswered || len(got.Replies) != 2 {
		t.Fatalf("state %s, %d replies; want answered, 2", got.State, len(got.Replies))
	}
	if notified.Load() != 2 {
		t.Fatalf("notify called %d times, want 2 (duplicate must not notify)", notified.Load())
	}

	// Unknown message, and known message in the wrong conversation: 404.
	if rec := post(`{"conversation_id":"chief","in_reply_to":"nope","reply_id":"r2","text":"x"}`); rec.Code != 404 {
		t.Fatalf("unknown in_reply_to: %d", rec.Code)
	}
	if rec := post(`{"conversation_id":"other","in_reply_to":"` + m.MessageID + `","reply_id":"r3","text":"x"}`); rec.Code != 404 {
		t.Fatalf("wrong conversation: %d", rec.Code)
	}
	// Over 32 KB text.
	big := `{"conversation_id":"chief","in_reply_to":"` + m.MessageID + `","reply_id":"r4","text":"` + strings.Repeat("y", maxReplyBytes+1) + `"}`
	if rec := post(big); rec.Code != 413 {
		t.Fatalf("big reply: %d", rec.Code)
	}

	// Prompt fetch returns full text.
	req := httptest.NewRequest("GET", "/bridge/v1/prompts/"+m.MessageID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "hello chief") || !strings.Contains(rec.Body.String(), `"state":"answered"`) {
		t.Fatalf("prompt fetch: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWakeRetriesSameIDThenFails(t *testing.T) {
	old := wakeRetryDelays
	wakeRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { wakeRetryDelays = old }()

	var calls atomic.Int32
	var idsMu sync.Mutex
	var ids []string
	wake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer wake-key" || r.Header.Get("X-Automation-Key") != "wake-key" {
			t.Errorf("wake auth headers wrong")
		}
		var p WakePayload
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p.Source != "eyrie" || p.Type != "eyrie.prompt" || p.ConversationID != "chief" {
			t.Errorf("payload %+v", p)
		}
		idsMu.Lock()
		ids = append(ids, p.MessageID)
		idsMu.Unlock()
		w.WriteHeader(503)
	}))
	defer wake.Close()

	store, _ := OpenStore(filepath.Join(t.TempDir(), "c.db"))
	defer store.Close()
	svc := NewService(store, Config{ChiefWakeURL: wake.URL, ChiefWakeKey: "wake-key"})
	m, err := svc.Send("chief", "ping")
	if err != nil {
		t.Fatal(err)
	}
	svc.wg.Wait()
	if calls.Load() != 4 {
		t.Fatalf("wake called %d times, want 4 (1 + 3 retries)", calls.Load())
	}
	idsMu.Lock()
	for _, id := range ids {
		if id != m.MessageID {
			t.Errorf("retry used message_id %s, want %s", id, m.MessageID)
		}
	}
	ids = nil
	idsMu.Unlock()
	got, _ := store.GetMessage(m.MessageID)
	if got.State != StateFailed || !strings.Contains(got.LastError, "503") {
		t.Fatalf("state %s err %q", got.State, got.LastError)
	}
	if strings.Contains(got.LastError, "wake-key") || strings.Contains(got.LastError, wake.URL) {
		t.Fatal("stored error leaks wake URL or key")
	}

	// Retry reuses the same id.
	calls.Store(0)
	if _, err := svc.Retry(m.MessageID); err != nil {
		t.Fatal(err)
	}
	svc.wg.Wait()
	if calls.Load() != 4 {
		t.Fatalf("retry: wake called %d times", calls.Load())
	}
	idsMu.Lock()
	defer idsMu.Unlock()
	for _, id := range ids {
		if id != m.MessageID {
			t.Fatalf("manual retry used message_id %s, want %s", id, m.MessageID)
		}
	}
	if got, _ := store.GetMessage(m.MessageID); got.LastError != "wake: HTTP 503" {
		t.Fatalf("retry last error %q", got.LastError)
	}
}

func TestWakeSuccessAndTruncation(t *testing.T) {
	var got WakePayload
	wake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(202)
	}))
	defer wake.Close()
	store, _ := OpenStore(filepath.Join(t.TempDir(), "c.db"))
	defer store.Close()
	svc := NewService(store, Config{ChiefWakeURL: wake.URL, ChiefWakeKey: "k"})
	long := strings.Repeat("é", maxWakeText+5)
	m, err := svc.Send("chief", long)
	if err != nil {
		t.Fatal(err)
	}
	svc.wg.Wait()
	if !got.Truncated || len([]rune(got.Text)) != maxWakeText {
		t.Fatalf("truncated=%v runes=%d", got.Truncated, len([]rune(got.Text)))
	}
	st, _ := store.GetMessage(m.MessageID)
	if st.State != StateWaiting || st.Text != long {
		t.Fatalf("state %s, stored text intact=%v", st.State, st.Text == long)
	}
}

func TestSendWithoutWakeConfigured(t *testing.T) {
	store, _ := OpenStore(filepath.Join(t.TempDir(), "c.db"))
	defer store.Close()
	svc := NewService(store, Config{})
	if _, err := svc.Send("chief", "x"); err != ErrWakeNotConfigured {
		t.Fatalf("err %v", err)
	}
}

// ── Config ──────────────────────────────────────────────────────────

func TestConfigPermsAndRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.toml")
	if _, err := LoadConfig(path); err != ErrNotConfigured {
		t.Fatalf("absent config: %v, want ErrNotConfigured", err)
	}
	tok, err := RotateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), tok) {
		t.Fatal("config stores the token, not just its hash")
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !tokenMatches(tok, c.BridgeTokenSHA256) || tokenMatches(tok+"x", c.BridgeTokenSHA256) {
		t.Fatal("hash mismatch")
	}
	// Preserves other keys on rotate.
	if err := os.WriteFile(path, append(raw, []byte("\nchief_wake_url = \"https://example.invalid/w\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateToken(path); err != nil {
		t.Fatal(err)
	}
	c, _ = LoadConfig(path)
	if c.ChiefWakeURL != "https://example.invalid/w" {
		t.Fatal("rotate dropped chief_wake_url")
	}
	// Group/world readable: refuse.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Fatalf("0640 accepted: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	good := HashToken("x")
	cases := []struct {
		name string
		c    Config
		ok   bool
	}{
		{"ok", Config{BridgeTokenSHA256: good}, true},
		{"no hash", Config{}, false},
		{"relative root", Config{BridgeTokenSHA256: good, Roots: map[string]string{"a": "rel"}}, false},
		{"bad alias", Config{BridgeTokenSHA256: good, Roots: map[string]string{"../x": "/tmp"}}, false},
		{"bad port", Config{BridgeTokenSHA256: good, Port: 70000}, false},
	}
	for _, tc := range cases {
		err := tc.c.Validate()
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", tc.name, err)
		}
	}
}

func TestExtraDenyOnlyAdds(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "draft.md"), "x\n")
	fsys, _ := NewFS(map[string]string{"r": root}, []string{"DRAFT*"})
	if !fsys.denied("draft.md") || !fsys.denied(".env") {
		t.Fatal("extra deny not applied or base deny lost")
	}
}
