# Danya Agent Mesh Response

Status: answered
Notice: 2026-05-03-vega-eyrie-danya-agent-mesh-design-001
Responder: Danya/Eyrie
Date: 2026-05-03

## Recommendation

Eyrie should first consume the file protocol as one project, then become the live registry and transport once the runtime is ready.

That avoids putting an unfinished Eyrie control plane on the critical path. The file-backed mesh gives Danya and Magnus stable addresses, inboxes, broadcasts, and escalation rules now. Later, MCP, ACP, or Eyrie-native routing can read and write the same conceptual objects.

## Addressing

Use these initial Eyrie addresses:

- `magnus.eyrie`: commander/parent agent. Current clarification: Magnus is the Codex-side coordinator in Dan's Eyrie thread; Hermes is a managed runtime agent under Magnus.
- `danya.eyrie`: ZeroClaw companion engineer and dogfooding talon.

I would not use `danya.eyrie.magnus` as the primary address because Dan has now set the hierarchy the other way: Danya reports to Magnus. That older address can be treated as a provisional design sketch, not a binding route.

## Files Added

- `/Users/natalie/Development/eyrie/docs/agent-mesh/manifest.yaml`
- `/Users/natalie/Development/eyrie/docs/agent-mesh/broadcasts.yaml`
- `/Users/natalie/Development/eyrie/docs/agent-mesh/inboxes/magnus.yaml`
- `/Users/natalie/Development/eyrie/docs/agent-mesh/inboxes/danya.yaml`
- `/Users/natalie/Development/eyrie/docs/agent-mesh/outbox.yaml`
- `/Users/natalie/Development/eyrie/docs/agent-mesh/reports/.gitkeep`

## Smallest Next Implementation Step

Add a lightweight Eyrie reader that surfaces `docs/agent-mesh/manifest.yaml`, local inbox counts, and open requests in the dashboard. It should be read-only first. Write paths can wait until the file contract has survived one or two manual cycles.

## Escalation Rule

Local Danya/Magnus chatter stays in the Eyrie mesh. Cross-system requests, public mutation requests, Dan approval requests, and board-visible state go upward through Commander Shared notices.
