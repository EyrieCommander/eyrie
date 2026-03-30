package server

import (
	"errors"
	"net/http"

	"github.com/Audacity88/eyrie/internal/adapter"
)

// writeAdapterError writes a structured JSON error response with the
// appropriate HTTP status code based on the adapter error type.
//
// WHY this exists: Without it, all adapter errors (401 auth expired,
// connection refused, timeout, etc.) are returned as 500 Internal Server
// Error. The frontend can't distinguish "agent is down" from "token
// expired" from a real bug. This helper maps sentinel errors from the
// adapter package to proper HTTP semantics so the UI can show targeted
// guidance (e.g. "re-pair" button for 401, "start agent" for 502).
func writeAdapterError(w http.ResponseWriter, err error) {
	status := adapterHTTPStatus(err)
	code := adapterErrorCode(err)
	// Expose the real error message for known sentinel errors (auth expired,
	// agent unreachable, etc.) since those are actionable. For unrecognized
	// errors (500), use a generic message to avoid leaking internal details.
	msg := "internal server error"
	if code != "internal_error" {
		msg = err.Error()
	}
	writeJSON(w, status, map[string]string{
		"error": msg,
		"code":  code,
	})
}

// adapterHTTPStatus maps adapter sentinel errors to HTTP status codes.
func adapterHTTPStatus(err error) int {
	switch {
	case errors.Is(err, adapter.ErrUnauthorized):
		return http.StatusUnauthorized // 401
	case errors.Is(err, adapter.ErrForbidden):
		return http.StatusForbidden // 403
	case errors.Is(err, adapter.ErrAgentUnreachable):
		return http.StatusBadGateway // 502
	case errors.Is(err, adapter.ErrAgentTimeout):
		return http.StatusGatewayTimeout // 504
	default:
		return http.StatusInternalServerError // 500
	}
}

// adapterErrorCode returns a machine-readable error code for frontend
// consumption. The frontend matches on these strings to decide which UI
// state to show (error banner, re-pair prompt, "start agent" hint, etc.).
func adapterErrorCode(err error) string {
	switch {
	case errors.Is(err, adapter.ErrUnauthorized):
		return "auth_expired"
	case errors.Is(err, adapter.ErrForbidden):
		return "access_denied"
	case errors.Is(err, adapter.ErrAgentUnreachable):
		return "agent_unreachable"
	case errors.Is(err, adapter.ErrAgentTimeout):
		return "agent_timeout"
	default:
		return "internal_error"
	}
}
