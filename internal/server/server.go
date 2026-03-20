package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/discovery"
)

//go:embed all:static
var staticFS embed.FS

// Server is the Eyrie web dashboard backend.
type Server struct {
	cfg    config.Config
	mux    *http.ServeMux
	server *http.Server
	hidden *config.HiddenStore
}

func New(cfg config.Config) *Server {
	hidden, err := config.NewHiddenStore()
	if err != nil {
		slog.Warn("failed to load hidden sessions store", "error", err)
		hidden = nil
	}
	s := &Server{cfg: cfg, hidden: hidden}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	s.server = &http.Server{
		Handler:      s.mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streams need unbounded writes
		IdleTimeout:  60 * time.Second,
	}
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/agents", s.handleListAgents)
	s.mux.HandleFunc("GET /api/agents/{name}/config", s.handleAgentConfig)
	s.mux.HandleFunc("POST /api/agents/{name}/{action}", s.handleAgentAction)
	s.mux.HandleFunc("GET /api/agents/{name}/logs", s.handleAgentLogs)
	s.mux.HandleFunc("GET /api/agents/{name}/activity", s.handleAgentActivity)
	s.mux.HandleFunc("GET /api/agents/{name}/sessions", s.handleAgentSessions)
	s.mux.HandleFunc("POST /api/agents/{name}/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/agents/{name}/sessions/{session}/messages", s.handleAgentMessages)
	s.mux.HandleFunc("POST /api/agents/{name}/chat", s.handleAgentChat)
	s.mux.HandleFunc("DELETE /api/agents/{name}/sessions/{session}", s.handleDeleteSession)
	s.mux.HandleFunc("DELETE /api/agents/{name}/sessions/{session}/purge", s.handlePurgeSession)
	s.mux.HandleFunc("POST /api/agents/{name}/sessions/{session}/hide", s.handleHideSession)
	s.mux.HandleFunc("POST /api/agents/{name}/sessions/{session}/unhide", s.handleUnhideSession)
	s.mux.HandleFunc("PUT /api/agents/{name}/config", s.handleAgentConfigUpdate)
	s.mux.HandleFunc("POST /api/agents/{name}/config/validate", s.handleAgentConfigValidate)
	s.mux.HandleFunc("GET /api/agents/{name}/terminal/ws", s.handleTerminal)

	// Registry and install endpoints
	s.mux.HandleFunc("GET /api/registry/frameworks", s.handleListFrameworks)
	s.mux.HandleFunc("GET /api/registry/frameworks/{id}", s.handleFrameworkDetail)
	s.mux.HandleFunc("POST /api/registry/install", s.handleInstallFramework)
	s.mux.HandleFunc("GET /api/registry/install/status", s.handleInstallStatus)

	// Serve embedded frontend
	distFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		slog.Error("failed to create sub filesystem for static assets", "error", err)
		return
	}
	fileServer := http.FileServer(http.FS(distFS))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// For SPA routing: serve index.html for paths that don't match a file
		path := r.URL.Path
		if path != "/" {
			// Try to open the file; if it doesn't exist, serve index.html
			f, err := distFS.Open(path[1:]) // strip leading /
			if err != nil {
				r.URL.Path = "/"
			} else {
				f.Close()
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Dashboard.Host, s.cfg.Dashboard.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	return s.server.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// runDiscovery is a helper used by API handlers.
func (s *Server) runDiscovery(ctx context.Context) discovery.Result {
	return discovery.Run(ctx, s.cfg)
}
