package server

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// browserGuard protects the management API from requests a browser makes
// on behalf of some other website. CORS only stops a page from *reading*
// responses; it does not stop the request (a form POST, a text/plain fetch)
// and does not apply to WebSockets at all, so without this any page Dan
// visits could POST to agent actions or open /api/terminal/ws.
//
// Rules, each checked on its own:
//
//  1. Host (every request): must name this machine as configured. With the
//     default loopback bind that means 127.0.0.0/8, ::1 or localhost, which
//     blocks DNS rebinding (an attacker's domain resolved to 127.0.0.1 is
//     same-origin with itself, so only Host gives it away). A specific
//     configured bind address is also allowed. A wildcard bind (0.0.0.0, ::,
//     empty) disables only this rule, with a startup warning, because the
//     reachable names are unknown; rules 2-3 still apply.
//  2. Sec-Fetch-Site (state-changing requests and WebSocket upgrades): when
//     present it must be same-origin or none.
//  3. Origin (same requests): when present it must equal this request's own
//     origin, i.e. the scheme it actually arrived on plus Host.
//     X-Forwarded-Proto is ignored. The Vite dev proxy keeps the dev
//     server's Host and Origin, so dev requests match.
//
// Requests with neither Origin nor Sec-Fetch-Site come from non-browser
// clients (curl, the eyrie CLI, agents) and are allowed: a browser always
// sends Origin on cross-origin POSTs and on every WebSocket handshake.
type browserGuard struct {
	next       http.Handler
	checkHost  bool
	extraHosts map[string]bool // lower-cased hostnames allowed besides loopback
}

func newBrowserGuard(next http.Handler, bindHost string) *browserGuard {
	g := &browserGuard{next: next, checkHost: true, extraHosts: map[string]bool{}}
	h := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(bindHost), "["), "]"))
	switch {
	case h == "" || h == "0.0.0.0" || h == "::":
		g.checkHost = false
		slog.Warn("management API binds every interface; Host checks are off (Origin checks still apply). Bind 127.0.0.1 unless remote access is intended.", "host", bindHost)
	case isLoopbackName(h):
		// default: loopback names only
	default:
		g.extraHosts[h] = true
	}
	return g
}

func (g *browserGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.checkHost && !g.hostAllowed(r.Host) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden host"})
		return
	}
	if stateChanging(r) {
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request refused"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != ownOrigin(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
			return
		}
	}
	g.next.ServeHTTP(w, r)
}

func (g *browserGuard) hostAllowed(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
	return isLoopbackName(host) || g.extraHosts[host]
}

// stateChanging: anything but GET/HEAD/OPTIONS, plus WebSocket upgrades
// (a GET that opens a live channel, e.g. a terminal).
func stateChanging(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return isWebSocketUpgrade(r)
	}
	return true
}

func isWebSocketUpgrade(r *http.Request) bool {
	for _, v := range r.Header.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), "upgrade") && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				return true
			}
		}
	}
	return false
}

// ownOrigin is scheme://Host for the connection this request arrived on.
func ownOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func isLoopbackName(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
