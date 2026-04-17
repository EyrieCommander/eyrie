package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/manager"
	"github.com/Audacity88/eyrie/internal/project"
)

// CommanderInfo represents the commander. Since Phase 5, the commander
// is Eyrie itself — a built-in LLM loop, not a separate agent instance.
// This struct is still used in the hierarchy response for UI display.
type CommanderInfo struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Status        string `json:"status"`
	HierarchyRole string `json:"hierarchy_role"`
}

// builtInCommander returns the static commander info for Eyrie itself.
func builtInCommander() *CommanderInfo {
	return &CommanderInfo{
		Name:          "eyrie",
		DisplayName:   "Eyrie",
		Status:        "running",
		HierarchyRole: "commander",
	}
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
	instStore := s.instanceStore
	projStore := s.projectStore

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

	commander := builtInCommander()

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

// handleGetCommander returns the commander info. Since Phase 5 this is
// always the built-in Eyrie commander — no discovery or file lookup.
// Kept as a separate endpoint because the frontend polls it independently
// of the full hierarchy tree.
func (s *Server) handleGetCommander(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"commander": builtInCommander()})
}


// handleBriefCaptain sends a project-scoped briefing to a captain agent.
// POST /api/projects/{id}/captain/brief
func (s *Server) handleBriefCaptain(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	// Load project
	store := s.projectStore
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
		instStore := s.instanceStore
		if instStore != nil {
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

