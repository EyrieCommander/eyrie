package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/fileutil"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/manager"
	"github.com/Audacity88/eyrie/internal/project"
)

// CommanderInfo represents the commander, which can be either a
// provisioned instance or an existing legacy agent.
type CommanderInfo struct {
	// If it's a provisioned instance, these come from instance.Instance
	ID            string `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Framework     string `json:"framework"`
	Port          int    `json:"port"`
	Status        string `json:"status"`
	HierarchyRole string `json:"hierarchy_role"`
	// Whether this is a legacy agent (discovered, not provisioned)
	Legacy bool `json:"legacy"`
}

// HierarchyTree is the full tree response: commander → projects → agents.
type HierarchyTree struct {
	Commander *CommanderInfo `json:"commander,omitempty"`
	Projects    []ProjectTree    `json:"projects"`
}

type ProjectTree struct {
	Project      project.Project     `json:"project"`
	Captain *instance.Instance  `json:"captain,omitempty"`
	Talons       []instance.Instance `json:"talons"`
}

func (s *Server) handleGetHierarchy(w http.ResponseWriter, r *http.Request) {
	instStore, err := instance.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	projStore, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	instances, err := instStore.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list instances: " + err.Error()})
		return
	}
	projects, err := projStore.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list projects: " + err.Error()})
		return
	}

	// Build instance lookup
	instByID := make(map[string]instance.Instance, len(instances))
	for _, inst := range instances {
		instByID[inst.ID] = inst
	}

	// Find commander — can be an instance ID or a legacy agent name
	var commander *CommanderInfo
	coordRef := loadCommanderRef()
	if coordRef.InstanceID != "" {
		if c, ok := instByID[coordRef.InstanceID]; ok {
			commander = &CommanderInfo{
				ID: c.ID, Name: c.Name, DisplayName: c.DisplayName,
				Framework: c.Framework, Port: c.Port, Status: string(c.Status),
				HierarchyRole: string(c.HierarchyRole),
			}
		}
	} else if coordRef.AgentName != "" {
		// Legacy agent — look it up via discovery
		disc := s.runDiscovery(r.Context())
		for _, ar := range disc.Agents {
			if ar.Agent.Name == coordRef.AgentName {
				status := "stopped"
				if ar.Alive {
					status = "running"
				}
				displayName := readWorkspaceField(ar.Agent.ConfigPath, "IDENTITY.md", "Name:")
				if displayName == "" {
					displayName = coordRef.AgentName
				}
				commander = &CommanderInfo{
					ID: coordRef.AgentName, Name: coordRef.AgentName,
					DisplayName: displayName,
					Framework: ar.Agent.Framework, Port: ar.Agent.Port,
					Status: status, HierarchyRole: "commander", Legacy: true,
				}
				break
			}
		}
	}

	// Build project trees
	var trees []ProjectTree
	for _, p := range projects {
		tree := ProjectTree{
			Project:    p,
			Talons: []instance.Instance{},
		}
		if p.OrchestratorID != "" {
			if o, ok := instByID[p.OrchestratorID]; ok {
				tree.Captain = &o
			}
		}
		for _, aid := range p.RoleAgentIDs {
			if a, ok := instByID[aid]; ok {
				tree.Talons = append(tree.Talons, a)
			}
		}
		trees = append(trees, tree)
	}
	if trees == nil {
		trees = []ProjectTree{}
	}

	writeJSON(w, http.StatusOK, HierarchyTree{
		Commander: commander,
		Projects:    trees,
	})
}

// handleGetCommander returns just the commander info without building the full
// hierarchy tree. For provisioned instances this reads a JSON file + instance
// metadata — no discovery scan needed. Much faster than GET /api/hierarchy.
func (s *Server) handleGetCommander(w http.ResponseWriter, r *http.Request) {
	ref := loadCommanderRef()
	if ref.InstanceID == "" && ref.AgentName == "" {
		writeJSON(w, http.StatusOK, map[string]any{"commander": nil})
		return
	}

	if ref.InstanceID != "" {
		instStore, err := instance.NewStore()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		inst, err := instStore.Get(ref.InstanceID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"commander": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"commander": CommanderInfo{
				ID: inst.ID, Name: inst.Name, DisplayName: inst.DisplayName,
				Framework: inst.Framework, Port: inst.Port,
				Status: string(inst.Status), HierarchyRole: string(inst.HierarchyRole),
			},
		})
		return
	}

	// Legacy agent — needs discovery for status
	disc := s.runDiscovery(r.Context())
	for _, ar := range disc.Agents {
		if ar.Agent.Name == ref.AgentName {
			status := "stopped"
			if ar.Alive {
				status = "running"
			}
			displayName := readWorkspaceField(ar.Agent.ConfigPath, "IDENTITY.md", "Name:")
			if displayName == "" {
				displayName = ref.AgentName
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"commander": CommanderInfo{
					ID: ref.AgentName, Name: ref.AgentName,
					DisplayName: displayName,
					Framework: ar.Agent.Framework, Port: ar.Agent.Port,
					Status: status, HierarchyRole: "commander", Legacy: true,
				},
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commander": nil})
}

func (s *Server) handleSetCommander(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID string `json:"instance_id,omitempty"`
		AgentName  string `json:"agent_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if body.InstanceID == "" && body.AgentName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance_id or agent_name is required"})
		return
	}
	if body.InstanceID != "" && body.AgentName != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provide either instance_id or agent_name, not both"})
		return
	}

	// Verify the target exists
	if body.InstanceID != "" {
		store, err := instance.NewStore()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if _, err := store.Get(body.InstanceID); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "instance not found"})
			return
		}
	} else {
		// Verify the legacy agent exists via discovery
		disc := s.runDiscovery(r.Context())
		found := false
		for _, ar := range disc.Agents {
			if ar.Agent.Name == body.AgentName {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
			return
		}
	}

	ref := commanderRef{InstanceID: body.InstanceID, AgentName: body.AgentName}
	if err := saveCommanderRef(ref); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBriefCommander sends the Commander briefing message to the agent
// and returns the session key so the frontend can navigate to it.
func (s *Server) handleBriefCommander(w http.ResponseWriter, r *http.Request) {
	ref := loadCommanderRef()
	if ref.AgentName == "" && ref.InstanceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no commander set"})
		return
	}

	// Find the agent — match by AgentName or InstanceID
	disc := s.runDiscovery(r.Context())
	var found *discovery.AgentResult
	for i := range disc.Agents {
		a := disc.Agents[i].Agent
		if (ref.AgentName != "" && a.Name == ref.AgentName) ||
			(ref.InstanceID != "" && a.InstanceID == ref.InstanceID) {
			found = &disc.Agents[i]
			break
		}
	}
	agentName := ref.AgentName
	if agentName == "" {
		agentName = ref.InstanceID
	}
	if found == nil || !found.Alive {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "commander agent not found or not running"})
		return
	}

	agent := discovery.NewAgent(found.Agent)
	briefing := composeBriefing()

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// resetExisting=false: idempotent — if already briefed, just return the session key
	streamBriefing(r.Context(), agent, agentName, "eyrie-commander-briefing", briefing, sse, false)
}

// handleBriefCaptain sends a project-scoped briefing to a captain agent.
// POST /api/projects/{id}/captain/brief
func (s *Server) handleBriefCaptain(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	// Load project
	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open project store"})
		return
	}
	proj, err := store.Get(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}
	if proj.OrchestratorID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project has no captain assigned"})
		return
	}

	// Find the captain agent — auto-start if it's a stopped instance
	disc := s.runDiscovery(r.Context())
	var found *discovery.AgentResult
	for i := range disc.Agents {
		a := disc.Agents[i].Agent
		if a.Name == proj.OrchestratorID || a.InstanceID == proj.OrchestratorID {
			found = &disc.Agents[i]
			break
		}
	}
	if found == nil || !found.Alive {
		// Try to auto-start if it's a provisioned instance
		instStore, instErr := instance.NewStore()
		if instErr == nil {
			if inst, getErr := instStore.Get(proj.OrchestratorID); getErr == nil {
				slog.Info("auto-starting captain for briefing", "instance", inst.Name)
				if startErr := manager.ExecuteWithConfig(r.Context(), inst.Framework, inst.ConfigPath, manager.ActionStart); startErr != nil {
					slog.Warn("failed to auto-start captain", "instance", inst.Name, "error", startErr)
					writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "captain is stopped and failed to start: " + startErr.Error()})
					return
				}
				if err := instStore.UpdateStatus(inst.ID, instance.StatusStarting); err != nil {
				slog.Warn("failed to update instance status to starting", "instance", inst.ID, "error", err)
			}
				// Wait for agent to become reachable (poll discovery)
				for attempt := 0; attempt < 10; attempt++ {
					select {
					case <-time.After(time.Second):
					case <-r.Context().Done():
						writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "request cancelled while waiting for captain to start"})
						return
					}
					disc = s.runDiscovery(r.Context())
					for i := range disc.Agents {
						a := disc.Agents[i].Agent
						if a.Name == proj.OrchestratorID || a.InstanceID == proj.OrchestratorID {
							found = &disc.Agents[i]
							break
						}
					}
					if found != nil && found.Alive {
						_ = instStore.UpdateStatus(inst.ID, instance.StatusRunning)
						break
					}
					found = nil
				}
			}
		}
		if found == nil || !found.Alive {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "captain agent not found or not running"})
			return
		}
	}

	agent := discovery.NewAgent(found.Agent)
	briefing := composeCaptainBriefing(proj)
	sessionName := "eyrie-captain-briefing"

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// resetExisting=true: always re-brief the captain (project context may have changed)
	agentName := proj.OrchestratorID
	streamBriefing(r.Context(), agent, agentName, sessionName, briefing, sse, true)
}

func composeCaptainBriefing(proj *project.Project) string {
	text, err := renderBriefing("captain-general.md", BriefingContext{
		ProjectName: proj.Name,
		ProjectID:   proj.ID,
		Goal:        proj.Goal,
		Description: proj.Description,
		AgentName:   proj.OrchestratorID,
	})
	if err != nil {
		slog.Warn("failed to render captain briefing template", "error", err)
		return fmt.Sprintf("You are the Captain of the %q project.", proj.Name)
	}
	return text
}

// readWorkspaceField reads a workspace file and extracts a value after a label like "**Name:**"
func readWorkspaceField(configPath, filename, label string) string {
	expanded := config.ExpandHome(configPath)
	workspaceDir := filepath.Join(filepath.Dir(expanded), "workspace")
	data, err := os.ReadFile(filepath.Join(workspaceDir, filename))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, label); idx >= 0 {
			val := strings.TrimSpace(line[idx+len(label):])
			// Strip markdown bold markers (e.g., "** Danya" from "- **Name:** Danya")
			val = strings.Trim(val, "*")
			val = strings.TrimSpace(val)
			return val
		}
	}
	return ""
}

func composeBriefing() string {
	text, err := renderBriefing("commander-general.md", BriefingContext{})
	if err != nil {
		slog.Warn("failed to render commander briefing template", "error", err)
		return "You are the Commander of this Eyrie. You oversee all projects and agent teams."
	}
	return text
}

// commanderRef stores either an instance ID or a legacy agent name.
type commanderRef struct {
	InstanceID string `json:"instance_id,omitempty"`
	AgentName  string `json:"agent_name,omitempty"`
}

func commanderPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		slog.Warn("neither UserHomeDir nor HOME is set; falling back to os.TempDir() for commander.json — persistence is not guaranteed")
		home = os.TempDir()
	}
	return filepath.Join(home, ".eyrie", "commander.json")
}

func loadCommanderRef() commanderRef {
	data, err := os.ReadFile(commanderPath())
	if err != nil {
		return commanderRef{}
	}
	var ref commanderRef
	if json.Unmarshal(data, &ref) != nil {
		return commanderRef{}
	}
	return ref
}

func saveCommanderRef(ref commanderRef) error {
	path := commanderPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWrite(path, data, 0o644)
}
