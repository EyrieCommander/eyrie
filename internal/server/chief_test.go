package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Audacity88/eyrie/internal/bridge"
	"github.com/Audacity88/eyrie/internal/config"
)

// Acceptance check (d), management side: the dashboard still binds loopback
// by default, the Chief routes live only on the management mux, and the
// bridge's handler (which is what Funnel/tunnel exposes) returns 404 for
// every management route — including the chief routes it shares a store with.
func TestManagementStaysLoopbackAndOffBridge(t *testing.T) {
	if h := config.DefaultConfig().Dashboard.Host; h != "127.0.0.1" {
		t.Fatalf("management API default host %q, want 127.0.0.1", h)
	}

	const tok = "eyb_test"
	store, err := bridge.OpenStore(filepath.Join(t.TempDir(), "chief.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := bridge.Config{Port: bridge.DefaultPort, BridgeTokenSHA256: bridge.HashToken(tok), ChiefWakeURL: "http://127.0.0.1:1/", ChiefWakeKey: "k"}
	svc := bridge.NewService(store, cfg)
	defer svc.Close()
	fsys, _ := bridge.NewFS(nil, nil)
	bsrv := bridge.NewServer(cfg, fsys, svc, nil)

	// A minimal management server with only the chief routes registered.
	s := &Server{mux: http.NewServeMux(), events: NewEventBus()}
	s.AttachChief(svc)
	s.registerChiefRoutes()

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/chief/status", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("management chief status: %d %s", rec.Code, rec.Body.String())
	}

	for _, p := range []string{"/api/chief/status", "/api/chief/messages", "/api/chief/events", "/api/agents", "/api/commander/chat", "/api/reference"} {
		for _, m := range []string{"GET", "POST"} {
			req := httptest.NewRequest(m, p, nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			rec := httptest.NewRecorder()
			bsrv.Handler().ServeHTTP(rec, req)
			if rec.Code != 404 {
				t.Errorf("bridge %s %s: %d, want 404", m, p, rec.Code)
			}
		}
	}
}

func TestChiefSendStoresBeforeDelivery(t *testing.T) {
	store, err := bridge.OpenStore(filepath.Join(t.TempDir(), "chief.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Wake points at a closed port: delivery fails, but the prompt is durable.
	svc := bridge.NewService(store, bridge.Config{ChiefWakeURL: "http://127.0.0.1:1/", ChiefWakeKey: "k"})
	defer svc.Close()
	s := &Server{mux: http.NewServeMux(), events: NewEventBus()}
	s.AttachChief(svc)
	s.registerChiefRoutes()

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/chief/messages", strings.NewReader(`{"text":"hello"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	var m bridge.Message
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m.MessageID == "" || m.ConversationID != "chief" {
		t.Fatalf("bad message %+v", m)
	}
	got, err := store.GetMessage(m.MessageID)
	if err != nil || got.Text != "hello" {
		t.Fatalf("not stored: %v %+v", err, got)
	}

	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/chief/messages", strings.NewReader(`{"text":"   "}`)))
	if rec.Code != 400 {
		t.Fatalf("empty prompt: %d", rec.Code)
	}

	// Disabled bridge: 503, not a crash.
	s2 := &Server{mux: http.NewServeMux(), events: NewEventBus()}
	s2.registerChiefRoutes()
	rec = httptest.NewRecorder()
	s2.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/chief/messages", nil))
	if rec.Code != 503 {
		t.Fatalf("disabled: %d", rec.Code)
	}
}
