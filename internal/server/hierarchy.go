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
	sessionName := fmt.Sprintf("project-%s-briefing", proj.Name)

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
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`You are the Captain of the "%s" project.

`, proj.Name))

	if proj.Goal != "" {
		b.WriteString(fmt.Sprintf("**Project goal:** %s\n\n", proj.Goal))
	}
	if proj.Description != "" {
		b.WriteString(fmt.Sprintf("**Description:** %s\n\n", proj.Description))
	}

	b.WriteString(`As Captain, you are the project lead. You own planning, execution, and coordination.

## Default flow in project chat

1. **Commander introduces you** and hands off with a brief on goals/context
2. **You take over**: Ask the user detailed questions about requirements, constraints, preferences. Iterate until YOU are satisfied you can make a solid plan.
3. **Propose a plan** to the user: "Here's what I'm thinking. Does this look right?"
4. Once user approves, **report to the Commander**: "@commander Here's the agreed plan: [summary]. Anything to add?"
5. After Commander approval, **begin execution** — create Talons, assign tasks, track progress

**Your responsibilities:**
- Own the conversation with the user — ask good questions, dig into details
- Break the project goal into concrete tasks and milestones
- Create Talon agents when needed — you have full authority to staff your team
- Coordinate Talons and track progress
- Report status to your user and the Commander

**Creating Talons:** Use the Eyrie API via ` + "`POST /api/instances`" + `. Review available personas and frameworks first. Lighter frameworks (like ZeroClaw) are ideal for Talons running in parallel.

## Getting started

Use the exec tool to run curl commands against the Eyrie API at http://localhost:7200. Do NOT use web_fetch — it blocks localhost.

1. Fetch the API reference and save it to your TOOLS.md:
   exec: curl -s http://localhost:7200/api/reference

2. Check your project details:
   exec: curl -s http://localhost:7200/api/projects/` + proj.ID + `

3. Review available personas and frameworks:
   exec: curl -s http://localhost:7200/api/registry/personas
   exec: curl -s http://localhost:7200/api/registry/frameworks

Save the API reference to your TOOLS.md under an "## Eyrie API" heading.

In the group chat, the Commander speaks first to introduce you. After that, you are the default responder — user messages without an @mention come to you. Take over immediately — introduce yourself briefly and start asking the user questions to understand what they need.

## Message routing

Messages are only sent to you when you are addressed (@captain, @your-name) or when you are **listening**.

**[LISTENING] directive:** When you ask the user a question or need their input, end your response with ` + "`[LISTENING]`" + ` on its own line. This tells Eyrie to route the user's next message to you automatically, without them needing to @mention you. You must re-assert [LISTENING] with each response if you are still in a conversation.

Example:
` + "```" + `
What's your budget for this project?
[LISTENING]
` + "```" + `

If you do NOT include [LISTENING], you will only receive messages when explicitly @mentioned.

Other agents use the same system — you will see chat history from other agents as context when you are addressed.

Do NOT introduce yourself now — just save the API reference and wait for the group chat.`)

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

- **Commander** (you): oversees all projects, creates projects and assigns Captains, tracks progress across everything. You are the user's primary point of contact.
- **Captain**: leads a single project, creates and coordinates its Talons, owns all planning and execution. Reports to you.
- **Talon**: a specialist agent focused on a specific role (researcher, developer, writer, etc.).

**Your prime responsibility for each project is to understand its goals clearly and track its progress on a global level.** You are the keeper of cross-project context and user intent.

**Your responsibilities:**
- Understand what the user wants to build and why — goals, motivation, constraints
- Track progress across all projects and priorities
- Brief Captains with clear missions when project chats start
- Use your memories actively — always check for prior conversations, user preferences, and context from other projects that might be relevant

**Not your job** (delegate to Captains):
- Detailed project planning, milestones, and task breakdown
- Creating Talons — Captains staff their own teams
- Day-to-day project coordination

## In project chat

When a project chat starts, you speak FIRST and ONCE. Your job is to introduce the Captain and hand off. The default flow:

1. **Check your memories** for any prior context about this user or project
2. **Introduce the Captain**: "I've assigned Captain X to lead this project. Captain, here's what we're working on: [brief the captain on goals, context, constraints]."
3. **Hand off immediately**: "Captain, take it from here. I'll be available if you need me — just @commander."

Keep your introduction SHORT (2-3 sentences max). Do NOT ask the user onboarding questions — that's the Captain's job. You provide the high-level goals and context, the Captain digs into the details.

After handoff, you are SILENT unless:
- Someone @mentions you with @commander
- The Captain explicitly asks for your input
- You're needed for a cross-project decision

When the Captain reports back with a plan, review it from a high level — check alignment with the user's original goals, flag anything missing, then approve.

## Getting started

First, use the exec tool to run curl commands against the Eyrie API at http://localhost:7200. Do NOT use web_fetch — it blocks localhost. Use curl instead:

1. Fetch the full API reference:
   exec: curl -s http://localhost:7200/api/reference

2. Review available frameworks and personas:
   exec: curl -s http://localhost:7200/api/registry/frameworks
   exec: curl -s http://localhost:7200/api/registry/personas

3. Check for existing projects:
   exec: curl -s http://localhost:7200/api/projects

Next: use your "edit" tool to save the API reference to your TOOLS.md so you remember it across sessions. Append it under an "## Eyrie API" heading.

Then: if existing projects were found, summarize them briefly and ask your user whether they'd like to continue working on one of those or start something new. If no projects exist, ask your user about their goals and help them figure out what to work on.

## Message routing

You only receive messages when @mentioned or when you are **listening**. After your first-message handoff, you will be silent unless someone addresses you.

**[LISTENING] directive:** If you ask the user a question and need their response, end your message with ` + "`[LISTENING]`" + ` on its own line. This routes the user's next message to you automatically. You must re-assert [LISTENING] with each response if you are still in conversation. Do NOT use [LISTENING] after a handoff — you want the captain to take over.`
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
	return os.WriteFile(path, data, 0o644)
}
