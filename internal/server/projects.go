package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
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
		Name           *string `json:"name"`
		Description    *string `json:"description"`
		Goal           *string `json:"goal"`
		Status         *string `json:"status"`
		OrchestratorID *string `json:"orchestrator_id"`
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

	if err := store.Save(*p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
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
		// If SSE headers haven't been sent yet the error is returned before
		// any events; otherwise it's too late to change HTTP status so we
		// emit it as an SSE error event.
		sse.WriteError(err.Error())
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

// reorderCommanderFirst moves the commander to the front of the participant
// list so it speaks first (e.g., to introduce a project before the captain).
func reorderCommanderFirst(participants []projectParticipant) {
	for i, p := range participants {
		if p.role == "commander" && i > 0 {
			// Shift earlier entries right, place commander at front
			entry := participants[i]
			copy(participants[1:i+1], participants[:i])
			participants[0] = entry
			return
		}
	}
}

// handleProjectIntake manages the 1:1 commander intake conversation before
// the group chat starts. The user chats with the commander to establish
// project goals, motivation, and scope.
// POST /api/projects/{id}/intake
func (s *Server) handleProjectIntake(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	store, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	proj, err := store.Get(projectID)
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		} else {
			slog.Error("failed to load project", "id", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return
	}

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message required"})
		return
	}

	// Find commander
	ref := loadCommanderRef()
	commanderName := ref.AgentName
	if commanderName == "" {
		commanderName = ref.InstanceID
	}
	if commanderName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no commander set"})
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
	if err := orch.RunIntakeChat(r.Context(), proj, body.Message, commanderName, sse); err != nil {
		sse.WriteError(err.Error())
	}
}

// handleProjectIntakeMessages returns the intake conversation for a project.
// GET /api/projects/{id}/intake
func (s *Server) handleProjectIntakeMessages(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	cs, err := project.NewChatStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	msgs, err := cs.IntakeMessages(projectID, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []project.ChatMessage{}
	}
	writeJSON(w, http.StatusOK, msgs)
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
