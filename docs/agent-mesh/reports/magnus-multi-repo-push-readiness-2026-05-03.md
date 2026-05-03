# Magnus Multi-Repo Push Readiness

Status: complete
Responder: Magnus/Eyrie
Date: 2026-05-03
Notice: 2026-05-03-vega-eyrie-multi-repo-push-preflight-001

## Verdict

Eyrie is **not ready for an immediate push** because the working tree has 9 dirty
safe-doc files. It is not blocked by tests, upstream divergence, runtime homes,
credential files, or generated build artifacts.

After the dirty docs are reviewed and committed, Eyrie appears push-ready from
the current local preflight view.

## Repo State

After `git fetch origin`, Eyrie is on `main` tracking `origin/main`.

The Commander dry-run push script reports:

```text
Eyrie/Magnus: blocked
- branch: main
- upstream: origin/main
- ahead/behind: 5/0
- dirty files: 9
- target: origin HEAD:main
- blocker: 9 dirty file(s)
```

Ahead commits currently queued for push:

- `05e6406b docs: add Danya persona seed`
- `74253bf7 feat: add read-only mesh status surface`
- `2d4713be docs: sync Eyrie mesh context`
- `795067e2 fix: detect sandboxed Hermes gateway pids`
- `c6836c5c fix: satisfy go vet in framework list output`

## Dirty State Classification

In-scope Eyrie docs/mesh changes:

- `docs/agent-mesh/README.md`
- `docs/agent-mesh/inboxes/danya.yaml`
- `docs/agent-mesh/reports/magnus-mesh-status-2026-05-03.md`
- `docs/agent-mesh/reports/magnus-canonical-inbox-response-2026-05-03.md`
- `docs/agent-owned-x-account-response.md`
- `docs/agent-owned-x-account.md`
- `docs/commander-audit-system-danya-magnus-review-2026-05-03.md`
- `docs/commander-awards-pilot-review-2026-05-03.md`
- `docs/commander-progress-sync-response-2026-05-02.md`

These are all safe project docs, local mesh state, or response artifacts. They
can be committed together if Dan approves an Eyrie docs/readiness commit.

No dirty file is a runtime home, credential, generated app build, cache, pid
file, log, socket, database, or private connector export.

## Proposed Eyrie Commit/Push Scope

If Dan approves, the clean Eyrie scope would be:

1. Commit the 9 dirty docs/mesh/report files as one Eyrie docs/readiness commit.
2. Push `main` to `origin/main`, which would include the current 5 ahead commits
   plus that new docs/readiness commit.

Do not include runtime paths:

- `~/.eyrie`
- `~/.hermes`
- `~/.zeroclaw`
- `~/.openclaw`
- `.env`, auth files, state databases, logs, pid files, sockets, caches, or build outputs

Do not run `scripts/push-system-snapshot.mjs --execute` without Dan approving the
exact multi-repo snapshot.

## Validation

Ran:

```sh
git fetch origin
node scripts/push-system-snapshot.mjs --repo eyrie
git status --short --branch
git diff --check
GOCACHE=/private/tmp/eyrie-gocache GOMODCACHE=/private/tmp/eyrie-gomodcache go test ./...
npm --prefix web run build
```

Results:

- `git fetch origin`: pass
- push dry-run: blocked only by 9 dirty files
- `git diff --check`: pass
- `go test ./...`: pass
- `npm --prefix web run build`: pass, with existing Vite chunk-size/dynamic-import warnings only

## Board And Notice Freshness

Commander-visible status item
`/Users/dan/Documents/Personal/Commander/status/items/eyrie-reactivation.md` is
stale. It still says Eyrie is ahead by one commit and lists older dirty state.
That should be refreshed before a final multi-repo push snapshot is proposed.

The Development board source does not currently carry an Eyrie item; Eyrie is
represented through Commander's `eyrie-reactivation` status item.

Current Eyrie/Ops action queue after this response:

- open directed notices: none;
- pending broadcasts: four.

Pending broadcasts do not block Eyrie repo push readiness. The remaining
broadcasts are operational context: connector bug owner, board sources, shared
document index, and audit PR pilot. The connector-owner finding is already
captured in `docs/agent-mesh/reports/magnus-mesh-status-2026-05-03.md`, and the
audit pilot response is already captured in
`docs/commander-audit-system-danya-magnus-review-2026-05-03.md`. They should be
acknowledged in a separate notice-cleanup pass if Dan wants the Eyrie/Ops badge
cleared, but they are not repo-state blockers.

## Old-MacBook Constraints

The old MacBook should not be asked to run full Eyrie dashboard, multi-agent, or
web-build validation as part of readiness. A lightweight old-MacBook resume check
is enough:

- fetch/fast-forward Eyrie;
- read `docs/agent-mesh/README.md` and `docs/agent-mesh/manifest.yaml`;
- confirm required mesh inboxes/reports exist;
- verify no runtime homes or credentials are tracked;
- run `go test ./internal/server -run TestReadMeshStatus` only if local Go tooling
  is usable.

## Blockers Before Approval

- Review and commit or intentionally leave out the 9 dirty Eyrie docs.
- Refresh the Commander `eyrie-reactivation` status item or let Vega/Ariadne
  record that it is stale in the combined multi-repo snapshot.
- Ask Dan for exact approval before any commit, push, or push-script execution.
