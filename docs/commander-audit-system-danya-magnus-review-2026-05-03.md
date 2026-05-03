# Review: Commander Audit System From Eyrie

Reviewer: Magnus/Eyrie
Status: complete
Verdict: approve-for-pilot
Notice: 2026-05-03-vega-eyrie-captains-audit-pilot-review-001

## Findings

- [P2] Audit packets map cleanly to Eyrie, but they should not become live transport.

  The packet shape is a good fit for Eyrie's captain/Talon model: a captain owns scope and approval boundaries, Talons write review artifacts, and the durable packet preserves source context. That should remain a review artifact layer, not the message bus itself. Local mesh notices should route work; audit packets should preserve review evidence.

- [P2] Eyrie needs a packet index before this scales beyond pilots.

  `AUDIT.md` gives a useful convention, but Eyrie and Commander will need a compact machine-readable index if agents are expected to discover open packets, assigned reviewers, status, and response paths. The existing notice system already has an index; audit packets do not yet. Do not block the pilot on this, but add it before making audits routine.

- [P2] Keep packet ownership in the repo that owns the change.

  Commander audit packets belong in Commander. Eyrie review responses can live in Eyrie and be linked from the Commander notice, as this file does. If Eyrie later implements an audit UI, it should read packets from their owning repos rather than copying them into a central store.

- [P3] The Danya routing path worked, but the return path is still manual.

  The local Eyrie mesh correctly routed Ariadne's dogfood-audit request through Magnus into `docs/agent-mesh/inboxes/danya.yaml`. That proves the addressing shape, but there is still no automatic status import, acknowledgement update, or Commander response closure. The next useful Eyrie dogfood task is to make the read-only mesh dashboard show these edges.

## Eyrie Mapping

Use this mapping for now:

- Commander Shared notice: cross-system request and response pointer.
- Eyrie local mesh inbox: assignment to Magnus, Danya, Hermes, or another Eyrie subordinate.
- Audit packet: durable review object owned by the repo being changed.
- Review artifact: written by the reviewing agent in its own repo or project folder and linked back from the notice or packet.
- Commander board/status: only for board-visible state, blockers, or completed results.

This keeps routine local work local while still letting Vega see meaningful state.

## Danya Angle

Danya's existing mesh-design response recommends that Eyrie first consume the file protocol as one project, then become the live registry and transport later. I agree. The audit system is a good dogfood case precisely because it has bounded packets, named reviewers, response artifacts, and approval boundaries.

The friction point is manual closure: after a Talon writes a response, a parent still has to update multiple files by hand. Eyrie should solve read visibility before write automation.

## Recommendation

Pilot the audit system as local-first review infrastructure. Do not require it for every small Commander routing or dashboard cleanup. Require it for protocol changes, shared-system changes, risky scripts, and cross-system process changes.

The first Eyrie implementation task is not an audit writer. It is a read-only audit/mesh status reader that can show:

- open audit packets;
- requested reviewers;
- linked local mesh notices;
- response artifact paths;
- validation state;
- approval boundary.

## Validation

Read:

- `/Users/dan/Documents/Personal/Commander/AUDIT.md`
- `/Users/dan/Documents/Personal/Commander/audits/2026-05-03-audit-system/REQUEST.md`
- `/Users/dan/Documents/Personal/Commander/audits/2026-05-03-audit-system/reviews/vega.md`
- `/Users/dan/Documents/Personal/Commander/Shared/AGENT_MESH_DESIGN.md`
- `/Users/natalie/Development/eyrie/docs/agent-mesh/inboxes/danya.yaml`
- `/Users/natalie/Development/eyrie/docs/danya-agent-mesh-response-2026-05-03.md`

Ran:

```sh
GOCACHE=/private/tmp/eyrie-gocache GOMODCACHE=/private/tmp/eyrie-gomodcache go test ./...
```

Result: pass.

