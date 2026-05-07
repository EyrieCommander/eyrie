package adapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestZeroClawInterruptPostsAbortRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.EscapedPath() != "/api/sessions/project%20chat/abort" {
			t.Errorf("path = %s, want /api/sessions/project%%20chat/abort", r.URL.EscapedPath())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Errorf("authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"aborted"}`)
	}))
	defer server.Close()

	adapter := NewZeroClawAdapter("zc", "ZeroClaw", server.URL, "secret-token", "")
	if err := adapter.Interrupt(context.Background(), "project chat"); err != nil {
		t.Fatalf("Interrupt returned error: %v", err)
	}
	if !called {
		t.Fatalf("Interrupt did not call ZeroClaw abort endpoint")
	}
}

func TestZeroClawInterruptTreatsNoActiveResponseAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"no_active_response"}`)
	}))
	defer server.Close()

	adapter := NewZeroClawAdapter("zc", "ZeroClaw", server.URL, "", "")
	if err := adapter.Interrupt(context.Background(), "session"); err != nil {
		t.Fatalf("Interrupt returned error for no_active_response: %v", err)
	}
}

func TestZeroClawInterruptReturnsStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "abort failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewZeroClawAdapter("zc", "ZeroClaw", server.URL, "", "")
	err := adapter.Interrupt(context.Background(), "session")
	if err == nil {
		t.Fatalf("Interrupt returned nil for HTTP 500")
	}
	if !strings.Contains(err.Error(), "/api/sessions/session/abort returned 500") {
		t.Fatalf("error = %q, want status context", err.Error())
	}
}
