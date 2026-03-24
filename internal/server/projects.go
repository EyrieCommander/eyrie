package server

import (
	"context"
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Message == "" {
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
		if errors.Is(err, project.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		} else {
			slog.Error("failed to load project for chat", "project", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return
	}

	// Pre-flight: ensure at least the commander is running before starting the chat.
	// On the first message the commander is required; on subsequent messages we need
	// at least one participant (commander or captain).
	disc := s.runDiscovery(r.Context())
	participants := resolveProjectParticipants(proj, disc)
	if len(participants) == 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "no agents available — make sure the commander is running",
		})
		return
	}

	// Store the user message
	cs, err := project.NewChatStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open chat store"})
		return
	}

	existing, err := cs.Messages(projectID, 1)
	if err != nil {
		slog.Warn("failed to check existing messages", "project", projectID, "error", err)
	}
	firstMessage := err == nil && len(existing) == 0

	// Parse @mentions
	mention := parseMention(body.Message)

	userMsg := project.ChatMessage{
		ID:        uuid.New().String(),
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

	// On first message, add a system message with project context after the user's message
	var initContent string
	var initMsg *project.ChatMessage
	if firstMessage {
		initContent = fmt.Sprintf("project \"%s\" chat started. (project_id: %s)", proj.Name, proj.ID)
		if proj.Goal != "" {
			initContent += fmt.Sprintf("\ngoal: %s", proj.Goal)
		}
		if proj.Description != "" {
			initContent += fmt.Sprintf("\n%s", proj.Description)
		}
		// No instruction text — the commander already knows its job from its briefing.
		// The project info above is enough context.
		initMsg = &project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    "eyrie",
			Role:      "system",
			Content:   initContent,
			Timestamp: time.Now(),
			Mention:   "commander",
		}
		if err := cs.Append(projectID, *initMsg); err != nil {
			slog.Warn("failed to save init message", "project", projectID, "error", err)
		}
	}

	// On the first message, reorder so commander goes first — the commander
	// introduces the project, then the captain takes over.
	if firstMessage {
		reorderCommanderFirst(participants)
	}

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

	if initMsg != nil {
		imData, _ := json.Marshal(map[string]any{"type": "message", "message": initMsg})
		fmt.Fprintf(w, "data: %s\n\n", imData)
		flusher.Flush()
	}

	// Each agent has its own session for this project (e.g., "project-chess-coach").
	// The agent's framework handles conversation history natively via session
	// persistence — no need to paste the full conversation into the prompt.
	//
	// We format the user message with sender attribution so agents can tell
	// who said what in the group conversation.
	sessionKey := "project-" + projectID

	// Track prior responses in this turn so later agents see what earlier
	// agents said (e.g., captain sees commander's introduction).
	var turnHistory []string

	// Broadcast to agents sequentially: on first message commander goes first,
	// otherwise captain first, then commander, then talons.
	for _, p := range participants {
		// On the first message, only the commander responds — the captain
		// waits for the commander to establish context and hand off.
		if firstMessage && p.role != "commander" {
			continue
		}

		// If there's a mention and it's not for this agent, skip
		if mention != "" && !strings.EqualFold(mention, p.name) && !strings.EqualFold(mention, p.role) {
			continue
		}

		agent := discovery.NewAgent(p.agent)

		// Build the message for this agent:
		// - On first message, commander gets the system init context
		// - Agents later in the sequence see earlier agents' responses
		var labeledMsg string
		if firstMessage && p.role == "commander" {
			labeledMsg = fmt.Sprintf("[system]: %s\n\n[user]: %s", initContent, body.Message)
		} else {
			labeledMsg = fmt.Sprintf("[user]: %s", body.Message)
		}

		// Prepend earlier agents' responses from this turn so the agent
		// has full context (avoids race with fire-and-forget sync).
		if len(turnHistory) > 0 {
			labeledMsg = strings.Join(turnHistory, "\n\n") + "\n\n" + labeledMsg
		}

		ch, err := agent.StreamMessage(r.Context(), labeledMsg, sessionKey)
		if err != nil {
			slog.Warn("failed to send to participant", "agent", p.name, "error", err)
			errData, _ := json.Marshal(map[string]any{
				"type":   "agent_event",
				"sender": p.name,
				"role":   p.role,
				"event":  map[string]string{"type": "error", "content": err.Error()},
			})
			fmt.Fprintf(w, "data: %s\n\n", errData)
			flusher.Flush()
			continue
		}

		// Collect the response and tool calls, streaming intermediate events to the client
		var responseContent string
		var toolCalls []project.ChatPart
		for ev := range ch {
			switch ev.Type {
			case "done":
				responseContent = ev.Content
			case "tool_start":
				toolCalls = append(toolCalls, project.ChatPart{
					Type: "tool_call",
					ID:   ev.ToolID,
					Name: ev.Tool,
					Args: ev.Args,
				})
			case "tool_result":
				// Match by tool ID or last unfinished tool call
				for i := len(toolCalls) - 1; i >= 0; i-- {
					if (ev.ToolID != "" && toolCalls[i].ID == ev.ToolID) ||
						(ev.ToolID == "" && toolCalls[i].Name == ev.Tool && toolCalls[i].Output == "") {
						toolCalls[i].Output = ev.Output
						if ev.Success != nil && !*ev.Success {
							toolCalls[i].Error = true
						}
						break
					}
				}
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

		// Track this response so later agents in the sequence see it
		turnHistory = append(turnHistory, fmt.Sprintf("[%s (%s)]: %s", p.name, p.role, responseContent))

		// Build parts from tool calls + final text
		var parts []project.ChatPart
		if len(toolCalls) > 0 {
			parts = append(parts, toolCalls...)
			if responseContent != "" {
				parts = append(parts, project.ChatPart{Type: "text", Text: responseContent})
			}
		}

		// Store the agent's response in Eyrie's project log
		agentMsg := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    p.name,
			Role:      p.role,
			Content:   responseContent,
			Timestamp: time.Now(),
			Parts:     parts,
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
				// Use a detached context — r.Context() is cancelled when the handler returns
				bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, _ = a.SendMessage(bgCtx, msg, sk)
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

	// Captain first (project lead, responds by default)
	if proj.OrchestratorID != "" {
		captainName := resolveInstanceName(proj.OrchestratorID, disc)
		for _, ar := range disc.Agents {
			if ar.Agent.Name == captainName {
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
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

	disc := s.runDiscovery(r.Context())
	var commanderAgent adapter.DiscoveredAgent
	found := false
	for _, ar := range disc.Agents {
		if ar.Agent.Name == commanderName && ar.Alive {
			commanderAgent = ar.Agent
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "commander not running"})
		return
	}

	agent := discovery.NewAgent(commanderAgent)

	// Chat store for intake messages
	cs, err := project.NewChatStore()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to open chat store"})
		return
	}

	// Check if this is the first intake message
	intakeMessages, intakeErr := cs.IntakeMessages(projectID, 1)
	firstIntake := intakeErr == nil && len(intakeMessages) == 0
	if intakeErr != nil {
		slog.Warn("failed to check intake messages", "project", projectID, "error", intakeErr)
	}

	// Save user message
	userMsg := project.ChatMessage{
		ID:        uuid.New().String(),
		Sender:    "user",
		Role:      "user",
		Content:   body.Message,
		Timestamp: time.Now(),
	}
	if err := cs.AppendIntake(projectID, userMsg); err != nil {
		slog.Warn("failed to save intake message", "project", projectID, "error", err)
	}

	// Build the message to send to the commander
	sessionKey := "project-" + projectID + "-intake"
	var labeledMsg string
	if firstIntake {
		// First message: include intake prompt
		intakePrompt := fmt.Sprintf(`Your user is setting up a new project called "%s".`, proj.Name)
		if proj.Goal != "" {
			intakePrompt += fmt.Sprintf("\nThey've noted the goal: %s", proj.Goal)
		}
		if proj.Description != "" {
			intakePrompt += fmt.Sprintf("\nDescription: %s", proj.Description)
		}
		intakePrompt += `

Have a brief conversation with them to understand what they want to accomplish. Ask about:
- What they want to build or achieve
- Their motivation and why this matters to them
- Any constraints, preferences, or context that would help the project team

Keep it conversational — 2-3 focused questions. Don't plan the project or propose a team yet. You're just gathering context so you can introduce the user and their goals to the project captain later.`
		labeledMsg = fmt.Sprintf("[system]: %s\n\n[user]: %s", intakePrompt, body.Message)
	} else {
		labeledMsg = fmt.Sprintf("[user]: %s", body.Message)
	}

	// Set up SSE streaming
	flusher, ok := startSSE(w)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	// Send user message event
	umData, _ := json.Marshal(map[string]any{"type": "message", "message": userMsg})
	fmt.Fprintf(w, "data: %s\n\n", umData)
	flusher.Flush()

	// Stream commander response
	ch, err := agent.StreamMessage(r.Context(), labeledMsg, sessionKey)
	if err != nil {
		errData, _ := json.Marshal(map[string]string{"type": "error", "error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errData)
		flusher.Flush()
		return
	}

	var responseContent string
	for ev := range ch {
		if ev.Type == "done" {
			responseContent = ev.Content
		}
		evData, _ := json.Marshal(map[string]any{
			"type":   "agent_event",
			"sender": commanderName,
			"role":   "commander",
			"event":  ev,
		})
		fmt.Fprintf(w, "data: %s\n\n", evData)
		flusher.Flush()
	}

	// Save commander response
	if responseContent != "" {
		cmdMsg := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    commanderName,
			Role:      "commander",
			Content:   responseContent,
			Timestamp: time.Now(),
		}
		if err := cs.AppendIntake(projectID, cmdMsg); err != nil {
			slog.Warn("failed to save intake response", "project", projectID, "error", err)
		}

		msgData, _ := json.Marshal(map[string]any{"type": "message", "message": cmdMsg})
		fmt.Fprintf(w, "data: %s\n\n", msgData)
		flusher.Flush()
	}

	doneData, _ := json.Marshal(map[string]string{"type": "done"})
	fmt.Fprintf(w, "data: %s\n\n", doneData)
	flusher.Flush()
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
