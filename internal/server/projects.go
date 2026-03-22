package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/discovery"
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
		p.Status = *update.Status
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	// Load project to get participants
	pStore, err := project.NewStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open project store"})
		return
	}
	proj, err := pStore.Get(projectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}

	// Store the user message
	cs, err := project.NewChatStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open chat store"})
		return
	}

	// Parse @mentions
	mention := parseMention(body.Message)

	userMsg := project.ChatMessage{
		ID:        uuid.New().String()[:8],
		Sender:    "user",
		Role:      "user",
		Content:   body.Message,
		Timestamp: time.Now(),
		Mention:   mention,
	}
	if err := cs.Append(projectID, userMsg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save message"})
		return
	}

	// Discover participating agents
	disc := s.runDiscovery(r.Context())
	participants := resolveProjectParticipants(proj, disc)

	// Set up SSE streaming
	flusher, ok := startSSE(w)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	// Send the stored user message back as first event
	umData, _ := json.Marshal(map[string]any{"type": "message", "message": userMsg})
	fmt.Fprintf(w, "data: %s\n\n", umData)
	flusher.Flush()

	// Each agent has its own session for this project (e.g., "project-chess-coach").
	// The agent's framework handles conversation history natively via session
	// persistence — no need to paste the full conversation into the prompt.
	//
	// We format the user message with sender attribution so agents can tell
	// who said what in the group conversation.
	sessionKey := "project-" + projectID

	// Broadcast to agents sequentially: captain first, then commander, then talons.
	for _, p := range participants {
		// If there's a mention and it's not for this agent, skip
		if mention != "" && !strings.EqualFold(mention, p.name) && !strings.EqualFold(mention, p.role) {
			continue
		}

		agent := discovery.NewAgent(p.agent)

		// Send the message with sender label — the agent's session has prior
		// history, so it knows the conversation context already.
		labeledMsg := fmt.Sprintf("[user]: %s", body.Message)

		ch, err := agent.StreamMessage(r.Context(), labeledMsg, sessionKey)
		if err != nil {
			slog.Warn("failed to send to participant", "agent", p.name, "error", err)
			continue
		}

		// Collect the response, streaming intermediate events to the client
		var responseContent string
		for ev := range ch {
			if ev.Type == "done" {
				responseContent = ev.Content
			}
			// Stream intermediate events (tool calls, chunks) with sender label
			evData, _ := json.Marshal(map[string]any{
				"type":   "agent_event",
				"sender": p.name,
				"role":   p.role,
				"event":  ev,
			})
			fmt.Fprintf(w, "data: %s\n\n", evData)
			flusher.Flush()
		}

		// Skip [PASS] responses — agents self-regulate whether to respond
		trimmed := strings.TrimSpace(responseContent)
		if trimmed == "[PASS]" || trimmed == "" {
			continue
		}

		// Store the agent's response in Eyrie's project log
		agentMsg := project.ChatMessage{
			ID:        uuid.New().String()[:8],
			Sender:    p.name,
			Role:      p.role,
			Content:   responseContent,
			Timestamp: time.Now(),
		}
		if err := cs.Append(projectID, agentMsg); err != nil {
			slog.Warn("failed to save agent response", "agent", p.name, "error", err)
		}

		// Also send the response to all OTHER agents' project sessions
		// so they see it in their history on the next turn
		for _, other := range participants {
			if other.name == p.name {
				continue
			}
			otherAgent := discovery.NewAgent(other.agent)
			labeled := fmt.Sprintf("[%s (%s)]: %s", p.name, p.role, responseContent)
			// Fire-and-forget: send as a "user" message to their session
			// so it appears in their conversation history
			go func(a adapter.Agent, msg, sk string) {
				_, _ = a.SendMessage(r.Context(), msg, sk)
			}(otherAgent, labeled, sessionKey)
		}

		// Send the stored message as SSE event
		msgData, _ := json.Marshal(map[string]any{"type": "message", "message": agentMsg})
		fmt.Fprintf(w, "data: %s\n\n", msgData)
		flusher.Flush()
	}

	// Signal completion
	doneData, _ := json.Marshal(map[string]string{"type": "done"})
	fmt.Fprintf(w, "data: %s\n\n", doneData)
	flusher.Flush()
}

type projectParticipant struct {
	name  string
	role  string
	agent adapter.DiscoveredAgent
}

func resolveProjectParticipants(proj *project.Project, disc discovery.Result) []projectParticipant {
	var participants []projectParticipant

	// Captain first (project lead, responds by default)
	if proj.OrchestratorID != "" {
		for _, ar := range disc.Agents {
			if ar.Agent.Name == proj.OrchestratorID || ar.Agent.InstanceID == proj.OrchestratorID {
				if ar.Alive {
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
				if ar.Alive {
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
				participants = append(participants, projectParticipant{
					name: ar.Agent.Name, role: "talon", agent: ar.Agent,
				})
				break
			}
		}
	}

	return participants
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
