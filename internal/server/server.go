package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/commander"
	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/project"
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
	vault  *config.KeyVault

	// WHY cached stores: Handlers previously called NewStore() per request
	// (38 call sites across projects.go, instances.go, hierarchy.go). Each
	// call does os.UserHomeDir() + os.MkdirAll(). These are initialized
	// once in New() and shared across all requests. Thread safety is handled
	// by each store's internal RWMutex.
	projectStore  *project.Store
	chatStore     *project.ChatStore
	instanceStore *instance.Store

	// commander is the built-in LLM-driven orchestrator. The user chats
	// with it directly via /api/commander/chat. It has direct access to
	// the project store via its tool registry.
	//
	// May be nil at startup if no API key is configured. The handler
	// guard (commanderAvailable) attempts lazy initialization on each
	// request so the user can add a key via the Settings page and the
	// commander comes online without a server restart.
	commander   *commander.Commander
	commanderMu sync.Mutex

	// activeChats stores cancel functions for in-flight project chat
	// orchestrations. Keyed by project ID. Used by the stop endpoint
	// to cancel the detached agent context.
	activeChats sync.Map // map[string]context.CancelFunc

	// pairAttempted tracks which agents we've already tried to auto-pair
	// so we don't spawn goroutines on every discovery poll.
	pairAttempted sync.Map // map[string]bool
}

func New(cfg config.Config) (*Server, error) {
	hidden, err := config.NewHiddenStore()
	if err != nil {
		slog.Warn("failed to load hidden sessions store", "error", err)
		hidden = nil
	}
	vault, err := config.NewKeyVault()
	if err != nil {
		slog.Warn("failed to load key vault", "error", err)
		vault = config.GetKeyVault() // fallback to singleton (empty if both fail)
	}
	projStore, err := project.NewStore()
	if err != nil {
		return nil, fmt.Errorf("project store: %w", err)
	}
	chatSt, err := project.NewChatStore()
	if err != nil {
		return nil, fmt.Errorf("chat store: %w", err)
	}
	instStore, err := instance.NewStore()
	if err != nil {
		return nil, fmt.Errorf("instance store: %w", err)
	}
	s := &Server{
		cfg:           cfg,
		hidden:        hidden,
		vault:         vault,
		events:        NewEventBus(),
		projectStore:  projStore,
		chatStore:     chatSt,
		instanceStore: instStore,
	}
	// Commander is constructed AFTER s is populated so its tools can
	// receive method values of server methods (runDiscovery, send, etc.).
	// Method values close over the receiver pointer, so the callbacks
	// will have access to the fully-initialized server when invoked.
	cmd, err := commander.NewDefault(commander.DefaultConfig{
		Projects:      projStore,
		Chat:          chatSt,
		Discovery:     s.runDiscovery,
		SendToProject: s.sendCommanderMessageToProject,
		RestartAgent:  s.restartAgentByName,
		Vault:         vault,
	})
	if err != nil {
		// Non-fatal: the server runs without a commander. The chat
		// endpoints return 503 and the UI shows a "configure API key"
		// card. The user can set a key via the Settings page and restart.
		slog.Warn("commander unavailable (set OPENROUTER_API_KEY or ANTHROPIC_API_KEY)", "error", err)
	}
	s.commander = cmd // may be nil
	s.mux = http.NewServeMux()
	s.registerRoutes()
	s.server = &http.Server{
		Handler:      corsHandler(s.mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streams need unbounded writes
		IdleTimeout:  60 * time.Second,
	}
	return s, nil
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
	s.mux.HandleFunc("GET /api/terminal/ws", s.handleShellTerminal)
	s.mux.HandleFunc("GET /api/agents/{name}/models", s.handleAgentModels)
	s.mux.HandleFunc("PUT /api/agents/{name}/display-name", s.handleUpdateDisplayName)

	// Registry and install endpoints
	s.mux.HandleFunc("GET /api/registry/frameworks", s.handleListFrameworks)
	s.mux.HandleFunc("GET /api/registry/frameworks/{id}", s.handleFrameworkDetail)
	s.mux.HandleFunc("POST /api/registry/install", s.handleInstallFramework)
	s.mux.HandleFunc("GET /api/registry/install/status", s.handleInstallStatus)
	s.mux.HandleFunc("GET /api/registry/install/{id}/logs", s.handleInstallLogs)
	s.mux.HandleFunc("POST /api/registry/uninstall", s.handleUninstallFramework)
	s.mux.HandleFunc("GET /api/registry/frameworks/{id}/config", s.handleFrameworkConfigRead)
	s.mux.HandleFunc("PUT /api/registry/frameworks/{id}/config", s.handleFrameworkConfigPatch)
	s.mux.HandleFunc("GET /api/registry/frameworks/{id}/health", s.handleFrameworkHealthProxy)

	// API reference (self-documenting, consumed by agents)
	s.mux.HandleFunc("GET /api/reference", s.handleAPIReference)

	// Instance endpoints
	s.mux.HandleFunc("GET /api/instances", s.handleListInstances)
	s.mux.HandleFunc("POST /api/instances", s.handleCreateInstance)
	s.mux.HandleFunc("GET /api/instances/{id}", s.handleGetInstance)
	s.mux.HandleFunc("PUT /api/instances/{id}", s.handleUpdateInstance)
	s.mux.HandleFunc("DELETE /api/instances/{id}", s.handleDeleteInstance)
	s.mux.HandleFunc("POST /api/instances/migrate", s.handleMigrateInstances)
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
	s.mux.HandleFunc("GET /api/projects/{id}/chat/status", s.handleProjectChatStatus)
	s.mux.HandleFunc("POST /api/projects/{id}/chat/stop", s.handleProjectChatStop)
	s.mux.HandleFunc("DELETE /api/projects/{id}/chat", s.handleProjectChatClear)
	s.mux.HandleFunc("POST /api/projects/{id}/reset", s.handleProjectReset)
	s.mux.HandleFunc("GET /api/projects/{id}/activity", s.handleProjectActivity)
	s.mux.HandleFunc("GET /api/projects/{id}/events", s.handleProjectEvents)

	// Commander (built-in LLM orchestrator — the user's chat surface)
	s.mux.HandleFunc("POST /api/commander/chat", s.handleCommanderChat)
	s.mux.HandleFunc("GET /api/commander/history", s.handleCommanderHistory)
	s.mux.HandleFunc("DELETE /api/commander/history", s.handleCommanderClear)
	s.mux.HandleFunc("POST /api/commander/confirm/{id}", s.handleCommanderConfirm)
	s.mux.HandleFunc("GET /api/commander/memory", s.handleCommanderMemory)

	// Metrics
	s.mux.HandleFunc("GET /api/metrics", s.handleMetrics)

	// Hierarchy endpoints
	s.mux.HandleFunc("GET /api/hierarchy", s.handleGetHierarchy)
	s.mux.HandleFunc("GET /api/hierarchy/commander", s.handleGetCommander)
	s.mux.HandleFunc("POST /api/projects/{id}/captain/brief", s.handleBriefCaptain)

	// Key vault endpoints
	s.mux.HandleFunc("GET /api/keys", s.handleListKeys)
	s.mux.HandleFunc("PUT /api/keys/{provider}", s.handleSetKey)
	s.mux.HandleFunc("DELETE /api/keys/{provider}", s.handleDeleteKey)
	s.mux.HandleFunc("POST /api/keys/{provider}/validate", s.handleValidateKey)

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

// runDiscovery is a helper used by API handlers. After discovery, it
// triggers auto-pairing for any alive ZeroClaw agents that lack a token.
func (s *Server) runDiscovery(ctx context.Context) discovery.Result {
	result := discovery.Run(ctx, s.cfg)

	// Auto-pair alive ZeroClaw agents that have no token. The attempt map
	// prevents concurrent pairing goroutines for the same agent. On failure,
	// the entry is cleared so the next discovery cycle retries.
	for _, ar := range result.Agents {
		if ar.Alive && ar.Agent.Framework == adapter.FrameworkZeroClaw && ar.Agent.Token == "" {
			if _, loaded := s.pairAttempted.LoadOrStore(ar.Agent.Name, true); !loaded {
				agentName := ar.Agent.Name
				agentPort := ar.Agent.Port
				go func() {
					autoPairZeroClaw(agentName, agentPort)
					// Check if token was actually stored — if not, clear
					// the flag so we retry on next discovery cycle.
					if ts, err := config.NewTokenStore(); err == nil {
						if ts.Get(agentName) == "" {
							s.pairAttempted.Delete(agentName)
						}
					}
				}()
			}
		}
	}

	return result
}

// corsHandler wraps a handler with CORS headers. Only localhost origins are
// reflected (for Vite dev server). In production the frontend is same-origin
// so no CORS header is needed. Non-matching origins get no CORS header,
// causing the browser to block the cross-origin request.
func corsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isLocalhostOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLocalhostOrigin checks whether an Origin header refers to a localhost address.
func isLocalhostOrigin(origin string) bool {
	// Match http://localhost:*, http://127.0.0.1:*, http://[::1]:*
	for _, prefix := range []string{
		"http://localhost", "https://localhost",
		"http://127.0.0.1", "https://127.0.0.1",
		"http://[::1]", "https://[::1]",
	} {
		if origin == prefix || len(origin) > len(prefix) && origin[:len(prefix)] == prefix && origin[len(prefix)] == ':' {
			return true
		}
	}
	return false
}
