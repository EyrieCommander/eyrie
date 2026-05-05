---
id: eyrie-board-intake-mode
title: Eyrie Board Intake Mode
status: capture
priority: normal
captain_column: backlog
commander_column: sync-mesh
owner: Eyrie/Ops
primary_agent: Magnus/Eyrie
posted_by: Magnus/Eyrie
source_label: Danya/Eyrie
source: /Users/natalie/Development/eyrie/docs/agent-mesh/reports/danya-board-dogfood-2026-05-06.md
summary: Danya's dogfood report recommends a subordinate-agent intake mode with my-queue filtering, source links, visible deliverables, and stale-state checks.
next_action: Decide whether this is the next Eyrie board implementation slice before resuming broader product work.
task_state: todo
assigned_to: Magnus/Eyrie
accountable_agent: Magnus/Eyrie
origin_notice_id: 2026-05-06-vega-eyrie-board-state-and-danya-dogfood-001
notification_refs:
  - 2026-05-06-vega-eyrie-board-state-and-danya-dogfood-001
commander_visible: true
updated: 2026-05-06
---

# Eyrie Board Intake Mode

Danya's board-dogfood report says the first Eyrie Captain Board slice is useful
as a Captain-level status surface, but not yet sufficient as subordinate-agent
daily intake.

## Current Goal

Track the next board improvement as a concrete local implementation option
instead of leaving it buried in report prose.

## Next Action

Decide whether to implement the next slice now. The proposed slice is:

- a subordinate-agent "my queue" view;
- card links to source artifacts;
- visible deliverable, response path, origin notice, and approval boundary;
- stale-state checks between local inboxes and generated board summaries.

## Approval Boundary

Local Eyrie board implementation and docs updates are allowed as private project
maintenance. Commits, pushes, public/GitHub mutations, credential changes,
runtime-home changes, external calls, and destructive cleanup require explicit
approval.
