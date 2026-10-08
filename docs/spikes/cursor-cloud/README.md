# H-CUR0: Cursor Cloud Agents API, capability spike

Read-only. No account key, no remote agent, no repository change. Cursor is
a dispatch target only: the picker chooses the model, Eyrie launches it.

## Sources (retrieved 2026-10-08)

- https://cursor.com/docs/cloud-agent/api/endpoints (Cloud Agents API v1
  reference, marked **public beta: "APIs may change before general
  availability"**). Page sha256 `62a07a7f02841ac712ead1c26ca9df2726f765d790a95b56b045d328c4e54307`.
- https://cursor.com/docs-static/cloud-agents-openapi.yaml (OpenAPI 3.0.3,
  `info.version: 1.0.0`). sha256 `664e695207e9f72dd7dd296d60b576e68575f8e71ce18e6263a554850c072570`.
- https://cursor.com/docs/api (auth, rate limits). sha256 `4e2f201d5203ad589b0d49fae8106114b0d62b97db52756a0c99b92a533aaea7`.
- https://cursor.com/pricing (plans: Cloud agents listed from Individual/Pro
  up; no per-agent price on the page).

The hashes are of the pages as fetched; they will change when Cursor edits
them. Re-check before H-CUR1 starts.

## Capabilities, as documented

Mapped to the H-01 contract (`internal/harness`, branch
h-01-execution-contract, not merged; read at 72495816 and re-checked at
1b9573cd). Where H-01 cannot yet express what Cursor needs, that is listed
under "H-01 prerequisites" below; those block H-CUR1.

| H-01 field | Cursor v1 | Status |
|---|---|---|
| Launch | `POST /v1/agents` creates an agent and its first run | supported |
| Exact model | `model.id` from `GET /v1/models`; unknown id is `400 invalid_model`. Omitting `model` falls back to user/team/system default | supported, **Eyrie must always send `model`** (H-01 forbids silent fallback) |
| Model list | `GET /v1/models` ("recommended" set, with aliases and params) | supported; list is per key, so `Capabilities.Models` needs a live call |
| Repo/branch scope | `repos[].url` + `startingRef` (branch or SHA), or `prUrl`; max 20 repos. `workOnCurrentBranch=false` (default) pushes to a new `cursor/...` branch; `true` pushes to the starting ref | supported; **Eyrie must send `workOnCurrentBranch:false` and `autoCreatePR:false`** so an attempt never writes to the approved ref or opens a PR by itself |
| Workspace | Cursor-hosted VM (`env.type: cloud`) or self-hosted `pool`/`machine` | no local path; H-01 `Workspace` (absolute local path) does not apply. Needs a contract decision (see gaps) |
| Idempotent start | client-supplied `agentId` (`bc-<uuid>`); re-POST is `409 agent_id_conflict` | supported; maps to H-01 attempt ID, so an ambiguous start can be reconciled by `GET /v1/agents/{agentId}` |
| Status | `GET /v1/agents/{id}/runs/{runId}`: `CREATING`, `RUNNING`, then terminal `FINISHED`, `ERROR`, `CANCELLED`, `EXPIRED` | supported (poll); SSE stream also documented |
| Progress events | `GET .../runs/{runId}/stream` (SSE: status, assistant, thinking, tool_call, result, error, done; resumable with `Last-Event-ID`; may return `410 stream_expired`) | supported; Eyrie must not persist assistant/thinking text (H-01 confidentiality) |
| Cancel | `POST .../runs/{runId}/cancel`; terminal `CANCELLED`; already-terminal is `409 run_not_cancellable` | supported; confirm by polling for `CANCELLED` (cancel response is only `{id}`) |
| Resume | `POST /v1/agents/{id}/runs` adds a follow-up run on the same agent; only one active run (`409 agent_busy`) | supported (native session = agent id) |
| Usage | `GET /v1/agents/{id}/usage[?runId=]`: input/output/cache tokens per run; "runs without recorded usage report zeros" | tokens supported, **no cost field**. Per docs/eyrie-api.md the kind is `quota` (plan usage, no per-call price, usd null), not `reported` (which means a reported cost). All-zero usage is `unknown`. Needs H-01 prerequisite P3 |
| Native approvals | none documented in v1: no approval/permission events or respond endpoint | **unsupported**; the agent runs unattended in its VM. Needs H-01 prerequisite P1 |
| Auth | user API key or service-account key, Basic or Bearer | key held by Dan; not used here |
| Rate limits | Cloud Agents API: "standard rate limiting"; default is 20 requests/minute unless an endpoint says otherwise; 429 with `Retry-After` and `X-RateLimit-*` headers | poll interval must stay well under 20/min across all attempts |
| Cost | no price per run in the API or on the pricing page; billed via the Cursor plan's usage | unknown; spend scope is Dan's |
| Webhooks | "coming soon" for v1 (v0 only) | unsupported in v1; poll or SSE |

## Fixtures

`fixtures/` holds one request/response per call the adapter needs, built
from the documented examples with placeholder ids. Check them with:

    python3 docs/spikes/cursor-cloud/validate_fixtures.py [spec.yaml]

It fetches Cursor's OpenAPI spec (or reads a local copy), refuses to run
unless the spec's sha256 matches the pinned snapshot above, validates each
fixture against its component schema, and requires three negative controls
to fail (a run with an undocumented status, a run without `agentId`, a
create without `prompt`). It exits 0 only if all of that holds. The spec is
Cursor's document and is not vendored; the pin makes a changed spec stop
the check instead of silently validating against something else. Needs
PyYAML and jsonschema. Result at the pin: 14 fixtures ok, 3 controls
rejected. A fixture with a made-up status fails it (checked). For an
offline check on the Air, a copy at the pinned hash is at
`/Users/dan/.zeroclaw/agents/experiment_fable/workspace/workbench/cursor/openapi.yaml`
(not in the repo; pass it as the argument).

The fixtures:

- create-agent request (explicit model, `agentId`, one repo, no PR, no
  push to the starting ref) and response
- get-run: running, finished, error, expired, cancelled
- cancel-run response; errors run_not_cancellable (409), agent_id_conflict
  (409), invalid_model (400), rate_limit_exceeded (429)
- agent usage, list models

No real account data. Error `message` values are placeholders: the docs
give codes, not messages.

## Go/no-go for H-CUR1

**Go, scoped and conditional**: the API supports a bounded launch-and-poll
adapter, but H-CUR1 must not start until the H-01 prerequisites below land.
The adapter rules:

1. Always send `model.id`; check it against `GET /v1/models` before launch;
   refuse on `invalid_model` (no default fallback).
2. Send `agentId` = a uuid derived from the H-01 attempt id, so a start
   whose response was lost is reconciled by GET, never re-POSTed blind.
3. Send `workOnCurrentBranch:false`, `autoCreatePR:false`; record the pushed
   `git.branches[]` as the outcome, not a merge.
4. Capabilities: Launch, Cancel, Resume, Usage true; **NativeApprovals
   false**. Usage is tokens with kind `quota` and cost unknown (P3).
5. The adapter's Wait returns the terminal status it actually read
   (`FINISHED`, `ERROR`, `CANCELLED`, `EXPIRED`), and H-01 records that
   (P2). A cancel call that errors (including `409 run_not_cancellable`,
   which means the run was already terminal) or times out does not decide
   the outcome: the adapter reconciles with `GET .../runs/{runId}` within
   the cancel grace. A terminal state read there wins: `FINISHED` is
   succeeded even though a cancel was requested, `CANCELLED` is cancelled,
   `ERROR`/`EXPIRED` are failed. Only an outcome still unresolved when the
   grace runs out (no terminal read, or the reads keep failing) is
   `unknown`. Required H-CUR1 fixture sequences: cancel 409 then GET
   FINISHED (succeeded); cancel 200 then GET CANCELLED (cancelled); cancel
   timeout then GET RUNNING until grace ends (unknown); cancel 200 then GET
   failing until grace ends (unknown).
6. Never persist `result`, assistant or thinking text in receipts.

## H-01 prerequisites (block H-CUR1)

Checked against h-01-execution-contract @ 1b9573cd.

- **P1, unattended dispatch.** `Check` never looks at
  `Capabilities.NativeApprovals`, and `Request` has no way to say an attempt
  requires per-action approval. Setting NativeApprovals false therefore
  does not stop a Cursor dispatch. Needed: a Request field for the approval
  mode (for example "requires interactive approval" versus "pre-approved
  for unattended run", the latter only with an `ApprovalBinding`), and
  `Check` refusing an interactive request on an adapter without native
  approvals. Until then the Cursor adapter must refuse every request
  itself, so H-CUR1 waits.
- **P2, confirmed terminal state.** In the cancel path `Run` records
  `cancelled` whenever Wait returns nil or ErrCancelled. If the run
  finished before the cancel landed, Cursor reports `FINISHED`, and the
  receipt would say cancelled. Needed: the adapter's Result (or a typed
  error) carries the runtime's observed terminal state, and Run records
  that state: `FINISHED` stays succeeded even after a cancel request, only
  `CANCELLED` is cancelled. A cancel or poll error triggers reconciliation
  (rule 5); only an outcome still unresolved at the end of the grace is
  unknown.
- **P3, token usage without cost.** H-01 `Usage` has `CostUSD float64`, so
  missing cost reads as $0, and its kinds follow eyrie-api.md where
  `reported` means a reported cost. Needed: cost as a nullable field (or a
  separate "cost unknown" flag) so tokens keep their provenance while cost
  stays null with kind `quota`, matching docs/eyrie-api.md.

These are H-01 changes, not Cursor ones; the H-01 owner decides how.

## Not decided here (needs Dan or chief)

- H-01 `Workspace` is an absolute local path; Cursor runs in its own VM on
  a repo URL + ref. Either H-01 gains a remote-repo workspace form, or the
  Cursor adapter maps a local repo to its remote and refuses if it can't.
- Whether an unattended cloud agent (no per-action approval) is acceptable
  for Eyrie at all, and on which repos. This is a policy choice.
- Spend and key: the API needs Dan's Cursor key and is billed to his plan;
  no live call until he scopes it (waiting on Dan).
- The API is public beta; pin the OpenAPI hash above in H-CUR1 tests and
  re-check before any live use.

## Not verified

Everything above is from the docs. Nothing was called: no key was used and
no agent was created. Behaviour that the docs leave open (actual rate limit
numbers for Cloud Agents endpoints, how long EXPIRED takes, whether
`envVars` beta ignores silently on this account) stays unverified until a
live, Dan-approved run.
