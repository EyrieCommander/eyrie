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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/manager"
	"github.com/Audacity88/eyrie/internal/persona"
	"github.com/Audacity88/eyrie/internal/project"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	store := s.projectStore
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
	store := s.projectStore
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

	store := s.projectStore
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
	s.injectSystemMessage(p.ID, detail)

	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store := s.projectStore
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
		oldCaptain := p.OrchestratorID
		p.OrchestratorID = *update.OrchestratorID
		// Inject system message when captain is assigned/changed
		if *update.OrchestratorID != "" && *update.OrchestratorID != oldCaptain {
			captainName := *update.OrchestratorID
			if is := s.instanceStore; is != nil {
				if inst, err := is.Get(*update.OrchestratorID); err == nil {
					captainName = inst.Name
				}
			}
			s.injectSystemMessage(id, fmt.Sprintf("captain assigned: %s", captainName))
			// WHY brief on assignment: The general briefing teaches the captain
			// to use ZeroClaw's native shell tool and the Eyrie API. Without it,
			// the LLM defaults to Claude Code tools (Bash, Read) which have a
			// separate permission system that blocks headless agents.
			go s.ensureCaptainBriefing(p)
		}
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
	store := s.projectStore

	// Before deleting, clean up agent sessions for this project
	proj, _ := store.Get(id)
	if proj != nil {
		sessionKey := proj.ID
		disc := s.runDiscovery(r.Context())
		participants := resolveProjectParticipants(proj, disc, s.instanceStore)
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

	store := s.projectStore
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
	if is := s.instanceStore; is != nil {
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
	s.injectSystemMessage(projectID, detail)
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

	store := s.projectStore
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
	if is := s.instanceStore; is != nil {
		if inst, err := is.Get(instanceID); err == nil {
			agentName = inst.Name
		}
	}
	s.injectSystemMessage(projectID, fmt.Sprintf("%s removed from project", agentName))
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
	cs := s.chatStore
	if cs == nil {
		writeJSON(w, http.StatusOK, []project.ChatMessage{})
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
	pStore := s.projectStore
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

	cs := s.chatStore

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	orch := &ChatOrchestrator{
		cfg:           s.runDiscovery,
		chatStore:     cs,
		instanceStore: s.instanceStore,
		activeChats:   &s.activeChats,
	}
	if err := orch.RunProjectChat(r.Context(), proj, body.Message, sse); err != nil {
		if sse.Sent() {
			sse.WriteError(err.Error())
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
}

// handleProjectChatStatus returns whether a project chat response is in-flight.
// GET /api/projects/{id}/chat/status
func (s *Server) handleProjectChatStatus(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	_, active := s.activeChats.Load(projectID)
	writeJSON(w, http.StatusOK, map[string]any{"streaming": active})
}

// handleProjectChatStop cancels an in-flight project chat orchestration.
// POST /api/projects/{id}/chat/stop
func (s *Server) handleProjectChatStop(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	val, ok := s.activeChats.Load(projectID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no active chat"})
		return
	}
	cancel, castOk := val.(context.CancelFunc)
	if !castOk {
		slog.Error("activeChats entry has unexpected type", "project", projectID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	cancel()
	slog.Info("project chat stopped by user", "project", projectID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

type projectParticipant struct {
	name  string
	role  string
	agent adapter.DiscoveredAgent
}

// resolveInstanceName maps an instance ID or agent name to the discovered agent name.
func resolveInstanceName(id string, disc discovery.Result, instStore *instance.Store) string {
	// Direct match by name
	for _, ar := range disc.Agents {
		if ar.Agent.Name == id {
			return id
		}
	}
	// Try instance store lookup (UUID → name)
	if instStore != nil {
		if inst, err := instStore.Get(id); err == nil {
			return inst.Name
		}
	}
	return id
}

func resolveProjectParticipants(proj *project.Project, disc discovery.Result, instStore *instance.Store) []projectParticipant {
	var participants []projectParticipant
	seen := make(map[string]bool)

	// Captain first (project lead, responds by default)
	if proj.OrchestratorID != "" {
		captainName := resolveInstanceName(proj.OrchestratorID, disc, instStore)
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

	// WHY no commander: The commander is a system-level agent for cross-project
	// oversight, not a project chat participant. Users can still @mention the
	// commander by name if needed — the fallback lookup in the forwarding chain
	// will find them. But they're not in the default participant list.

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
	store := s.projectStore
	proj, err := store.Get(projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		} else {
			slog.Error("failed to load project for activity", "project", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
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
	participants := resolveProjectParticipants(proj, disc, s.instanceStore)

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

// parseMention extracts the first @mention from a message (e.g., "@captain" → "captain").
// Used for user message routing where only one respondent is selected.
func parseMention(msg string) string {
	mentions := parseMentions(msg)
	if len(mentions) > 0 {
		return mentions[0]
	}
	return ""
}

// parseMentions extracts all unique @mentions from a message, in order of appearance.
// Used for agent-to-agent forwarding where a response may mention multiple agents
// (e.g., "@talon-code do X" and "@talon-research do Y" in the same message).
func parseMentions(msg string) []string {
	seen := make(map[string]bool)
	var mentions []string
	for _, word := range strings.Fields(msg) {
		if strings.HasPrefix(word, "@") {
			mention := strings.TrimPrefix(word, "@")
			mention = strings.TrimRight(mention, ".,!?;:")
			if mention != "" && !seen[strings.ToLower(mention)] {
				seen[strings.ToLower(mention)] = true
				mentions = append(mentions, mention)
			}
		}
	}
	return mentions
}

// refreshProjectContext regenerates PROJECT.md for every agent in the given
// project. Called whenever the team roster or project details change so that
// all agents have up-to-date context about their project and teammates.
func (s *Server) refreshProjectContext(projectID string) {
	projStore := s.projectStore
	proj, err := projStore.Get(projectID)
	if err != nil {
		slog.Warn("refreshProjectContext: load project", "project", projectID, "error", err)
		return
	}
	instStore := s.instanceStore
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

// ensureCaptainBriefing sends a briefing to the captain if it doesn't have
// one (or if force=true, e.g., after a project reset). Runs in the background
// (goroutine) so it doesn't block the HTTP response.
func (s *Server) ensureCaptainBriefing(proj *project.Project, force ...bool) {
	// WHY 5s delay: The captain may have just been started or reset. Give
	// the agent a moment to stabilize before attempting the briefing.
	time.Sleep(5 * time.Second)

	// WHY 3 minutes: The briefing triggers an LLM call + tool execution
	// (curl API reference, save TOOLS.md). 60s was too short — the agent
	// needs time for inference + tool calls + streaming the response back.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	disc := s.runDiscovery(ctx)
	var found *discovery.AgentResult
	for i := range disc.Agents {
		a := disc.Agents[i].Agent
		if a.Name == proj.OrchestratorID || a.InstanceID == proj.OrchestratorID {
			found = &disc.Agents[i]
			break
		}
	}
	if found == nil || !found.Alive {
		slog.Debug("ensureCaptainBriefing: captain not found or not running", "orchestrator", proj.OrchestratorID)
		return
	}

	agent := discovery.NewAgent(found.Agent)
	if len(force) == 0 || !force[0] {
		sessions, err := agent.Sessions(ctx)
		if err != nil {
			slog.Debug("ensureCaptainBriefing: failed to list sessions", "error", err)
			return
		}
		for _, sess := range sessions {
			if sess.Key == "eyrie-captain-briefing" {
				return // already briefed
			}
		}
	}

	// Reset the briefing session before re-sending so the captain gets
	// a clean context with fresh project info (not stale + fresh mixed).
	resetCtx, resetCancel := context.WithTimeout(ctx, 5*time.Second)
	if resetErr := agent.ResetSession(resetCtx, "eyrie-captain-briefing"); resetErr != nil {
		slog.Debug("ensureCaptainBriefing: could not reset old briefing session", "error", resetErr)
	}
	resetCancel()

	briefing := composeCaptainBriefing(proj)
	agentName := proj.OrchestratorID
	if _, err := agent.SendMessage(ctx, briefing, "eyrie-captain-briefing"); err != nil {
		// Keep the full error in logs but don't leak backend details into
		// the project chat — users see a generic message and operators
		// check the server logs for diagnostics.
		slog.Warn("ensureCaptainBriefing: briefing failed", "captain", agentName, "error", err)
		s.injectSystemMessage(proj.ID, fmt.Sprintf("general briefing failed for %s (see server logs for details)", agentName))
	} else {
		slog.Info("ensureCaptainBriefing: captain briefed", "captain", agentName, "project", proj.ID)
		s.injectSystemMessage(proj.ID, fmt.Sprintf("general briefing sent to %s", agentName))
	}
}

// injectSystemMessage appends a system message to a project's chat log.
// Used to surface structural changes (agent created, removed, project
// updated) in the project chat regardless of whether the change was made
// by a user or an agent.
func (s *Server) injectSystemMessage(projectID, content string) {
	cs := s.chatStore
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
	cs := s.chatStore
	if err := cs.ClearChat(projectID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Also clear listening state so the next chat starts with default routing
	cs.ClearListening(projectID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// NOTE: projectSessionKey was removed. All session keys now use proj.ID
// directly. The old function had three naming schemes (slug, "project-"+UUID,
// short UUID) that caused mismatches on reset and cross-agent routing.

// POST /api/projects/{id}/reset
//
// WHY a dedicated endpoint: The frontend previously orchestrated reset as
// multiple sequential calls (clear chat, reset N sessions, etc.). This was
// fragile — a tab close mid-reset left the project half-cleaned. Moving it
// server-side makes it atomic and adds talon destruction: talons are
// disposable specialists, so a project reset should remove them entirely
// (stop + delete instance + remove from roster). Commander and captain are
// kept — they persist across resets.
func (s *Server) handleProjectReset(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	projStore := s.projectStore
	proj, err := projStore.Get(projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	// 1. Clear project chat history and listening state
	if cs := s.chatStore; cs != nil {
		if clrErr := cs.ClearChat(projectID); clrErr != nil {
			slog.Warn("reset: failed to clear chat", "project", projectID, "error", clrErr)
		}
		cs.ClearListening(projectID)
	}

	// 2. Reset sessions for captain: project chat session + briefing session
	sessionKey := proj.ID
	disc := s.runDiscovery(r.Context())
	for _, p := range resolveProjectParticipants(proj, disc, s.instanceStore) {
		if p.role == "talon" {
			continue // talons are destroyed below, not just reset
		}
		agent := discovery.NewAgent(p.agent)
		// Reset the project chat session
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		if err := agent.ResetSession(ctx, sessionKey); err != nil {
			slog.Debug("reset: could not reset project session", "agent", p.name, "error", err)
		}
		cancel()
		// Reset the briefing session so the captain gets a fresh briefing
		ctx2, cancel2 := context.WithTimeout(r.Context(), 5*time.Second)
		if err := agent.ResetSession(ctx2, "eyrie-captain-briefing"); err != nil {
			slog.Debug("reset: could not reset briefing session", "agent", p.name, "error", err)
		}
		cancel2()
		// Unhide the briefing session if it was hidden
		if s.hidden != nil {
			_ = s.hidden.Unhide(p.name, "eyrie-captain-briefing")
		}
	}

	// 3. Destroy talon instances in parallel: stop, delete from instance
	//    store, remove from project roster. Talons are disposable — a reset
	//    means fresh start. Parallel stop avoids 30s×N sequential timeout.
	instStore := s.instanceStore
	type destroyResult struct {
		name string
		ok   bool
	}
	resultCh := make(chan destroyResult, len(proj.RoleAgentIDs))
	var wg sync.WaitGroup
	for _, agentID := range proj.RoleAgentIDs {
		if instStore == nil {
			break
		}
		inst, getErr := instStore.Get(agentID)
		if getErr != nil {
			slog.Debug("reset: talon instance not found, skipping", "id", agentID, "error", getErr)
			continue
		}
		wg.Add(1)
		go func(inst *instance.Instance, id string) {
			defer wg.Done()
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			if stopErr := manager.ExecuteWithConfig(stopCtx, inst.Framework, inst.ConfigPath, manager.ActionStop); stopErr != nil {
				slog.Debug("reset: failed to stop talon", "instance", inst.Name, "error", stopErr)
			}
			stopCancel()
			if delErr := instStore.Delete(id); delErr != nil {
				slog.Warn("reset: failed to delete talon instance", "instance", inst.Name, "error", delErr)
				resultCh <- destroyResult{inst.Name, false}
			} else {
				resultCh <- destroyResult{inst.Name, true}
			}
		}(inst, agentID)
	}
	go func() { wg.Wait(); close(resultCh) }()
	var destroyed []string
	for r := range resultCh {
		if r.ok {
			destroyed = append(destroyed, r.name)
		}
	}

	// 4. Clear the project's agent roster (all talons removed)
	proj.RoleAgentIDs = nil
	proj.UpdatedAt = time.Now()
	if saveErr := projStore.Save(*proj); saveErr != nil {
		slog.Warn("reset: failed to save project after clearing talons", "error", saveErr)
	}

	// 5. Re-brief the captain (force=true: always re-brief on reset, even
	//    if the old briefing session exists — project context may have changed).
	if proj.OrchestratorID != "" {
		go s.ensureCaptainBriefing(proj, true)
	}

	// 6. Publish event for real-time UI
	s.events.Publish(ProjectEvent{
		Type:      "project_reset",
		ProjectID: projectID,
		Detail:    fmt.Sprintf("reset: chat cleared, %d talons destroyed", len(destroyed)),
		Timestamp: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "reset",
		"talons_destroyed": fmt.Sprintf("%d", len(destroyed)),
	})
}
