# Agent-Owned X Account Notice Response

Status: complete
Responder: Magnus/Eyrie
Date: 2026-05-03
Notice: 2026-05-02-rowan-eyrie-x-agent-001

## Response

Created the draft workflow plan at:

- `/Users/natalie/Development/eyrie/docs/agent-owned-x-account.md`

The design keeps milestone 1 draft-only. Eyrie acts as governance/control plane, and a runner such as Codex, Hermes, ZeroClaw, or EyrieClaw produces draft artifacts. No credentials, OAuth, posting, replying, liking, following, DMs, deletes, or other public X mutation are in scope.

## Key Decisions

- Start with `DraftTask` and `DraftArtifact`, not an X API adapter.
- The approval state is `approved_for_manual_use`, not `posted`.
- Runners receive constraints and source refs, never credentials.
- Later public actions must be exact-text confirm-gated and auditable.
- The account should be framed as agent-operated and Dan-owned or Dan-authorized.

## Validation

Read:

- `/Users/natalie/Development/eyrie/docs/spikes/eyrie-zeroclaw-autonomy.md`
- `/Users/natalie/Development/eyrie/docs/PLAN.md`
- `/Users/natalie/Development/eyrie/docs/TODO.md`
- `/Users/natalie/Development/eyrie/docs/plan-onboarding-flow.md`

Ran:

```sh
GOCACHE=/private/tmp/eyrie-gocache GOMODCACHE=/private/tmp/eyrie-gomodcache go test ./...
```

Result: pass.

## Risks

- Any real X integration will need separate policy, credentials design, and explicit Dan approval.
- Source freshness matters for public claims, so draft tasks must store source refs and timestamps.
- Runner output must be schema-constrained before Eyrie can safely automate batch draft creation.

## Next Recommended Task

Add a generic draft queue to Eyrie projects. Keep it platform-neutral so the same mechanism can support X posts, GitHub comments, status updates, or other human-approved public text later.