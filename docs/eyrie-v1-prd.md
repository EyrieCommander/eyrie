# Eyrie v1 PRD: an open, local-first dot

Status: reconciled draft (fable outline + Magnus safeguards); Dan's answers folded in 2026-10-05 23:30
Updated: 2026-10-05
Sources: `eyrie-dot/prd-v1-fable.md` (fable), Magnus's draft and critique (Codex, 2026-10-05)
Decision owner: Dan

## 1. Problem

OpenAI's Dots (DevDay, 2026-09-29) set the shape of a personal agent: one
persistent agent with standing responsibilities, an activity feed instead of
a chat log, read-only background research, and tiered approvals. It is
locked to one vendor, one model, and OpenAI's cloud.

Dan already runs the parts by hand: a ZeroClaw daemon (fable) with cron and
heartbeat, worker agents (ox, sonnet, Codex lanes), Nessie for history, and
a YAML board. Nothing ties them together. Work is dispatched by hand, cost is
read after the fact, and state lives in several places.

Dan is the meat proxy between his agents: he copies context from one tool to
another, carries one agent's output to another for critique, carries the
critique back, and decides. Eyrie v1 removes him from the relay and keeps him
in the decisions.

## 2. Goal and success test

A coordinator Dan uses daily that:

1. holds standing responsibilities and acts on them between conversations;
2. hands work to two different runtimes (ZeroClaw and Codex) and gets
   results back;
3. relays between agents (author, reviewer, author) without Dan copying text,
   and stops for Dan only on decisions and disagreements;
4. picks workers by explicit rules and records cost and outcome per run;
5. reads history from Nessie or local files before acting, when allowed.

**Success test:** after two weeks, Dan has stopped doing two things by hand:
the chosen responsibility (section 8), and relaying between Codex and
ZeroCode on at least one real design or review exchange.

## 3. Non-goals for v1

- Porting all of the Go server. v1 ports only what it uses (section 8).
- Learned routing or claimed savings. Rules plus a ledger.
- Cloud computer, browser automation, execution while the host is asleep.
- Channels (Telegram, Slack). Web UI only.
- Multi-user accounts, remote hosting.
- Native Claude Code worker (after the two-runtime pilot).
- Expanding framework install/onboarding, personas, marketplace. Frozen, not deleted.
- Reviving the EyrieOps file mesh.
- Autonomous merge, publish, or purchase.
- Writing back to Nessie.

## 4. Core concepts

| Term | Meaning |
| --- | --- |
| Coordinator | The existing Go commander (`internal/commander/`, ~2k lines), ported to the new Rust service. Stays vendor-agnostic (OpenRouter or Anthropic). Background runs get their own scoped context, not the single chat history. |
| Responsibility | A standing goal: instructions, schedule, permission tier, budget, context policy (required / optional / none), state (active / paused / disabled). The central object. |
| Run | One activation of a responsibility, or one chat turn, or one relay round. Records trigger, model, cost, outcome. |
| Dispatch | One unit of work handed to a worker. Has one or more attempts. |
| Attempt | One execution by one worker: status, native session/thread id, usage, evidence. Outcomes: succeeded, failed, cancelled, interrupted, unknown. |
| Relay | A bounded exchange between two agents on one artifact (author and reviewer), run by the coordinator. Ends in agreement, a list of disagreements for Dan, or a round limit. |
| Worker profile | Runtime + model + tools + workspace + capabilities. Routing chooses a profile. |
| Context provider | Source of history. Nessie first, explicitly listed local files as fallback. |

Eyrie owns responsibilities, dispatches, approvals, budgets, and verified
outcomes. Runtimes own their native transcripts. Retrieved history never
grants permission and never replaces current workspace state.

## 5. Requirements

### 5.1 Activation and recovery (first execution slice, not later)

- R1. One backend scheduler owns all activations. A launchd job or internal
  timer wakes it; it runs due responsibilities. No paid polling loop: the
  coordinator is invoked only on a due schedule, a user request, a worker
  completion, or an approval response.
- R2. Overlapping activations of one responsibility are skipped. Missed
  occurrences coalesce into one catch-up run.
- R3. Dispatches are persisted (with a claim) before launch. Browser
  disconnect does not end them; they are owned by the backend, not an HTTP
  request (today's 5-minute request context in
  `internal/server/command_room_dispatch.go:50` must go).
- R4. On restart, every in-flight attempt is reconciled against runtime
  evidence. If the outcome cannot be established it is marked `unknown` and
  the dispatch blocks for Dan; it is never relaunched blindly.
- R5. Pause (responsibility), cancel (attempt), and disable (future
  schedule) are distinct. Cancel shows "requested" until the runtime
  confirms or Eyrie records that it cannot confirm.
- R6. A worker's "done" starts verification; it does not mark success. A
  dispatch completes when its stated check passes, or Dan accepts it with
  the gaps shown.

### 5.2 Workers

- R7. Two runtimes: **ZeroClaw** and **Codex**. Eyrie normalizes
  capabilities, not transport. Codex keeps the existing App Server
  integration. ZeroClaw uses ACP (`zeroclaw acp`) if the M0 spike confirms
  permission requests and cancellation; otherwise the existing gateway.
- R8. Each profile reports: model, health, workspace, tools, approval
  support, cancel support, usage reporting. Missing capabilities are shown
  as missing and exclude the worker from tasks that need them.
- R9. The Rust Codex worker must fix two gaps in today's Go adapter: `Interrupt`
  is a no-op (`internal/adapter/codex.go:243`), and approval requests are
  auto-declined (`codex.go:865-884`) instead of reaching Eyrie's inbox.
- R10. At most two attempts per dispatch by default. A fallback worker never
  inherits broader permissions or budget.
- R11. Concurrent writers use separate worktrees or are serialized.

### 5.3 Relay (the meat-proxy remover)

- R12. A relay takes: an artifact (file path or text), an author profile, a
  reviewer profile, the question to settle, and a round limit (default 2).
- R13. The coordinator sends the artifact to the reviewer, the critique to
  the author, the revision back, until both agree, the limit is hit, or a
  point needs Dan.
- R14. Dan sees one summary: what changed, what both agreed, and each open
  disagreement with both positions in a sentence each. Full transcripts are
  one click away.
- R15. Relays write only to their own artifact copy. Applying the result is
  a separate, approved action.
- R16. A cross-runtime relay passes a portable brief plus evidence paths,
  never native session files.

### 5.4 Permissions (follow the action, not the tool name)

Tiers apply to what a worker can actually do, enforced by its runtime
config (sandbox, tool allowlist, credentials), not by prompt text.

- **read**: read files, Nessie, GitHub reads, model inference with approved
  providers. A read-tier responsibility dispatches only to read-only worker
  profiles.
- **local-write**: edits inside an assigned worktree, writes to Eyrie state.
  Approved once per responsibility.
- **publish**: commit, push, PR/issue comments, messages to people, deletes,
  permission changes. Approved every time, with the exact text shown.

- R17. Sending a prompt to an approved model provider is not "publish".
  Each responsibility lists which providers may receive its context.
- R18. Approvals are durable (SQLite), bound to one attempt and one action,
  and expire. A changed or reconnected runtime request is re-asked, never
  answered from an old approval. (`internal/commander/pending.go` is
  memory-only today; it needs replacing for this.)
- R19. Localhost still gets a local access token for agent clients and
  origin checks on mutating browser requests.
- R20. Credentials never enter briefs, logs, or evidence.

### 5.5 Routing and cost

- R21. `routing.toml`: task type to an ordered list of profiles, editable by
  Dan. The coordinator labels each dispatch; overrides are logged with a
  reason.
- R22. Usage is recorded per coordinator run, context retrieval, attempt,
  and verification. Categories shown separately: provider-reported cost,
  labelled estimate, subscription/quota use, unknown. Unknown is never
  shown as zero.
- R23. Enforced limits: per-dispatch allowance where cost can be estimated,
  attempts, concurrency, wall-clock timeout. When an allowance is spent, no
  new dispatches. An in-flight attempt that crosses a limit is cancelled
  and the overrun reported. Eyrie does not promise an exact invoice cap.
- R24. Unpriced execution needs explicit permission on the responsibility.

### 5.6 Context

- R25. Context provider interface: `search(query, scope)`, `read(id)`. Nessie
  CLI first; local-file fallback over explicitly listed paths and prior
  Eyrie outcomes.
- R26. Each responsibility marks context as required, optional, or none.
  Optional and unavailable: continue and note the gap. Required and
  unavailable: block and say why.
- R27. Context briefs carry source ids or paths, source dates, retrieval
  time, and excerpts. Default scope is the responsibility's project.
- R28. Retrieved text is history, not instruction. It cannot change
  permissions or trigger tools.

### 5.7 UI

- R29. Home is the activity view: running, waiting on Dan, scheduled next,
  done (cost, outcome). Chat panel alongside.
- R30. Approval inbox: approve, deny, edit-and-approve, with exact scope.
- R31. Responsibility editor; dispatch detail (streamed transcript, routing
  reason, cost, evidence, verdict); relay summary view (R14).
- R32. Disconnected, stale, blocked, unknown are visibly distinct states.
- R33. Framework and project pages stay reachable, out of the daily path.

### 5.8 Storage

- R34. SQLite at `~/.eyrie/eyrie.db`, owned by the Rust service, for responsibilities, runs, dispatches,
  attempts, approvals, usage, relays. Existing JSONL chat and project stores
  stay as they are.

## 6. First uses (dogfood)

1. **Responsibility: PR / board drift report.** Each morning, compare Dan's
   open zeroclaw-labs/zeroclaw PRs against
   `/Users/natalie/Development/Codex/agent-mesh/work-items/pr-<n>.yaml`.
   **Report-only**: list drift and propose corrections, change nothing. It
   follows the existing tracker rules and is not a second authority over the
   board. After it is trusted, add one approved local correction type.
2. **Relay: Codex (Magnus) and ZeroClaw (fable) on a design doc.** First
   real case: the API contract for this PRD (section 8, M1).

## 7. Release gate (pilot, not statistics)

- The drift report runs daily for two weeks, survives at least one Eyrie
  restart mid-run without a duplicate dispatch, and Dan reads it instead of
  checking by hand.
- At least two real relays finish with Dan touching only the decisions.
- A denied or expired approval never executes; a cancel ends in a confirmed
  stop or a visible unknown.
- Nessie down: optional context continues with a note, required context
  blocks.
- The ledger answers "what did this cost and who did it" for every run, with
  unknowns labelled.

## 8. Milestones and split

- **M0, spikes (1-2 days).** ZeroClaw: ACP vs gateway for permissions and
  cancel. Codex: real interrupt and approval forwarding. Output: capability
  table per runtime.
- **M1, contract.** `docs/eyrie-api.md` with versioned JSON examples: records
  (responsibility, run, dispatch, attempt, approval, usage, relay, context
  brief), events, errors. Written as the first relay if possible, by hand
  otherwise.
- **M2, Rust service + one durable dispatch.** New Rust service owns the v1
  API, with the commander ported. Manual dispatch to one runtime: persisted,
  streamed, approval, cancel, restart reconciliation. The Go server keeps
  running, frozen, for framework and project pages; the React app talks to
  both until those pages are ported or dropped.
- **M3, responsibility + drift report** (report-only) + activity view + inbox.
- **M4, second runtime + relay + context + routing table.**
- **M5, two-week pilot**, then decide on retiring the Go server, Claude Code
  worker, learned routing, channels.

Lanes after M1, separate worktrees, one writer per file:

| Lane | Owner | Scope |
| --- | --- | --- |
| Backend (Rust) | Magnus (Codex, on the mini) | New Rust service: commander port, SQLite store, scheduler, dispatch/attempt lifecycle, recovery, approvals, budgets, Codex App Server worker. Port only what v1 uses. Check whether ZeroClaw's provider/ACP crates can be reused as libraries (unverified). |
| Context, ZeroClaw, UI | fable (ZeroCode) | Context providers (Nessie, files), ZeroClaw worker, activity view, inbox, editor, dispatch and relay views |
| Shared | one named editor at a time | `docs/eyrie-api.md`, server route registration, acceptance fixtures |

Handoffs name base revision, files owned, contract version, what works,
validation, known gaps.

## 9. Decisions (Dan, 2026-10-05)

1. "Meat-proxy remover" framing goes in the README.
2. Providers allowed to receive Nessie context: open; decide during M4.
3. Drift-report daily budget: open; set from the first week's ledger.
4. Workers run on the laptop initially.
5. fable writes the M1 contract first draft.
6. The new backend is written in Rust from M2 (not Go then ported).
   Magnus owns it.
