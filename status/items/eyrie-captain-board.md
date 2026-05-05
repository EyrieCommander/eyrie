---
id: eyrie-captain-board
title: Eyrie Captain Board
status: done
priority: normal
captain_column: monitoring
commander_column: waiting
owner: Eyrie/Ops
primary_agent: Magnus/Eyrie
posted_by: Magnus/Eyrie
source_label: Eyrie
summary: Eyrie's first Captain Board slice exists; this item is now a maintenance marker for the durable board surface.
next_action: Maintain the board as Eyrie's durable task surface; add new items there first and expose only Commander-relevant summaries with commander_visible true.
task_state: maintenance
assigned_to: Magnus/Eyrie
accountable_agent: Magnus/Eyrie
origin_notice_id: 2026-05-05-vega-eyrie-captain-board-handoff-001
notification_refs:
  - 2026-05-05-vega-eyrie-captain-board-handoff-001
commander_visible: true
updated: 2026-05-06
---

# Eyrie Captain Board

Vega accepted the Captain Board model on 2026-05-05: notices wake agents up,
but local Captain boards own durable task state. Eyrie's first slice should
prove that model with local Markdown items, a generated Captain manifest, and a
local board page.

## Current Goal

Maintain the smallest useful Eyrie board that can show local implementation work,
subordinate-agent coordination, and runtime/watch-officer state without copying
notice contents into Commander.

## Next Action

Use this board as the source of truth for new long-running Eyrie work. Add or
update `status/items/*.md`, regenerate the manifest, and expose only items that
Dan or Vega should see globally with `commander_visible: true`.

## Approval Boundary

Local Eyrie docs, status items, and generated local manifests are in scope.
Commits, pushes, GitHub mutations, credential changes, runtime-home mutation,
external publication, and destructive cleanup still require explicit approval.
