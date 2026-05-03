# Magnus Sync-Safety Checks

Date: 2026-05-03
From: magnus.eyrie
To: Eyrie/Ops, Seshat/Commander
Response to: `/Users/dan/Documents/Personal/Commander/Shared/notices/magnus-inbox.yaml#2026-05-03-eyrie-magnus-sync-safety-eyrie-checks-001`

## Scope

These are the Eyrie-specific checks that should pass before old-MacBook or
two-computer sync is considered safe. They are scoped to `/Users/natalie/Development/eyrie`
and should complement, not replace, Seshat/Commander shared sync-safety scripts.

## Required Eyrie State

Before sync, verify these tracked-safe project artifacts exist and are classified
intentionally:

- `.hermes.md`
- `TWO_COMPUTER_SYNC_PLAN.md`
- `docs/agent-mesh/README.md`
- `docs/agent-mesh/manifest.yaml`
- `docs/agent-mesh/broadcasts.yaml`
- `docs/agent-mesh/outbox.yaml`
- `docs/agent-mesh/inboxes/magnus.yaml`
- `docs/agent-mesh/inboxes/hermes.yaml`
- `docs/agent-mesh/inboxes/danya.yaml`
- `docs/agent-mesh/inboxes/docs.yaml`
- `docs/agent-mesh/reports/magnus-mesh-status-2026-05-03.md`
- `docs/agent-mesh/reports/hermes-setup-audit-2026-05-03.md`
- `docs/agent-mesh/reports/magnus-hermes-response-2026-05-03.md`
- `docs/danya-lore.md`
- `docs/danya-agent-mesh-response-2026-05-03.md`

If the mesh status surface is included in the sync scope, also include the
related source/UI files as one explicit commit scope:

- `internal/server/mesh_status.go`
- `internal/server/mesh_status_test.go`
- `internal/server/server.go`
- `internal/server/reference.go`
- `internal/config/config.go`
- `web/src/components/MeshStatusPage.tsx`
- `web/src/App.tsx`
- `web/src/components/Sidebar.tsx`
- `web/src/lib/api.ts`
- `web/src/lib/types.ts`

Do not mix these blindly with unrelated local edits. Rowan's audit specifically
called out that the mesh docs, persona/lore work, and read-only status surface
need an explicit commit scope before Dan approves any sync.

## Forbidden Paths

Never track or sync runtime homes, credentials, or transient state as Eyrie
project state:

- `~/.eyrie`
- `~/.hermes`
- `~/.zeroclaw`
- `~/.openclaw`
- `~/.codex`
- `~/.local/share`, unless a specific safe file is reviewed and approved
- `.env`, `.env.*`, `auth.json`, OAuth credential files, cookies, refresh tokens
- `state.db`, session databases, logs, pid files, sockets, caches
- `node_modules`, Go build cache, Vite build outputs, generated binaries, and local app bundles

The checked-in `./eyrie` binary in this checkout is already known to be stale.
Do not treat binaries as authoritative sync state unless Dan explicitly approves
that release/distribution path.

## Mesh Root Configuration

Installed Eyrie must not depend on the process current working directory to find
the mesh. Use one of these explicit routes:

1. Set `EYRIE_AGENT_MESH_DIR=/Users/natalie/Development/eyrie/docs/agent-mesh`.
2. Or configure `~/.eyrie/config.toml` with:

```toml
[mesh]
agent_mesh_dir = "/Users/natalie/Development/eyrie/docs/agent-mesh"
```

On the old MacBook, if absolute paths differ, use a local-only
`.machine-profile.local.md` or local config override. Do not edit tracked docs
to encode machine-specific paths.

## Validation Commands

For the mesh status reader:

```sh
go test ./internal/server -run TestReadMeshStatus
```

For broader Eyrie source validation when code changes are in scope:

```sh
go test ./internal/server
git diff --check
```

If the web status surface is in scope and Node dependencies are available:

```sh
cd web
npm run build
```

If the Eyrie server is running, also verify the read-only endpoint:

```sh
curl -s http://localhost:7200/api/mesh/status
```

The response should report `available: true`, `parent_agent.id:
magnus.eyrie`, subordinates including `hermes.eyrie`, `danya.eyrie`, and
`docs.eyrie`, inbox summaries, latest outbox entry, report links, and Commander
refs. The endpoint must remain read-only.

## Old-MacBook Lightweight Resume Criteria

The old MacBook does not need to run the full Eyrie stack before resuming useful
work. It should be considered sync-ready for Eyrie if it can:

- clone or pull the Eyrie repo to the expected path or a documented local path;
- show a clean git state or an explicitly classified dirty state;
- read `docs/agent-mesh/README.md` and `docs/agent-mesh/manifest.yaml`;
- confirm the required mesh inboxes and reports exist;
- run the focused mesh reader test, or if Go/tooling is too heavy, perform a
  file-presence and YAML-parse check with local tooling;
- verify that no runtime homes or credential files are staged or tracked;
- record any path differences in an ignored machine-local profile.

Avoid full dashboard, multi-agent, or heavy web build validation on the old
MacBook unless Dan deliberately asks it to do that work.

## Likely Script Hooks

Seshat's shared script plan can call an Eyrie-specific check script later. The
Eyrie script should be read-only and should report:

- git branch/ahead/dirty status;
- classified dirty files by bucket: mesh docs, Danya/Magnus docs, source/UI,
  local pointers, and unrelated;
- missing required mesh files;
- mesh root resolution path;
- forbidden tracked paths;
- focused validation command results;
- old-MacBook lightweight fallback instructions when tooling is unavailable.

No script should commit, push, install frameworks, change credentials, mutate
Commander Shared notices, or acknowledge inbox notices without Dan's explicit
approval.
