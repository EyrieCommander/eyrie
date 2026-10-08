package server

import (
	"net"
	"strings"

	"github.com/Audacity88/eyrie/internal/adapter"
)

// redactStatusForHost drops CurrentTask, which can quote the start of an
// agent's prompt, unless the management API is bound to loopback. The
// dashboard host is configurable (0.0.0.0 is accepted), and a prompt preview
// must not be served to the network.
func redactStatusForHost(st *adapter.AgentStatus, dashboardHost string) {
	if st == nil || hostIsLoopback(dashboardHost) {
		return
	}
	st.CurrentTask = ""
}

// hostIsLoopback reports whether a listen host binds only loopback. Empty
// (all interfaces), unspecified and unresolvable names are not loopback.
func hostIsLoopback(host string) bool {
	h := strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
