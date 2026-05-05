---
id: danya-local-queue
title: Danya Local Queue
status: done
priority: normal
captain_column: monitoring
commander_column: sync-mesh
owner: Eyrie/Ops
primary_agent: Danya/Eyrie
posted_by: Magnus/Eyrie
source_label: Eyrie Local Mesh
source: /Users/natalie/Development/eyrie/docs/agent-mesh/inboxes/danya.yaml
summary: Danya completed the local board-dogfood task and reported that subordinate-agent intake needs clearer queue, source, and stale-state signals.
next_action: Magnus should review Danya's report and decide whether to implement the Eyrie Board Intake Mode item next.
task_state: done
task_id: eyrie-board-state-dogfood
assigned_to: Danya/Eyrie
accountable_agent: Magnus/Eyrie
origin_notice_id: 2026-05-06-vega-eyrie-board-state-and-danya-dogfood-001
notification_refs:
  - 2026-05-06-vega-eyrie-board-state-and-danya-dogfood-001
commander_visible: false
updated: 2026-05-06
---

# Danya Local Queue

Danya completed the board-dogfood assignment and wrote
`docs/agent-mesh/reports/danya-board-dogfood-2026-05-06.md`.

## Current Goal

Keep completed subordinate queue state visible without inflating Commander
notification counts or leaving stale active-work language behind.

## Next Action

Magnus should review the report and decide whether to implement the Eyrie Board
Intake Mode item next. Danya's key request is a subordinate-agent "my queue"
view with source links, deliverable and response paths, approval boundaries, and
stale-state checks between local inboxes and board summaries.

## Approval Boundary

Danya may read and acknowledge local Eyrie mesh policy notices. Commits, pushes,
GitHub actions, credential changes, runtime-home changes, external actions, and
destructive cleanup require explicit approval.
