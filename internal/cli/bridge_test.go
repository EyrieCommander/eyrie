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
