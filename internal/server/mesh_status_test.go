package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMeshStatusSummarizesOpenRequestsAndOutbox(t *testing.T) {
	root := t.TempDir()
	meshRoot := filepath.Join(root, "docs", "agent-mesh")
	writeTestFile(t, filepath.Join(meshRoot, "manifest.yaml"), `---
updated: "2026-05-03"
status: provisional
project: Eyrie
project_id: eyrie
owner: Danya/Eyrie
parent_agent:
  id: magnus.eyrie
  display_name: Magnus
  planned_framework: hermes
  role: commander
subordinates:
  - id: danya.eyrie
    display_name: Danya
    planned_framework: zeroclaw
    role: companion-engineer
    inbox: "`+filepath.Join(meshRoot, "inboxes", "danya.yaml")+`"
channels:
  parent_inbox: "`+filepath.Join(meshRoot, "inboxes", "magnus.yaml")+`"
  outbox: "`+filepath.Join(meshRoot, "outbox.yaml")+`"
  reports: "`+filepath.Join(meshRoot, "reports")+`"
`)
	writeTestFile(t, filepath.Join(meshRoot, "inboxes", "magnus.yaml"), `---
updated: "2026-05-03"
kind: inbox
recipient: magnus.eyrie
notices:
  - id: answered-one
    title: Answered
    status: answered
    acknowledgements:
      - agent: magnus.eyrie
        status: acknowledged
`)
	writeTestFile(t, filepath.Join(meshRoot, "inboxes", "danya.yaml"), `---
updated: "2026-05-03"
kind: inbox
recipient: danya.eyrie
notices:
  - id: open-one
    title: Open Request
    status: open
    priority: normal
    acknowledgements:
      - agent: danya.eyrie
        status: pending
`)
	writeTestFile(t, filepath.Join(meshRoot, "outbox.yaml"), `---
updated: "2026-05-03"
kind: outbox
owner: danya.eyrie
entries:
  - id: old-entry
    title: Old Entry
    status: sent
  - id: latest-entry
    title: Latest Entry
    status: sent
`)
	writeTestFile(t, filepath.Join(meshRoot, "reports", "report.md"), "# Report Title\n\nSee `/Users/dan/Documents/Personal/Commander/Shared/notices/eyrie-inbox.yaml`: `notice-123`.\n")

	t.Setenv("EYRIE_AGENT_MESH_DIR", meshRoot)

	status, err := readMeshStatus()
	if err != nil {
		t.Fatalf("readMeshStatus returned error: %v", err)
	}
	if !status.Available {
		t.Fatalf("expected mesh to be available")
	}
	if status.Owner != "Danya/Eyrie" {
		t.Fatalf("owner = %q, want Danya/Eyrie", status.Owner)
	}
	if len(status.Inboxes) != 2 {
		t.Fatalf("inboxes = %d, want 2", len(status.Inboxes))
	}
	var danya meshInboxSummary
	for _, inbox := range status.Inboxes {
		if inbox.Recipient == "danya.eyrie" {
			danya = inbox
		}
	}
	if danya.Open != 1 {
		t.Fatalf("danya open = %d, want 1", danya.Open)
	}
	if danya.Pending != 1 {
		t.Fatalf("danya pending = %d, want 1", danya.Pending)
	}
	if status.LatestOutbox == nil || status.LatestOutbox.ID != "latest-entry" {
		t.Fatalf("latest outbox = %#v, want latest-entry", status.LatestOutbox)
	}
	if len(status.Reports) != 1 || status.Reports[0].Title != "Report Title" {
		t.Fatalf("reports = %#v, want Report Title", status.Reports)
	}
	if len(status.CommanderRefs) != 1 || status.CommanderRefs[0].Notice != "notice-123" {
		t.Fatalf("commander refs = %#v, want notice-123", status.CommanderRefs)
	}
}

func TestReadMeshStatusUnavailableWhenManifestMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EYRIE_AGENT_MESH_DIR", root)

	status, err := readMeshStatus()
	if err != nil {
		t.Fatalf("readMeshStatus returned error: %v", err)
	}
	if status.Available {
		t.Fatalf("expected mesh to be unavailable")
	}
}

func TestReadMeshStatusUsesConfiguredMeshRoot(t *testing.T) {
	home := t.TempDir()
	meshRoot := filepath.Join(home, "work", "eyrie", "docs", "agent-mesh")
	writeTestFile(t, filepath.Join(meshRoot, "manifest.yaml"), `---
updated: "2026-05-03"
status: provisional
project: Eyrie
project_id: eyrie
owner: Magnus/Eyrie
parent_agent:
  id: magnus.eyrie
  display_name: Magnus
  planned_framework: hermes
  role: commander
channels: {}
`)
	writeTestFile(t, filepath.Join(home, ".eyrie", "config.toml"), `[mesh]
agent_mesh_dir = "~/work/eyrie/docs/agent-mesh"
`)
	t.Setenv("HOME", home)
	t.Setenv("EYRIE_AGENT_MESH_DIR", "")

	status, err := readMeshStatus()
	if err != nil {
		t.Fatalf("readMeshStatus returned error: %v", err)
	}
	if !status.Available {
		t.Fatalf("expected mesh to be available")
	}
	if status.Root != meshRoot {
		t.Fatalf("root = %q, want %q", status.Root, meshRoot)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
