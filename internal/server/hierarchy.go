package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Audacity88/eyrie/internal/config"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
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
			(ref.InstanceID != "" && a.Name == ref.InstanceID) {
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

	// Gather context for the briefing: installed frameworks, available personas
	briefing := composeBriefing()

	// Create a dedicated briefing session and activate it so the agent
	// uses it for the conversation (important for CLI-based frameworks like
	// ZeroClaw where the CLI always uses the active session).
	const briefingSessionName = "eyrie-commander-briefing"
	var sessionKey string

	// Clean up any existing briefing session first
	if sessions, sErr := agent.Sessions(r.Context()); sErr == nil {
		for _, sess := range sessions {
			if sess.Title == briefingSessionName {
				_ = agent.ResetSession(r.Context(), sess.Key)
			}
		}
	}
	sess, err := agent.CreateSession(r.Context(), briefingSessionName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eyrie: failed to create briefing session on %s: %v\n", agentName, err)
		// Fall back to default session
	} else {
		sessionKey = sess.Key
		// For ZeroClaw, activate the session so the CLI uses it
		type sessionActivator interface {
			ActivateSession(ctx context.Context, key string) error
		}
		if activator, ok := agent.(sessionActivator); ok {
			if aErr := activator.ActivateSession(r.Context(), sessionKey); aErr != nil {
				fmt.Fprintf(os.Stderr, "eyrie: failed to activate briefing session: %v\n", aErr)
			}
		}
	}

	// Stream the briefing as SSE so the frontend can show the response
	flusher, ok := startSSE(w)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	eventCh, err := agent.StreamMessage(r.Context(), briefing, sessionKey)
	if err != nil {
		// SSE headers already sent — emit error as SSE event
		errData, _ := json.Marshal(map[string]string{"type": "error", "error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errData)
		flusher.Flush()
		return
	}

	// First, send the session key so frontend knows where to navigate
	if sessionKey != "" {
		sessionEvent := map[string]string{"type": "session", "session_key": sessionKey}
		data, _ := json.Marshal(sessionEvent)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Then stream the agent's response
	for ev := range eventCh {
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
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

	// Find the captain agent
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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "captain agent not found or not running"})
		return
	}

	agent := discovery.NewAgent(found.Agent)

	// Compose captain briefing with project context
	briefing := composeCaptainBriefing(proj)

	// Create a dedicated briefing session
	sessionName := fmt.Sprintf("project-%s-briefing", proj.Name)
	var sessionKey string

	// Clean up existing briefing session
	if sessions, sErr := agent.Sessions(r.Context()); sErr == nil {
		for _, sess := range sessions {
			if sess.Title == sessionName {
				_ = agent.ResetSession(r.Context(), sess.Key)
			}
		}
	}
	sess, cErr := agent.CreateSession(r.Context(), sessionName)
	if cErr != nil {
		fmt.Fprintf(os.Stderr, "eyrie: failed to create captain briefing session: %v\n", cErr)
	} else {
		sessionKey = sess.Key
		type sessionActivator interface {
			ActivateSession(ctx context.Context, key string) error
		}
		if activator, ok := agent.(sessionActivator); ok {
			_ = activator.ActivateSession(r.Context(), sessionKey)
		}
	}

	// Stream the briefing as SSE
	flusher, ok := startSSE(w)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	eventCh, err := agent.StreamMessage(r.Context(), briefing, sessionKey)
	if err != nil {
		errData, _ := json.Marshal(map[string]string{"type": "error", "error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errData)
		flusher.Flush()
		return
	}

	if sessionKey != "" {
		sessionEvent := map[string]string{"type": "session", "session_key": sessionKey}
		data, _ := json.Marshal(sessionEvent)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for ev := range eventCh {
		data, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func composeCaptainBriefing(proj *project.Project) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`You are the Captain of the "%s" project.

`, proj.Name))

	if proj.Goal != "" {
		b.WriteString(fmt.Sprintf("**Project goal:** %s\n\n", proj.Goal))
	}
	if proj.Description != "" {
		b.WriteString(fmt.Sprintf("**Description:** %s\n\n", proj.Description))
	}

	b.WriteString(`As Captain, your responsibilities are:

1. Talk with your user to understand the project requirements in detail
2. Break the project goal into concrete tasks and milestones
3. Propose a team of Talons (specialist agents) — describe what roles you need and why
4. Coordinate your Talons once they're assigned to you
5. Track progress and report status to your user

**Important:** You do NOT create agents yourself. When you've determined what Talons you need, present your proposed team to your user. The Commander will review and provision the agents, then assign them to your project. You'll be notified when they're ready.

## Getting started

Use the exec tool to run curl commands against the Eyrie API at http://127.0.0.1:7200. Do NOT use web_fetch — it blocks localhost.

1. Fetch the API reference and save it to your TOOLS.md:
   exec: curl -s http://127.0.0.1:7200/api/reference

2. Check your project details and any assigned agents:
   exec: curl -s http://127.0.0.1:7200/api/projects/` + proj.ID + `

3. Review available personas (these are the kinds of Talons you can request):
   exec: curl -s http://127.0.0.1:7200/api/registry/personas

Save the API reference to your TOOLS.md under an "## Eyrie API" heading.

You will be added to a group chat with your user and the Commander. When the chat starts, the Commander will introduce the project and hand off to you. At that point, discuss requirements with the user and propose:
- An initial plan with milestones
- What Talon agents would be useful (roles, personas, frameworks)
- What to focus on first

Do NOT introduce yourself now — just save the reference and wait for the group chat to begin.`)

	return b.String()
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
	return `Your user has promoted you to Commander of their Eyrie — a system for managing AI agent teams.

As Commander, you oversee all of your user's projects. Eyrie organizes agents into a hierarchy:

- **Commander** (you): oversees all projects, creates Captains and Talons, tracks progress across everything.
- **Captain**: leads a single project, coordinates its Talons, tracks project progress, reports back to you.
- **Talon**: a specialist agent focused on a specific role (researcher, developer, writer, etc.).

You create both Captains and Talons. Captains manage their Talons day-to-day but don't create new ones. When creating agents, consider your user's needs and the framework characteristics: heavier frameworks (like OpenClaw) are best for Commanders and Captains who need rich tool access; lighter frameworks (like ZeroClaw) are ideal for Talons that need to run efficiently in parallel.

## Getting started

First, use the exec tool to run curl commands against the Eyrie API at http://127.0.0.1:7200. Do NOT use web_fetch — it blocks localhost. Use curl instead:

1. Fetch the full API reference:
   exec: curl -s http://127.0.0.1:7200/api/reference

2. Review available frameworks and personas:
   exec: curl -s http://127.0.0.1:7200/api/registry/frameworks
   exec: curl -s http://127.0.0.1:7200/api/registry/personas

3. Check for existing projects:
   exec: curl -s http://127.0.0.1:7200/api/projects

Next: use your "edit" tool to save the API reference to your TOOLS.md so you remember it across sessions. Append it under an "## Eyrie API" heading.

Then: if existing projects were found, summarize them briefly and ask your user whether they'd like to continue working on one of those or start something new. If no projects exist, ask your user about their goals and help them figure out what to work on and what team of agents would be most useful.`
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
	return os.WriteFile(path, data, 0o644)
}
