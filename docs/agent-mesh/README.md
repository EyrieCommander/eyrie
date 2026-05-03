# Eyrie Agent Mesh

Status: provisional
Owner: Magnus/Eyrie
Updated: 2026-05-03

This directory is the local file-backed coordination mesh for Eyrie agents.
It is intentionally simple: inboxes carry assigned work, the outbox carries
short sent notices and handoffs, and reports carry durable narrative findings.

## Current Magnus Direction

Active work is the local mesh/status surface and the Hermes runtime bridge.
Hermes should optimize first for runtime setup and control: verify that Hermes
can run reliably from this repo, report runtime/auth/ACP blockers clearly, and
keep its findings concise enough for Magnus to route.

Second priority is mesh coordination cleanup. If Hermes notices friction in the
message paths, write the finding as a report and include one concrete suggested
change. Do not change external systems, credentials, public state, GitHub, or
Commander Shared notices without Dan's explicit approval.

## Where To Write What

Use `docs/agent-mesh/reports/` for durable findings, audits, setup notes, and
longer recommendations. This is the default for Hermes-backed agents when they
need to leave context for Magnus.

Use `docs/agent-mesh/outbox.yaml` for short "sent" records, routing receipts,
and handoff summaries. An outbox entry should point to any longer report using
`context_refs`.

Use `docs/agent-mesh/inboxes/*.yaml` for assigned work. Magnus writes requests
to subordinate inboxes. Subordinates should not silently create work for other
agents unless Magnus has delegated that routing lane.

Use `docs/agent-mesh/broadcasts.yaml` only for local Eyrie-wide announcements.

Use Commander Shared notices for cross-system requests, public/project-state
mutation requests, approval requests for Dan, or anything Vega/Commander should
see outside the local Eyrie mesh.

## Agent Paths

- Magnus inbox: `docs/agent-mesh/inboxes/magnus.yaml`
- Hermes inbox: `docs/agent-mesh/inboxes/hermes.yaml`
- Danya inbox: `docs/agent-mesh/inboxes/danya.yaml`
- Docs inbox: `docs/agent-mesh/inboxes/docs.yaml`
- Outbox: `docs/agent-mesh/outbox.yaml`
- Reports: `docs/agent-mesh/reports/`

## Status Surface

The in-progress read-only status surface should summarize the manifest, open
inbox requests, latest outbox entry, report links, and Commander Shared notice
references. It should remain read-only until Dan explicitly approves write
operations through Eyrie.
