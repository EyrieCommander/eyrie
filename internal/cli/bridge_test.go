package cli

import (
	"testing"

	"github.com/Audacity88/eyrie/internal/bridge"
	"github.com/Audacity88/eyrie/internal/config"
)

// Review 10, finding 2: a bridge port equal to the dashboard port is
// refused before anything binds, so management keeps its port.
func TestBridgeConflictWithDashboardPort(t *testing.T) {
	dash := config.DefaultConfig()
	if why := bridgeConflict(bridge.Config{Port: dash.Dashboard.Port}, dash); why == "" {
		t.Fatal("bridge on the dashboard port accepted")
	}
	if why := bridgeConflict(bridge.Config{Port: bridge.DefaultPort}, dash); why != "" {
		t.Fatalf("default ports flagged: %s", why)
	}
}

// Review 12: the bridge refuses to start unless the management API binds
// loopback only (acceptance check d).
func TestBridgeRequiresLoopbackManagement(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":     true,
		"127.0.0.2":     true,
		"::1":           true,
		"[::1]":         true,
		"localhost":     true,
		"LOCALHOST":     true,
		"0.0.0.0":       false,
		"":              false, // every interface
		"::":            false,
		"100.101.102.1": false, // tailnet
		"192.168.1.10":  false,
		"mini.local":    false,
		"example.com":   false,
	}
	for host, ok := range cases {
		dash := config.DefaultConfig()
		dash.Dashboard.Host = host
		why := bridgeConflict(bridge.Config{Port: bridge.DefaultPort}, dash)
		if (why == "") != ok {
			t.Errorf("dashboard.host %q: conflict %q, want ok=%v", host, why, ok)
		}
	}
}
