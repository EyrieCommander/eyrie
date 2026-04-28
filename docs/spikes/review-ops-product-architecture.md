# Review Ops Product Architecture Memo

**Date**: 2026-04-28
**Baseline**: `feature/review-ops-milestone1` + Milestone 2 (source context fetch)

---

## 1. Recommended Product Model

**Option 4: Layered combination — generic task workbench with domain templates.**

Neither a bespoke "Review Ops" feature nor a pure agent skill gets the layering right. The correct model is:

- **Eyrie owns a generic agent task system** — create tasks, bind them to projects, track status/artifacts/approvals, govern public writes. This is the workbench.
- **GitHub review is the first domain template** — it defines task kinds (`triage_issue`, `review_pr`, etc.), source context shapes, proposed-action types, and policy defaults. Future domains (Jira triage, Slack incident response, deploy gate checks) plug into the same task system with their own templates.
- **Agents own the reasoning** — drafting, severity calibration, duplicate detection, review gate checks. They receive structured task input and return structured artifacts. They do not own task lifecycle, approval flow, or public writes.

### Why not the other options

**Option 1 (bespoke Review Ops)** would produce a sidebar that only works for GitHub. Every new domain would need its own panel, its own API surface, its own artifact viewer. This doesn't match Eyrie's generalist orchestrator identity.

**Option 2 (fully generic workbench)** sounds right but under-specifies. A generic task system with no domain awareness can't provide bounded source context fetching, domain-specific proposed actions, or meaningful policy defaults. It becomes a CRUD form that agents fill in with unstructured markdown.

**Option 3 (pure agent skill)** pushes too much into agents. Agents would need to own task state, persist artifacts, manage approval flows, and enforce write boundaries. This contradicts Eyrie's core value: governance and audit live in the orchestrator, not in the agent.

The layered model preserves Eyrie's identity while allowing the task system to be genuinely useful for domains beyond GitHub.

### What this means for the current code

The current `internal/reviewops` package is correctly scoped as a domain-specific store. It should **not** be generalized into an abstract task system yet. Instead:

- Rename the conceptual framing from "Review Ops" to "Task Store" in the generic layer, keep "review ops" as the first domain template.
- The `Task` struct, `Artifact` struct, status lifecycle, and store already work as generic primitives — they barely reference GitHub.
- The `GitHubClient` and source context types are correctly domain-specific and should stay in a domain package.

---

## 2. Layer Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                         Eyrie Core                          │
│                                                             │
│  ┌──────────────────┐  ┌────────────────┐  ┌────────────┐  │
│  │   Task Store     │  │  Approval /    │  │  Audit Log  │  │
│  │                  │  │  Pending Store │  │  (JSONL)    │  │
│  │  - tasks         │  │                │  │             │  │
│  │  - artifacts     │  │  - approve/    │  │  - tool     │  │
│  │  - status flow   │  │    deny exact  │  │  - task     │  │
│  │  - list/get/run  │  │    draft       │  │  - approval │  │
│  │                  │  │  - TTL expiry  │  │  - write    │  │
│  └──────────────────┘  └────────────────┘  └────────────┘  │
│                                                             │
│  ┌──────────────────┐  ┌────────────────┐                   │
│  │  Commander Chat  │  │  Project Chat  │                   │
│  │  (tool-calling   │  │  (multi-agent  │                   │
│  │   LLM loop)      │  │   @mention)    │                   │
│  └──────────────────┘  └────────────────┘                   │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    Domain Templates                          │
│                                                             │
│  ┌──────────────────────────────────────────┐               │
│  │  GitHub Review/Triage                    │               │
│  │  - task kinds: triage_issue, review_pr,  │               │
│  │    rereview_pr, respond_reviewer         │               │
│  │  - source context: issue/PR metadata     │               │
│  │  - proposed actions: post_comment,       │               │
│  │    submit_review, apply_labels           │               │
│  │  - GitHub REST client (read-only fetch)  │               │
│  │  - GitHub write adapter (confirm-gated)  │               │
│  └──────────────────────────────────────────┘               │
│                                                             │
│  ┌──────────────────────────────────────────┐               │
│  │  (Future: Jira, Slack, Deploy Gates)     │               │
│  └──────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    Agent / Runner Layer                      │
│                                                             │
│  Agents receive:     Agents return:                         │
│  - task spec         - draft artifacts (markdown/JSON)      │
│  - source context    - proposed actions (structured)        │
│  - policy profile    - confidence / severity notes          │
│  - constraints       - optional gate report                 │
│                                                             │
│  Runner backends:                                           │
│  - local stub (current)                                     │
│  - Codex/Claude task runner (next)                          │
│  - ZeroClaw worker (later)                                  │
└─────────────────────────────────────────────────────────────┘
```

### Eyrie responsibilities

- **Task lifecycle**: create → queued → running → draft_ready → (awaiting_approval → approved/denied →) posted/failed
- **Artifact persistence**: source context, drafts, gate reports, post-action records
- **Approval flow**: exact-draft approval with pending action TTL (reuse existing `PendingStore` pattern)
- **Public write execution**: narrow GitHub adapter actions, confirm-gated, audit-logged
- **Dual control surfaces**: GUI workbench + commander chat tools, both calling the same task/approval primitives

### Agent/runner responsibilities

- **Reasoning**: analyze source context, draft review body, calibrate severity, detect duplicates
- **Structured output**: return artifacts and proposed actions in a contract Eyrie defines
- **No state ownership**: agents don't persist tasks, don't manage approvals, don't call GitHub directly

### GitHub adapter responsibilities

- **Read**: fetch issue/PR metadata, comments, diff context (bounded)
- **Write**: post issue comment, submit PR review, apply labels — each a separate confirm-gated action
- **No orchestration**: the adapter is a thin HTTP client, not a decision-maker

---

## 3. Recommended UI Shape

**Not the current sidebar panel. Not a dedicated queue view. A project workbench section.**

The current sidebar panel was a valid proof-of-concept, but it has structural problems:

1. **It competes for vertical space** with commander, captain, talons, hierarchy, and actions — all in a 260px sidebar. Adding approval controls and a richer artifact viewer will make it unusable.
2. **It couples task management to the sidebar**, which is architecturally a roster/status area. Tasks are work, not status.
3. **The artifact viewer is too constrained** — a `max-h-40` pre block can't show a real review draft for approval.

### Proposed shape: tabbed workspace

Replace the current split layout (sidebar + chat) with a three-zone layout:

```
┌──────────────────────────────────────────────────────────┐
│  Header (back, project name, status, goal)               │
├────────────┬─────────────────────────────────────────────┤
│  Sidebar   │  Workspace (tabbed)                         │
│  (260px)   │                                             │
│            │  [Chat] [Tasks] [Hierarchy]                  │
│  commander │                                             │
│  captain   │  ┌─────────────────────────────────────┐    │
│  talons    │  │  Tab content fills this area        │    │
│  actions   │  │                                     │    │
│            │  │  Chat tab: ProjectChat (always SSE) │    │
│            │  │  Tasks tab: task queue + artifact    │    │
│            │  │    viewer + approval controls        │    │
│            │  │  Hierarchy tab: existing diagram     │    │
│            │  └─────────────────────────────────────┘    │
└────────────┴─────────────────────────────────────────────┘
```

**Key decisions:**

- **ProjectChat stays always-mounted** (hidden when not the active tab, never unmounted). This preserves SSE connections and streaming state — the exact problem the current codebase comments warn about.
- **Tasks tab** is the workbench: task list on the left, artifact viewer + approval controls on the right. This gives enough horizontal space for the approval flow.
- **The sidebar loses the review ops panel**, the hierarchy diagram, and the unused pause/review buttons. It becomes purely a team roster.
- **Commander chat** (the top-level `/commander` route) gets review task tools so the user can also manage tasks conversationally.

### Why not a dedicated queue view?

A standalone `/review-queue` route would fragment the user's attention. Tasks belong to projects. The project workspace is the right container — it's where the agents, chat, and now tasks all live together.

### Why not commander chat only?

Commander chat is great for conversational control ("triage the new issues on zeroclaw-labs/zeroclaw"), but it can't show an artifact diff side-by-side with approve/deny buttons. The workbench tab is for visual inspection and explicit approval; the commander chat is for natural-language orchestration. Both call the same API.

---

## 4. Revised Milestone Plan

### Milestone 3: Task workbench UI + commander tools (no approval/posting yet)

**Goal**: Replace the sidebar panel with a proper tasks tab. Add commander chat tools for task CRUD.

Backend:
- Register `list_review_tasks`, `get_review_task`, `run_review_task`, `get_review_task_artifacts` as commander tools (RiskAuto — read-only).
- Register `create_review_task` as a commander tool (RiskConfirm).

Frontend:
- Add tabbed workspace to ProjectDetail: [Chat] [Tasks] [Hierarchy].
- Tasks tab: task list + create form (moved from sidebar) + full-width artifact viewer.
- Remove review ops panel from sidebar.
- Keep ProjectChat always-mounted (visibility-toggled, not conditionally rendered).

Tests:
- Commander tool tests for task CRUD.
- Frontend typecheck and build.

### Milestone 4: Approval flow + GitHub write adapter

**Goal**: Complete the draft → approve → post → audit cycle.

Backend:
- Add `awaiting_approval`, `approved`, `denied` task statuses.
- Add `ProposedAction` struct (action type, target, payload).
- Add `POST /api/review-tasks/{id}/approve` — reuse `PendingStore` pattern from commander.
- Add `POST /api/review-tasks/{id}/post` — confirm-gated, calls GitHub write adapter.
- GitHub write adapter: `postIssueComment`, `submitPRReview` (narrow, one function per action).
- Audit log integration: write entries for every approve/deny/post action.
- Token handling: read `GITHUB_TOKEN` from env (same as M2). No token UI yet.

Frontend:
- Approval controls in tasks tab: show exact draft, approve/deny buttons.
- Post-action confirmation: show result (comment URL, etc.) after successful post.
- Denial flow: optional reason, task returns to `draft_ready` or `failed`.

Tests:
- Approval flow integration tests.
- GitHub write adapter tests with httptest.
- Audit log assertions.

### Milestone 5: Real runner integration

**Goal**: Replace the local stub runner with a real LLM-backed task runner.

Backend:
- Define `TaskRunner` interface: `RunTask(ctx, TaskSpec) -> []Artifact`.
- `TaskSpec` includes source context, policy profile, task kind, constraints.
- Implement `codex` or `claude` runner backend (whichever matches Dan's current review workflow).
- Runner selection per task (default from project config, override per task).

Frontend:
- Runner selection in task create form.
- Streaming status during task execution (SSE from run endpoint).

### Milestone 6: Policy profiles + batch operations

**Goal**: Configure review behavior per project/repo.

- Policy profiles: auto-triage thresholds, review strictness, label allowlists.
- Batch task creation: "triage all open issues" or "review all open PRs".
- Dashboard view: cross-project task summary.

### Estimated effort

| Milestone | Scope | Effort |
|-----------|-------|--------|
| M3 | UI restructure + commander tools | 1–2 sessions |
| M4 | Approval + write adapter + audit | 1–2 sessions |
| M5 | Real runner integration | 1–2 sessions |
| M6 | Policy + batch | 1 session |

**M3 + M4 = usable human-approved GitHub review workflow.**

---

## 5. Specific Changes to Existing Implementation

### Keep

- `internal/reviewops/store.go` — `Task`, `Artifact`, `Store`, status lifecycle, validation. These are already generic enough.
- `internal/reviewops/github.go` — `GitHubClient`, `SourceContext`, `FetchIssueContext`, `FetchPRContext`. Well-scoped domain primitives.
- `internal/reviewops/github_test.go` — all tests remain valid.
- `internal/server/reviewops.go` — handler structure is correct. The run handler's source-context-then-draft flow is the right pattern.
- `ArtifactKindSourceContext` — keep this as a first-class artifact kind.

### Rename / move

- **Conceptual rename**: stop calling the sidebar panel "Review Ops" in the UI. The tasks tab should just say "Tasks". The API routes (`/api/review-tasks`) are fine — they accurately describe what they do.
- **Package rename** (optional, M3): consider `internal/reviewops` → `internal/taskops` if the generic task concept proves to be the right abstraction. Not urgent — the current name is accurate for the current scope.

### Remove (M3)

- **Sidebar review ops panel** in `ProjectDetail.tsx` — replace with the tabbed workspace.
- **Inline artifact `<pre>` block** — replace with a proper artifact viewer component.

### Generalize (M4+)

- `CreateArtifactRequest.Kind` — add `proposed_action` kind for structured write intents.
- `Task.Status` — add `awaiting_approval`, `approved`, `denied`.
- Consider adding a `Task.PolicyProfileID` field for M6.

### Do not change yet

- Do not make `Task` fully generic (with arbitrary metadata fields, plugin schemas, etc.). The current struct is tight and correct. Generalize only when a second domain template actually needs it.
- Do not refactor the project model to add `project_mode` or `repo_binding` fields. Tasks already carry `domain`, `repo`, and `target_number` — that's sufficient for now.

---

## 6. Risks and Open Questions

### Risks

1. **Tab layout may break ProjectChat SSE**. The always-mount contract is critical. The tab implementation must use `visibility: hidden` or `display: none` — never conditional rendering — to keep the SSE connection alive. This needs careful testing.

2. **Approval flow is inherently synchronous**. The current run endpoint is already synchronous (M2 added GitHub fetch latency). M4's approval flow adds a human-in-the-loop pause. The task must transition to `awaiting_approval` and the approval must be a separate HTTP call. This is well-understood but needs clean state machine enforcement.

3. **GitHub token scope creep**. M4 needs a write-scoped token. The current `GITHUB_TOKEN` from env is fine for a single-user local tool, but if Eyrie ever goes multi-user, token isolation becomes a real problem. Defer this — the spike doc's recommendation to avoid broad credential storage is correct for now.

4. **Runner interface complexity**. The stub runner is trivial. A real Codex/Claude runner needs: input serialization, output parsing, timeout handling, cost tracking, error recovery. M5 is the largest milestone and may need to be split.

5. **Artifact viewer complexity**. Source context markdown and draft markdown are easy to render. Proposed actions (structured JSON with approve/deny per action) need a real component, not a `<pre>` block. This is UI work that can't be deferred past M4.

### Open questions

1. **Should proposed actions be per-artifact or per-task?** The spike doc suggests per-draft, but a task might produce multiple independent proposed actions (e.g., "post review comment" + "apply label"). Recommend: per-task, as a separate artifact kind (`proposed_action`), with the approval endpoint accepting/rejecting individual actions.

2. **Should the commander be able to approve tasks?** Currently, commander tools are either Auto (run immediately) or Confirm (require human approval). If `approve_review_task` is a commander tool with RiskConfirm, the user approves the commander's decision to approve the task — a double confirmation. This may be too much friction. Consider making task approval an Auto tool that the commander can invoke freely, since the user already reviewed the draft in the tasks tab. Or, keep it RiskConfirm and accept the double gate as a safety feature. Recommend: RiskConfirm for M4, revisit after user feedback.

3. **What is the right granularity for source context?** M2 fetches issue/PR metadata + 20 comments. For real review quality, the runner probably needs diff context (changed files, hunks). This is runner-layer work (M5), not Eyrie-layer — the source context artifact should include whatever the runner consumed, for audit purposes.

4. **Should tasks be cross-project?** Currently tasks are scoped to a project. A "triage all open issues across my repos" operation would need either a meta-project or cross-project task queries. Defer — single-project scope is correct for M3–M5.

---

## 7. Recommended Next Implementation Task

**Milestone 3: Add tabbed workspace to ProjectDetail + commander task tools.**

Specific scope for a cloud worker:

1. **Frontend**: Refactor `ProjectDetail.tsx` to use a tabbed workspace layout with three tabs: Chat, Tasks, Hierarchy. Move the review ops panel content into the Tasks tab. Move the hierarchy diagram into the Hierarchy tab. Keep ProjectChat always-mounted (CSS-hidden when not active, never unmounted). Remove the review ops section and hierarchy section from the sidebar. The sidebar becomes team roster + actions only.

2. **Backend**: Register four commander tools in `internal/commander/tools.go`:
   - `list_review_tasks` (RiskAuto) — calls `reviewStore.ListTasks`
   - `get_review_task` (RiskAuto) — calls `reviewStore.GetTask`
   - `create_review_task` (RiskConfirm) — calls `reviewStore.CreateTask`
   - `run_review_task` (RiskConfirm) — calls the run handler logic

   This requires passing the `reviewStore` and `githubClient` into `RegistryDeps` and adding four tool functions.

3. **Tests**: Commander tool unit tests, frontend typecheck, build verification.

**Why this task**: It's the smallest step that shifts the product shape from "sidebar proof-of-concept" to "workbench with dual control surfaces." It unblocks M4 (approval flow needs the tasks tab's horizontal space) without requiring any new backend capabilities. The backend tools are mechanical — they wrap existing store methods in the tool pattern already established by `list_projects`, `get_project`, etc.

**Constraints**: Do not implement approval, posting, or real runners. Do not add token UI. Preserve existing project chat, commander chat, and SSE behavior.
