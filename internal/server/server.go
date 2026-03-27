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

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
)

//go:embed all:static
var staticFS embed.FS

// Server is the Eyrie web dashboard backend.
type Server struct {
	cfg    config.Config
	mux    *http.ServeMux
	server *http.Server
	hidden *config.HiddenStore
	events *EventBus
}

func New(cfg config.Config) *Server {
	hidden, err := config.NewHiddenStore()
	if err != nil {
		slog.Warn("failed to load hidden sessions store", "error", err)
		hidden = nil
	}
	s := &Server{cfg: cfg, hidden: hidden, events: NewEventBus()}
	s.mux = http.NewServeMux()
	s.registerRoutes()
	s.server = &http.Server{
		Handler:      corsHandler(s.mux),
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
	s.mux.HandleFunc("POST /api/agents/{name}/sessions/{session}/reset", s.handleResetSession)
	s.mux.HandleFunc("DELETE /api/agents/{name}/sessions/{session}/destroy", s.handleDestroySession)
	s.mux.HandleFunc("POST /api/agents/{name}/sessions/{session}/hide", s.handleHideSession)
	s.mux.HandleFunc("POST /api/agents/{name}/sessions/{session}/unhide", s.handleUnhideSession)
	s.mux.HandleFunc("PUT /api/agents/{name}/config", s.handleAgentConfigUpdate)
	s.mux.HandleFunc("POST /api/agents/{name}/config/validate", s.handleAgentConfigValidate)
	s.mux.HandleFunc("GET /api/agents/{name}/terminal/ws", s.handleTerminal)
	s.mux.HandleFunc("GET /api/agents/{name}/models", s.handleAgentModels)
	s.mux.HandleFunc("PUT /api/agents/{name}/display-name", s.handleUpdateDisplayName)

	// Registry and install endpoints
	s.mux.HandleFunc("GET /api/registry/frameworks", s.handleListFrameworks)
	s.mux.HandleFunc("GET /api/registry/frameworks/{id}", s.handleFrameworkDetail)
	s.mux.HandleFunc("POST /api/registry/install", s.handleInstallFramework)
	s.mux.HandleFunc("GET /api/registry/install/status", s.handleInstallStatus)

	// API reference (self-documenting, consumed by agents)
	s.mux.HandleFunc("GET /api/reference", s.handleAPIReference)

	// Instance endpoints
	s.mux.HandleFunc("GET /api/instances", s.handleListInstances)
	s.mux.HandleFunc("POST /api/instances", s.handleCreateInstance)
	s.mux.HandleFunc("GET /api/instances/{id}", s.handleGetInstance)
	s.mux.HandleFunc("PUT /api/instances/{id}", s.handleUpdateInstance)
	s.mux.HandleFunc("DELETE /api/instances/{id}", s.handleDeleteInstance)
	s.mux.HandleFunc("POST /api/instances/{id}/{action}", s.handleInstanceAction)

	// Project endpoints
	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	s.mux.HandleFunc("GET /api/projects/{id}", s.handleGetProject)
	s.mux.HandleFunc("PUT /api/projects/{id}", s.handleUpdateProject)
	s.mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	s.mux.HandleFunc("POST /api/projects/{id}/agents", s.handleAddProjectAgent)
	s.mux.HandleFunc("DELETE /api/projects/{id}/agents/{instanceId}", s.handleRemoveProjectAgent)
	s.mux.HandleFunc("GET /api/projects/{id}/chat", s.handleProjectChatMessages)
	s.mux.HandleFunc("POST /api/projects/{id}/chat", s.handleProjectChatSend)
	s.mux.HandleFunc("DELETE /api/projects/{id}/chat", s.handleProjectChatClear)
	s.mux.HandleFunc("GET /api/projects/{id}/activity", s.handleProjectActivity)
	s.mux.HandleFunc("GET /api/projects/{id}/events", s.handleProjectEvents)

	// Metrics
	s.mux.HandleFunc("GET /api/metrics", s.handleMetrics)

	// Hierarchy endpoints
	s.mux.HandleFunc("GET /api/hierarchy", s.handleGetHierarchy)
	s.mux.HandleFunc("POST /api/hierarchy/commander", s.handleSetCommander)
	s.mux.HandleFunc("POST /api/hierarchy/commander/brief", s.handleBriefCommander)
	s.mux.HandleFunc("POST /api/projects/{id}/captain/brief", s.handleBriefCaptain)

	// Persona endpoints (also aliased under /api/registry/ for consistency)
	s.mux.HandleFunc("GET /api/registry/personas", s.handleListPersonas)
	s.mux.HandleFunc("GET /api/personas", s.handleListPersonas)
	s.mux.HandleFunc("GET /api/personas/categories", s.handleListCategories)
	s.mux.HandleFunc("GET /api/personas/{id}", s.handleGetPersona)
	s.mux.HandleFunc("POST /api/personas/install", s.handleInstallPersona)
	s.mux.HandleFunc("PUT /api/personas/{id}", s.handleUpdatePersona)
	s.mux.HandleFunc("DELETE /api/personas/{id}", s.handleDeletePersona)

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

// corsHandler wraps a handler with permissive CORS headers for development.
// In production the frontend is served from the same origin so CORS is a no-op.
func corsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
