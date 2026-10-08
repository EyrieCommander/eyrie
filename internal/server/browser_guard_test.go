package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Audacity88/eyrie/internal/config"
)

type guardReq struct {
	name, method, host, origin, site string
	ws                               bool
	want                             int // 200 = reached the handler
}

func runGuard(t *testing.T, g http.Handler, hits *atomic.Int32, cases []guardReq) {
	t.Helper()
	for _, c := range cases {
		before := hits.Load()
		req := httptest.NewRequest(c.method, "/api/agents/x/restart", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			req.Header.Set("Sec-Fetch-Site", c.site)
		}
		if c.ws {
			req.Header.Set("Connection", "keep-alive, Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		reached := hits.Load() > before
		if c.want == 200 && !reached {
			t.Errorf("%s: refused (%d), want allowed", c.name, rec.Code)
		}
		if c.want != 200 && (reached || rec.Code != c.want) {
			t.Errorf("%s: code %d reached=%v, want %d and never reach the handler", c.name, rec.Code, reached, c.want)
		}
	}
}

// S2-09: management mutations and WebSocket upgrades refuse cross-site and
// DNS-rebinding requests; refused requests never reach a handler.
func TestBrowserGuardLoopbackBind(t *testing.T) {
	var hits atomic.Int32
	g := newBrowserGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }), "127.0.0.1")
	runGuard(t, g, &hits, []guardReq{
		// allowed
		{"same-origin POST", "POST", "127.0.0.1:7200", "http://127.0.0.1:7200", "same-origin", false, 200},
		{"localhost POST", "POST", "localhost:7200", "http://localhost:7200", "same-origin", false, 200},
		{"vite dev proxy POST", "POST", "localhost:5173", "http://localhost:5173", "same-origin", false, 200},
		{"CLI POST without browser headers", "POST", "127.0.0.1:7200", "", "", false, 200},
		{"GET read", "GET", "127.0.0.1:7200", "", "", false, 200},
		{"cross-site GET read (CORS blocks reading)", "GET", "127.0.0.1:7200", "https://evil.example", "cross-site", false, 200},
		{"same-origin websocket", "GET", "127.0.0.1:7200", "http://127.0.0.1:7200", "same-origin", true, 200},
		// refused
		{"cross-origin POST", "POST", "127.0.0.1:7200", "https://evil.example", "cross-site", false, 403},
		{"cross-origin POST, no fetch metadata", "POST", "127.0.0.1:7200", "https://evil.example", "", false, 403},
		{"cross-site metadata, no origin", "POST", "127.0.0.1:7200", "", "cross-site", false, 403},
		{"same-site (other port) metadata", "POST", "127.0.0.1:7200", "http://127.0.0.1:7200", "same-site", false, 403},
		{"https origin over http", "POST", "127.0.0.1:7200", "https://127.0.0.1:7200", "", false, 403},
		{"other localhost port", "DELETE", "127.0.0.1:7200", "http://127.0.0.1:9999", "", false, 403},
		{"null origin", "PUT", "127.0.0.1:7200", "null", "", false, 403},
		{"cross-site websocket (terminal)", "GET", "127.0.0.1:7200", "https://evil.example", "cross-site", true, 403},
		{"websocket from other origin, no metadata", "GET", "127.0.0.1:7200", "https://evil.example", "", true, 403},
		{"DNS rebinding read", "GET", "evil.example:7200", "", "", false, 403},
		{"DNS rebinding mutation", "POST", "evil.example:7200", "http://evil.example:7200", "same-origin", false, 403},
		{"DNS rebinding websocket", "GET", "evil.example:7200", "http://evil.example:7200", "same-origin", true, 403},
	})
}

// A specific non-loopback bind (e.g. a tailnet IP) allows that host name,
// and still refuses other hosts and cross-origin mutations.
func TestBrowserGuardSpecificBind(t *testing.T) {
	var hits atomic.Int32
	g := newBrowserGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }), "100.101.102.103")
	runGuard(t, g, &hits, []guardReq{
		{"tailnet same-origin", "POST", "100.101.102.103:7200", "http://100.101.102.103:7200", "same-origin", false, 200},
		{"loopback still fine", "POST", "127.0.0.1:7200", "", "", false, 200},
		{"rebinding host", "GET", "evil.example:7200", "", "", false, 403},
		{"cross-origin mutation", "POST", "100.101.102.103:7200", "https://evil.example", "cross-site", false, 403},
	})
}

// A wildcard bind can't know its names, so Host checks are off, but
// Origin and fetch-metadata checks still refuse cross-site mutations.
func TestBrowserGuardWildcardBindKeepsOriginChecks(t *testing.T) {
	var hits atomic.Int32
	g := newBrowserGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }), "0.0.0.0")
	runGuard(t, g, &hits, []guardReq{
		{"any host read", "GET", "mini.local:7200", "", "", false, 200},
		{"same-origin mutation by name", "POST", "mini.local:7200", "http://mini.local:7200", "same-origin", false, 200},
		{"cross-origin mutation", "POST", "mini.local:7200", "https://evil.example", "cross-site", false, 403},
		{"cross-site websocket", "GET", "mini.local:7200", "https://evil.example", "", true, 403},
	})
}

// The guard is installed on the server New actually builds: requests go
// through s.server.Handler (what Start serves), not a helper. A refused
// cross-site request must not reach the route; a same-origin one must.
func TestNewServerHandlerRefusesCrossSite(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // New creates stores under ~/.eyrie
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	cfg := config.DefaultConfig()
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := s.server.Handler
	do := func(method, path, host, origin string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"name":"x"}`))
		req.Host = host
		req.Header.Set("Content-Type", "text/plain")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := do("POST", "/api/projects", "127.0.0.1:7200", "https://evil.example"); got != 403 {
		t.Errorf("cross-site POST /api/projects: %d, want 403", got)
	}
	if got := do("GET", "/api/projects", "evil.example:7200", ""); got != 403 {
		t.Errorf("rebinding GET /api/projects: %d, want 403", got)
	}
	ws := httptest.NewRequest("GET", "/api/terminal/ws?session=eyrie-guard-test", nil)
	ws.Host = "127.0.0.1:7200"
	ws.Header.Set("Origin", "https://evil.example")
	ws.Header.Set("Connection", "Upgrade")
	ws.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ws)
	if rec.Code != 403 {
		t.Errorf("cross-site terminal websocket: %d, want 403", rec.Code)
	}
	if got := do("GET", "/api/projects", "127.0.0.1:7200", ""); got != 200 {
		t.Errorf("loopback GET /api/projects: %d, want 200", got)
	}
	// No project was created by the refused POST.
	req := httptest.NewRequest("GET", "/api/projects", nil)
	req.Host = "127.0.0.1:7200"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), `"name":"x"`) {
		t.Fatal("refused cross-site POST created a project")
	}
}
