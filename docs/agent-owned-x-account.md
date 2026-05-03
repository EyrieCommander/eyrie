# Draft-Only Agent-Operated X Account Workflow

Status: draft
Owner: Magnus/Eyrie
Date: 2026-05-03

## Goal

Design the first safe Eyrie and ZeroClaw workflow for an agent-operated X account.

The first milestone is draft-only. No account creation, credential storage, OAuth, posting, replying, liking, following, deleting, DMing, scraping beyond approved sources, or other public mutation is in scope.

## Operating Principle

Eyrie is the governance and control plane. ZeroClaw or another runner can produce draft work. The user approves exact public text before anything external happens.

The account framing should be transparent: agent-operated and Dan-owned or Dan-authorized, not legally self-owned.

## Milestone 1: Draft Queue Only

Create an Eyrie project mode for an X account workflow with these objects:

- `AccountProfile`: public-facing identity constraints, topics, tone, disallowed topics, and approval policy.
- `DraftTask`: the unit of work, such as `draft_post`, `draft_reply`, `summarize_thread`, or `propose_content_calendar`.
- `DraftArtifact`: exact proposed text plus rationale and source refs.
- `ApprovalRecord`: Dan's explicit approval, denial, or edit request.
- `PublicActionRecord`: deferred for later milestones; not used in milestone 1.

Draft task statuses:

- `queued`
- `running`
- `draft_ready`
- `needs_revision`
- `approved_for_manual_use`
- `rejected`
- `archived`

Do not include `posted` in milestone 1.

## Milestone 1 UI

Add a compact panel inside an Eyrie project:

- create draft task;
- show source refs and constraints;
- run selected local runner;
- display exact draft text;
- approve for manual use, request revision, reject, or archive.

The approval button should say `approve for manual use`, not `post`.

## Runner Shape

Use the same `TaskRunner` direction from `docs/spikes/eyrie-zeroclaw-autonomy.md`.

Milestone 1 runner options:

- Codex/Hermes runner for direct draft generation;
- ZeroClaw-backed worker later, once Eyrie can provision and observe it cleanly;
- EyrieClaw embedded Talon for simple drafting once its skill/context path is good enough.

The runner receives:

- account profile constraints;
- task type;
- source refs;
- requested angle;
- banned actions;
- output schema.

The runner returns only artifacts, logs, and proposed next actions. It never receives credentials.

## Safety Gates

Required gates:

- no X credentials in Eyrie milestone 1;
- no OAuth implementation in milestone 1;
- no browser automation for public writes;
- no external mutation tools exposed to draft runners;
- source refs required for factual claims;
- exact draft text preserved in the artifact;
- user approval recorded separately from draft generation;
- public posting remains manual outside Eyrie.

## Later Milestone: Confirm-Gated Public Action

Only after the draft queue is useful, add a narrow action adapter:

- `x_post_tweet`
- `x_reply`
- `x_delete_own_post`

Every action must require exact-text confirmation and produce an audit record with request, approved text, actor, timestamp, API response, and external URL/id. Default policy stays supervised.

## Recommended Next Implementation Task

Implement milestone 1 as an internal draft queue, not an X integration:

1. Add `DraftTask` and `DraftArtifact` stores.
2. Add one local runner that writes draft artifacts.
3. Add a project panel to inspect, revise, approve for manual use, reject, or archive drafts.
4. Reuse Eyrie's existing commander confirmation and audit concepts where possible.