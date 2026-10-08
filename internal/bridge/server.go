package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBodyBytes   = 64 << 10
	maxReplyBytes  = 32 << 10
	maxConcurrent  = 4
	ratePerMinute  = 60
	rateBurst      = 20
	authFailPerMin = 10
)

// Server is the bridge listener. It has its own mux; nothing from the
// management API is registered here and nothing here proxies to it.
type Server struct {
	cfg     Config
	fs      *FS
	svc     *Service
	log     *AccessLog
	limits  *limiter
	sem     chan struct{}
	handler http.Handler
	http    *http.Server
}

// NewServer builds the bridge handler. svc may be nil (fs-only bridge).
func NewServer(cfg Config, fsys *FS, svc *Service, log *AccessLog) *Server {
	s := &Server{
		cfg:    cfg,
		fs:     fsys,
		svc:    svc,
		log:    log,
		limits: newLimiter(),
		sem:    make(chan struct{}, maxConcurrent),
	}
	s.handler = s.wrap(s.route)
	s.http = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return s
}

// Handler exposes the full bridge handler (used by tests).
func (s *Server) Handler() http.Handler { return s.handler }

// Addr is the loopback address the bridge binds. Never 0.0.0.0.
func (s *Server) Addr() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.cfg.Port)) }

// Serve listens on 127.0.0.1:<port> and serves until Shutdown.
func (s *Server) Serve() error {
	ln, err := net.Listen("tcp", s.Addr())
	if err != nil {
		return fmt.Errorf("bridge listen %s: %w", s.Addr(), err)
	}
	return s.ServeListener(ln)
}

// ServeListener serves on an existing listener (tests).
func (s *Server) ServeListener(ln net.Listener) error {
	err := s.http.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops the listener.
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

// reqInfo is filled by handlers for the access log. Never content.
type reqInfo struct {
	root string
	path string
}

type ctxKey struct{}

func info(r *http.Request) *reqInfo {
	if ri, ok := r.Context().Value(ctxKey{}).(*reqInfo); ok {
		return ri
	}
	return &reqInfo{}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// clientIP is the peer address, or the configured forwarding header when the
// peer is loopback (Funnel and cloudflared both connect from localhost).
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.cfg.ClientIPHeader != "" {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if v := r.Header.Get(s.cfg.ClientIPHeader); v != "" {
				first := strings.TrimSpace(strings.Split(v, ",")[0])
				if net.ParseIP(first) != nil {
					return first
				}
			}
		}
	}
	return host
}

// wrap applies, in order: hygiene headers, body cap, access log, auth-fail
// throttle, auth, per-token rate limit, concurrency cap.
func (s *Server) wrap(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ri := &reqInfo{}
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, ri))
		sw := &statusWriter{ResponseWriter: w}
		h := sw.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		r.Body = http.MaxBytesReader(sw, r.Body, maxBodyBytes)
		ip := s.clientIP(r)
		authFailed := false
		defer func() {
			if s.log != nil {
				s.log.Write(AccessEntry{
					Time:       start.UTC().Format(time.RFC3339Nano),
					Remote:     ip,
					Method:     r.Method,
					Route:      routeLabel(r.URL.Path),
					Root:       ri.root,
					Path:       ri.path,
					Status:     sw.status,
					Bytes:      sw.bytes,
					DurationMS: time.Since(start).Milliseconds(),
					AuthFailed: authFailed,
				})
			}
		}()

		if s.limits.authBlocked(ip) {
			tooMany(sw, time.Minute)
			return
		}
		tok, ok := bearer(r)
		if !ok || !tokenMatches(tok, s.cfg.BridgeTokenSHA256) {
			authFailed = true
			s.limits.authFail(ip)
			sw.WriteHeader(http.StatusUnauthorized)
			return
		}
		if wait, ok := s.limits.allow(s.cfg.BridgeTokenSHA256); !ok {
			tooMany(sw, wait)
			return
		}
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		default:
			tooMany(sw, time.Second)
			return
		}
		next(sw, r)
	})
}

func tooMany(w http.ResponseWriter, wait time.Duration) {
	secs := int((wait + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return "", false
	}
	return strings.TrimSpace(h[len(p):]), true
}

// routeLabel maps a path to a fixed label so the log never records ids or
// arbitrary client paths in the route field.
func routeLabel(p string) string {
	switch {
	case p == "/bridge/v1/reply":
		return "reply"
	case strings.HasPrefix(p, "/bridge/v1/prompts/"):
		return "prompts"
	case p == "/bridge/v1/fs/roots":
		return "fs.roots"
	case p == "/bridge/v1/fs/list":
		return "fs.list"
	case p == "/bridge/v1/fs/read":
		return "fs.read"
	case p == "/bridge/v1/fs/search":
		return "fs.search"
	default:
		return "other"
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
}

// route is an explicit switch over the only paths the bridge has. Any other
// path (including every /api/* management route) is 404. Paths are matched
// on the decoded URL path; there is no prefix routing.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/bridge/v1/reply":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		s.handleReply(w, r)
	case strings.HasPrefix(p, "/bridge/v1/prompts/") && !strings.Contains(p[len("/bridge/v1/prompts/"):], "/"):
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		s.handlePrompt(w, r, p[len("/bridge/v1/prompts/"):])
	case p == "/bridge/v1/fs/roots", p == "/bridge/v1/fs/list", p == "/bridge/v1/fs/read", p == "/bridge/v1/fs/search":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		if s.fs == nil || len(s.fs.roots) == 0 {
			notFound(w) // no roots configured: every fs call 404s
			return
		}
		switch p {
		case "/bridge/v1/fs/roots":
			writeJSON(w, http.StatusOK, map[string]any{"roots": s.fs.Aliases()})
		case "/bridge/v1/fs/list":
			s.handleList(w, r)
		case "/bridge/v1/fs/read":
			s.handleRead(w, r)
		case "/bridge/v1/fs/search":
			s.handleSearch(w, r)
		}
	default:
		notFound(w)
	}
}

func fsFail(w http.ResponseWriter, err error) {
	var fe *fsError
	if errors.As(err, &fe) {
		writeJSON(w, fe.status, map[string]string{"error": fe.code})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

func (s *Server) fsArgs(r *http.Request) (root, path string) {
	q := r.URL.Query()
	root, path = q.Get("root"), q.Get("path")
	ri := info(r)
	ri.root = root
	// Log the client's relative path, truncated; never file contents.
	if utf8.RuneCountInString(path) > 512 {
		path2 := []rune(path)[:512]
		ri.path = string(path2)
	} else {
		ri.path = path
	}
	return root, path
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	root, path := s.fsArgs(r)
	entries, truncated, err := s.fs.List(root, path)
	if err != nil {
		fsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "truncated": truncated})
}

func intParam(r *http.Request, name string, def, min, max int) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return 0, false
	}
	if n > max {
		n = max
	}
	return n, true
}

func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	root, path := s.fsArgs(r)
	offset, ok1 := intParam(r, "offset", 1, 1, 1<<30)
	limit, ok2 := intParam(r, "limit", defaultReadLines, 1, maxReadLines)
	if !ok1 || !ok2 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_range"})
		return
	}
	res, err := s.fs.Read(root, path, offset, limit)
	if err != nil {
		fsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	root, path := s.fsArgs(r)
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" || len(q) > 256 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_query"})
		return
	}
	max, ok := intParam(r, "max", maxSearchHits, 1, maxSearchHits)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_max"})
		return
	}
	hits, truncated, err := s.fs.Search(root, path, q, max)
	if err != nil {
		fsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "truncated": truncated})
}

type replyBody struct {
	ConversationID string `json:"conversation_id"`
	InReplyTo      string `json:"in_reply_to"`
	ReplyID        string `json:"reply_id"`
	Text           string `json:"text"`
	Final          *bool  `json:"final"`
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	if s.svc == nil {
		notFound(w)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
		return
	}
	var b replyBody
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_json"})
		return
	}
	if b.ConversationID == "" || b.InReplyTo == "" || b.ReplyID == "" || len(b.ReplyID) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_fields"})
		return
	}
	if len(b.Text) > maxReplyBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too_large"})
		return
	}
	if !utf8.ValidString(b.Text) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_text"})
		return
	}
	final := true
	if b.Final != nil {
		final = *b.Final
	}
	dup, err := s.svc.Store.AddReply(b.ConversationID, b.InReplyTo, b.ReplyID, b.Text, final)
	if errors.Is(err, ErrUnknownMessage) {
		notFound(w)
		return
	}
	if err != nil {
		slog.Warn("bridge: store reply failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if !dup {
		s.svc.notify(b.ConversationID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": dup})
}

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request, id string) {
	if s.svc == nil || id == "" {
		notFound(w)
		return
	}
	m, err := s.svc.Store.GetMessage(id)
	if err != nil {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": m.ConversationID,
		"message_id":      m.MessageID,
		"text":            m.Text,
		"ts":              m.TS.UTC().Format(time.RFC3339),
		"state":           m.State,
	})
}
