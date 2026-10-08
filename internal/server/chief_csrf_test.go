package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Audacity88/eyrie/internal/bridge"
)

// Review 7, finding 1: Chief mutations refuse cross-site requests, and a
// refused request never wakes the chief.
func TestChiefMutationsRefuseCrossSite(t *testing.T) {
	var wakes atomic.Int32
	wake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wakes.Add(1)
		w.WriteHeader(202)
	}))
	defer wake.Close()

	store, err := bridge.OpenStore(filepath.Join(t.TempDir(), "chief.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := bridge.NewService(store, bridge.Config{ChiefWakeURL: wake.URL, ChiefWakeKey: "k"})
	s := &Server{mux: http.NewServeMux(), events: NewEventBus()}
	s.AttachChief(svc)
	s.registerChiefRoutes()

	send := func(host, origin, ctype, site, body string) int {
		req := httptest.NewRequest("POST", "/api/chief/messages", strings.NewReader(body))
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec.Code
	}
	body := `{"text":"hi"}`
	refused := []struct {
		name                      string
		host, origin, ctype, site string
		want                      int
	}{
		{"evil origin", "127.0.0.1:7200", "https://evil.example", "application/json", "cross-site", 403},
		{"text/plain simple request", "127.0.0.1:7200", "https://evil.example", "text/plain", "cross-site", 403},
		{"text/plain same origin", "127.0.0.1:7200", "http://127.0.0.1:7200", "text/plain", "same-origin", 415},
		{"form post", "127.0.0.1:7200", "", "application/x-www-form-urlencoded", "", 415},
		{"no origin but cross-site fetch", "127.0.0.1:7200", "", "application/json", "cross-site", 403},
		{"other localhost port", "127.0.0.1:7200", "http://127.0.0.1:9999", "application/json", "same-site", 403},
		{"DNS rebinding host", "evil.example:7200", "http://evil.example:7200", "application/json", "same-origin", 403},
		{"null origin", "127.0.0.1:7200", "null", "application/json", "cross-site", 403},
	}
	for _, tc := range refused {
		if got := send(tc.host, tc.origin, tc.ctype, tc.site, body); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	svc.Close() // waits for any delivery loop a wrongly-accepted request started
	if n := wakes.Load(); n != 0 {
		t.Fatalf("refused requests woke the chief %d times", n)
	}

	// Allowed: same-origin UI (and the Vite dev proxy, which keeps the dev
	// server's Host and Origin), and a non-browser client with no Origin.
	svc2 := bridge.NewService(store, bridge.Config{ChiefWakeURL: wake.URL, ChiefWakeKey: "k"})
	s.AttachChief(svc2)
	defer svc2.Close()
	for _, tc := range []struct{ name, host, origin, site string }{
		{"same origin", "127.0.0.1:7200", "http://127.0.0.1:7200", "same-origin"},
		{"localhost name", "localhost:7200", "http://localhost:7200", "same-origin"},
		{"vite dev proxy", "localhost:5173", "http://localhost:5173", "same-origin"},
		{"curl", "127.0.0.1:7200", "", ""},
	} {
		if got := send(tc.host, tc.origin, "application/json", tc.site, body); got != http.StatusAccepted {
			t.Errorf("%s: %d, want 202", tc.name, got)
		}
	}

	// Retry is guarded the same way.
	req := httptest.NewRequest("POST", "/api/chief/messages/x/retry", strings.NewReader("{}"))
	req.Host = "127.0.0.1:7200"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("cross-site retry: %d, want 403", rec.Code)
	}
}
