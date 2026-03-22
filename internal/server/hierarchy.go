package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/discovery"
	"github.com/natalie/eyrie/internal/instance"
	"github.com/natalie/eyrie/internal/project"
)

// CommanderInfo represents the coordinator, which can be either a
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

// HierarchyTree is the full tree response: coordinator → projects → agents.
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

	// Find coordinator — can be an instance ID or a legacy agent name
	var coordinator *CommanderInfo
	coordRef := loadCommanderRef()
	if coordRef.InstanceID != "" {
		if c, ok := instByID[coordRef.InstanceID]; ok {
			coordinator = &CommanderInfo{
				ID: c.ID, Name: c.Name, DisplayName: c.DisplayName,
				Framework: c.Framework, Port: c.Port, Status: c.Status,
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
				coordinator = &CommanderInfo{
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
		Commander: coordinator,
		Projects:    trees,
	})
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

	ref := coordinatorRef{InstanceID: body.InstanceID, AgentName: body.AgentName}
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
	agentName := ref.AgentName
	if agentName == "" {
		agentName = ref.InstanceID
	}
	if agentName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no commander set"})
		return
	}

	// Find the agent
	disc := s.runDiscovery(r.Context())
	var found *discovery.AgentResult
	for i := range disc.Agents {
		if disc.Agents[i].Agent.Name == agentName {
			found = &disc.Agents[i]
			break
		}
	}
	if found == nil || !found.Alive {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "commander agent not found or not running"})
		return
	}

	agent := discovery.NewAgent(found.Agent)

	// Gather context for the briefing: installed frameworks, available personas
	briefing := composeBriefing()

	// Reset any existing briefing session, then create a fresh one
	const briefingSessionName = "eyrie-commander-briefing"
	if sessions, sErr := agent.Sessions(r.Context()); sErr == nil {
		for _, s := range sessions {
			if s.Title == briefingSessionName {
				_ = agent.ResetSession(r.Context(), s.Key)
			}
		}
	}
	session, err := agent.CreateSession(r.Context(), briefingSessionName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eyrie: failed to create briefing session on %s: %v\n", agentName, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to create briefing session: %v", err)})
		return
	}

	// Stream the briefing as SSE so the frontend can show the response
	flusher, ok := startSSE(w)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	eventCh, err := agent.StreamMessage(r.Context(), briefing, session.Key)
	if err != nil {
		// SSE headers already sent — emit error as SSE event
		errData, _ := json.Marshal(map[string]string{"type": "error", "error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errData)
		flusher.Flush()
		return
	}

	// First, send the session key so frontend knows where to navigate
	sessionEvent := map[string]string{"type": "session", "session_key": session.Key}
	data, _ := json.Marshal(sessionEvent)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	// Then stream the agent's response
	for ev := range eventCh {
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
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
			return val
		}
	}
	return ""
}

func composeBriefing() string {
	return `You are now the Commander of my Eyrie — a system for managing AI agent teams.

Eyrie organizes agents into a hierarchy to help users accomplish their goals:

- **Commander** (you): oversees all projects. Creates Captains and Talons, tracks progress across everything.
- **Captain**: leads a single project. Coordinates its Talons, tracks project progress, reports back to you.
- **Talon**: a specialist agent focused on a specific role (researcher, developer, writer, etc.).

You create both Captains and Talons. Captains manage their Talons day-to-day and can create new ones if needed. When creating agents, consider the user's needs and the framework size/resource usage: heavier frameworks are best for Commanders who need rich tool access; lighter frameworks are ideal for Talons that need to run efficiently in parallel.

## Getting started

Use the exec tool to run curl commands against the Eyrie API at http://127.0.0.1:7200. Do NOT use web_fetch — it blocks localhost. Use curl instead:

1. Fetch the full API reference (save the important parts to your TOOLS.md):
   exec: curl -s http://127.0.0.1:7200/api/reference

2. Review available frameworks:
   exec: curl -s http://127.0.0.1:7200/api/registry/frameworks

3. Review available personas:
   exec: curl -s http://127.0.0.1:7200/api/registry/personas

4. Check for existing projects:
   exec: curl -s http://127.0.0.1:7200/api/projects

Save what you learn to your TOOLS.md so you remember it across sessions.

Then check: if existing projects were found, summarize them briefly and ask the user whether they'd like to continue working on one of those or start something new. If no projects exist, ask the user about their goals and help them figure out what to work on and what team of agents would be most useful.`
}

// coordinatorRef stores either an instance ID or a legacy agent name.
type coordinatorRef struct {
	InstanceID string `json:"instance_id,omitempty"`
	AgentName  string `json:"agent_name,omitempty"`
}

func coordinatorPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eyrie", "coordinator.json")
}

func loadCommanderRef() coordinatorRef {
	data, err := os.ReadFile(coordinatorPath())
	if err != nil {
		return coordinatorRef{}
	}
	var ref coordinatorRef
	if json.Unmarshal(data, &ref) != nil {
		return coordinatorRef{}
	}
	return ref
}

func saveCommanderRef(ref coordinatorRef) error {
	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(coordinatorPath(), data, 0o644)
}
