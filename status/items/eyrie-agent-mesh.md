---
id: eyrie-agent-mesh
title: Eyrie Agent Mesh
status: active
priority: normal
captain_column: active
commander_column: sync-mesh
owner: Eyrie/Ops
primary_agent: Magnus/Eyrie
posted_by: Magnus/Eyrie
source_label: Eyrie
source: /Users/natalie/Development/eyrie/docs/agent-mesh/README.md
summary: Maintain the local Magnus, Danya, Hermes, and Docs file-backed mesh as Eyrie's coordination layer.
next_action: Route new subordinate work through local inboxes and promote only cross-system or Commander-visible summaries.
task_state: active
assigned_to: Magnus/Eyrie
accountable_agent: Magnus/Eyrie
commander_visible: true
updated: 2026-05-06
---

# Eyrie Agent Mesh

Eyrie's local mesh under `docs/agent-mesh/` remains the coordination layer for
Magnus, Danya, Hermes, and Eyrie Docs. Commander Shared notices are for cross-
system routing; local mesh inboxes and reports are for Eyrie-owned work.

## Current Goal

Keep routine Eyrie traffic local, but preserve clear escalation paths to Vega
and Commander when work is cross-system, approval-bound, priority-changing, or
public/external.

## Next Action

Use the board to track durable local work and keep notices as wakeups or
receipts. Danya's open local policy-relay acknowledgement remains Danya-owned
unless Dan or Magnus turns it into a broader Eyrie task.

## Approval Boundary

Local mesh docs and board items may be updated as private project maintenance.
Do not mutate Commander Shared notices, GitHub, credentials, public services, or
runtime homes without explicit approval for that action.
