// Package server — orchestrate.go
//
// WHY: Multi-agent project chat orchestration
//
// This file implements the routing and handoff protocol for project chat.
// The design evolved through several iterations:
//
//  1. ORIGINAL: Fan-out to ALL agents on every user message, with cross-agent
//     sync (N-1 hidden LLM calls per message). This was slow, expensive, and
//     produced chaotic multi-agent responses.
//
//  2. MIDDLE: Single-respondent with [LISTENING]/[PASS] directives. [PASS]
//     chained to the next agent in a priority list. Removed because @mention
//     forwarding made it redundant and the priority-chain semantics were
//     confusing for agents.
//
//  3. CURRENT: Single-respondent routing with [LISTENING] + @mention handoff.
//     Only ONE agent responds per user message. If it @mentions another agent,
//     that agent gets forwarded the message automatically (same SSE turn).
//     [LISTENING] claims the user's next message. No [PASS].
//
// WHY this priority ordering:
//
//	@mention > [LISTENING] agent > commander (first msg) > captain (default)
//
//	- @mention: Explicit user intent always wins.
//	- [LISTENING]: Agent asked a question and is awaiting the answer. Routing
//	  elsewhere would break conversational flow.
//	- Commander first on first message: The commander introduces the project
//	  and hands off to the captain via @mention.
//	- Captain as default: After handoff, the captain owns execution. All
//	  non-directed messages go to the captain.
//
// WHY inline role instructions:
//
//	Previously, agents were briefed in separate sessions during provisioning.
//	The problem: briefing instructions didn't carry over to the project chat
//	session. Inline injection on first message ensures agents always have their
//	instructions when the conversation starts. Templates live in briefings/.
//
// WHY context injection:
//
//	Instead of cross-agent sync (sending the full conversation to every agent
//	after each turn), we prepend the last 20 messages as labeled context when
//	addressing an agent. This is simpler, cheaper (no hidden LLM calls), and
//	more reliable (no fire-and-forget goroutines that silently fail).
//
// WHY detached context:
//
//	Agent LLM calls can take 30-60 seconds. If the user closes the browser
//	tab, the HTTP request context cancels, which would abort the agent call
//	mid-response. The detached context (5-min timeout) ensures the agent
//	finishes and the response is persisted to chat storage even if the SSE
//	connection drops. The user sees the response on their next poll/refresh.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/discovery"
	"github.com/Audacity88/eyrie/internal/instance"
	"github.com/Audacity88/eyrie/internal/project"
)

// ChatOrchestrator encapsulates the multi-agent orchestration logic for
// project chat and intake conversations, decoupled from HTTP concerns.
type ChatOrchestrator struct {
	cfg           func(ctx context.Context) discovery.Result // runs discovery
	chatStore     *project.ChatStore
	instanceStore *instance.Store
	activeChats   *sync.Map // map[projectID]context.CancelFunc — for stop endpoint
	// triggerSender/triggerRole override the default "user"/"user" attribution
	// of the trigger message. Set by callers that inject messages from a
	// non-user source (e.g. the commander's send_to_project tool). Empty
	// string falls back to "user".
	triggerSender string
	triggerRole   string
}

// RunProjectChat executes the core project-chat loop: stores the user
// message, resolves participants, fans out to each agent sequentially,
// streams events via SSE, and syncs turn history across agents.
func (o *ChatOrchestrator) RunProjectChat(ctx context.Context, proj *project.Project, message string, sse *SSEWriter) error {
	projectID := proj.ID

	// Use a detached context for agent interactions so they complete even
	// if the HTTP client disconnects. SSE writes may fail (broken pipe)
	// but the response will still be persisted to chat storage.
	// The cancel is also stored in activeChats so the stop endpoint can
	// interrupt the orchestration on user request.
	agentCtx, agentCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer agentCancel()
	if o.activeChats != nil {
		// WHY LoadOrStore: Store+defer Delete races when two concurrent
		// RunProjectChat calls overlap — the second overwrites the first's
		// cancel func, then the first's defer deletes the second's entry.
		// LoadOrStore detects the existing entry and rejects the duplicate.
		if _, loaded := o.activeChats.LoadOrStore(projectID, agentCancel); loaded {
			agentCancel()
			return fmt.Errorf("another chat is already in progress for this project")
		}
		defer o.activeChats.Delete(projectID)
	}

	// Resolve participants
	disc := o.cfg(agentCtx)
	participants := resolveProjectParticipants(proj, disc, o.instanceStore)
	if len(participants) == 0 {
		return fmt.Errorf("no agents available — make sure the captain is running")
	}

	// WHY: Check for first USER message specifically — system messages from
	// provisioning ("project created", "captain assigned") don't count.
	// First-message triggers inline role instructions for the captain.
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
		}
		if err := o.chatStore.Append(projectID, initMsg); err != nil {
			return fmt.Errorf("failed to save init message: %w", err)
		}
		sse.WriteEvent(map[string]any{"type": "message", "message": initMsg})
	}

	// Store and emit the trigger message. Sender/role default to "user"
	// but can be overridden (e.g. by the commander's send_to_project tool
	// to attribute the message to "eyrie" with role "commander").
	triggerSender := o.triggerSender
	if triggerSender == "" {
		triggerSender = "user"
	}
	triggerRole := o.triggerRole
	if triggerRole == "" {
		triggerRole = "user"
	}
	userMsg := project.ChatMessage{
		ID:        uuid.New().String(),
		Sender:    triggerSender,
		Role:      triggerRole,
		Content:   message,
		Timestamp: time.Now(),
		Mention:   mention,
	}
	if err := o.chatStore.Append(projectID, userMsg); err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}
	sse.WriteEvent(map[string]any{"type": "message", "message": userMsg})

	// Each agent has its own session for this project, keyed by project ID.
	// WHY project ID, not a slug: We had three different naming schemes
	// (slug, "project-"+UUID, short UUID) causing session mismatches on
	// reset and cross-agent communication. The ID is the canonical key.
	sessionKey := proj.ID

	// Build the single respondent for this message.
	// Priority: @mention > [LISTENING] agent > captain (default)
	// Only ONE agent responds per user message. Agent-to-agent handoff happens
	// via @mentions in the response (see forwarding logic after the main call).
	// WHY no commander in project chat: The commander is a system-level agent
	// for cross-project oversight, not a project chat participant. Adding it
	// to the critical path doubled the round trips before any work happened.
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
	} else {
		// Captain is the default responder (including first message)
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
	recentMsgs, msgErr := o.chatStore.Messages(projectID, 20)
	if msgErr != nil {
		slog.Warn("failed to load recent messages for context injection", "project", projectID, "error", msgErr)
	}
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
	if firstMessage && initContent != "" {
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
	// Find the captain's name — used in both direct briefing and forwarding briefing
	captainName := ""
	for _, pp := range participants {
		if pp.role == "captain" {
			captainName = pp.name
			break
		}
	}

	if firstMessage {

		ctx := BriefingContext{
			ProjectName: proj.Name,
			ProjectID:   proj.ID,
			Goal:        proj.Goal,
			Description: proj.Description,
			CaptainName: captainName,
			AgentName:   p.name,
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

		routingRules, rrErr := renderBriefing("routing-rules.md", ctx)
		if rrErr != nil {
			slog.Warn("failed to render routing rules template", "error", rrErr)
			routingRules = ""
		}

		labeledMsg = roleInstructions + routingRules + "\n\n" + labeledMsg

		// Store a system message so the user can see that a briefing was sent.
		// The full briefing text is in Detail — the frontend renders it as
		// an expandable section so the user can inspect what agents received.
		briefingNote := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    "eyrie",
			Role:      "system",
			Content:   fmt.Sprintf("briefing sent to %s (%s)", p.name, p.role),
			Timestamp: time.Now(),
			Detail:    roleInstructions + routingRules,
		}
		if appendErr := o.chatStore.Append(projectID, briefingNote); appendErr != nil {
			slog.Warn("failed to save briefing note", "error", appendErr)
		}
		sse.WriteEvent(map[string]any{"type": "message", "message": briefingNote})
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

	// Collect the response and tool calls, streaming intermediate events.
	// WHY incremental persistence: If the server crashes during streaming,
	// everything between the user message and the final Append is lost.
	// We assign the response message an ID upfront and periodically
	// append partial snapshots. ChatStore.Messages() deduplicates by ID,
	// keeping only the last occurrence.
	responseMsgID := uuid.New().String()
	var responseContent string
	var streamedBuilder strings.Builder
	var toolCalls []project.ChatPart
	// 500ms throttle — don't write on every chunk
	lastPartialSave := time.Now()
	const partialSaveInterval = 500 * time.Millisecond

	for ev := range ch {
		switch ev.Type {
		case "done":
			responseContent = ev.Content
		case "delta":
			streamedBuilder.WriteString(ev.Content)
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

		// Periodically persist a partial snapshot so content survives a crash.
		if time.Since(lastPartialSave) > partialSaveInterval && streamedBuilder.Len() > 0 {
			partialMsg := project.ChatMessage{
				ID:        responseMsgID,
				Sender:    p.name,
				Role:      p.role,
				Content:   streamedBuilder.String(),
				Timestamp: time.Now(),
			}
			if err := o.chatStore.Append(projectID, partialMsg); err != nil {
				slog.Warn("failed to save partial response", "agent", p.name, "error", err)
			}
			lastPartialSave = time.Now()
		}
	}

	// If the user hit stop, persist whatever was streamed so far and
	// emit a system message. The partial content is valuable — the user
	// may want to see what the agent was saying before they stopped it.
	if agentCtx.Err() != nil {
		slog.Info("project chat stopped by user", "project", projectID, "agent", p.name)

		// Best-effort: ask the framework to discard the in-flight response.
		intCtx, intCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := agent.Interrupt(intCtx, sessionKey); err != nil {
			slog.Warn("interrupt failed", "agent", p.name, "error", err)
		}
		intCancel()

		// Store the partial agent response (if any content was streamed)
		if streamedBuilder.Len() > 0 {
			partialMsg := project.ChatMessage{
				ID:        responseMsgID,
				Sender:    p.name,
				Role:      p.role,
				Content:   streamedBuilder.String() + "\n\n[stopped by user]",
				Timestamp: time.Now(),
			}
			if err := o.chatStore.Append(projectID, partialMsg); err != nil {
				slog.Warn("failed to save partial response on stop", "agent", p.name, "error", err)
			}
			sse.WriteEvent(map[string]any{"type": "message", "message": partialMsg})
		}

		stopMsg := project.ChatMessage{
			ID:        uuid.New().String(),
			Sender:    "eyrie",
			Role:      "system",
			Content:   fmt.Sprintf("%s was stopped", p.name),
			Timestamp: time.Now(),
		}
		if err := o.chatStore.Append(projectID, stopMsg); err != nil {
			slog.Warn("failed to save stop message", "project", projectID, "error", err)
		}
		sse.WriteEvent(map[string]any{"type": "message", "message": stopMsg})
		sse.WriteDone()
		return nil
	}

	// WHY directive parsing: [LISTENING] is parsed from the END of the response.
	// Agents include it as the last token so it's easy to strip from display
	// content. [LISTENING] means "I want the next message" — whether from a
	// user or from agents responding to my @mentions. Without it, the agent
	// goes idle (only responds when @mentioned). This unifies user→agent and
	// agent→agent routing under one mechanism.
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
			ID:        responseMsgID,
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

	// WHY auto-listen on @mention: LLMs don't reliably include [LISTENING]
	// in every response, even when briefed to do so. The instruction works
	// on the first response (briefing is fresh) but fades over long
	// conversations. If the respondent @mentioned other agents, they're
	// delegating work and should always hear back — set listening implicitly.
	responseMentions := parseMentions(displayContent)
	if !isListening && len(responseMentions) > 0 {
		o.chatStore.SetListening(projectID, p.name)
		slog.Info("auto-listen: agent @mentioned others", "agent", p.name, "project", projectID)
	}

	// Compact the JSONL to remove partial snapshots now that the final
	// message is stored. Runs in a goroutine so it doesn't block SSE.
	go func() {
		if err := o.chatStore.Compact(projectID); err != nil {
			slog.Warn("chat compaction failed", "project", projectID, "error", err)
		}
	}()

	// WHY agent-to-agent @mention forwarding with chaining:
	// When an agent's response contains @another-agent, automatically forward
	// the message to the mentioned agent. This enables chains like:
	//   captain → @talon (do work) → talon responds → captain gets follow-up
	//
	// WHY [LISTENING] follow-up: After forwarding completes, if the agent that
	// triggered the @mentions set [LISTENING], route the accumulated context
	// back to them. This unifies the routing model — [LISTENING] means "I want
	// the next message" regardless of whether it comes from a user or an agent.
	// Example: captain says "@talon do X [LISTENING]" → talon responds → captain
	// gets a follow-up turn with the talon's response in context.
	//
	// WHY max 5 hops / 3 rounds: Hops limit @mention chains within one round.
	// Rounds limit [LISTENING] follow-ups (agent→forward→listener→forward→...).
	// Together they cap the total agent calls per user message.
	const maxForwardHops = 5
	const maxRounds = 3
	var cachedFreshDisc *discovery.Result // lazily populated on first fallback lookup
	for round := 0; round < maxRounds; round++ {
		forwarded := false
		for hop := 0; hop < maxForwardHops; hop++ {
			lastAgentContent := lastStoredAgentMsg(contextLines)
			if lastAgentContent == "" {
				break
			}
			agentMentions := parseMentions(lastAgentContent)
			if len(agentMentions) == 0 {
				break
			}

			// Determine who just spoke (to avoid forwarding back to them)
			lastSpeaker := ""
			if len(contextLines) > 0 {
				last := contextLines[len(contextLines)-1]
				if idx := strings.Index(last, " ("); idx > 1 {
					lastSpeaker = last[1:idx] // strip leading "["
				}
			}

			// Process each @mention from the agent's response sequentially.
			// WHY sequential not parallel: SSE is a single stream — interleaving
			// responses from concurrent agents would produce garbled output.
			for _, agentMention := range agentMentions {
				var mentionedAgent *projectParticipant
				for i, pp := range participants {
					if (strings.EqualFold(agentMention, pp.name) || strings.EqualFold(agentMention, pp.role)) && pp.name != lastSpeaker {
						mentionedAgent = &participants[i]
						break
					}
				}

				// WHY fallback lookup: Agents created mid-conversation (e.g., captain
				// provisioning talons during its response) aren't in the participants
				// list, which was resolved before the conversation started. Fall back
				// to the instance store + fresh discovery to find newly created agents.
				if mentionedAgent == nil && o.instanceStore != nil {
					instances, _ := o.instanceStore.List()
					for _, inst := range instances {
						if strings.EqualFold(agentMention, inst.Name) && inst.ProjectID == projectID {
							if cachedFreshDisc == nil {
								d := o.cfg(agentCtx)
								cachedFreshDisc = &d
							}
							for _, ar := range cachedFreshDisc.Agents {
								if ar.Agent.Name == inst.Name && ar.Alive {
									newParticipant := projectParticipant{
										name:  ar.Agent.Name,
										role:  string(inst.HierarchyRole),
										agent: ar.Agent,
									}
									participants = append(participants, newParticipant)
									mentionedAgent = &participants[len(participants)-1]
									slog.Info("found newly created agent via fallback", "agent", inst.Name, "project", projectID)
									break
								}
							}
							break
						}
					}
				}

				if mentionedAgent == nil {
					slog.Debug("mentioned agent not found or not alive", "mention", agentMention, "project", projectID)
					continue // Try next mention
				}

				slog.Info("agent-to-agent mention detected", "from", lastSpeaker, "to", mentionedAgent.name, "project", projectID, "hop", hop+1)
				sse.WriteEvent(map[string]any{
					"type": "debug",
					"msg":  fmt.Sprintf("agent @mention: %s → %s (hop %d)", lastSpeaker, mentionedAgent.name, hop+1),
				})

				// Build context for the mentioned agent
				fwdAgent := discovery.NewAgent(mentionedAgent.agent)
				var forwardMsg string
				if len(contextLines) > 0 {
					forwardMsg = strings.Join(contextLines, "\n")
				}

				// Inject role instructions on first message for agents that haven't
				// been briefed yet (the main respondent was briefed above).
				if firstMessage {
					fwdCtx := BriefingContext{
						ProjectName: proj.Name,
						ProjectID:   proj.ID,
						Goal:        proj.Goal,
						Description: proj.Description,
						CaptainName: captainName,
						AgentName:   mentionedAgent.name,
					}
					var fwdTemplate string
					switch mentionedAgent.role {
					case "commander":
						fwdTemplate = "commander-project-chat.md"
					case "captain":
						fwdTemplate = "captain-project-chat.md"
					default:
						fwdTemplate = "talon-project-chat.md"
					}
					if fwdInstructions, fwdErr := renderBriefing(fwdTemplate, fwdCtx); fwdErr == nil {
						fwdRouting, _ := renderBriefing("routing-rules.md", fwdCtx)
						forwardMsg = fwdInstructions + fwdRouting + "\n\n" + forwardMsg

						briefingNote := project.ChatMessage{
							ID:        uuid.New().String(),
							Sender:    "eyrie",
							Role:      "system",
							Content:   fmt.Sprintf("briefing sent to %s (%s)", mentionedAgent.name, mentionedAgent.role),
							Timestamp: time.Now(),
							Detail:    fwdInstructions + fwdRouting,
						}
						if appendErr := o.chatStore.Append(projectID, briefingNote); appendErr != nil {
							slog.Warn("failed to save forwarding briefing note", "error", appendErr)
						}
						sse.WriteEvent(map[string]any{"type": "message", "message": briefingNote})
					}
				}

				ch, fwdErr := fwdAgent.StreamMessage(agentCtx, forwardMsg, sessionKey)
				if fwdErr != nil {
					slog.Warn("failed to forward to mentioned agent", "agent", mentionedAgent.name, "error", fwdErr)
					errMsg := project.ChatMessage{
						ID:        uuid.New().String(),
						Sender:    "eyrie",
						Role:      "system",
						Content:   fmt.Sprintf("failed to reach %s: %v", mentionedAgent.name, fwdErr),
						Timestamp: time.Now(),
					}
					if appendErr := o.chatStore.Append(projectID, errMsg); appendErr != nil {
						slog.Warn("failed to save error message", "project", projectID, "error", appendErr)
					}
					sse.WriteEvent(map[string]any{"type": "message", "message": errMsg})
					continue // Try next mention
				}

				res := o.consumeAgentStream(ch, sse, mentionedAgent.name, mentionedAgent.role, projectID)
				if display := o.storeAgentResponse(res, sse, mentionedAgent.name, mentionedAgent.role, projectID); display != "" {
					contextLines = append(contextLines, fmt.Sprintf("[%s (%s)]: %s", mentionedAgent.name, mentionedAgent.role, display))
					forwarded = true
				}
			} // end inner mention loop

			if !forwarded {
				break // No mentions could be resolved or produced content
			}
		} // end forwarding hop loop

		// [LISTENING] follow-up: if an agent set [LISTENING] and forwarding
		// happened, route the accumulated context back to the listener.
		listener := o.chatStore.Listener(projectID)
		if listener == "" || !forwarded {
			break // No listener or nothing to follow up on
		}

		// Find the listener in participants
		var listenerAgent *projectParticipant
		for i, pp := range participants {
			if pp.name == listener {
				listenerAgent = &participants[i]
				break
			}
		}
		if listenerAgent == nil {
			break
		}

		o.chatStore.ClearListening(projectID)

		slog.Info("[LISTENING] follow-up: routing back to listener", "agent", listener, "project", projectID, "round", round+1)
		sse.WriteEvent(map[string]any{
			"type": "debug",
			"msg":  fmt.Sprintf("[LISTENING] follow-up → %s (round %d)", listener, round+1),
		})

		// Build context and send to listener
		fupAgent := discovery.NewAgent(listenerAgent.agent)
		var fupMsg string
		if len(contextLines) > 0 {
			fupMsg = strings.Join(contextLines, "\n")
		}

		fupCh, fupErr := fupAgent.StreamMessage(agentCtx, fupMsg, sessionKey)
		if fupErr != nil {
			slog.Warn("failed to reach listener for follow-up", "agent", listener, "error", fupErr)
			errMsg := project.ChatMessage{
				ID:        uuid.New().String(),
				Sender:    "eyrie",
				Role:      "system",
				Content:   fmt.Sprintf("failed to reach %s for follow-up: %v", listener, fupErr),
				Timestamp: time.Now(),
			}
			if appendErr := o.chatStore.Append(projectID, errMsg); appendErr != nil {
				slog.Warn("failed to save follow-up error", "project", projectID, "error", appendErr)
			}
			sse.WriteEvent(map[string]any{"type": "message", "message": errMsg})
			break
		}

		// Stream the listener's follow-up response
		fupRes := o.consumeAgentStream(fupCh, sse, listenerAgent.name, listenerAgent.role, projectID)
		if display := o.storeAgentResponse(fupRes, sse, listenerAgent.name, listenerAgent.role, projectID); display != "" {
			contextLines = append(contextLines, fmt.Sprintf("[%s (%s)]: %s", listenerAgent.name, listenerAgent.role, display))
		}

		// Reset forwarded flag for next round — the listener's response
		// may contain @mentions that trigger another forwarding chain.
		forwarded = false
		// Continue outer loop → forwarding chain runs on listener's new response

	} // end round loop

	// Signal completion
	sse.WriteDone()
	return nil
}

// agentStreamResult holds the output of consuming an agent's response stream.
type agentStreamResult struct {
	display     string // content with [LISTENING] stripped
	isListening bool
	toolCalls   []project.ChatPart
}

// consumeAgentStream reads events from a streaming agent response, emits SSE
// events, strips [LISTENING], stores the message, and returns the result.
// Used by both @mention forwarding and [LISTENING] follow-up to avoid duplicating
// the stream-collect-store pattern (~60 lines each).
func (o *ChatOrchestrator) consumeAgentStream(
	ch <-chan adapter.ChatEvent,
	sse *SSEWriter,
	agentName, agentRole, projectID string,
) agentStreamResult {
	var content string
	var toolCalls []project.ChatPart
	for ev := range ch {
		switch ev.Type {
		case "done":
			content = ev.Content
		case "tool_start":
			toolCalls = append(toolCalls, project.ChatPart{
				Type: "tool_call", ID: ev.ToolID, Name: ev.Tool, Args: ev.Args,
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
			"sender": agentName,
			"role":   agentRole,
			"event":  ev,
		})
	}

	trimmed := strings.TrimSpace(content)
	display := trimmed
	isListening := strings.HasSuffix(display, "[LISTENING]")
	if isListening {
		display = strings.TrimSpace(strings.TrimSuffix(display, "[LISTENING]"))
		o.chatStore.SetListening(projectID, agentName)
	}

	return agentStreamResult{
		display:     display,
		isListening: isListening,
		toolCalls:   toolCalls,
	}
}

// storeAgentResponse creates a ChatMessage from stream results, stores it, and emits via SSE.
// Returns the stored message content (empty if nothing to store).
func (o *ChatOrchestrator) storeAgentResponse(
	res agentStreamResult,
	sse *SSEWriter,
	agentName, agentRole, projectID string,
) string {
	if res.display == "" {
		return ""
	}
	var parts []project.ChatPart
	if len(res.toolCalls) > 0 {
		parts = append(parts, res.toolCalls...)
		parts = append(parts, project.ChatPart{Type: "text", Text: res.display})
	}
	msg := project.ChatMessage{
		ID:        uuid.New().String(),
		Sender:    agentName,
		Role:      agentRole,
		Content:   res.display,
		Timestamp: time.Now(),
		Parts:     parts,
	}
	if err := o.chatStore.Append(projectID, msg); err != nil {
		slog.Warn("failed to save agent response", "agent", agentName, "error", err)
	}
	sse.WriteEvent(map[string]any{"type": "message", "message": msg})
	return res.display
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
