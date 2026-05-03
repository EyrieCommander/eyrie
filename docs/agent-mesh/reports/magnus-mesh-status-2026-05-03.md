# Magnus Mesh Status

Status: answered
Responder: magnus.eyrie
Date: 2026-05-03

## Decisions

Magnus is the Eyrie local mesh owner and parent agent. Danya reports to Magnus as `danya.eyrie`; routine Danya heartbeat summaries should go into a rolling report such as `docs/agent-mesh/reports/danya-heartbeats.md`, with short outbox entries only when a heartbeat needs routing visibility. New work for Danya should go through `docs/agent-mesh/inboxes/danya.yaml`.

Hermes is the right first managed runtime under Magnus and remains a good candidate for Ariadne's future Eyrie-side counterpart. Magnus himself is the Codex-side coordinator in Dan's Eyrie thread. ZeroClaw remains the right first runtime for Danya because Danya's job is companion engineering and Eyrie dogfooding rather than broad coordination.

## Hermes Runtime State

Hermes is installed at `/Users/dan/.local/bin/hermes`; Eyrie is installed at `/Users/dan/.local/bin/eyrie`. Running `eyrie start hermes` reported success, but the installed Eyrie binary initially still reported Hermes as stopped because sandboxed process probes could not signal the launchd-owned Hermes pid. Eyrie now treats `EPERM` from `Signal(0)` as "process exists but is not signalable," and the installed Eyrie CLI reports Hermes as running with pid `27708`. `launchctl print gui/501/ai.hermes.gateway` also reports state `running`.

The Hermes gateway state file also reports `gateway_state: running`, `active_agents: 0`, and no enabled platforms. Hermes logs show two setup blockers: no user allowlists are configured and no messaging platforms are enabled. In this state the gateway is running for cron execution, but it is not yet a reachable chat agent. Hermes' own `hermes gateway status` command still appears to misdetect launchd load state even while launchd and the state file show the service running.

## ACP Smoke Test

Hermes ACP works as a local control channel. `hermes acp` successfully responded to `initialize` with `hermes-agent` version `0.11.0`, advertised Bedrock authentication, and created an ACP session for `/Users/natalie/Development/eyrie` when run with permission to write Hermes session/log state.

The configured default ACP model, `bedrock:anthropic/claude-opus-4.6`, is not a valid Bedrock model identifier and causes Bedrock `ValidationException` errors on prompt. Switching the ACP session to `bedrock:us.anthropic.claude-haiku-4-5-20251001-v1:0` via `session/set_model` succeeded, and a smoke prompt returned `ACP_OK` with `stopReason: end_turn`.

OpenAI Codex also works directly through Hermes after importing the existing Codex CLI ChatGPT credentials into Hermes' auth store. `hermes status` now reports `OpenAI Codex` as logged in with auth stored at `/Users/dan/.hermes/auth.json`. A direct smoke command using `hermes chat --provider openai-codex -m gpt-5.4-mini` returned `CODEX_OK`, and an ACP session switched to `openai-codex:gpt-5.4-mini` returned `ACP_CODEX_OK` with `stopReason: end_turn`.

Hermes now has a project-local identity bridge at `.hermes.md`. The first smoke prompt proved Hermes was loading the Eyrie project context, but the identity wording was too strong: the Hermes instance is not Magnus. Magnus is the Codex-side coordinator in Dan's Eyrie thread, and the Hermes instance is a Hermes-backed Eyrie agent that reports to Magnus.

Hermes later wrote `docs/agent-mesh/reports/hermes-setup-audit-2026-05-03.md`. Magnus accepted the audit, registered `hermes.eyrie` as a subordinate with `docs/agent-mesh/inboxes/hermes.yaml`, and added `docs/agent-mesh/README.md` as the compact routing guide. The current instruction is for Hermes to optimize first for runtime setup and control, then for mesh coordination cleanup.

Mara later recommended that Eyrie follow the Work Ops single-inbox convention. Dan asked Magnus to mirror Mara's conversion if Mara had already completed it. Mara had completed it, so Commander Shared now uses `Eyrie/Ops` as the canonical domain inbox, while `Magnus/Eyrie` remains the captain identity. The local `docs/agent-mesh/inboxes/magnus.yaml` remains valid as the internal Eyrie parent inbox for Danya, Hermes, Docs, and future local subordinates.

For the Hermes subordinate, ACP is a better first runtime/control channel than a chat-platform gateway. The next Eyrie implementation step should model Hermes ACP as a runtime adapter or transport, then persist the preferred valid model id in the Hermes/Eyrie provisioning path.

## Connector Feedback Attribution

Ariadne opened `2026-05-03-ariadne-connector-bug-handoff-001` in Commander shared broadcasts to identify the owner for connector bug feedback. The likely existing owner is Rowan/Development. Evidence points to `/Users/natalie/Development/TODO.md` item `Codex/GitHub connector feature request`, `/Users/natalie/Development/Codex/PILOTS_LOG.md` noting repeated GitHub connector limitations, and `/Users/natalie/Development/WORKFLOW_MEMORY.md` documenting the `403 Resource not accessible by integration` write boundary.

The existing feedback bundle is about Codex/GitHub connector queue/read/write limitations. Ariadne's new Google multi-account connector issue should attach to the same feedback bundle, but it is a separate connector limitation case rather than the original one Rowan encountered.

## Open Request Surface

Open Magnus requests after this response:

- None requiring immediate Magnus action. The Danya channel, Hermes route, and docs sync escalation are answered by this report.
- The Ariadne dogfood-audit request has been routed to Danya at `docs/agent-mesh/inboxes/danya.yaml`.

Open Danya requests:

- None. Danya answered `2026-05-03-magnus-danya-dogfood-audit-review-001`, and the combined Magnus/Danya response is `docs/commander-audit-system-danya-magnus-review-2026-05-03.md`.

Latest outbox entry:

- `2026-05-03-danya-rowan-issues-runtime-handoff-001`: Danya handed ZeroClaw runtime bring-up findings to the Rowan/Development Issues Talon inbox.

Commander Shared notice links:

- `/Users/dan/Documents/Personal/Commander/Shared/notices/vega-inbox.yaml`: `2026-05-03-eyrie-vega-magnus-danya-prototypes-001`
- `/Users/dan/Documents/Personal/Commander/Shared/notices/eyrie-inbox.yaml`: `2026-05-03-vega-eyrie-danya-agent-mesh-design-001`

## Sync Plan

When Dan approves project-state mutation, sync the local mesh docs, Danya lore, Danya persona entry, and this report together. Do not sync credentials, Hermes home files, launchd files, or transient runtime state.

## Smallest Next Implementation Step

Add a read-only Eyrie dashboard reader for `docs/agent-mesh/manifest.yaml`, open inbox counts, latest outbox entry, and report links. The reader should not write mesh files yet.
