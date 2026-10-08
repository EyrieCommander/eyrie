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
h-01-execution-contract @ 72495816, not merged).

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
| Usage | `GET /v1/agents/{id}/usage[?runId=]`: input/output/cache tokens per run; "runs without recorded usage report zeros" | tokens supported, **no cost field**; a zero must be recorded as `unknown`, not 0 (H-01 rule) |
| Native approvals | none documented in v1: no approval/permission events or respond endpoint | **unsupported**; the agent runs unattended in its VM |
| Auth | user API key or service-account key, Basic or Bearer | key held by Dan; not used here |
| Rate limits | Cloud Agents API: "standard rate limiting"; default is 20 requests/minute unless an endpoint says otherwise; 429 with `Retry-After` and `X-RateLimit-*` headers | poll interval must stay well under 20/min across all attempts |
| Cost | no price per run in the API or on the pricing page; billed via the Cursor plan's usage | unknown; spend scope is Dan's |
| Webhooks | "coming soon" for v1 (v0 only) | unsupported in v1; poll or SSE |

## Fixtures

`fixtures/` holds one request/response per call the adapter needs, built
from the documented examples with placeholder ids, and checked against the
OpenAPI schemas (all 14 validate; a run with an undocumented status is
rejected):

- create-agent request (explicit model, `agentId`, one repo, no PR, no
  push to the starting ref) and response
- get-run: running, finished, error, expired, cancelled
- cancel-run response; errors run_not_cancellable (409), agent_id_conflict
  (409), invalid_model (400), rate_limit_exceeded (429)
- agent usage, list models

No real account data. Error `message` values are placeholders: the docs
give codes, not messages.

## Go/no-go for H-CUR1

**Go, scoped**, for an adapter that launches one bounded attempt and polls
it to a terminal state, with these rules:

1. Always send `model.id`; check it against `GET /v1/models` before launch;
   refuse on `invalid_model` (no default fallback).
2. Send `agentId` = a uuid derived from the H-01 attempt id, so a start
   whose response was lost is reconciled by GET, never re-POSTed blind.
3. Send `workOnCurrentBranch:false`, `autoCreatePR:false`; record the pushed
   `git.branches[]` as the outcome, not a merge.
4. Capabilities: Launch, Cancel, Resume, Usage (tokens, `reported`; zero
   means unknown) true; **NativeApprovals false**. H-01's Check must then
   refuse a Cursor request that needs interactive approvals: the approval
   happens once, before dispatch (S2-07 / PK-I1), and Cursor runs
   unattended.
5. Cancel is confirmed only by a later `CANCELLED` read; a cancel call that
   errors or times out leaves the attempt `unknown`.
6. Never persist `result`, assistant or thinking text in receipts.

**Not decided here (needs Dan or chief):**

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
