package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandleCommandRoomReadsFileBackedSources(t *testing.T) {
	root := t.TempDir()
	meshRoot := filepath.Join(root, "docs", "agent-mesh")
	magnusInbox := filepath.Join(meshRoot, "inboxes", "magnus.yaml")
	clioInbox := filepath.Join(meshRoot, "inboxes", "clio.yaml")
	runtimeRegistry := filepath.Join(root, "docs", "runtime-registry")

	writeTestFile(t, filepath.Join(meshRoot, "manifest.yaml"), `---
updated: "2026-05-08"
status: provisional
project: Eyrie
project_id: eyrie
owner: Magnus/Eyrie
parent_agent:
  id: magnus.eyrie
  display_name: Magnus
  planned_framework: codex
  role: commander
subordinates:
  - id: clio.eyrie
    display_name: Clio
    planned_framework: codex
    role: documentation-specialist
    inbox: "`+clioInbox+`"
channels:
  parent_inbox: "`+magnusInbox+`"
  reports: "`+filepath.Join(meshRoot, "reports")+`"
  runtime_registry: "`+runtimeRegistry+`"
`)
	writeTestFile(t, magnusInbox, `---
updated: "2026-05-08"
recipient: magnus.eyrie
notices: []
`)
	writeTestFile(t, clioInbox, `---
updated: "2026-05-08"
recipient: clio.eyrie
notices: []
`)
	writeTestFile(t, filepath.Join(root, "status", "eyrie-command-board.json"), `{
  "generated_at": "2026-05-08T00:00:00Z",
  "captain": "Magnus/Eyrie",
  "domain": "Eyrie",
  "items": [
    {
      "id": "command-room",
      "title": "Command Room",
      "status": "active",
      "priority": "high",
      "lane": "mission-control",
      "owner": "Eyrie/Ops",
      "primary_agent": "Magnus/Eyrie",
      "summary": "Read-only file-backed command room.",
      "next_action": "Render board and registry signals.",
      "commander_visible": true
    }
  ]
}`)
	writeTestFile(t, filepath.Join(runtimeRegistry, "hermes.eyrie.yaml"), `---
runtime_id: hermes.eyrie
display_name: Hermes
status: configured
parent_agent: Magnus/Eyrie
owning_domain: Eyrie
role: runtime-control-agent
framework: hermes-acp
transport:
  primary: acp
workspace: "/tmp/hermes"
current_assignment: "support command-room checks"
`)

	t.Setenv("EYRIE_AGENT_MESH_DIR", meshRoot)

	req := httptest.NewRequest(http.MethodGet, "/api/command-room", nil)
	rec := httptest.NewRecorder()
	(&Server{}).handleCommandRoom(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload commandRoomResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Board == nil || len(payload.Board.Items) != 1 || payload.Board.Items[0].ID != "command-room" {
		t.Fatalf("board = %#v, want command-room item", payload.Board)
	}
	if len(payload.RuntimeRegistry) != 1 || payload.RuntimeRegistry[0].ID != "hermes.eyrie" {
		t.Fatalf("runtimes = %#v, want hermes.eyrie", payload.RuntimeRegistry)
	}
	if payload.Mesh.Channels.DocsInbox != clioInbox {
		t.Fatalf("docs inbox = %q, want %q", payload.Mesh.Channels.DocsInbox, clioInbox)
	}
	if len(payload.ApprovalBoundary) == 0 {
		t.Fatalf("approval boundary was empty")
	}
}
