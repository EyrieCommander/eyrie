# Eyrie: Project Orchestrator with Agent Hierarchy

## Vision

Transform Eyrie from a framework manager into a **project orchestrator** with a three-tier agent hierarchy. Agents are persistent entities with real identity (IDENTITY.md, SOUL.md, MEMORY.md), not disposable LLM sessions.

## Three-Tier Agent Hierarchy

### 1. Master Commander (one per user)
- Has its own identity, memory, and workspace
- Knows about ALL the user's projects
- Can create project orchestrators via Eyrie's API
- Relays project progress to the user
- User picks which framework it runs on

### 2. Project Orchestrators (one per project)
- Created by the coordinator (or user via UI)
- Knows project goals, tracks progress
- Can create role agents via Eyrie's API
- Has its own identity and memory
- Responsible for its project's progress

### 3. Role Agents / Implementers (many per project)
- Created by orchestrators (or user via UI)
- Specific roles: researcher, dev-lead, marketer, etc.
- Use persona templates as starting point for identity
- Develop their own memory over time
- Responsible for their own role

## Key Concept: Personas Are Identity Templates

A persona is NOT just a system prompt. It's a **workspace template** that includes:

```
persona-template/
  IDENTITY.md.tmpl    # Name, personality, vibe
  SOUL.md.tmpl        # Core behavioral philosophy
  USER.md.tmpl        # Human context
  BOOTSTRAP.md.tmpl   # First-run initialization
  MEMORY.md.tmpl      # Seed memories
  TOOLS.md.tmpl       # Available tools + Eyrie API access
```

When instantiated, template variables ({{.Name}}, {{.Role}}, {{.ProjectName}}, etc.) are rendered and written to the new agent's workspace. The agent then develops its own memories from there.

**Persona Validation**: Each IdentityTemplate value is pre-parsed as a Go template with `missingkey=error` to catch syntax errors early. Templates are parsed with an empty `FuncMap` (no custom functions allowed) and scanned for dangerous pipelines (`call`, `template`, `exec`, `index`, `slice`) which are rejected. Only a whitelisted set of top-level variables (Name, Role, ProjectName, DisplayName, Framework, EyrieURL, ParentAgent, Description) are permitted. Rendered values are escaped to prevent path traversal or shell injection during file writes. HierarchyRole is validated via `Valid()` against the allowed set (`commander`, `captain`, `talon`, or empty for standalone). Only privileged principals (admins or owning agents) may create or modify personas with IdentityTemplate values; unauthorized attempts are rejected and logged. Template render errors during instantiation return a 4xx API error with a descriptive message; the error is also recorded in the instance's Status field so parent agents can recover.

### HierarchyRole

A string type in `internal/instance/instance.go` with constants: `commander`, `captain`, `talon`, and empty string (standalone). Includes a `Valid()` method.

### Persona Schema Extension

Add three new fields to the existing Persona struct:
- **IdentityTemplate** (`map[string]string`) — identity template files (key=filename, value=Go template string)
- **MemorySeeds** (`[]string`) — seed memories for MEMORY.md
- **HierarchyRole** — hierarchy role this persona is designed for

## Agent Instances

Each instance is a **real agent deployment** with its own config, workspace, port, and process.

### Instance Schema

Each instance tracks:
- **Identity**: ID (full UUID), Name (slug), DisplayName, Framework, PersonaID
- **Hierarchy**: HierarchyRole, ProjectID, ParentID (instance that created this one)
- **Runtime**: Port (auto-allocated 43000-43999), ConfigPath, WorkspacePath, AuthToken (OpenClaw)
- **Status**: Status (`created`/`running`/`stopped`/`error`), PID, LastSeen, HealthStatus (`healthy`/`unhealthy`/`unknown`), RestartCount
- **Metadata**: CreatedAt, CreatedBy (`user` or parent instance ID)

### Instance Directory Structure

```
~/.eyrie/instances/<instance-id>/
    instance.json           # Eyrie metadata
    config.toml             # (or .json/.yaml per framework)
    workspace/
        IDENTITY.md         # Rendered from persona template
        SOUL.md
        MEMORY.md
        USER.md
        BOOTSTRAP.md        # Deleted by agent after first session
        TOOLS.md            # Includes Eyrie API instructions
        memory/
            brain.db        # (ZeroClaw) or daily .md files
        sessions/
```

## Projects

Each project tracks: ID, Name, Description, Goal, OrchestratorID (instance ID), RoleAgentIDs (instance IDs), Status, CreatedAt, CreatedBy (`user` or coordinator ID).

Storage: `~/.eyrie/projects/<project-id>.json`

**Project Status Transitions:**
- Initial state on creation: `"active"`
- Valid states: `"active"`, `"paused"`, `"completed"`, `"archived"`
- Allowed transitions: active→paused, paused→active, active→completed, paused→completed, completed→archived
- Completing a project is a terminal state (no reopen); archiving removes it from active views

## Agents Creating Agents: HTTP REST API

Agents call Eyrie's REST API to create other agents. This works because:
- All frameworks support HTTP tool calls (ZeroClaw: `http_fetch`, OpenClaw: `web-fetch`)
- No new SDK dependencies needed
- Framework-agnostic

The coordinator's TOOLS.md includes instructions for calling the Eyrie REST API at `http://127.0.0.1:7200` — creating/listing instances, start/stop lifecycle actions, and creating projects. The `auto_start=true` flag attempts synchronous start; on failure the status becomes `error` but the instance is still created.

## New API Endpoints

### Instance Endpoints (`internal/server/instances.go`)
```
GET    /api/instances                    # List all instances
POST   /api/instances                    # Create (provision) new instance
GET    /api/instances/{id}               # Get instance detail
PUT    /api/instances/{id}               # Update (rename, change persona)
DELETE /api/instances/{id}               # Deprovision (stop + remove config)
POST   /api/instances/{id}/{action}      # start / stop / restart
```

### Project Endpoints (`internal/server/projects.go`)
```
GET    /api/projects                     # List projects
POST   /api/projects                     # Create project
GET    /api/projects/{id}                # Get project detail
PUT    /api/projects/{id}                # Update project
DELETE /api/projects/{id}                # Archive project
```

### Hierarchy Endpoint (`internal/server/hierarchy.go`)
```
GET    /api/hierarchy                    # Full tree: commander -> projects -> agents
POST   /api/hierarchy/commander          # Set the commander (body: {"instance_id": "..."} or {"agent_name": "..."} — mutually exclusive)
POST   /api/hierarchy/commander/brief    # Send commander briefing (SSE stream)
```

## Storage Layout

```
~/.eyrie/
├── config.toml               # Existing
├── commander.json             # Which instance is THE commander
├── projects/
│   └── <project-id>.json
├── instances/
│   └── <instance-id>/
│       ├── instance.json
│       ├── config.toml        # (or .json/.yaml)
│       └── workspace/
│           ├── IDENTITY.md
│           ├── SOUL.md
│           ├── MEMORY.md
│           ├── USER.md
│           ├── BOOTSTRAP.md
│           ├── TOOLS.md
│           └── memory/
├── personas/                  # Existing
├── cache/                     # Existing
└── tokens.json                # Existing
```

## Frontend Flow

### Sidebar Evolution
```
eyrie
├── hierarchy          # NEW: tree view (commander > projects > agents)
├── projects           # NEW: project list
├── {legacy agents}    # Existing agents (backward compatible)
├── personas
├── install
└── settings
```

### User Journey

1. **Setup Coordinator** — Choose framework, pick/customize persona, name it, create
2. **Create Project** — Describe goals; coordinator (or user) creates project + orchestrator
3. **Populate Agents** — Orchestrator recommends role agents; user approves or creates manually
4. **Monitor** — Hierarchy tree shows coordinator > projects > orchestrators > role agents

### New Pages
- `HierarchyPage.tsx` — Tree visualization
- `CoordinatorSetup.tsx` — First-time setup wizard
- `ProjectListPage.tsx` — Grid of project cards
- `ProjectDetail.tsx` — Project detail with orchestrator + role agents

## Discovery Integration

Modify `internal/discovery/discovery.go` to scan `~/.eyrie/instances/` after the normal config path scan. For each instance directory: read `instance.json` for framework/name/port, validate and skip corrupt files with a warning, verify the framework config exists, and scan it using the instance name. Errors are logged but non-fatal (instance dir may not exist yet).

## Manager Integration

Add an `ExecuteWithConfig()` function to `internal/manager/manager.go` that accepts a framework name, config path, and lifecycle action. It dispatches to the appropriate CLI command per framework (e.g., `zeroclaw daemon --config`, `openclaw gateway start --config`). This allows instances with custom config paths to be managed alongside legacy agents.

## Files to Create

| File | Purpose |
|------|---------|
| `internal/instance/instance.go` | Instance struct |
| `internal/instance/store.go` | Instance CRUD (JSON, mutex-protected) |
| `internal/instance/ports.go` | Port allocation: sequential scan of 43000-43999, skipping ports used by existing instances (in-memory map) and probing OS availability via `portAvailable()` (bind test); returns error when range exhausted |
| `internal/instance/provisioner.go` | Config gen, workspace scaffolding, identity rendering |
| `internal/project/project.go` | Project struct |
| `internal/project/store.go` | Project CRUD |
| `internal/server/instances.go` | Instance API handlers |
| `internal/server/projects.go` | Project API handlers |
| `internal/server/hierarchy.go` | Hierarchy tree endpoint |
| `web/src/components/HierarchyPage.tsx` | Tree visualization |
| `web/src/components/ProjectListPage.tsx` | Project grid |
| `web/src/components/ProjectDetail.tsx` | Project detail |
| `web/src/components/CoordinatorSetup.tsx` | Setup wizard |
| `internal/instance/instance_test.go` | Unit tests for instance store and provisioner |
| `internal/instance/provisioner_test.go` | Provisioner integration tests |
| `internal/server/middleware.go` | Rate limiting and logging middleware |
| `cmd/eyrie/instance.go` | CLI commands for instance management |

## Files to Modify

| File | Change |
|------|--------|
| `internal/persona/schema.go` | Add IdentityTemplate, MemorySeeds, HierarchyRole fields |
| `internal/discovery/discovery.go` | Scan ~/.eyrie/instances/ for instance configs |
| `internal/discovery/config.go` | Accept name override for instance configs |
| `internal/manager/manager.go` | Add ExecuteWithConfig() with --config flag |
| `internal/server/server.go` | Register instance, project, hierarchy routes |
| `web/src/lib/types.ts` | Add Instance, Project, HierarchyTree types |
| `web/src/lib/api.ts` | Add instance + project API functions |
| `web/src/components/Sidebar.tsx` | Add hierarchy + projects nav |
| `web/src/App.tsx` | Add new routes |

## Implementation Notes

### ID Generation
Instance and Project IDs are generated as full UUIDs by default to ensure collision resistance. However, `validateID()` in `store.go` accepts any string matching `^[a-zA-Z0-9_-]+$` (alphanumerics, hyphens, underscores), so custom slug IDs are also valid. This allows human-friendly IDs in testing or manual creation while UUIDs remain the default for programmatic creation.

### Storage Safety
- All JSON writes in the store modules should use atomic write patterns (write to .tmp then rename)
- File permissions for session data use 0o600 to avoid world-readable secrets
- Instance IDs are validated by `validateID()` in `store.go` using regex `^[a-zA-Z0-9_-]+$` (matches RFC4122 UUIDs and custom slug IDs); returns `"invalid instance ID %q: must contain only alphanumerics, hyphens, and underscores"` on failure
- Persona IDs are validated in `persona/store.go` via `id != filepath.Base(id)` check (rejects path separators, `.`, `..`) plus non-empty check; returns `"invalid persona ID %q"` on failure
- Consider migrating to SQLite/BoltDB for transactional guarantees if corruption becomes an issue

### Commander Uniqueness
- The `handleSetCommander` endpoint rejects requests that provide both `instance_id` and `agent_name`
- Only one commander is stored in `commander.json` at a time; setting a new one replaces the old
- `commander.json` schema: `{"instance_id": "string (optional)", "agent_name": "string (optional)"}` — exactly one field must be set; stored at `~/.eyrie/commander.json`
- Migration: if `commander.json` does not exist, the hierarchy page shows a setup wizard; no migration is needed since the file is created on first commander assignment
- The hierarchy page shows a setup wizard when no commander is configured

### API Security (Future Work)
- Per-instance API tokens (token issuance at instance creation) — OpenClaw instances already get auth tokens via `inst.AuthToken`
- Agent-to-agent creation endpoints should require token authentication: middleware checks `Authorization: Bearer <token>` against the calling instance's AuthToken, rejects with 401/403 and logs the attempt
- Scope-based authorization (coordinator → captain → talon chain)
- Audit logging for API calls (successful and failed creation attempts)
- Rate limiting middleware
- Token validation middleware for agent-to-agent API calls — gate creation workflow behind this middleware or a feature flag

### Error Handling
- Server handlers distinguish 404 (not found) from 500 (internal error) using sentinel errors (`instance.ErrNotFound`, `project.ErrNotFound`, `persona.ErrNotFound`) and `errors.Is()` — no string matching
- Provisioning validation errors use sentinels (`instance.ErrNameExists`, `instance.ErrRequiredField`, `instance.ErrUnsupportedFramework`) → HTTP 400; all other errors → HTTP 500 with slog.Error
- Discovery errors are logged but non-fatal (degraded discovery is better than no discovery)
- Instance provisioning uses a deferred cleanup guarded by a success flag: on entry, `success := false` with `defer func() { if !success { os.RemoveAll(instDir) } }()`, and `success = true` set just before the normal return. This ensures any early return or new error path triggers cleanup automatically without ad-hoc `os.RemoveAll` calls at each failure point. Provisioning is safe to retry with the same name after cleanup since the name uniqueness check runs at the start

### Process Monitoring (Instance struct fields)
- PID, LastSeen, HealthStatus, RestartCount are tracked on Instance
- These fields use `omitempty` to avoid breaking existing instance.json files
- HealthStatus uses states: `"healthy"`, `"unhealthy"`, `"unknown"` (default on creation)
- RestartCount is incremented by the lifecycle manager on each restart action
- LastSeen is updated when a successful health check or activity event is received
- Future: a `monitorInstanceHealth` goroutine on a configurable interval (e.g. 10s) to poll health endpoints and update these fields automatically

## Backward Compatibility

- Existing "legacy" agents (discovered from ~/.zeroclaw/, ~/.openclaw/) work unchanged
- All existing API endpoints unchanged
- Projects, instances, and hierarchy are opt-in additions
- No migration required

## Build Order

1. Instance system (store, provisioner, config gen, port allocation)
2. Discovery + manager integration (instances appear as agents)
3. Instance API endpoints
4. Project system (store + API)
5. Hierarchy endpoint (tree query)
6. Frontend: hierarchy page + coordinator setup
7. Frontend: project pages
8. Frontend: sidebar + routing updates
