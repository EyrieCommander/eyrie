package server

import (
	"testing"

	"github.com/Audacity88/eyrie/internal/adapter"
)

func TestHostIsLoopback(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "127.0.0.2": true, "localhost": true, "LOCALHOST": true,
		"::1": true, "[::1]": true,
		"": false, "0.0.0.0": false, "::": false, "192.168.1.5": false,
		"100.64.0.1": false, "my-mac.local": false, "localhost.evil.com": false,
	} {
		if got := hostIsLoopback(host); got != want {
			t.Errorf("hostIsLoopback(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestRedactStatusForHost(t *testing.T) {
	st := &adapter.AgentStatus{BusyState: "busy", CurrentTask: "session s: secret plan"}
	redactStatusForHost(st, "127.0.0.1")
	if st.CurrentTask == "" {
		t.Fatal("loopback dashboard lost current_task")
	}
	redactStatusForHost(st, "0.0.0.0")
	if st.CurrentTask != "" || st.BusyState != "busy" {
		t.Fatalf("non-loopback status = %+v; want task cleared, busy kept", st)
	}
	redactStatusForHost(nil, "0.0.0.0")
}
