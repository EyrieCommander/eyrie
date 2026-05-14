package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type commandRoomDispatchBoardItem struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Status       string `json:"status,omitempty"`
	Priority     string `json:"priority,omitempty"`
	Lane         string `json:"lane,omitempty"`
	Owner        string `json:"owner,omitempty"`
	PrimaryAgent string `json:"primary_agent,omitempty"`
	Source       string `json:"source,omitempty"`
	Summary      string `json:"summary,omitempty"`
	NextAction   string `json:"next_action,omitempty"`
}

type commandRoomDispatchRequest struct {
	TargetAgent string                       `json:"target_agent"`
	BoardItem   commandRoomDispatchBoardItem `json:"board_item"`
	Note        string                       `json:"note,omitempty"`
}

func (s *Server) handleCommandRoomDispatch(w http.ResponseWriter, r *http.Request) {
	var req commandRoomDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	req.TargetAgent = strings.TrimSpace(req.TargetAgent)
	req.BoardItem.ID = strings.TrimSpace(req.BoardItem.ID)
	req.BoardItem.Title = strings.TrimSpace(req.BoardItem.Title)
	if req.TargetAgent == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_agent is required"})
		return
	}
	if req.BoardItem.ID == "" || req.BoardItem.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "board_item.id and board_item.title are required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	agent, err := s.findAgent(ctx, req.TargetAgent)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	sessionKey := commandRoomDispatchSessionKey(req.BoardItem.ID)
	sse.WriteEvent(map[string]any{
		"type":        "dispatch",
		"agent":       req.TargetAgent,
		"session_key": sessionKey,
		"board_item":  req.BoardItem.ID,
	})

	events, err := agent.StreamMessage(ctx, commandRoomDispatchPayload(req.BoardItem, req.Note), sessionKey)
	if err != nil {
		sse.WriteError(err.Error())
		return
	}
	for event := range events {
		sse.WriteEvent(event)
	}
}

var commandRoomDispatchUnsafeSessionChars = regexp.MustCompile(`[^a-zA-Z0-9_.:-]+`)

func commandRoomDispatchSessionKey(boardItemID string) string {
	cleaned := strings.TrimSpace(boardItemID)
	cleaned = commandRoomDispatchUnsafeSessionChars.ReplaceAllString(cleaned, "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		cleaned = "unassigned"
	}
	return "eyrie-command-room:" + cleaned
}

func commandRoomDispatchPayload(item commandRoomDispatchBoardItem, note string) string {
	var b strings.Builder
	b.WriteString("You are receiving an Eyrie command-room assignment.\n\n")
	b.WriteString("Board item:\n")
	writeDispatchField(&b, "id", item.ID)
	writeDispatchField(&b, "title", item.Title)
	writeDispatchField(&b, "status", item.Status)
	writeDispatchField(&b, "priority", item.Priority)
	writeDispatchField(&b, "lane", item.Lane)
	writeDispatchField(&b, "owner", item.Owner)
	writeDispatchField(&b, "primary agent", item.PrimaryAgent)
	writeDispatchField(&b, "source", item.Source)
	if item.Summary != "" {
		b.WriteString("\nSummary: ")
		b.WriteString(item.Summary)
		b.WriteString("\n")
	}
	if item.NextAction != "" {
		b.WriteString("\nNext action: ")
		b.WriteString(item.NextAction)
		b.WriteString("\n")
	}
	if strings.TrimSpace(note) != "" {
		b.WriteString("\nOperator note: ")
		b.WriteString(strings.TrimSpace(note))
		b.WriteString("\n")
	}
	b.WriteString("\nRespond with a concise execution note for Magnus. Say what you can do now, what is blocked, and whether this should be relayed into the Eyrie local mesh.\n")
	b.WriteString("Do not commit, push, mutate GitHub, change credentials, edit runtime homes, launch or stop runtimes, or perform external actions unless Dan explicitly approves.")
	return b.String()
}

func writeDispatchField(b *strings.Builder, label string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(b, "- %s: %s\n", label, strings.TrimSpace(value))
}
