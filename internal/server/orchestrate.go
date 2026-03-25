package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/project"
)

// ChatOrchestrator encapsulates the multi-agent orchestration logic for
// project chat and intake conversations, decoupled from HTTP concerns.
type ChatOrchestrator struct {
	cfg       func(ctx context.Context) discovery.Result // runs discovery
	chatStore *project.ChatStore
}

// RunProjectChat executes the core project-chat loop: stores the user
// message, resolves participants, fans out to each agent sequentially,
// streams events via SSE, and syncs turn history across agents.
func (o *ChatOrchestrator) RunProjectChat(ctx context.Context, proj *project.Project, message string, sse *SSEWriter) error {
	projectID := proj.ID

	// Resolve participants
	disc := o.cfg(ctx)
	participants := resolveProjectParticipants(proj, disc)
	if len(participants) == 0 {
		return fmt.Errorf("no agents available — make sure the commander is running")
	}

	// Check if this is the first message
	existing, err := o.chatStore.Messages(projectID, 1)
	if err != nil {
		slog.Warn("failed to check existing messages", "project", projectID, "error", err)
	}
	firstMessage := err == nil && len(existing) == 0

	// Parse @mentions
	mention := parseMention(message)

	// Store user message
	userMsg := project.ChatMessage{
		ID:        uuid.New().String(),
		Sender:    "user",
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
		Mention:   mention,
	}
	if err := o.chatStore.Append(projectID, userMsg); err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Send user message event
	sse.WriteEvent(map[string]any{"type": "message", "message": userMsg})

	// On first message, add a system message with project context
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
		initMsg = &project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    "eyrie",
			Role:      "system",
			Content:   initContent,
			Timestamp: time.Now(),
			Mention:   "commander",
		}
		if err := o.chatStore.Append(projectID, *initMsg); err != nil {
			slog.Warn("failed to save init message", "project", projectID, "error", err)
		}
	}

	// On the first message, reorder so commander goes first
	if firstMessage {
		reorderCommanderFirst(participants)
	}

	// Send init message event if present
	if initMsg != nil {
		sse.WriteEvent(map[string]any{"type": "message", "message": initMsg})
	}

	// Each agent has its own session for this project
	sessionKey := "project-" + projectID

	// Track prior responses in this turn so later agents see what earlier
	// agents said
	var turnHistory []string

	// Broadcast to agents sequentially
	for _, p := range participants {
		// On the first message, only the commander responds
		if firstMessage && p.role != "commander" {
			continue
		}

		// If there's a mention and it's not for this agent, skip
		if mention != "" && !strings.EqualFold(mention, p.name) && !strings.EqualFold(mention, p.role) {
			continue
		}

		agent := discovery.NewAgent(p.agent)

		// Build the message for this agent
		var labeledMsg string
		if firstMessage && p.role == "commander" {
			labeledMsg = fmt.Sprintf("[system]: %s\n\n[user]: %s", initContent, message)
		} else {
			labeledMsg = fmt.Sprintf("[user]: %s", message)
		}

		// Prepend earlier agents' responses from this turn
		if len(turnHistory) > 0 {
			labeledMsg = strings.Join(turnHistory, "\n\n") + "\n\n" + labeledMsg
		}

		ch, err := agent.StreamMessage(ctx, labeledMsg, sessionKey)
		if err != nil {
			slog.Warn("failed to send to participant", "agent", p.name, "error", err)
			sse.WriteEvent(map[string]any{
				"type":   "agent_event",
				"sender": p.name,
				"role":   p.role,
				"event":  map[string]string{"type": "error", "content": err.Error()},
			})
			continue
		}

		// Collect the response and tool calls, streaming intermediate events
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
			sse.WriteEvent(map[string]any{
				"type":   "agent_event",
				"sender": p.name,
				"role":   p.role,
				"event":  ev,
			})
		}

		// Skip [PASS] responses
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

		// Store the agent's response
		agentMsg := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    p.name,
			Role:      p.role,
			Content:   responseContent,
			Timestamp: time.Now(),
			Parts:     parts,
		}
		if err := o.chatStore.Append(projectID, agentMsg); err != nil {
			slog.Warn("failed to save agent response", "agent", p.name, "error", err)
		}

		// Fire-and-forget cross-agent sync for turn history
		for _, other := range participants {
			if other.name == p.name {
				continue
			}
			otherAgent := discovery.NewAgent(other.agent)
			labeled := fmt.Sprintf("[%s (%s)]: %s", p.name, p.role, responseContent)
			go func(a adapter.Agent, msg, sk, agentName string) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if _, err := a.SendMessage(bgCtx, msg, sk); err != nil {
					slog.Debug("failed to sync message to agent", "agent", agentName, "session", sk, "error", err)
				}
			}(otherAgent, labeled, sessionKey, other.name)
		}

		// Send the stored message as SSE event
		sse.WriteEvent(map[string]any{"type": "message", "message": agentMsg})
	}

	// Signal completion
	sse.WriteDone()
	return nil
}

// RunIntakeChat executes the 1:1 intake conversation between the user
// and the commander agent, streaming events via SSE.
func (o *ChatOrchestrator) RunIntakeChat(ctx context.Context, proj *project.Project, message string, commanderName string, sse *SSEWriter) error {
	projectID := proj.ID

	// Find commander agent via discovery
	disc := o.cfg(ctx)
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
		return fmt.Errorf("commander not running")
	}

	agent := discovery.NewAgent(commanderAgent)

	// Check if this is the first intake message
	intakeMessages, intakeErr := o.chatStore.IntakeMessages(projectID, 1)
	firstIntake := intakeErr == nil && len(intakeMessages) == 0
	if intakeErr != nil {
		slog.Warn("failed to check intake messages", "project", projectID, "error", intakeErr)
	}

	// Store user message
	userMsg := project.ChatMessage{
		ID:        uuid.New().String(),
		Sender:    "user",
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	}
	if err := o.chatStore.AppendIntake(projectID, userMsg); err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Build the message to send to the commander
	sessionKey := "project-" + projectID + "-intake"
	var labeledMsg string
	if firstIntake {
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
		labeledMsg = fmt.Sprintf("[system]: %s\n\n[user]: %s", intakePrompt, message)
	} else {
		labeledMsg = fmt.Sprintf("[user]: %s", message)
	}

	// Send user message event
	sse.WriteEvent(map[string]any{"type": "message", "message": userMsg})

	// Stream commander response
	ch, err := agent.StreamMessage(ctx, labeledMsg, sessionKey)
	if err != nil {
		sse.WriteError(err.Error())
		return nil // error already sent via SSE
	}

	var responseContent string
	for ev := range ch {
		if ev.Type == "done" {
			responseContent = ev.Content
		}
		sse.WriteEvent(map[string]any{
			"type":   "agent_event",
			"sender": commanderName,
			"role":   "commander",
			"event":  ev,
		})
	}

	// Store commander response
	if responseContent != "" {
		cmdMsg := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    commanderName,
			Role:      "commander",
			Content:   responseContent,
			Timestamp: time.Now(),
		}
		if err := o.chatStore.AppendIntake(projectID, cmdMsg); err != nil {
			slog.Warn("failed to save intake response", "project", projectID, "error", err)
		}

		sse.WriteEvent(map[string]any{"type": "message", "message": cmdMsg})
	}

	sse.WriteDone()
	return nil
}

// streamBriefing sends a briefing message to an agent in a named session,
// streaming events via SSE. If resetExisting is true, any existing session
// with the given name is reset before re-briefing. If resetExisting is
// false and the session already exists, the session key is sent and the
// function returns immediately (idempotent briefing).
func streamBriefing(ctx context.Context, agent adapter.Agent, agentName string, sessionName string, briefingText string, sse *SSEWriter, resetExisting bool) error {
	var sessionKey string

	if sessions, sErr := agent.Sessions(ctx); sErr == nil {
		for _, sess := range sessions {
			if sess.Title == sessionName {
				if resetExisting {
					_ = agent.ResetSession(ctx, sess.Key)
				} else {
					// Already briefed — just return the session key
					sse.WriteEvent(map[string]string{"type": "session", "session_key": sess.Key})
					sse.WriteEvent(map[string]string{"type": "done", "content": ""})
					return nil
				}
				break
			}
		}
	}

	// Create new briefing session
	sess, err := agent.CreateSession(ctx, sessionName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eyrie: failed to create briefing session on %s: %v\n", agentName, err)
		// Fall back to default session
	} else {
		sessionKey = sess.Key
		// For frameworks that support it, activate the session
		type sessionActivator interface {
			ActivateSession(ctx context.Context, key string) error
		}
		if activator, ok := agent.(sessionActivator); ok {
			if aErr := activator.ActivateSession(ctx, sessionKey); aErr != nil {
				fmt.Fprintf(os.Stderr, "eyrie: failed to activate briefing session: %v\n", aErr)
			}
		}
	}

	// Stream the briefing
	eventCh, err := agent.StreamMessage(ctx, briefingText, sessionKey)
	if err != nil {
		sse.WriteError(err.Error())
		return nil // error already sent via SSE
	}

	// Send session key first so frontend knows where to navigate
	if sessionKey != "" {
		sse.WriteEvent(map[string]string{"type": "session", "session_key": sessionKey})
	}

	// Stream the agent's response
	for ev := range eventCh {
		sse.WriteEvent(ev)
	}

	return nil
}
