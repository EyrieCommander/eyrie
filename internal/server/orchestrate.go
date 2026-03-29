// Package server — orchestrate.go
//
// WHY: Multi-agent project chat orchestration
//
// This file implements the routing and handoff protocol for project chat.
// The design evolved through several iterations:
//
// 1. ORIGINAL: Fan-out to ALL agents on every user message, with cross-agent
//    sync (N-1 hidden LLM calls per message). This was slow, expensive, and
//    produced chaotic multi-agent responses.
//
// 2. MIDDLE: Single-respondent with [LISTENING]/[PASS] directives. [PASS]
//    chained to the next agent in a priority list. Removed because @mention
//    forwarding made it redundant and the priority-chain semantics were
//    confusing for agents.
//
// 3. CURRENT: Single-respondent routing with [LISTENING] + @mention handoff.
//    Only ONE agent responds per user message. If it @mentions another agent,
//    that agent gets forwarded the message automatically (same SSE turn).
//    [LISTENING] claims the user's next message. No [PASS].
//
// WHY this priority ordering:
//   @mention > [LISTENING] agent > commander (first msg) > captain (default)
//
//   - @mention: Explicit user intent always wins.
//   - [LISTENING]: Agent asked a question and is awaiting the answer. Routing
//     elsewhere would break conversational flow.
//   - Commander first on first message: The commander introduces the project
//     and hands off to the captain via @mention.
//   - Captain as default: After handoff, the captain owns execution. All
//     non-directed messages go to the captain.
//
// WHY inline role instructions:
//   Previously, agents were briefed in separate sessions during provisioning.
//   The problem: briefing instructions didn't carry over to the project chat
//   session. Inline injection on first message ensures agents always have their
//   instructions when the conversation starts. Templates live in briefings/.
//
// WHY context injection:
//   Instead of cross-agent sync (sending the full conversation to every agent
//   after each turn), we prepend the last 20 messages as labeled context when
//   addressing an agent. This is simpler, cheaper (no hidden LLM calls), and
//   more reliable (no fire-and-forget goroutines that silently fail).
//
// WHY detached context:
//   Agent LLM calls can take 30-60 seconds. If the user closes the browser
//   tab, the HTTP request context cancels, which would abort the agent call
//   mid-response. The detached context (5-min timeout) ensures the agent
//   finishes and the response is persisted to chat storage even if the SSE
//   connection drops. The user sees the response on their next poll/refresh.
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

	// WHY: Check for first USER message specifically — system messages from
	// provisioning ("project created", "captain assigned") don't count.
	// First-message triggers commander-first routing and inline role instructions.
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

	// On first message, emit the system context message BEFORE the user message
	// so it appears above the user's message in the chat timeline.
	var initContent string
	if firstMessage {
		initContent = fmt.Sprintf("project \"%s\" chat started. (project_id: %s)", proj.Name, proj.ID)
		if proj.Goal != "" {
			initContent += fmt.Sprintf("\ngoal: %s", proj.Goal)
		}
		if proj.Description != "" {
			initContent += fmt.Sprintf("\n%s", proj.Description)
		}
		initMsg := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    "eyrie",
			Role:      "system",
			Content:   initContent,
			Timestamp: time.Now(),
			Mention:   "commander",
		}
		if err := o.chatStore.Append(projectID, initMsg); err != nil {
			return fmt.Errorf("failed to save init message: %w", err)
		}
		sse.WriteEvent(map[string]any{"type": "message", "message": initMsg})
	}

	// Store and emit user message
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
	sse.WriteEvent(map[string]any{"type": "message", "message": userMsg})

	// Each agent has its own session for this project
	sessionKey := projectSessionKey(proj)

	// Build the single respondent for this message.
	// Priority: @mention > [LISTENING] agent > commander (first msg) > captain (default)
	// Only ONE agent responds per user message. Agent-to-agent handoff happens
	// via @mentions in the response (see forwarding logic after the main call).
	var respondent *projectParticipant
	if mention != "" {
		for i, p := range participants {
			if strings.EqualFold(mention, p.name) || strings.EqualFold(mention, p.role) {
				respondent = &participants[i]
				break
			}
		}
	} else if listener := o.chatStore.Listener(projectID); listener != "" {
		for i, p := range participants {
			if p.name == listener {
				respondent = &participants[i]
				break
			}
		}
		o.chatStore.ClearListening(projectID)
	} else if firstMessage {
		// Commander speaks first — hands off to captain via @mention
		for i, p := range participants {
			if p.role == "commander" {
				respondent = &participants[i]
				break
			}
		}
	} else {
		// Captain is the default responder
		for i, p := range participants {
			if p.role == "captain" {
				respondent = &participants[i]
				break
			}
		}
	}

	if respondent == nil {
		sse.WriteDone()
		return fmt.Errorf("no agent available to respond")
	}
	p := *respondent

	// WHY context injection: Instead of syncing the full conversation to every
	// agent after each turn (N-1 hidden LLM calls, fire-and-forget goroutines
	// that silently failed), we prepend the last 20 messages as labeled context.
	// Simpler, cheaper, and the agent sees the same conversation the user sees.
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

	// Send to the single respondent. Agent-to-agent handoff happens via
	// @mentions in the response (forwarding logic below), not via [PASS].
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

	// WHY inline role instructions: Previously, agents were briefed in separate
	// sessions during provisioning. But briefing instructions didn't carry to
	// the project chat session — agents entered without knowing the protocol.
	// Inline injection on first message ensures agents always have their
	// instructions when the conversation starts, regardless of session state.
	if firstMessage {
		// Find the captain's name for the commander's instructions
		captainName := ""
		for _, pp := range participants {
			if pp.role == "captain" {
				captainName = pp.name
				break
			}
		}

		ctx := BriefingContext{
			ProjectName: proj.Name,
			ProjectID:   proj.ID,
			Goal:        proj.Goal,
			Description: proj.Description,
			CaptainName: captainName,
		}

		// Select the template based on role
		var templateFile string
		switch p.role {
		case "commander":
			templateFile = "commander-project-chat.md"
		case "captain":
			templateFile = "captain-project-chat.md"
		default:
			templateFile = "talon-project-chat.md"
		}

		roleInstructions, err := renderBriefing(templateFile, ctx)
		if err != nil {
			slog.Warn("failed to render role instructions", "role", p.role, "error", err)
			roleInstructions = fmt.Sprintf("[system]: You are a %s in this project chat.", p.role)
		}

		routingRules, _ := renderBriefing("routing-rules.md", ctx)

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

	// WHY directive parsing: [LISTENING] is parsed from the END of the response.
	// Agents include it as the last token so it's easy to strip from display
	// content. [LISTENING] means "I asked a question, route the user's next
	// message back to me." Without it, the agent goes idle (only responds when
	// @mentioned). Agent-to-agent handoff uses @mentions, not directives.
	trimmed := strings.TrimSpace(responseContent)
	isListening := strings.HasSuffix(trimmed, "[LISTENING]")

	sse.WriteEvent(map[string]any{
		"type": "debug",
		"msg":  fmt.Sprintf("%s responded", p.name),
		"detail": map[string]any{
			"isListening": isListening,
			"responseEnd": trimmed[max(0, len(trimmed)-50):],
		},
	})

	// Strip [LISTENING] from the display content
	displayContent := trimmed
	if isListening {
		displayContent = strings.TrimSpace(strings.TrimSuffix(displayContent, "[LISTENING]"))
	}

	// Store and display the response
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

	// Handle [LISTENING] directive
	if isListening {
		o.chatStore.SetListening(projectID, p.name)
		slog.Info("agent set listening", "agent", p.name, "project", projectID)
	}

	// WHY agent-to-agent @mention forwarding:
	// When an agent's response contains @another-agent, automatically forward
	// the message to the mentioned agent as a follow-up turn. This enables
	// the captain to report to the commander (e.g., "@commander here's the
	// plan") without requiring the user to relay messages manually. The
	// mentioned agent sees the full context + the mentioning agent's message.
	//
	// This runs AFTER the priority loop so it doesn't interfere with normal
	// routing. It only fires when an agent explicitly @mentions another agent
	// in its response content.
	lastAgentContent := lastStoredAgentMsg(contextLines)
	if lastAgentContent != "" {
		if agentMention := parseMention(lastAgentContent); agentMention != "" {
			// Find the mentioned agent in participants (but not the one who just spoke)
			var mentionedAgent *projectParticipant
			lastSpeaker := ""
			if len(contextLines) > 0 {
				// Extract speaker name from last context line "[name (role)]: content"
				last := contextLines[len(contextLines)-1]
				if idx := strings.Index(last, " ("); idx > 1 {
					lastSpeaker = last[1:idx] // strip leading "["
				}
			}
			for i, p := range participants {
				if (strings.EqualFold(agentMention, p.name) || strings.EqualFold(agentMention, p.role)) && p.name != lastSpeaker {
					mentionedAgent = &participants[i]
					break
				}
			}

			if mentionedAgent != nil {
				slog.Info("agent-to-agent mention detected", "from", lastSpeaker, "to", mentionedAgent.name, "project", projectID)
				sse.WriteEvent(map[string]any{
					"type": "debug",
					"msg":  fmt.Sprintf("agent @mention: %s → %s", lastSpeaker, mentionedAgent.name),
				})

				// Build context for the mentioned agent
				agent := discovery.NewAgent(mentionedAgent.agent)
				var forwardMsg string
				if len(contextLines) > 0 {
					forwardMsg = strings.Join(contextLines, "\n")
				}

				ch, err := agent.StreamMessage(agentCtx, forwardMsg, sessionKey)
				if err != nil {
					slog.Warn("failed to forward to mentioned agent", "agent", mentionedAgent.name, "error", err)
				} else {
					var fwdContent string
					var fwdToolCalls []project.ChatPart
					for ev := range ch {
						switch ev.Type {
						case "done":
							fwdContent = ev.Content
						case "tool_start":
							fwdToolCalls = append(fwdToolCalls, project.ChatPart{
								Type: "tool_call", ID: ev.ToolID, Name: ev.Tool, Args: ev.Args,
							})
						case "tool_result":
							for i := len(fwdToolCalls) - 1; i >= 0; i-- {
								if (ev.ToolID != "" && fwdToolCalls[i].ID == ev.ToolID) ||
									(ev.ToolID == "" && fwdToolCalls[i].Name == ev.Tool && fwdToolCalls[i].Output == "") {
									fwdToolCalls[i].Output = ev.Output
									if ev.Success != nil && !*ev.Success {
										fwdToolCalls[i].Error = true
									}
									break
								}
							}
						}
						sse.WriteEvent(map[string]any{
							"type":   "agent_event",
							"sender": mentionedAgent.name,
							"role":   mentionedAgent.role,
							"event":  ev,
						})
					}

					// Strip directives and store
					fwdTrimmed := strings.TrimSpace(fwdContent)
					fwdDisplay := fwdTrimmed
					if strings.HasSuffix(fwdDisplay, "[LISTENING]") {
						fwdDisplay = strings.TrimSpace(strings.TrimSuffix(fwdDisplay, "[LISTENING]"))
						o.chatStore.SetListening(projectID, mentionedAgent.name)
					}

					if fwdDisplay != "" {
						var fwdParts []project.ChatPart
						if len(fwdToolCalls) > 0 {
							fwdParts = append(fwdParts, fwdToolCalls...)
							fwdParts = append(fwdParts, project.ChatPart{Type: "text", Text: fwdDisplay})
						}
						fwdMsg := project.ChatMessage{
							ID:        uuid.New().String(),
							Sender:    mentionedAgent.name,
							Role:      mentionedAgent.role,
							Content:   fwdDisplay,
							Timestamp: time.Now(),
							Parts:     fwdParts,
						}
						if err := o.chatStore.Append(projectID, fwdMsg); err != nil {
							slog.Warn("failed to save forwarded response", "agent", mentionedAgent.name, "error", err)
						}
						sse.WriteEvent(map[string]any{"type": "message", "message": fwdMsg})
					}
				}
			}
		}
	}

	// Signal completion
	sse.WriteDone()
	return nil
}

// lastStoredAgentMsg returns the content portion of the last agent message in
// contextLines (format: "[name (role)]: content"). Returns "" if the last line
// is a user or system message, since we only want to detect @mentions in agent
// responses.
func lastStoredAgentMsg(contextLines []string) string {
	if len(contextLines) == 0 {
		return ""
	}
	last := contextLines[len(contextLines)-1]
	// Skip user and system messages
	if strings.HasPrefix(last, "[user]:") || strings.HasPrefix(last, "[system]:") {
		return ""
	}
	// Extract content after "]: "
	if idx := strings.Index(last, "]: "); idx != -1 {
		return last[idx+3:]
	}
	return ""
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
