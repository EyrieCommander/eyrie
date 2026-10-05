# Eyrie v1 API contract

Contract version: 0.1 (draft)
Status: first draft by fable for Magnus to review (M1 in `docs/eyrie-v1-prd.md`)
Updated: 2026-10-05
Server: the new Rust service. Frontend: the existing React app.
Editing rule: one named editor at a time. Every change bumps the version and
adds a line to the changelog at the bottom.

This file is the source of truth for what crosses the wire between the Rust
backend and the UI (and any agent client). Language-internal types are free
to differ.

---

## 1. Conventions

**Base URL.** `http://127.0.0.1:<port>/api/v1`. Port configurable in
`~/.eyrie/config.toml` under `[service] port`; default 7271. The Go server
keeps its own port and routes; the two never share a path.

**Auth.**
- Agent and CLI clients send `Authorization: Bearer <token>`. The token is
  read from `~/.eyrie/service-token` (mode 600, created on first start).
- Browser requests from the Eyrie UI origin are accepted without the token
  for `GET`. Mutating browser requests (`POST`, `PUT`, `PATCH`, `DELETE`)
  must carry `X-Eyrie-Client: web` and a matching `Origin`; anything else
  gets `403 origin_rejected`.

**IDs.** Prefixed ULIDs, opaque to clients:
`rsp_` responsibility, `run_` run, `dsp_` dispatch, `att_` attempt,
`apv_` approval, `rly_` relay, `ctx_` context brief, `evt_` event.
Worker profiles use a readable slug (`zeroclaw-default`, `codex-gpt6-sol`).

**Time.** RFC 3339, UTC, millisecond precision: `2026-10-06T01:00:00.000Z`.
Schedules carry their own IANA timezone.

**Idempotency.** Every `POST` that creates something or starts work accepts
`Idempotency-Key: <client uuid>`. A repeated key within 24 h returns the
original response with `200` and header `Idempotent-Replay: true`, and
starts nothing. The UI always sends one.

**Pagination.** List endpoints take `?limit=` (default 50, max 200) and
`?cursor=`; responses carry `next_cursor` (null at the end).

**Versioning.** Every response carries header `Eyrie-Contract: 0.1`.
Additive changes (new optional fields, new event types) are minor bumps.
Clients ignore unknown fields and unknown event types.

---

## 2. Shared value types

### 2.1 Usage

Cost is never a bare number. Unknown is never zero.

```json
{
  "kind": "reported",
  "usd": 0.0412,
  "input_tokens": 18234,
  "cached_input_tokens": 16900,
  "output_tokens": 812,
  "model": "anthropic/claude-sonnet-4.6",
  "source": "openrouter"
}
```

| `kind` | Meaning | `usd` |
| --- | --- | --- |
| `reported` | Provider or runtime reported the cost | number |
| `estimated` | Eyrie computed it from tokens × price table | number, shown labelled |
| `quota` | Subscription/plan usage, no per-call price | null; `quota_note` string |
| `unknown` | No usage data | null |

Aggregates (`usage_total`) are a list of these grouped by `kind`, never one
summed figure across kinds:

```json
"usage_total": [
  {"kind": "reported", "usd": 0.31, "count": 4},
  {"kind": "unknown", "usd": null, "count": 1}
]
```

### 2.2 Usage attribution

Every usage record carries `attributed_to`: `coordinator`, `context`,
`attempt`, or `verification`, plus the owning ids.

### 2.3 Tier

`"read" | "local_write" | "publish"`. Defined in PRD 5.4.

### 2.4 Blocked reason

```json
{"code": "approval_pending", "message": "Waiting on apv_01J…", "ref": "apv_01J…"}
```

Codes: `approval_pending`, `budget_exhausted`, `worker_unavailable`,
`context_required_unavailable`, `outcome_unknown`, `input_needed`,
`capability_missing`.

---

## 3. Records

### 3.1 Responsibility

```json
{
  "id": "rsp_01JABCDEF",
  "title": "PR / board drift report",
  "instructions": "Each morning, compare Dan's open zeroclaw-labs/zeroclaw PRs against the board cards in /Users/natalie/Development/Codex/agent-mesh/work-items/pr-<n>.yaml. Report drift and propose corrections. Change nothing.",
  "state": "active",
  "schedule": {"kind": "cron", "expr": "0 9 * * *", "tz": "Asia/Singapore"},
  "next_due_at": "2026-10-06T01:00:00.000Z",
  "tier": "read",
  "allowed_profiles": ["zeroclaw-default", "codex-gpt6-sol"],
  "allowed_providers": ["openrouter"],
  "context": {"policy": "optional", "providers": ["nessie", "files"], "paths": ["/Users/natalie/Development/Codex/agent-mesh/work-items"]},
  "budget": {"per_run_usd": 1.0, "per_day_usd": 2.0, "allow_unpriced": false, "max_attempts_per_dispatch": 2, "run_timeout_s": 900},
  "project_id": null,
  "created_at": "2026-10-05T15:30:00.000Z",
  "updated_at": "2026-10-05T15:30:00.000Z",
  "last_run": {"id": "run_01J…", "status": "succeeded", "finished_at": "…"}
}
```

- `state`: `active` | `paused` (no new runs; in-flight continue) |
  `disabled` (no future schedule; manual runs allowed).
- `schedule`: `{"kind":"cron",...}` | `{"kind":"interval","every_s":N}` |
  `{"kind":"manual"}`.
- `context.policy`: `required` | `optional` | `none`.
- `budget.per_run_usd` / `per_day_usd` may be null (no allowance; unpriced
  rules still apply).

### 3.2 Run

```json
{
  "id": "run_01JRUN",
  "responsibility_id": "rsp_01JABCDEF",
  "trigger": {"kind": "schedule", "due_at": "2026-10-06T01:00:00.000Z", "coalesced": 0},
  "status": "succeeded",
  "started_at": "…", "finished_at": "…",
  "summary": "3 PRs drifted: #10804 card says draft, PR is ready; …",
  "dispatch_ids": ["dsp_01J…"],
  "context_brief_id": "ctx_01J…",
  "usage_total": [{"kind": "reported", "usd": 0.21, "count": 3}],
  "blocked": null,
  "error": null
}
```

- `trigger.kind`: `schedule` | `manual` | `chat` | `dispatch_completed` |
  `approval_resolved` | `relay`. `coalesced` = missed occurrences folded in.
- `status`: `queued` | `running` | `blocked` | `succeeded` | `failed` |
  `cancelled` | `skipped_overlap`.
- Chat turns are also runs (`responsibility_id: null`, `trigger.kind: chat`).

### 3.3 Dispatch

```json
{
  "id": "dsp_01JDSP",
  "run_id": "run_01JRUN",
  "task_type": "review",
  "brief": "List open PRs authored by Audacity88 … report each mismatch with card path.",
  "cwd": "/Users/natalie/Development/Codex/agent-mesh",
  "tier": "read",
  "acceptance": {"method": "user_accept", "criteria": "Every open PR appears once with card status and PR status."},
  "routing": {"rule": "review[0]", "profile": "zeroclaw-default", "override_reason": null, "candidates_rejected": [{"profile": "codex-gpt6-sol", "reason": "budget"}]},
  "status": "verifying",
  "blocked": null,
  "attempt_ids": ["att_01JATT"],
  "result": {"text": "…final worker message…", "artifacts": [{"path": "/…/drift-2026-10-06.md", "bytes": 2210}]},
  "verification": {"method": "user_accept", "status": "pending", "notes": null},
  "verdict": null,
  "usage_total": [...],
  "created_at": "…", "updated_at": "…"
}
```

- `status`: `draft` | `queued` | `running` | `blocked` | `verifying` |
  `completed` | `failed` | `cancelled`.
- `acceptance.method`: `command` (shell check, `command` field, exit 0 +
  `expect` substring), `artifact_exists`, `reviewer` (another profile),
  `user_accept`. A worker's final message moves status to `verifying`,
  never straight to `completed`.
- `verdict`: null | `accepted` | `rejected` (Dan's, with optional `note`).

### 3.4 Attempt

```json
{
  "id": "att_01JATT",
  "dispatch_id": "dsp_01JDSP",
  "n": 1,
  "profile": "zeroclaw-default",
  "status": "succeeded",
  "native": {"runtime": "zeroclaw", "transport": "acp", "session_id": "3dc9…"},
  "started_at": "…", "finished_at": "…",
  "cancel": null,
  "usage": [{"kind": "reported", "usd": 0.17, "attributed_to": "attempt", "...": "..."}],
  "error": null
}
```

- `status`: `queued` | `running` | `cancel_requested` | `succeeded` |
  `failed` | `cancelled` | `interrupted` | `unknown`.
- `interrupted`: Eyrie stopped while it ran and recovery confirmed it did
  not finish. `unknown`: recovery could not tell; the dispatch goes
  `blocked` with `outcome_unknown` and nothing relaunches until Dan resolves.
- `cancel`: `{"requested_at": "…", "confirmed_at": "…" | null, "unconfirmable": bool}`.

### 3.5 Approval

```json
{
  "id": "apv_01JAPV",
  "status": "pending",
  "tier": "publish",
  "requested_by": {"kind": "attempt", "id": "att_01JATT", "profile": "codex-gpt6-sol"},
  "action": {
    "kind": "shell",
    "summary": "Post a comment on PR #10804",
    "exact": "ghz pr comment 10804 -R zeroclaw-labs/zeroclaw --body-file /…/c.md",
    "body": "Full text of the comment …",
    "cwd": "/…"
  },
  "scope": "once",
  "native_request_id": "item/commandExecution/requestApproval:42",
  "created_at": "…",
  "expires_at": "2026-10-06T03:00:00.000Z",
  "decision": null
}
```

- `status`: `pending` | `approved` | `denied` | `expired` | `superseded`
  (runtime restarted or request changed; must be re-asked).
- `scope`: `once` | `responsibility` (only valid for `local_write`; covers
  the same action kind for that responsibility until revoked).
- `action.kind`: `shell`, `file_write`, `file_delete`, `network`,
  `github_write`, `message`, `permission_change`, `tool` (generic; `exact`
  holds the tool name and args JSON).
- `decision`: `{"by": "dan", "at": "…", "result": "approved", "edited": {…} | null, "note": "…"}`.
  `edited` replaces fields of `action` (e.g. body text) for edit-and-approve.

### 3.6 Relay

```json
{
  "id": "rly_01JRLY",
  "title": "Reconcile v1 API contract",
  "artifact": {"path": "/Users/natalie/Development/eyrie/docs/eyrie-api.md", "working_copy": "/Users/dan/.eyrie/relays/rly_01JRLY/eyrie-api.md"},
  "question": "Is this contract sufficient for M2 and M3? Propose concrete edits.",
  "author": "zeroclaw-default",
  "reviewer": "codex-gpt6-sol",
  "round_limit": 2,
  "status": "needs_dan",
  "rounds": [
    {"n": 1, "reviewer_dispatch": "dsp_…", "author_dispatch": "dsp_…", "changed": true},
    {"n": 2, "reviewer_dispatch": "dsp_…", "author_dispatch": "dsp_…", "changed": false}
  ],
  "summary": {
    "changed": "Added attempt.native.transport; split blocked reason codes.",
    "agreed": ["SQLite owned by Rust service", "Idempotency-Key on all creating POSTs"],
    "disagreements": [
      {"point": "Global vs per-run event stream", "author": "One global stream with filters is enough for v1.", "reviewer": "Per-attempt transcripts should be a separate stream to keep the global one small."}
    ]
  },
  "usage_total": [...],
  "created_at": "…", "updated_at": "…"
}
```

- `status`: `running` | `agreed` | `needs_dan` | `round_limit` | `failed` |
  `cancelled`.
- The relay edits only `working_copy`. Applying it to `artifact.path` is a
  `local_write` approval.

### 3.7 Context brief

```json
{
  "id": "ctx_01JCTX",
  "run_id": "run_01JRUN",
  "policy": "optional",
  "status": "partial",
  "query": "board card conventions for zeroclaw PRs",
  "retrieved_at": "…",
  "sources": [
    {"provider": "nessie", "ref": "nessie:8F2C…", "title": "…", "source_date": "2026-09-17", "excerpt": "…"},
    {"provider": "files", "ref": "/Users/natalie/Development/Codex/agent-mesh/work-items/pr-10804.yaml", "source_date": "2026-10-04", "excerpt": "…"}
  ],
  "gaps": [{"provider": "nessie", "reason": "not_running"}],
  "contradictions": []
}
```

- `status`: `complete` | `partial` | `unavailable`. With policy
  `required`, `unavailable` blocks the run (`context_required_unavailable`).

### 3.8 Worker profile

```json
{
  "id": "zeroclaw-default",
  "runtime": "zeroclaw",
  "transport": "acp",
  "model": "anthropic/claude-sonnet-4.6",
  "host": "local",
  "workspace_roots": ["/Users/natalie/Development"],
  "max_tier": "local_write",
  "health": {"status": "ok", "checked_at": "…", "detail": null},
  "capabilities": {
    "approval_forwarding": true,
    "cancel": true,
    "usage_reporting": "reported",
    "read_only_mode": true,
    "streaming": true,
    "resume_session": false
  }
}
```

- `capabilities.*` values are measured by the M0 spike and re-probed on
  health check, not declared. Unknown is `null`, shown as unknown.
- A dispatch needing a capability the profile lacks is rejected at routing
  (`capability_missing`), not attempted.

### 3.9 Routing table

```json
{
  "version": 3,
  "rules": {
    "review":      ["zeroclaw-default", "codex-gpt6-sol"],
    "code_change": ["codex-gpt6-sol", "zeroclaw-default"],
    "research":    ["zeroclaw-default"],
    "summarize":   ["zeroclaw-cheap"],
    "mechanical":  ["zeroclaw-cheap"]
  }
}
```

Backed by `~/.eyrie/routing.toml`. `PUT` requires the current `version`
(optimistic lock; `409 version_conflict` otherwise).

---

## 4. Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/health` | `{status, contract, version, db: "ok"}`; no auth |
| GET | `/activity` | Home view in one call (below) |
| GET | `/responsibilities` | List |
| POST | `/responsibilities` | Create (body: 3.1 without server fields) |
| GET | `/responsibilities/{id}` | Read |
| PATCH | `/responsibilities/{id}` | Partial update; `state` changes here |
| DELETE | `/responsibilities/{id}` | Only when no run is in flight; else `409` |
| POST | `/responsibilities/{id}/run` | Manual run now → `202 {run_id}` |
| GET | `/runs?responsibility_id=&status=` | List |
| GET | `/runs/{id}` | Read, with embedded dispatch summaries |
| POST | `/runs/{id}/cancel` | Cancel all in-flight attempts of the run |
| GET | `/dispatches?status=&run_id=` | List |
| POST | `/dispatches` | Manual dispatch (M2): `{task_type, brief, cwd, tier, acceptance, profile?}` → `202 {dispatch_id}` |
| GET | `/dispatches/{id}` | Read, with attempts |
| POST | `/dispatches/{id}/cancel` | Cancel current attempt |
| POST | `/dispatches/{id}/retry` | New attempt; `{profile?}`; respects attempt cap |
| POST | `/dispatches/{id}/resolve` | Resolve `outcome_unknown`: `{outcome: "succeeded"\|"failed", note}` |
| POST | `/dispatches/{id}/verdict` | `{verdict: "accepted"\|"rejected", note}` |
| GET | `/attempts/{id}/transcript` | Stored transcript chunks (paged) |
| GET | `/approvals?status=pending` | Inbox |
| GET | `/approvals/{id}` | Read |
| POST | `/approvals/{id}/decide` | `{result: "approved"\|"denied", edited?, note?, scope?}`; `409` if not pending |
| GET | `/relays` | List |
| POST | `/relays` | `{title, artifact_path, question, author, reviewer, round_limit}` → `202 {relay_id}` |
| GET | `/relays/{id}` | Read |
| POST | `/relays/{id}/cancel` | Cancel |
| POST | `/relays/{id}/apply` | Request approval to copy the working copy over the artifact → `{approval_id}` |
| GET | `/context-briefs/{id}` | Read |
| GET | `/profiles` | Worker profiles with health and capabilities |
| POST | `/profiles/{id}/probe` | Re-run capability probe |
| GET | `/routing` | Routing table |
| PUT | `/routing` | Replace (with `version`) |
| GET | `/usage?since=&until=&group_by=responsibility\|profile\|kind` | Ledger rollups |
| POST | `/chat` | Coordinator chat turn; SSE response (section 5.3) |
| GET | `/chat/history` | Persisted chat messages |
| GET | `/events` | Global SSE stream (section 5) |
| POST | `/tick` | Run due responsibilities now (launchd hook); token only |

### 4.1 `GET /activity`

```json
{
  "running":   [{"kind": "run", "id": "run_…", "title": "PR / board drift report", "started_at": "…", "profile": "zeroclaw-default"}],
  "waiting":   [{"kind": "approval", "id": "apv_…", "summary": "Post a comment on PR #10804", "tier": "publish", "expires_at": "…"},
                {"kind": "dispatch", "id": "dsp_…", "summary": "Outcome unknown after restart", "blocked": {"code": "outcome_unknown"}},
                {"kind": "relay", "id": "rly_…", "summary": "2 disagreements"}],
  "scheduled": [{"responsibility_id": "rsp_…", "title": "…", "next_due_at": "…"}],
  "done":      [{"kind": "run", "id": "run_…", "title": "…", "status": "succeeded", "finished_at": "…", "usage_total": [...]}],
  "budget":    [{"responsibility_id": "rsp_…", "day_spent": [{"kind": "reported", "usd": 0.4}], "per_day_usd": 2.0}],
  "as_of_seq": 1842
}
```

`as_of_seq` lets the UI open `/events?since=1842` with no gap.

---

## 5. Events (SSE)

### 5.1 Envelope

`GET /events?since=<seq>&kinds=run,dispatch,...` (also honours
`Last-Event-ID`). Each message:

```
id: 1843
event: dispatch.status
data: {"seq":1843,"ts":"2026-10-06T01:00:12.004Z","type":"dispatch.status","subject":{"kind":"dispatch","id":"dsp_01JDSP"},"data":{"from":"running","to":"verifying"}}
```

- `seq` is a gap-free, monotonic integer from the database. Events are
  persisted; `since` replays from the store (retention: 7 days).
- A `heartbeat` event every 15 s (`data: {}`) so the UI can show
  "disconnected" honestly.
- The UI treats events as invalidation hints and refetches the subject; the
  `data` payload is a convenience, not the full record.

### 5.2 Event types

| Type | `data` |
| --- | --- |
| `responsibility.changed` | `{fields: [...]}` |
| `run.status` | `{from, to, blocked?}` |
| `dispatch.status` | `{from, to, blocked?}` |
| `attempt.status` | `{from, to}` |
| `attempt.output` | transcript chunk (5.3); only with `kinds=attempt.output` or `subject=att_…` |
| `approval.created` | `{tier, summary, expires_at}` |
| `approval.resolved` | `{result}` |
| `relay.status` | `{from, to, round}` |
| `usage.recorded` | a 2.1 usage record + `attributed_to` |
| `profile.health` | `{status, detail}` |
| `heartbeat` | `{}` |

`attempt.output` is opt-in so the global stream stays small. The dispatch
detail view subscribes with `/events?subject=att_01JATT&since=0`.

### 5.3 Transcript chunks and chat

Same shapes as today's commander SSE (`internal/commander/events.go`), so
the existing chat UI code carries over:

```json
{"type": "delta", "text": "…"}
{"type": "tool_call", "id": "…", "name": "…", "args": {}}
{"type": "tool_result", "id": "…", "name": "…", "output": "…", "error": false}
{"type": "message", "role": "assistant", "content": "…"}
{"type": "confirm_required", "approval_id": "apv_…"}
{"type": "done", "usage": {…2.1…}}
{"type": "error", "error": "…"}
```

Change from today: `confirm_required` carries an `approval_id` from the
durable store instead of the in-memory `pa_` id; `done` carries a usage
record instead of bare token counts.

---

## 6. Errors

```json
{"error": {"code": "budget_exhausted", "message": "Daily allowance of $2.00 spent for rsp_01JABCDEF.", "details": {"responsibility_id": "rsp_01JABCDEF"}}}
```

| HTTP | `code` |
| --- | --- |
| 400 | `invalid_request` (`details.fields`) |
| 401 | `unauthorized` |
| 403 | `origin_rejected`, `tier_exceeded` |
| 404 | `not_found` |
| 409 | `conflict`, `version_conflict`, `not_pending`, `in_flight`, `overlap` |
| 422 | `capability_missing`, `budget_exhausted`, `unpriced_not_allowed`, `attempt_limit`, `context_required_unavailable` |
| 503 | `worker_unavailable`, `db_unavailable` |
| 500 | `internal` (message is the real error; localhost only) |

---

## 7. Behaviour the contract promises

1. A creating `POST` with a repeated `Idempotency-Key` never starts a second
   run, dispatch, or relay.
2. A dispatch is persisted before any worker process starts.
3. No attempt is relaunched while its predecessor is `unknown`.
4. `approved` only executes the action as stored (or as `edited`), bound to
   one attempt. `expired` and `superseded` never execute.
5. Usage of kind `unknown` is never rendered or summed as $0.
6. A worker's final message never sets a dispatch to `completed`.
7. Missed schedule occurrences produce at most one run.

Each of these gets an acceptance fixture in M2/M3.

## 8. Fixtures

The JSON blocks above are fixtures v0.1. When M2 starts, extract them to
`docs/eyrie-api/fixtures/<record>.json` and validate both the Rust
serializers and the TypeScript types against them in CI.

## 9. Open points for Magnus

1. Port 7271 and the token/origin scheme: fine for the Rust service?
2. One global event stream with opt-in transcripts (5.2) vs a separate
   transcript endpoint.
3. Is `seq` from SQLite (single writer) acceptable, or do you want
   per-subject versions instead?
4. `acceptance.method = command`: run by the Rust service directly, or
   dispatched to a read-only worker?
5. Anything M2 needs that is missing.

## Changelog

- 0.1 (2026-10-05, fable): first draft.
