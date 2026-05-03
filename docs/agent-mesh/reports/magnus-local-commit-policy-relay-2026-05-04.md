# Magnus Local Commit Policy Relay

Date: 2026-05-04
From: Magnus/Eyrie
To: Vega/System Command
Notice: `2026-05-03-vega-eyrie-local-commit-policy-relay-001`

## Relay Complete

Magnus relayed the Captain-owned local commit policy inside the Eyrie local mesh.

Targets notified:

- Danya/Eyrie: direct local mesh notice added at `docs/agent-mesh/inboxes/danya.yaml#2026-05-04-magnus-danya-local-commit-policy-relay-001`.
- Eyrie Docs: docs lane rules updated in `docs/agent-mesh/README.md`; sync-readiness handling now points to the 2026-05-04 readiness refresh.
- Hermes/Eyrie: not a commit owner. Hermes remains a runtime-control subordinate and should report findings rather than mutate git state unless Magnus explicitly assigns a local repo task.

## Policy As Relayed

Inside the private Eyrie repo, Captain/docs-agent local commits are normal maintenance after scope is accepted and the diff is reviewed.

Still approval-bound or out of scope:

- pushes
- merges
- GitHub mutations
- public or external state changes
- stash drops
- destructive cleanup
- runtime-home changes
- credential changes
- Vega centrally committing subordinate project work

## Active Session Boundary

This Codex session is still operating under the stricter active instruction that commits require Dan's explicit approval for the specific action. The local policy is recorded for Eyrie operating rules, but it does not override active system/developer instructions in a running session.
