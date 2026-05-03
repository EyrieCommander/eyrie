# Eyrie Agent Mesh

Status: provisional
Owner: Magnus/Eyrie
Updated: 2026-05-04

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

Captain-owned local commits inside this private Eyrie repo are normal maintenance
after scope is accepted and the diff is reviewed. Active session instructions can
still be stricter; when a running Codex session says commits require Dan's
explicit approval, follow that session boundary. Pushes, merges, GitHub
mutations, public or external actions, stash drops, destructive cleanup,
runtime-home changes, and credential changes remain approval-bound.

## Where To Write What

Commander Shared Eyrie routing uses one domain inbox. The canonical
Commander-level inbox is `Eyrie/Ops`
(`/Users/dan/Documents/Personal/Commander/Shared/notices/eyrie-inbox.yaml`).
`Magnus/Eyrie` is the captain identity for notice text, acknowledgements,
payloads, and response artifacts, not a second live Commander Shared queue.

Inside this repo, `docs/agent-mesh/inboxes/magnus.yaml` remains the local parent
inbox for Eyrie subordinates such as Danya, Hermes, and Docs. That local mesh
inbox is separate from Commander Shared routing.

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

## Identity And Logs

- Magnus lore: `docs/magnus-lore.md`
- Magnus pilot log: `docs/magnus-pilot-log.md`
- Danya lore: `docs/danya-lore.md`
- Danya pilot log: `docs/danya-pilot-log.md`
- Eyrie Docs lane note: `docs/eyrie-docs-lore.md`
- Eyrie Docs pilot log: `docs/eyrie-docs-pilot-log.md`

## Status Surface

The in-progress read-only status surface should summarize the manifest, open
inbox requests, latest outbox entry, report links, and Commander Shared notice
references. It should remain read-only until Dan explicitly approves write
operations through Eyrie.
