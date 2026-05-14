package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Audacity88/eyrie/internal/adapter"
	"github.com/Audacity88/eyrie/internal/instance"
)

func TestCommandRoomDispatchBuildsAuditableAgentPayload(t *testing.T) {
	item := commandRoomDispatchBoardItem{
		ID:           "zeroclaw-v080-review-dogfood",
		Title:        "ZeroClaw v0.8 Review Dogfood",
		Status:       "active",
		Priority:     "high",
		Lane:         "active",
		Owner:        "Eyrie/Ops",
		PrimaryAgent: "Magnus/Eyrie",
		Source:       "/Users/dan/Documents/Personal/EyrieOps/status/items/zeroclaw-v080-review-dogfood.md",
		Summary:      "Use Eyrie as the visible control surface for the ZeroClaw v0.8 review program.",
		NextAction:   "Report whether it improves visibility, routing, state tracking, synthesis, and live agent-mesh messaging.",
	}

	sessionKey := commandRoomDispatchSessionKey(item.ID)
	if sessionKey != "eyrie-command-room:zeroclaw-v080-review-dogfood" {
		t.Fatalf("session key = %q, want stable board item session", sessionKey)
	}

	payload := commandRoomDispatchPayload(item, "Please start with the routing and state tracking parts.")
	for _, want := range []string{
		"You are receiving an Eyrie command-room assignment.",
		"Board item:",
		"- id: zeroclaw-v080-review-dogfood",
		"- title: ZeroClaw v0.8 Review Dogfood",
		"- priority: high",
		"- source: /Users/dan/Documents/Personal/EyrieOps/status/items/zeroclaw-v080-review-dogfood.md",
		"Summary: Use Eyrie as the visible control surface for the ZeroClaw v0.8 review program.",
		"Next action: Report whether it improves visibility, routing, state tracking, synthesis, and live agent-mesh messaging.",
		"Operator note: Please start with the routing and state tracking parts.",
		"Do not commit, push, mutate GitHub, change credentials, edit runtime homes, launch or stop runtimes, or perform external actions unless Dan explicitly approves.",
	} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q:\n%s", want, payload)
		}
	}
}

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
	t.Setenv("EYRIE_DEVELOPMENT_MESH_DIR", filepath.Join(root, "missing-development-mesh"))

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

func TestHandleCommandRoomImportsDevelopmentMeshAndRuntimeSmoke(t *testing.T) {
	root := t.TempDir()
	meshRoot := filepath.Join(root, "eyrie", "docs", "agent-mesh")
	magnusInbox := filepath.Join(meshRoot, "inboxes", "magnus.yaml")
	developmentMeshRoot := filepath.Join(root, "Development", "Codex", "agent-mesh")
	smokePath := filepath.Join(developmentMeshRoot, "reports", "rowan-zeroclaw-v080-eyrie-live-router-runtime-smoke-2026-05-14.md")
	quillResponsePath := filepath.Join(developmentMeshRoot, "agents", "quill", "quill-zeroclaw-v080-eyrie-live-router-dogfood-2026-05-14.md")

	writeTestFile(t, filepath.Join(meshRoot, "manifest.yaml"), `---
updated: "2026-05-14"
status: provisional
project: Eyrie
project_id: eyrie
owner: Magnus/Eyrie
parent_agent:
  id: magnus.eyrie
  display_name: Magnus
  planned_framework: codex
  role: commander
channels:
  parent_inbox: "`+magnusInbox+`"
  reports: "`+filepath.Join(meshRoot, "reports")+`"
`)
	writeTestFile(t, magnusInbox, `---
updated: "2026-05-14"
recipient: magnus.eyrie
notices: []
`)
	writeTestFile(t, filepath.Join(developmentMeshRoot, "inboxes", "codex.yaml"), `---
updated: "2026-05-14"
recipient: development.codex
notices:
  - id: 2026-05-14-rowan-forge-zeroclaw-v080-eyrie-live-router-001
    title: Superseded ZeroClaw v0.8.0 Eyrie live-router dogfood
    created: "2026-05-14T11:00:00Z"
    from: development.rowan
    to:
      - development.forge
    parent: development.rowan
    status: superseded
    priority: high
    summary: Superseded PR #6398 planning assignment.
    request: Use the ZeroClaw v0.8.0 PR #6398 artifacts.
    context_refs:
      - zeroclaw-labs/zeroclaw#6398
  - id: 2026-05-14-rowan-quill-zeroclaw-v080-eyrie-live-router-001
    title: ZeroClaw v0.8.0 Eyrie live-router dogfood
    created: "2026-05-14T12:00:00Z"
    from: development.rowan
    to:
      - development.quill
    parent: development.rowan
    status: answered
    priority: high
    summary: Coordinate PR #6398 runtime-smoke evidence for the Eyrie live-router slice.
    request: Use the ZeroClaw v0.8.0 PR #6398 artifacts and report what Eyrie can safely ingest.
    response: "`+quillResponsePath+`"
    approval_boundary: Read-only local mesh and runtime evidence; no GitHub or runtime mutation.
    context_refs:
      - zeroclaw-labs/zeroclaw#6398
      - "`+smokePath+`"
`)
	writeTestFile(t, filepath.Join(developmentMeshRoot, "work-items", "zeroclaw-v080-eyrie-live-router.yaml"), `---
id: zeroclaw-v080-eyrie-live-router
title: ZeroClaw v0.8.0 Eyrie live-router dogfood
status: active
priority: high
owner: development.rowan
summary: PR #6398 is the scoped live-router bridge target.
next_action: Keep Eyrie import read-only until Dan approves runtime control.
updated: "2026-05-14"
`)
	writeTestFile(t, quillResponsePath, "# Quill Live Router Dogfood\n\nPR #6398 can be imported as durable mesh state.\n")
	writeTestFile(t, smokePath, `# Rowan ZeroClaw v0.8.0 Eyrie Live Router Runtime Smoke

PR: zeroclaw-labs/zeroclaw#6398
Head tested: bad7770bad7770bad7770bad7770bad7770bad7770

- Scratch config: /private/tmp/zeroclaw-runtime-smoke/config.toml
- Runtime-resolved workspace: /private/tmp/zeroclaw-runtime-smoke/data
- Requested worktree path: /Users/natalie/Development/claws/zeroclaw
- Real worktree path: /Users/natalie/Development/claws/.zeroclaw/pr-6398
- Gateway logs: /private/tmp/zeroclaw-runtime-smoke/gateway.log
- Gateway start on approved port 42618 was blocked because an existing daemon was listening there.
- Gateway start on alternate scratch port 42619 succeeded with isolated runtime state.
- GET /health on 42619 returned status: ok, paired: false, require_pairing: true.
- The gateway served the web dashboard from /Applications/ZeroClaw.app/Contents/Resources/web/dist, not from the PR worktree.
- The one-time pairing code printed in the local gateway log was redacted after shutdown.
`)

	t.Setenv("EYRIE_AGENT_MESH_DIR", meshRoot)
	t.Setenv("EYRIE_DEVELOPMENT_MESH_DIR", developmentMeshRoot)

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
	if payload.DevelopmentMesh == nil {
		t.Fatalf("development mesh was nil")
	}
	if payload.DevelopmentMesh.Scope != "zeroclaw-labs/zeroclaw#6398" {
		t.Fatalf("development scope = %q, want PR #6398", payload.DevelopmentMesh.Scope)
	}
	if len(payload.DevelopmentMesh.Assignments) != 2 {
		t.Fatalf("assignments = %#v, want current and superseded PR #6398 assignments", payload.DevelopmentMesh.Assignments)
	}
	assignment := payload.DevelopmentMesh.Assignments[0]
	if assignment.ID != "2026-05-14-rowan-quill-zeroclaw-v080-eyrie-live-router-001" || assignment.Provenance != "durable mesh state" {
		t.Fatalf("assignment = %#v, want durable mesh PR #6398 assignment sorted before superseded history", assignment)
	}
	if len(payload.DevelopmentMesh.RuntimeSmokes) != 1 {
		t.Fatalf("runtime smokes = %#v, want one smoke card", payload.DevelopmentMesh.RuntimeSmokes)
	}
	smoke := payload.DevelopmentMesh.RuntimeSmokes[0]
	if smoke.Status != "warning" {
		t.Fatalf("smoke status = %q, want warning", smoke.Status)
	}
	if !hasCommandRoomFact(smoke.Facts, "Runtime-resolved workspace", "/private/tmp/zeroclaw-runtime-smoke/data", "runtime telemetry") {
		t.Fatalf("smoke facts = %#v, missing runtime workspace provenance", smoke.Facts)
	}
	if !hasCommandRoomFact(smoke.Facts, "Source worktree", "/Users/natalie/Development/claws/.zeroclaw/pr-6398", "durable mesh state") {
		t.Fatalf("smoke facts = %#v, missing source worktree provenance", smoke.Facts)
	}
	if !hasCommandRoomDataSource(payload.DataSources, "development mesh", developmentMeshRoot, "available") {
		t.Fatalf("data sources = %#v, missing development mesh source", payload.DataSources)
	}
}

func TestHandleCommandRoomIncludesProvisionedZeroClawAgents(t *testing.T) {
	root := t.TempDir()
	meshRoot := filepath.Join(root, "eyrie", "docs", "agent-mesh")
	magnusInbox := filepath.Join(meshRoot, "inboxes", "magnus.yaml")

	writeTestFile(t, filepath.Join(meshRoot, "manifest.yaml"), `---
updated: "2026-05-14"
status: provisional
project: Eyrie
project_id: eyrie
owner: Magnus/Eyrie
parent_agent:
  id: magnus.eyrie
  display_name: Magnus
  planned_framework: codex
  role: commander
channels:
  parent_inbox: "`+magnusInbox+`"
`)
	writeTestFile(t, magnusInbox, `---
updated: "2026-05-14"
recipient: magnus.eyrie
notices: []
`)

	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	store, err := instance.NewStore()
	if err != nil {
		t.Fatalf("new instance store: %v", err)
	}
	if err := store.Save(instance.Instance{
		ID:            "zeroclaw-talon-1",
		Name:          "zeroclaw-talon-1",
		DisplayName:   "ZeroClaw Talon 1",
		Framework:     adapter.FrameworkZeroClaw,
		HierarchyRole: instance.RoleTalon,
		ProjectID:     "eyrie-stop-test",
		ParentID:      "magnus",
		Port:          42631,
		ConfigPath:    filepath.Join(home, ".eyrie", "instances", "zeroclaw-talon-1", "config.toml"),
		WorkspacePath: filepath.Join(home, ".eyrie", "instances", "zeroclaw-talon-1", "workspace"),
		Status:        instance.StatusRunning,
		HealthStatus:  "healthy",
		CreatedBy:     "magnus",
		CreatedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("save zeroclaw instance: %v", err)
	}
	if err := store.Save(instance.Instance{
		ID:          "hermes-helper",
		Name:        "hermes-helper",
		DisplayName: "Hermes Helper",
		Framework:   adapter.FrameworkHermes,
		Status:      instance.StatusRunning,
		CreatedAt:   time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("save hermes instance: %v", err)
	}

	t.Setenv("EYRIE_AGENT_MESH_DIR", meshRoot)
	t.Setenv("EYRIE_DEVELOPMENT_MESH_DIR", filepath.Join(root, "missing-development-mesh"))

	req := httptest.NewRequest(http.MethodGet, "/api/command-room", nil)
	rec := httptest.NewRecorder()
	(&Server{instanceStore: store}).handleCommandRoom(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload commandRoomResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.ZeroClawAgents) != 1 {
		t.Fatalf("zeroclaw agents = %#v, want one provisioned zeroclaw agent", payload.ZeroClawAgents)
	}
	agent := payload.ZeroClawAgents[0]
	if agent.Name != "zeroclaw-talon-1" || agent.WorkspacePath == "" || agent.Provenance != "Eyrie instance metadata" {
		t.Fatalf("zeroclaw agent = %#v, want instance metadata with workspace provenance", agent)
	}
	if agent.HierarchyRole != "talon" || agent.ProjectID != "eyrie-stop-test" || agent.Port != 42631 {
		t.Fatalf("zeroclaw agent = %#v, want talon/project/port metadata", agent)
	}
}

func hasCommandRoomFact(facts []commandRoomFact, label string, value string, provenance string) bool {
	for _, fact := range facts {
		if fact.Label == label && fact.Value == value && fact.Provenance == provenance {
			return true
		}
	}
	return false
}

func hasCommandRoomDataSource(sources []commandRoomDataSource, label string, path string, status string) bool {
	for _, source := range sources {
		if source.Label == label && source.Path == path && source.Status == status {
			return true
		}
	}
	return false
}
