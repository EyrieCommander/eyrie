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

**Phase 1 — Fix Agent Reliability (unblock the factory)**
1. [ ] **Fix captain not responding to briefing** — diagnose model/tool issue, test with different models
2. [ ] **Fix cross-agent message delivery** — add retry logic, delivery confirmation, error logging
3. [ ] **Test end-to-end flow** — commander → captain → talon creation → @mention response

**Phase 2 — Activity Tracking (backend)**
4. [ ] **Project activity endpoint** — `GET /api/projects/{id}/activity` aggregating events from all project agents
5. [ ] **Agent status enrichment** — busy/idle inference, current task, last active
6. [ ] **Project progress tracking** — progress %, deadline field on Project struct
7. [ ] **Metrics endpoint** — `GET /api/metrics` for dashboard stats

**Phase 3 — Control Room UI (frontend)**
8. [ ] **Real-time project events** — SSE event bus (`GET /api/projects/{id}/events`) for live UI updates
9. [ ] **Project workspace** — live split view: hierarchy diagram + agent roster + chat workspace
10. [ ] **Mission control** — dashboard with metrics, project cards, commander access
11. [ ] **Agent profile** — identity/soul/memory display + 1:1 chat
12. [ ] **Activity timeline** — chronological event feed with filters

**Phase 4 — Agent Context (backend)**
13. [ ] **Project context in provisioning** — PROJECT.md in talon workspace with project info + team roster
14. [ ] **Dynamic context updates** — regenerate PROJECT.md when team or project changes
15. [ ] **System messages for structural changes** — visible in chat regardless of who (user or agent) made the change

**Phase 5 — User Override (frontend)**
16. [ ] **Persona picker** — grid of persona cards for talon provisioning
17. [ ] **Project creation with commander** — option to create via UI or ask commander

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

- [ ] **Project intake + group chat**: Test end-to-end flow (intake with commander → group chat → captain takes over)
- [ ] **Captain briefing**: Captain should silently bootstrap (fetch API ref, save TOOLS.md) without introducing itself — introduction happens in group chat
- [ ] **Instance provisioning for all frameworks**: Currently only ZeroClaw tested. Need OpenClaw and Hermes instance provisioning (config gen, port alloc, startup)
- [ ] **Captain creating talons**: Captain should be able to call `POST /api/instances` via curl to create talons during conversation
- [ ] **Commander creating captains**: Commander should provision captain instances when setting up new projects
- [ ] **Cross-agent messaging**: After an agent responds in project chat, sync the response to other participants' sessions
- [ ] **Daily sync cron**: Captains sync progress with commander daily; commander aggregates and syncs with user
- [ ] **ZeroClaw observe-group**: Cherry-pick or reimplement `observe_group` from closed PR #4328 so ZeroClaw agents can store group history without responding
- [ ] **OpenClaw observe-group**: Use native `requireMention: true` in group config for project chat participants

## Bugs

- [x] **Config editor corrupts TOML**: Fixed — raw text editor writes directly to disk (`WriteRawAtomic`); inline field editor coerces JSON `float64` back to `int64` before TOML encoding (`CoerceJSONNumbers`).
- [x] **DestroySession TOCTOU**: Fixed — replaced file surgery on `sessions.json` with OpenClaw's native `sessions.delete` RPC (which was already available). Eyrie no longer touches `sessions.json` directly for active session deletion.
- [x] **API key broken after ZeroClaw rebuild**: Fixed — root cause was Eyrie's config editor writing masked `***MASKED***` (from ZeroClaw's GET /api/config) directly to disk, bypassing ZeroClaw's mask-restoration logic. Fix: proxy config saves through ZeroClaw's PUT /api/config when agent is online; reject disk writes containing masked placeholders as safety net. Restored working key from provisioned instance.
- [ ] **Vite proxy buffers SSE responses**: The Vite dev server proxy (`http-proxy`) buffers SSE POST responses instead of streaming them. Events only appear when the response completes. Workaround: bypass proxy for SSE by calling Go backend directly (`SSE_BASE = "http://localhost:7200"` in dev) + CORS handler. Doesn't affect production (same-origin, no proxy).

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

- [ ] **Eyrie virtual channel**: Register Eyrie as a native channel in ZeroClaw/OpenClaw/Hermes (like Telegram/Discord). Deeper integration than WebSocket-based project chat.
- [ ] **PicoClaw support**: Fourth framework option — lighter than ZeroClaw for simple Talon roles
- [ ] **Project templates**: Pre-built team compositions (e.g., "SaaS Launch" = Captain + dev + marketing + research Talons)
- [ ] **Agent-to-agent protocol**: Define coordination patterns (shared context, task handoffs, status updates)
- [ ] **Server middleware layer**: Request logging, panic recovery, and rate limiting middleware — per PLAN.md `internal/server/middleware.go`. Currently all 52 routes are registered bare with no central error handling or observability.
