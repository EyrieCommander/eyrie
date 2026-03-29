package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/persona"
	"github.com/Audacity88/eyrie/internal/project"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	projects, err := store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if projects == nil {
		projects = []project.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p, err := store.Get(id)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req project.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project name is required"})
		return
	}

	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p, err := store.Create(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Inject system message about project creation
	detail := fmt.Sprintf("project created: %s", p.Name)
	if p.Goal != "" {
		detail += fmt.Sprintf("\ngoal: %s", p.Goal)
	}
	if p.Description != "" {
		detail += fmt.Sprintf("\ndescription: %s", p.Description)
	}
	injectSystemMessage(p.ID, detail)

	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p, err := store.Get(id)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	var update struct {
		Name           *string    `json:"name"`
		Description    *string    `json:"description"`
		Goal           *string    `json:"goal"`
		Status         *string    `json:"status"`
		OrchestratorID *string    `json:"orchestrator_id"`
		Progress       *int       `json:"progress"`
		Deadline       *time.Time `json:"deadline"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if update.Name != nil {
		if *update.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project name cannot be empty"})
			return
		}
		p.Name = *update.Name
	}
	if update.Description != nil {
		p.Description = *update.Description
	}
	if update.Goal != nil {
		p.Goal = *update.Goal
	}
	if update.Status != nil {
		newStatus := project.ProjectStatus(*update.Status)
		if !newStatus.Valid() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status: " + *update.Status})
			return
		}
		if !project.CanTransition(p.Status, newStatus) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("cannot transition from %q to %q", p.Status, newStatus)})
			return
		}
		p.Status = newStatus
	}
	if update.OrchestratorID != nil {
		p.OrchestratorID = *update.OrchestratorID
	}
	if update.Progress != nil {
		if *update.Progress < 0 || *update.Progress > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "progress must be 0-100"})
			return
		}
		p.Progress = *update.Progress
	}
	if update.Deadline != nil {
		p.Deadline = update.Deadline
	}

	if err := store.Save(*p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Publish relevant events for real-time UI updates
	if update.Progress != nil {
		s.events.Publish(ProjectEvent{
			Type:      "progress_updated",
			ProjectID: id,
			Detail:    fmt.Sprintf("progress updated to %d%%", *update.Progress),
			Timestamp: time.Now(),
		})
	}
	if update.Goal != nil {
		s.events.Publish(ProjectEvent{
			Type:      "goal_changed",
			ProjectID: id,
			Detail:    fmt.Sprintf("goal updated: %s", *update.Goal),
			Timestamp: time.Now(),
		})
	}

	// Refresh PROJECT.md for all project agents when project details change
	if update.Goal != nil || update.Description != nil || update.Progress != nil || update.Deadline != nil {
		s.refreshProjectContext(id)
	}

	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Before deleting, clean up agent sessions for this project
	proj, _ := store.Get(id)
	if proj != nil {
		sessionKey := projectSessionKey(proj)
		disc := s.runDiscovery(r.Context())
		participants := resolveProjectParticipants(proj, disc)
		for _, p := range participants {
			agent := discovery.NewAgent(p.agent)
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			if err := agent.DeleteSession(ctx, sessionKey); err != nil {
				slog.Debug("could not delete project session", "agent", p.name, "session", sessionKey, "error", err)
			}
			cancel()
		}
	}

	if err := store.Delete(id); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAddProjectAgent(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var body struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.InstanceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance_id is required"})
		return
	}

	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := store.AddAgent(projectID, body.InstanceID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	// Resolve instance details for event + system message
	agentName := body.InstanceID
	agentFramework := ""
	agentPort := 0
	if is, err := instance.NewStore(); err == nil {
		if inst, err := is.Get(body.InstanceID); err == nil {
			agentName = inst.Name
			agentFramework = inst.Framework
			agentPort = inst.Port
		}
	}

	// System message + context refresh so all agents see the team change
	detail := fmt.Sprintf("user added %s to project", agentName)
	if agentFramework != "" {
		detail = fmt.Sprintf("user added %s (%s, :%d)", agentName, agentFramework, agentPort)
	}
	injectSystemMessage(projectID, detail)
	s.refreshProjectContext(projectID)

	s.events.Publish(ProjectEvent{
		Type:      "agent_created",
		ProjectID: projectID,
		Agent:     agentName,
		AgentRole: "talon",
		Detail:    detail,
		Timestamp: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func (s *Server) handleRemoveProjectAgent(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	instanceID := r.PathValue("instanceId")

	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := store.RemoveAgent(projectID, instanceID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	// Resolve instance name for the event + system message
	agentName := instanceID
	if is, err := instance.NewStore(); err == nil {
		if inst, err := is.Get(instanceID); err == nil {
			agentName = inst.Name
		}
	}
	injectSystemMessage(projectID, fmt.Sprintf("%s removed from project", agentName))
	s.refreshProjectContext(projectID)
	s.events.Publish(ProjectEvent{
		Type:      "agent_removed",
		ProjectID: projectID,
		Agent:     agentName,
		Detail:    fmt.Sprintf("%s removed from project", agentName),
		Timestamp: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleProjectChatMessages returns the shared conversation for a project.
// GET /api/projects/{id}/chat
func (s *Server) handleProjectChatMessages(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	cs, err := project.NewChatStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open chat store"})
		return
	}
	messages, err := cs.Messages(projectID, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if messages == nil {
		messages = []project.ChatMessage{}
	}
	writeJSON(w, http.StatusOK, messages)
}

// handleProjectChatSend receives a user message, stores it, then broadcasts
// to all participating agents and streams their responses via SSE.
// POST /api/projects/{id}/chat
func (s *Server) handleProjectChatSend(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	// Load project
	pStore, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open project store"})
		return
	}
	proj, err := pStore.Get(projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		} else {
			slog.Error("failed to load project for chat", "project", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return
	}

	cs, err := project.NewChatStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open chat store"})
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	orch := &ChatOrchestrator{
		cfg:       s.runDiscovery,
		chatStore: cs,
	}
	if err := orch.RunProjectChat(r.Context(), proj, body.Message, sse); err != nil {
		if sse.Sent() {
			sse.WriteError(err.Error())
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
}

type projectParticipant struct {
	name  string
	role  string
	agent adapter.DiscoveredAgent
}

// resolveInstanceName maps an instance ID or agent name to the discovered agent name.
func resolveInstanceName(id string, disc discovery.Result) string {
	// Direct match by name
	for _, ar := range disc.Agents {
		if ar.Agent.Name == id {
			return id
		}
	}
	// Try instance store lookup (UUID → name)
	if store, err := instance.NewStore(); err == nil {
		if inst, err := store.Get(id); err == nil {
			return inst.Name
		}
	}
	return id
}

func resolveProjectParticipants(proj *project.Project, disc discovery.Result) []projectParticipant {
	var participants []projectParticipant
	seen := make(map[string]bool)

	// Captain first (project lead, responds by default)
	if proj.OrchestratorID != "" {
		captainName := resolveInstanceName(proj.OrchestratorID, disc)
		for _, ar := range disc.Agents {
			if ar.Agent.Name == captainName {
				if ar.Alive && !seen[ar.Agent.Name] {
					seen[ar.Agent.Name] = true
					participants = append(participants, projectParticipant{
						name: ar.Agent.Name, role: "captain", agent: ar.Agent,
					})
				}
				break
			}
		}
	}

	// Commander
	ref := loadCommanderRef()
	commanderName := ref.AgentName
	if commanderName == "" {
		commanderName = ref.InstanceID
	}
	if commanderName != "" {
		for _, ar := range disc.Agents {
			if ar.Agent.Name == commanderName {
				if ar.Alive && !seen[ar.Agent.Name] {
					seen[ar.Agent.Name] = true
					participants = append(participants, projectParticipant{
						name: ar.Agent.Name, role: "commander", agent: ar.Agent,
					})
				}
				break
			}
		}
	}

	// Talons
	for _, agentID := range proj.RoleAgentIDs {
		for _, ar := range disc.Agents {
			if (ar.Agent.Name == agentID || ar.Agent.InstanceID == agentID) && ar.Alive {
				if !seen[ar.Agent.Name] {
					seen[ar.Agent.Name] = true
					participants = append(participants, projectParticipant{
						name: ar.Agent.Name, role: "talon", agent: ar.Agent,
					})
				}
				break
			}
		}
	}

	return participants
}

// ProjectActivityEvent is an ActivityEvent tagged with the agent that produced it.
type ProjectActivityEvent struct {
	adapter.ActivityEvent
	Agent     string `json:"agent"`
	AgentRole string `json:"agent_role,omitempty"`
}

// handleProjectActivity returns recent activity events from all agents in a project.
// GET /api/projects/{id}/activity?limit=50&type=tool_call
func (s *Server) handleProjectActivity(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	proj, err := store.Get(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}

	// Parse query params
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); n != 1 || err != nil {
			limit = 50
		}
	}
	typeFilter := r.URL.Query().Get("type")

	// Resolve all project agents
	disc := s.runDiscovery(r.Context())
	participants := resolveProjectParticipants(proj, disc)

	// Collect activity from each agent (historical snapshot only)
	var allEvents []ProjectActivityEvent
	for _, p := range participants {
		agent := discovery.NewAgent(p.agent)
		// Use a short-lived context — we only want the historical batch,
		// not the live SSE stream.
		collectCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		ch, err := agent.TailActivity(collectCtx)
		if err != nil {
			cancel()
			slog.Debug("skipping activity for agent", "agent", p.name, "error", err)
			continue
		}
		// Drain the channel until it blocks (historical events come first,
		// then live events which we don't want). The timeout ensures we
		// don't wait for live events.
		for ev := range ch {
			if typeFilter != "" && ev.Type != typeFilter {
				continue
			}
			allEvents = append(allEvents, ProjectActivityEvent{
				ActivityEvent: ev,
				Agent:         p.name,
				AgentRole:     p.role,
			})
		}
		cancel()
	}

	// Sort by timestamp descending (most recent first)
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Timestamp.After(allEvents[j].Timestamp)
	})

	// Apply limit
	if len(allEvents) > limit {
		allEvents = allEvents[:limit]
	}

	writeJSON(w, http.StatusOK, allEvents)
}

// parseMention extracts an @mention from a message (e.g., "@captain" → "captain")
func parseMention(msg string) string {
	for _, word := range strings.Fields(msg) {
		if strings.HasPrefix(word, "@") {
			mention := strings.TrimPrefix(word, "@")
			mention = strings.TrimRight(mention, ".,!?;:")
			if mention != "" {
				return mention
			}
		}
	}
	return ""
}

// refreshProjectContext regenerates PROJECT.md for every agent in the given
// project. Called whenever the team roster or project details change so that
// all agents have up-to-date context about their project and teammates.
func (s *Server) refreshProjectContext(projectID string) {
	projStore, err := project.NewStore()
	if err != nil {
		slog.Warn("refreshProjectContext: project store", "error", err)
		return
	}
	proj, err := projStore.Get(projectID)
	if err != nil {
		slog.Warn("refreshProjectContext: load project", "project", projectID, "error", err)
		return
	}
	instStore, err := instance.NewStore()
	if err != nil {
		slog.Warn("refreshProjectContext: instance store", "error", err)
		return
	}
	instances, err := instStore.List()
	if err != nil {
		slog.Warn("refreshProjectContext: list instances", "error", err)
		return
	}

	// Look up persona descriptions once (best-effort)
	var persStore *persona.Store
	if ps, psErr := persona.NewStore(); psErr == nil {
		persStore = ps
	}

	// Collect project agents and their workspaces
	var members []project.TeamMember
	var workspaces []string
	for _, inst := range instances {
		if inst.ProjectID != projectID && inst.ID != proj.OrchestratorID {
			continue
		}
		role := string(inst.HierarchyRole)
		if role == "" {
			role = "agent"
		}
		desc := ""
		if persStore != nil && inst.PersonaID != "" {
			if pers, pErr := persStore.Get(inst.PersonaID); pErr == nil {
				desc = pers.Description
			}
		}
		members = append(members, project.TeamMember{
			Name:        inst.Name,
			DisplayName: inst.DisplayName,
			Role:        role,
			Description: desc,
			Framework:   inst.Framework,
		})
		workspaces = append(workspaces, inst.WorkspacePath)
	}

	content := project.RenderProjectMD(*proj, members)

	for _, ws := range workspaces {
		mdPath := filepath.Join(ws, "PROJECT.md")
		if wErr := os.WriteFile(mdPath, []byte(content), 0o644); wErr != nil {
			slog.Warn("refreshProjectContext: write PROJECT.md", "path", mdPath, "error", wErr)
		}
	}
	slog.Debug("refreshProjectContext: updated", "project", projectID, "agents", len(workspaces))
}

// injectSystemMessage appends a system message to a project's chat log.
// Used to surface structural changes (agent created, removed, project
// updated) in the project chat regardless of whether the change was made
// by a user or an agent.
func injectSystemMessage(projectID, content string) {
	cs, err := project.NewChatStore()
	if err != nil {
		slog.Warn("injectSystemMessage: chat store", "error", err)
		return
	}
	msg := project.ChatMessage{
		ID:        uuid.New().String(),
		Sender:    "eyrie",
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
	}
	if err := cs.Append(projectID, msg); err != nil {
		slog.Warn("injectSystemMessage: append", "project", projectID, "error", err)
	}
}

// handleProjectChatClear deletes the project's chat history.
// DELETE /api/projects/{id}/chat
func (s *Server) handleProjectChatClear(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	cs, err := project.NewChatStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := cs.ClearChat(projectID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Also clear listening state so the next chat starts with default routing
	cs.ClearListening(projectID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// projectSessionKey returns the session key for a project. Uses the stored
// SessionKey field if present, otherwise falls back to "project-<id>" for
// projects created before session keys were introduced.
func projectSessionKey(proj *project.Project) string {
	if proj.SessionKey != "" {
		return proj.SessionKey
	}
	return "project-" + proj.ID
}
