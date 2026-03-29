# Eyrie TODO

## Current State (2026-03-30)

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
- **#4852 (composite session backend)**: Merged then reverted in batch rollback. Re-submitted as **#5147** — open, awaiting review
- **#5148 (http_request per-host allowlist)**: Open — replaces blanket `allow_private_hosts: bool` with per-host `Vec<String>`, backward compatible via custom serde deserializer

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
- [x] **Config editor expands all defaults**: Fixed — all adapters now read config from disk first (user overrides only), falling back to API only if the file is inaccessible.
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
- [x] **Nanobot / ShibaClaw evaluation**: Cloned both to `claws/nanobot/` and `claws/shibaclaw/`. Posted security audit to zeroclaw-labs/zeroclaw/discussions/4876. Not integrating yet.
  - **v0.0.6b reassessment (2026-03-29)**: ShibaClaw fixed 6/9 findings — removed litellm, fixed CORS (safe defaults), masked auth token in logs, redacted secrets in /api/settings, set `restrict_to_workspace: true`, and implemented randomized tool output delimiters. Still not integrating because the 3 remaining issues are the ones that matter for Eyrie: blocklist-only shell exec (9 regex patterns, no sandbox), gateway binds `0.0.0.0` by default, and `os.execv` restart with no permission check. All inherited from Nanobot upstream. Revisit when ShibaClaw adds real shell sandboxing or Nanobot merges the Bubblewrap PR (HKUDS/nanobot#1873). Note: maintainer said he's open to adding a plain REST/WS API alongside Socket.IO for Eyrie integration once security blockers are resolved.
- [ ] **Auto-fix button**: Error events on the dashboard get a "fix it" button that dispatches to either an agent (via existing orchestration) or Claude Code (via `claude -p` subprocess with structured JSON output). Server endpoint `POST /api/errors/{id}/autofix` with `backend: "claude-code" | "agent"` parameter. Claude Code path is a thin integration, not a full adapter.
- [ ] **EyrieClaw (embedded agent)**: Go-native agent loop that runs inside the Eyrie process as goroutines instead of separate framework processes. Zero-overhead talons — no gateway, no port, no HTTP roundtrip. Uses OpenAI-compatible API client for LLM calls, Go channels for orchestrator communication. Value prop: spin up 20 lightweight talons without 20 processes. Research phase — evaluating Go LLM libraries and what Eyrie internals can be reused.
- [ ] **Project templates**: Pre-built team compositions (e.g., "SaaS Launch" = Captain + dev + marketing + research Talons)
- [ ] **Agent-to-agent protocol**: Define coordination patterns (shared context, task handoffs, status updates)
- [ ] **Server middleware layer**: Request logging, panic recovery, and rate limiting middleware — per PLAN.md `internal/server/middleware.go`. Currently all 52 routes are registered bare with no central error handling or observability.
