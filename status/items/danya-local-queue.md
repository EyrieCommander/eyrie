---
id: danya-local-queue
title: Danya Local Queue
status: active
priority: normal
captain_column: local
commander_column: sync-mesh
owner: Eyrie/Ops
primary_agent: Danya/Eyrie
posted_by: Magnus/Eyrie
source_label: Eyrie Local Mesh
source: /Users/natalie/Development/eyrie/docs/agent-mesh/inboxes/danya.yaml
summary: Danya has a local-only queue item and should dogfood the Eyrie board from a subordinate-agent perspective.
next_action: Danya should report what is confusing or missing when using the local-only queue and board as daily task intake.
task_state: active
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

Danya's local mesh inbox now contains a board-dogfood assignment. Vega asked
Magnus to have Danya use the local-only queue and board as an agent using the
system, then report what is confusing or missing. This is a local subordinate
queue item, not a Commander-visible Eyrie blocker.

## Current Goal

Keep subordinate queue state visible on the Eyrie board without inflating
Commander notification counts.

## Next Action

Danya should write a short local report on what fields, filters, or intake cues
would make the board easier to use as daily task intake. Magnus should only
escalate if the findings block Eyrie work or need Dan/Vega attention.

## Approval Boundary

Danya may read and acknowledge local Eyrie mesh policy notices. Commits, pushes,
GitHub actions, credential changes, runtime-home changes, external actions, and
destructive cleanup require explicit approval.
