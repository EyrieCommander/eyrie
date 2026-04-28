# Eyrie Autonomous ZeroClaw Workflow Spike

## Executive recommendation

Start with a **human-approved draft workflow** in Eyrie, not full autonomy:

1. Create a “review/triage task” inside a normal Eyrie project.
2. Let a worker agent (first milestone: Codex/Claude-style task runner) produce a draft artifact.
3. Require explicit approval for any GitHub write action.
4. Execute that write through a narrowly-scoped adapter action.
5. Persist complete audit context (inputs, draft, approver, action).

This aligns with Eyrie’s current architecture: Eyrie as commander/governance layer with external workers doing project work. It also preserves the current human safety contract while making Dan faster.

---

## Current reusable Eyrie pieces

### Concepts that already map cleanly to PR review + issue triage

- **Projects** already provide a stable container with status/lifecycle and team context.
- **Captain/talon model** can represent review roles (e.g., triage specialist, reviewer, response drafter) when needed.
- **Commander tool risk model** (`RiskAuto` vs `RiskConfirm`) directly maps to read-only vs approval-gated public actions.
- **Pending confirmation store** already provides explicit, expiring approval records.
- **Audit log** already captures write attempts/outcomes in append-only JSONL.
- **Project chat + commander chat** already provide two control surfaces: GUI-first operations and conversational assist.
- **Adapter boundary** is already framework-agnostic (`adapter.Agent`), keeping Eyrie from becoming ZeroClaw-specific.

### Existing UI surfaces that can be reused

- `CommanderChat` already renders tool calls, confirm-required actions, and approve/deny controls.
- `ProjectDetail` already presents Eyrie as built-in commander and can host a minimal task queue panel.
- Existing API client/types infrastructure (`web/src/lib/api.ts`, `web/src/lib/types.ts`) can absorb small additions without a large refactor.

---

## Proposed minimal workflow

### 1) “ZeroClaw review/triage project” as a normal Eyrie project mode

Add lightweight metadata to a project (rather than creating a parallel subsystem), e.g. `project_mode = github_review_ops` and repo binding (`owner/repo`).

### 2) Task-first model (smallest useful unit)

Introduce a `ReviewTask` record with task types:

- `triage_issue`
- `review_pr`
- `rereview_pr`
- `respond_reviewer`

Suggested statuses:

- `queued`
- `running`
- `draft_ready`
- `awaiting_approval`
- `approved`
- `posted`
- `denied`
- `failed`

### 3) Draft artifacts before any public write

Every task produces persisted draft artifacts first:

- proposed issue comment
- proposed PR review body + recommendation
- optional suggested labels
- optional gate report

No GitHub mutation occurs at draft generation time.

### 4) Approval + post as separate step

User approves exact draft in GUI (or via commander confirm flow), then Eyrie executes one narrow public action and records the result.

---

## Data model additions

Keep additions minimal and composable.

### 1) Project metadata extension (or sidecar)

- `repo_owner`
- `repo_name`
- `default_runner`
- `policy_profile_id`

### 2) `ReviewTask`

- `id`, `project_id`, `task_type`, `status`
- target selector (`issue_number` or `pr_number`)
- `runner_kind`, `runner_ref`
- `created_by`, timestamps
- optional source revision snapshot (e.g., PR head SHA)

### 3) `DraftArtifact`

- `id`, `task_id`, `kind`
- `content` (markdown/text)
- `proposed_actions` (structured write intents)

### 4) Approval linkage

- `task_id`, `draft_id`, `pending_id`
- `approved_by`, `approved_at`, `decision_reason`

### 5) Public action record

- action type
- request summary
- external IDs (`comment_id`, `review_id`, etc.)
- links to task + draft + approval + audit entry

---

## Commander tools/API additions

Prefer narrow primitives over one giant “do review” tool.

### Read-only tools/endpoints (auto)

- `github_get_issue`
- `github_get_pr`
- `github_list_reviews`
- `reviewtask_list`
- `reviewtask_get`
- `reviewtask_get_artifacts`

### Internal workflow write tools (usually auto)

- `reviewtask_create`
- `reviewtask_run`
- `reviewtask_save_draft`

### Public GitHub mutation tools (always confirm)

- `github_post_issue_comment`
- `github_submit_pr_review`
- `github_apply_labels` (narrow allowlist)
- (defer broader mutations like status checks unless needed)

### Minimal HTTP endpoints for GUI parity

- `POST /api/review-tasks`
- `GET /api/review-tasks?project_id=...`
- `GET /api/review-tasks/{id}`
- `POST /api/review-tasks/{id}/run`
- `GET /api/review-tasks/{id}/artifacts`
- `POST /api/review-tasks/{id}/approve`
- `POST /api/review-tasks/{id}/post` (confirm-gated)

This preserves dual control: GUI-first and commander chat both use the same underlying primitives.

---

## Agent-runner and adapter execution options

## Recommendation: do **not** start with ZeroClaw as the first worker substrate

For milestone 1, a **Codex/Claude-style task runner** is a better fit because it mirrors the current human workflow (inspect context → draft comment/review → optional gate output).

Then add ZeroClaw runner support behind the same interface.

### Proposed abstraction

Introduce a small `TaskRunner` interface (separate from long-lived `adapter.Agent`):

- `RunTask(ctx, taskSpec) -> artifacts + logs + proposed_actions`

### Candidate runner backends

- local Codex exec / Cursor subagents
- Claude CLI headless
- Codex Cloud task wrapper
- ZeroClaw-backed worker (later)
- future remote worker (Devin-style) if useful

### Why this sequencing is safer

- smallest path to value
- easiest to keep outputs deterministic/draft-oriented
- avoids forcing chat/lifecycle-oriented adapter contracts onto short-lived review jobs
- lets ZeroClaw RFC #5890 improvements plug in later without redesigning UI/API

---

## Safety and approval model

Safety boundaries required for v1:

1. **Explicit human approval before any public GitHub mutation.**
2. **Draft always visible before post.**
3. **Review gates default to PR review tasks only** (issue triage gates only on explicit request).
4. **Least-privilege credentials by default** (read + comment first, broader scopes opt-in).
5. **Complete audit chain** for every public action:
   - source context snapshot
   - generated draft
   - approval decision
   - executed action + response
   - timestamps + actor identity

Implementation note: reuse commander pending + audit semantics rather than introducing a second approval mechanism.

---

## Minimal UI

Build one compact supervision surface inside existing project UX.

### First UI slice

1. **Task queue panel** (in `ProjectDetail`):
   - create task (type + issue/PR number)
   - status chips
   - quick filters

2. **Task detail pane**:
   - source context summary
   - draft artifact viewer
   - proposed actions
   - approve/deny controls
   - post result

3. **Audit timeline segment**:
   - draft generated
   - approval requested
   - approval decision
   - action posted

No giant dashboard needed for milestone 1.

---

## Implementation sequence

1. Add `ReviewTask` + `DraftArtifact` stores and minimal CRUD.
2. Add read-only GitHub fetch primitives.
3. Add one runner backend (Codex/Claude-style) to produce drafts.
4. Add confirm-gated public write primitives and audit linkage.
5. Add minimal GUI task queue + draft approval UI in `ProjectDetail`.
6. Add second runner backend (ZeroClaw) behind same `TaskRunner` contract.
7. Harden policy controls (scopes, allowlists, per-action toggles).

---

## Risks and open questions

- **Schema drift across runners** (need strict artifact contract early).
- **Stale source context** between draft and posting (need SHA/version checks).
- **Multi-tab approval races** (need task versioning/locking semantics).
- **Scope creep in GitHub mutations** (defer labels/status broadening until basic flow is stable).
- **Task granularity choices** (single-post tasks vs full review lifecycle task; start simple).

---

## What should not be built yet

- Full autonomous posting without approval.
- Broad GitHub mutation surface (statuses/check-runs/projects/admin changes).
- Large cross-project command center dashboard.
- Heavy framework-specific abstractions before proving the generic task contract.
- Complex multi-agent negotiation/scheduling engines.

Focus on: **small, testable, draft-first, human-approved workflow** that immediately improves speed and safety.
