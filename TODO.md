# Eyrie TODO

## Current State (2026-03-26)

**Branch:** `feature/project-orchestrator`
**Vision:** Agentic factory with control room — agents drive, user oversees via real-time UI
**Design:** `eyrie/project-design.pen` (Pencil mockups), implementation plan at `~/.claude/plans/majestic-crunching-tiger.md`

### What's working:
- Commander system: select existing agent, briefing flow, hierarchy page with role descriptions
- Agent instances: provisioning ZeroClaw instances with own config/workspace/port
- Named sessions on ZeroClaw 0.5.7 (upstream merged our PR #4267 as #4275)
- Project CRUD with captain assignment
- Project group chat: backend (SSE streaming, per-agent sessions, @mention routing)
- Project group chat: frontend (messages, @mention autocomplete with keyboard nav)
- Commander intake flow: 1:1 pre-chat with commander to establish project goals before group chat
- Agent lifecycle: start/stop/restart (including provisioned instances)
- Session management: time-gap spacers, most-recent-first tabs, reset/delete
- Chat history from ZeroClaw's SQLite session DB + JSONL enrichment
- Activity event streaming from ZeroClaw (tool calls, LLM requests, session events)

### Role hierarchy:
- **Commander**: creates projects + assigns captains. User can also create projects via UI (dual control).
- **Captain**: project lead. Owns planning, execution, coordination. Creates and manages talons. User can also add talons via persona picker (dual control).
- **Talon**: specialist agent (researcher, developer, writer, etc.). Created by captain or user.
- **Daily sync (planned)**: captains sync with commander daily, commander syncs with user.

### Next steps (by phase):

**Phase 1 — Fix Agent Reliability** ✅
1. [x] **Sandbox + URL fix** — ZeroClaw seatbelt rejects 127.0.0.1 (#4764, fixed upstream by #4767). All URLs changed to localhost.
2. [x] **Cross-agent message delivery** — retry with backoff (2x), warn-level logging, failures as system messages
3. [x] **@mention routing** — mentions take priority over first-message-commander-only rule
4. [x] **Disconnect resilience** — agent context detached from HTTP request; responses persist if client disconnects
5. [x] **Captain background briefing** — briefing runs at captain assignment, not at chat start

**Phase 2 — Activity Tracking (backend)** ✅
6. [x] **Project activity endpoint** — `GET /api/projects/{id}/activity` aggregating events from all project agents
7. [x] **Agent status enrichment** — `BusyState` + `CurrentTask` inferred from `LastTask` timestamp
8. [x] **SSE event bus** — `GET /api/projects/{id}/events` with publishing from instance/project handlers

**Phase 3 — Control Room UI (frontend)** — in progress
9. [x] **Project workspace** — split view: agent roster sidebar + hierarchy diagram + chat workspace
10. [x] **Real-time SSE streaming** — project chat streams messages, tool calls, and deltas in real-time
11. [x] **Commander/captain routing** — commander speaks first, captain takes over, @mention for specific agents
12. [ ] **Mission control** — dashboard with metrics, project cards, commander access
13. [ ] **Agent profile** — identity/soul/memory display + 1:1 chat
14. [ ] **Activity timeline** — chronological event feed with filters

**Phase 4 — Agent Context (backend)**
15. [ ] **Project context in provisioning** — PROJECT.md in talon workspace with project info + team roster
16. [ ] **Dynamic context updates** — regenerate PROJECT.md when team or project changes
17. [ ] **System messages for structural changes** — visible in chat regardless of who (user or agent) made the change

**Phase 5 — User Override (frontend)**
18. [ ] **Persona picker** — grid of persona cards for talon provisioning
19. [ ] **Project creation with commander** — option to create via UI or ask commander

---

### ZeroClaw PRs:
- **#4275 (named sessions)**: Merged ✅ (our #4267 was superseded)
- **#4350 (streaming tool events)**: Closed — superseded by upstream #4175
- **#4584 (proxy tool event parsing)**: Closed — functionality landed upstream on master
- **#4764 (seatbelt 127.0.0.1 bug)**: Closed — fixed by #4767

---

## Dashboard improvements:
- [ ] **Re-pair button in dashboard**: When Eyrie gets a 401 from a ZeroClaw gateway, show a "re-pair" button that prompts for the pairing code and updates the stored token.
- [ ] **Graceful handling of stale tokens**: Show a clear "authentication expired" state instead of raw 500 error.

## Rich tool output display:
- [ ] **Canvas/frame renders**: Detect "Rendered html content to canvas" in tool output, extract frame ID, show inline preview or "view frame" link that navigates to the rendered content
- [ ] **HTML content preview**: When tool args contain `content_type: "html"`, render a sandboxed iframe preview of the HTML content inline in the tool card
- [ ] **Image outputs**: Detect image URLs or base64 image data in tool outputs, render inline `<img>` previews
- [ ] **Structured JSON responses**: Syntax-highlight JSON tool outputs (API responses, config reads) with collapsible sections for large payloads
- [ ] **File path links**: Detect file paths in tool outputs, make them clickable to open in the agent's file browser or navigate to the file
- [ ] **Diff display**: For tool outputs containing unified diffs, render with color-coded additions/deletions

## Security

- [ ] **Agent-to-Eyrie API access**: Currently agents use `curl` via `exec` tool to reach Eyrie's API at localhost:7200. OpenClaw's `web_fetch` blocks private IPs (SSRF policy). For production, explore:
  - Eyrie as an MCP server (agents connect via MCP protocol instead of HTTP)
  - Tailscale-based access (Eyrie binds to Tailscale IP, avoids private IP issue)
  - Agent-specific API tokens with scoped permissions
  - mTLS between agents and Eyrie
- [ ] **Auto-pairing for provisioned instances**: Currently provisioned ZeroClaw instances disable pairing (`require_pairing = false`). For production, Eyrie should auto-pair: start the daemon, capture the pairing code from stdout, call `POST /pair`, and save the auth token.
  - **Secure token storage**: Use restrictive file permissions (0o600) at minimum, prefer OS keyring integration. Tokens should support rotation/refresh under `~/.eyrie/tokens/`.
- [ ] **Stale daemon cleanup**: `runDetached` spawns background processes but doesn't kill existing ones on the same port. Before starting a new daemon, check for and kill any existing process on the target port.
- [ ] **API key provisioning for instances**: Currently copies encrypted api_key + .secret_key from parent ZeroClaw installation. Not ideal — shared secret key means one compromised instance exposes all. Let user choose:
  - Shared API key via env var (simplest)
  - Per-instance keys via `zeroclaw onboard` on each instance
  - Centralized key vault in Eyrie (inject at start time, never stored on disk)

## Functionality

- [x] **Project group chat**: Real-time SSE streaming with @mention routing — commander introduces, captain takes over
- [x] **Captain briefing**: Runs in background at captain assignment, not at chat start
- [x] **Captain creating talons**: Captain calls `POST /api/instances` via curl — tested end-to-end
- [x] **Cross-agent messaging**: Retry with backoff, failures surfaced as system messages
- [ ] **Instance provisioning for all frameworks**: ZeroClaw and PicoClaw provisioning implemented. Need OpenClaw and Hermes instance provisioning testing (config gen, port alloc, startup)
- [ ] **Commander creating captains**: Commander should provision captain instances when setting up new projects
- [ ] **Daily sync cron**: Captains sync progress with commander daily; commander aggregates and syncs with user
- [ ] **ZeroClaw observe-group**: Cherry-pick or reimplement `observe_group` from closed PR #4328 so ZeroClaw agents can store group history without responding
- [ ] **OpenClaw observe-group**: Use native `requireMention: true` in group config for project chat participants

## Bugs

- [x] **Config editor corrupts TOML**: Fixed — raw text editor writes directly to disk (`WriteRawAtomic`); inline field editor coerces JSON `float64` back to `int64` before TOML encoding (`CoerceJSONNumbers`).
- [x] **DestroySession TOCTOU**: Fixed — replaced file surgery on `sessions.json` with OpenClaw's native `sessions.delete` RPC (which was already available). Eyrie no longer touches `sessions.json` directly for active session deletion.
- [x] **API key broken after ZeroClaw rebuild**: Fixed — root cause was Eyrie's config editor writing masked `***MASKED***` (from ZeroClaw's GET /api/config) directly to disk, bypassing ZeroClaw's mask-restoration logic. Fix: proxy config saves through ZeroClaw's PUT /api/config when agent is online; reject disk writes containing masked placeholders as safety net. Restored working key from provisioned instance.
- [x] **SSE streaming not rendering**: Root cause was `mountedRef` pattern — React re-renders briefly unmounted ProjectChat, causing the SSE callback to hold a stale ref and silently drop all events. Fixed by removing mountedRef, always-mounting ProjectChat (overlays for setup prompts), and using AbortController for cleanup. Vite proxy streams SSE fine.
- [ ] **Config editor expands all defaults**: When saving config through the dashboard, ZeroClaw's GET /api/config returns all fields (including defaults). The editor writes the full config back to disk, bloating a 24-line provisioner config to 724 lines. Fix: read from disk for editing instead of from the API, so only user overrides are displayed and saved.
- [ ] **Vite proxy buffers SSE responses**: The Vite dev server proxy (`http-proxy`) buffers SSE POST responses instead of streaming them. Events only appear when the response completes. Workaround: bypass proxy for SSE by calling Go backend directly (`SSE_BASE = "http://localhost:7200"` in dev) + CORS handler. Doesn't affect production (same-origin, no proxy).

## Code Cleanup

- [ ] **SSE_BASE unused in api.ts**: `SSE_BASE` is declared for Vite dev SSE bypass but never used by streaming functions. Either wire it into `streamMessage`/`streamProjectChat`/`streamInstall` or remove it.
- [ ] **CORS allowlist from config**: Current `corsHandler` allows localhost only. For production, add `AllowedOrigins []string` to dashboard config and pass it to corsHandler.
- [ ] **SetCaptainDialog error surfacing**: `streamCaptainBriefing` callback only console.errors on failure and still calls `onDone()`. Surface briefing failures to the user via error state.
- [ ] **ProjectDetail reset validation**: The reset button's fetch calls don't check `response.ok`. Failures can be silently ignored.
- [ ] **ProjectListPage unmount safety**: The polling loop in `handleStartCaptain` can update state after unmount. Add AbortController or mounted ref.
- [ ] **InstallPage handleManage error overwrite**: `handleManage` unconditionally writes synthetic success into `installProgress`, potentially overwriting a prior error state.
- [ ] **AgentDetail name editing error feedback**: The display name form swallows failures silently. Add local error state to surface update failures.

## UI

- [x] **Extract shared chat component**: ChatPanel.tsx extracted from AgentDetail. ProjectChat imports shared sub-components (PartToolCallCard, StreamingCursor).
- [ ] **Background commander briefing**: Move commander briefing to a background task when assigned on the hierarchy page (no redirect to agent chat). The briefing bootstraps the commander (fetch API ref, save TOOLS.md) — the user doesn't need to watch it.
- [ ] **Hierarchy page**: Show agent status (running/stopped) with live refresh
- [ ] **Project detail**: Add activity timeline showing what each agent is doing
- [ ] **Persona catalog**: Expand with more curated personas and allow community sharing ("Claude Mart" concept)
- [ ] **Session management**: Test session group delete across all frameworks

## Integrations

- [ ] **Telegram bridge for project chat**: Mirror Eyrie project conversations into Telegram groups for mobile access
- [ ] **Discord bridge for project chat**: Same as Telegram bridge for Discord
- [ ] **Slack bridge**: Optional for teams using Slack

## Architecture

- [ ] **Eyrie virtual channel**: Register Eyrie as a native channel in ZeroClaw/OpenClaw/PicoClaw/Hermes (like Telegram/Discord). Deeper integration than WebSocket-based project chat.
- [x] **PicoClaw support**: Fourth framework — adapter (978 lines), discovery, provisioning, registry, install page all wired up. Pending:
  - [ ] **Post-install onboarding UI**: After installing PicoClaw from the install page, launch the framework's onboard wizard (e.g., `picoclaw onboard`) from the dashboard so the config file gets created and discovery can pick it up. Currently requires manual CLI onboarding.
  - [ ] **PicoClaw instance provisioning test**: Test end-to-end provisioning of PicoClaw instances from the hierarchy page (captain creating talons)
- [x] **Nanobot / ShibaClaw evaluation**: Cloned both to `claws/nanobot/` and `claws/shibaclaw/`. Security audit found critical issues inherited from Nanobot (blocklist-only shell exec, `0.0.0.0` default bind, `restrict_to_workspace: False`, unrestricted `os.execv` restart) plus ShibaClaw-specific WebUI issues (CORS wildcard, API key exposure, auth token in query params). ShibaClaw also still depends on `litellm`, which had a supply chain attack (HKUDS/nanobot#2439). Posted findings to zeroclaw-labs/zeroclaw/discussions/4876. Not integrating either — revisit if security posture improves.
- [ ] **Project templates**: Pre-built team compositions (e.g., "SaaS Launch" = Captain + dev + marketing + research Talons)
- [ ] **Agent-to-agent protocol**: Define coordination patterns (shared context, task handoffs, status updates)
- [ ] **Server middleware layer**: Request logging, panic recovery, and rate limiting middleware — per PLAN.md `internal/server/middleware.go`. Currently all 52 routes are registered bare with no central error handling or observability.
