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

	// Use a detached context for agent interactions so they complete even
	// if the HTTP client disconnects. SSE writes may fail (broken pipe)
	// but the response will still be persisted to chat storage.
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer agentCancel()

	// Resolve participants
	disc := o.cfg(agentCtx)
	participants := resolveProjectParticipants(proj, disc)
	if len(participants) == 0 {
		return fmt.Errorf("no agents available — make sure the commander is running")
	}

	// Check if this is the first user message (system messages from provisioning don't count)
	existing, err := o.chatStore.Messages(projectID, 0)
	if err != nil {
		return fmt.Errorf("failed to check existing messages for project %s: %w", projectID, err)
	}
	firstMessage := true
	for _, m := range existing {
		if m.Role == "user" || m.Role == "commander" || m.Role == "captain" || m.Role == "talon" {
			firstMessage = false
			break
		}
	}

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
			return fmt.Errorf("failed to save init message: %w", err)
		}
	}

	// Send init message event if present
	if initMsg != nil {
		sse.WriteEvent(map[string]any{"type": "message", "message": initMsg})
	}

	// Each agent has its own session for this project
	sessionKey := projectSessionKey(proj)

	// Build ordered priority list of who should respond.
	// Each agent gets a chance; if it responds with [PASS], the next one tries.
	// Priority: @mention > [LISTENING] agent > first-message commander > captain (default)
	var priority []projectParticipant
	if mention != "" {
		for _, p := range participants {
			if strings.EqualFold(mention, p.name) || strings.EqualFold(mention, p.role) {
				priority = append(priority, p)
				break
			}
		}
	} else if listener := o.chatStore.Listener(projectID); listener != "" {
		for _, p := range participants {
			if p.name == listener {
				priority = append(priority, p)
				break
			}
		}
		o.chatStore.ClearListening(projectID)
	} else if firstMessage {
		// Commander first, then captain as fallback if commander passes
		for _, p := range participants {
			if p.role == "commander" {
				priority = append(priority, p)
			}
		}
		for _, p := range participants {
			if p.role == "captain" {
				priority = append(priority, p)
			}
		}
	} else {
		// Captain first (default responder)
		for _, p := range participants {
			if p.role == "captain" {
				priority = append(priority, p)
			}
		}
	}

	if len(priority) == 0 {
		sse.WriteDone()
		return fmt.Errorf("no agent available to respond")
	}

	// Build context: prepend recent chat history so the agent has full picture
	recentMsgs, _ := o.chatStore.Messages(projectID, 20)
	var contextLines []string
	for _, m := range recentMsgs {
		if m.ID == userMsg.ID {
			continue
		}
		if m.Role == "system" {
			contextLines = append(contextLines, fmt.Sprintf("[system]: %s", m.Content))
		} else if m.Role == "user" {
			contextLines = append(contextLines, fmt.Sprintf("[user]: %s", m.Content))
		} else {
			contextLines = append(contextLines, fmt.Sprintf("[%s (%s)]: %s", m.Sender, m.Role, m.Content))
		}
	}

	// Try each agent in priority order; stop when one responds (not [PASS])
	for _, p := range priority {
	agent := discovery.NewAgent(p.agent)

	// Build the message with context
	var labeledMsg string
	if firstMessage && p.role == "commander" {
		labeledMsg = fmt.Sprintf("[system]: %s\n\n[user]: %s", initContent, message)
	} else if len(contextLines) > 0 {
		labeledMsg = strings.Join(contextLines, "\n") + "\n\n[user]: " + message
	} else {
		labeledMsg = fmt.Sprintf("[user]: %s", message)
	}

	// Inject role instructions + routing rules on first message.
	// This replaces the separate briefing sessions — agents get their
	// instructions inline when the conversation starts.
	if firstMessage {
		var roleInstructions string
		switch p.role {
		case "commander":
			roleInstructions = `[system]: You are the COMMANDER in this project chat. Your job:
1. Assess the project goals. If clear, briefly introduce the Captain and hand off with [PASS].
2. If goals are vague, ask 1-3 focused questions with [LISTENING] to clarify before handing off.
3. After handoff, you are SILENT unless @mentioned or the Captain reports back for review.
Keep it brief. Do NOT plan the project — that's the Captain's job.`
		case "captain":
			roleInstructions = fmt.Sprintf(`[system]: You are the CAPTAIN of project "%s". Your job:
1. After the Commander hands off, take over immediately.
2. Ask the user detailed questions about requirements and constraints with [LISTENING].
3. Once satisfied, propose a plan and ask the user to confirm with [LISTENING].
4. After user approval, report to the Commander with @commander for final review.
5. Then begin execution — create Talons via the Eyrie API (see your TOOLS.md).
You own this project end-to-end: planning, execution, coordination.`, proj.Name)
		default:
			roleInstructions = `[system]: You are a TALON (specialist agent) in this project chat.
Respond when @mentioned with your expertise. Use [LISTENING] if you need follow-up from the user.`
		}

		routingRules := `
ROUTING RULES — you MUST end every response with one of:
- [LISTENING] — you asked a question and need the user's next message
- [PASS] — you're done, hand off to the next agent
If you include neither, you go idle (only respond when @mentioned).`

		labeledMsg = roleInstructions + routingRules + "\n\n" + labeledMsg
	}

		sse.WriteEvent(map[string]any{
			"type": "debug",
			"msg":  fmt.Sprintf("routing to %s (%s)", p.name, p.role),
			"detail": map[string]any{
				"firstMessage": firstMessage,
				"mention":      mention,
				"listener":     o.chatStore.Listener(projectID),
				"msgPreview":   labeledMsg[:min(len(labeledMsg), 200)],
			},
		})

		ch, err := agent.StreamMessage(agentCtx, labeledMsg, sessionKey)
		if err != nil {
			slog.Warn("failed to send to participant", "agent", p.name, "error", err)
			sse.WriteEvent(map[string]any{
				"type":   "agent_event",
				"sender": p.name,
				"role":   p.role,
				"event":  map[string]string{"type": "error", "content": err.Error()},
			})
			sse.WriteDone()
			return fmt.Errorf("failed to stream to %s: %w", p.name, err)
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

	// Parse response directives
	trimmed := strings.TrimSpace(responseContent)
	isPassing := strings.HasSuffix(trimmed, "[PASS]") || trimmed == "[PASS]"
	isListening := strings.HasSuffix(trimmed, "[LISTENING]")

	sse.WriteEvent(map[string]any{
		"type": "debug",
		"msg":  fmt.Sprintf("%s responded", p.name),
		"detail": map[string]any{
			"isPassing":   isPassing,
			"isListening": isListening,
			"responseEnd": trimmed[max(0, len(trimmed)-50):],
		},
	})

	// Strip directives from the display content
	displayContent := trimmed
	if isPassing {
		displayContent = strings.TrimSpace(strings.TrimSuffix(displayContent, "[PASS]"))
	}
	if isListening {
		displayContent = strings.TrimSpace(strings.TrimSuffix(displayContent, "[LISTENING]"))
	}

	// Store and display the response if there's content (even with [PASS])
	if displayContent != "" {
		var parts []project.ChatPart
		if len(toolCalls) > 0 {
			parts = append(parts, toolCalls...)
			parts = append(parts, project.ChatPart{Type: "text", Text: displayContent})
		}

		agentMsg := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    p.name,
			Role:      p.role,
			Content:   displayContent,
			Timestamp: time.Now(),
			Parts:     parts,
		}
		if err := o.chatStore.Append(projectID, agentMsg); err != nil {
			slog.Warn("failed to save agent response", "agent", p.name, "error", err)
		}
		sse.WriteEvent(map[string]any{"type": "message", "message": agentMsg})

		// Add to context so the next agent in the chain sees this response
		contextLines = append(contextLines, fmt.Sprintf("[%s (%s)]: %s", p.name, p.role, displayContent))
	}

	// Handle directives
	if isListening {
		o.chatStore.SetListening(projectID, p.name)
		slog.Info("agent set listening", "agent", p.name, "project", projectID)
		break // Agent is listening, don't try next in priority
	}
	if isPassing {
		slog.Info("agent passed", "agent", p.name, "project", projectID)
		continue // Try the next agent in priority
	}

	// Normal response (no directive) — done
	break
	} // end priority loop

	// Signal completion
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
					if err := agent.ResetSession(ctx, sess.Key); err != nil {
					slog.Warn("failed to reset briefing session", "agent", agentName, "session", sess.Key, "error", err)
					return fmt.Errorf("failed to reset briefing session %q: %w", sess.Key, err)
				}
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
		return fmt.Errorf("failed to stream briefing: %w", err)
	}

	// Send session key first so frontend knows where to navigate
	if sessionKey != "" {
		sse.WriteEvent(map[string]string{"type": "session", "session_key": sessionKey})
	}

	// Stream the agent's response
	for ev := range eventCh {
		sse.WriteEvent(ev)
	}

	sse.WriteDone()
	return nil
}
