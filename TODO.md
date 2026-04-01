# Eyrie TODO

## Current State (2026-03-30)

**Branch:** `feature/project-orchestrator`
**Vision:** Agentic factory with control room — agents drive, user oversees via real-time UI
**Design:** `eyrie/project-design.pen` (Pencil mockups), implementation plan at `~/.claude/plans/majestic-crunching-tiger.md`

### What's working:
- Commander system: select/change commander, briefing on assignment, inline role instructions per project
- Agent instances: provisioning ZeroClaw/PicoClaw instances with isolated workspace/port/sessions
- Auto-pairing: provisioned instances get WebSocket auth tokens automatically on start
- Project CRUD with captain assignment + system messages on structural changes
- Project group chat: single-respondent routing with [LISTENING] + @mention agent-to-agent forwarding
- Briefing templates: extracted to markdown files in `internal/server/briefings/`
- Mission control: metric cards, swim-lane timeline, agent hierarchy subpage, commander bar
- Project workspace: split view with roster, hierarchy diagram, and always-mounted chat
- Agent lifecycle: start/stop/restart (including provisioned instances, autonomous mode)
- Session management: time-gap spacers, most-recent-first tabs, reset/delete
- Chat history from ZeroClaw's SQLite session DB + JSONL enrichment
- Activity event streaming from ZeroClaw (tool calls, LLM requests, session events)
- EyrieClaw embedded agents: in-process lightweight talons (in progress)

### Role hierarchy:
- **Commander**: creates projects + assigns captains. User can also create projects via UI (dual control).
- **Captain**: project lead. Owns planning, execution, coordination. Creates and manages talons. User can also add talons via persona picker (dual control).
- **Talon**: specialist agent (researcher, developer, writer, etc.). Created by captain or user.
- **Daily sync (planned)**: captains sync with commander daily, commander syncs with user.

### Next steps (by phase):

**Phase 3 — Control Room UI (frontend)**
9. [x] **Project workspace** — split view: agent roster sidebar + hierarchy diagram + chat workspace
10. [x] **Real-time SSE streaming** — project chat streams messages, tool calls, and deltas in real-time
11. [x] **Commander/captain routing** — commander speaks first, hands off to captain via @mention, agent-to-agent forwarding
12. [x] **Mission control** — dashboard with metrics, swim-lane timeline, hierarchy subpage, commander bar. Route: /mission-control
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

## Security

- [ ] **Agent-to-Eyrie API access**: Currently agents use `curl` via `exec` tool to reach Eyrie's API at localhost:7200. OpenClaw's `web_fetch` blocks private IPs (SSRF policy). For production, explore:
  - Eyrie as an MCP server (agents connect via MCP protocol instead of HTTP)
  - Tailscale-based access (Eyrie binds to Tailscale IP, avoids private IP issue)
  - Agent-specific API tokens with scoped permissions
  - mTLS between agents and Eyrie
- [ ] **Rate limit instance creation**: Agents in autonomous mode can create talons in a loop. Add a per-project rate limit (e.g., max 10 instances per project, max 5 per minute) to prevent runaway provisioning.
- [x] **Auto-pairing for provisioned instances**: Implemented — `autoPairZeroClaw()` runs on instance start, fetches paircode from `/admin/paircode`, pairs, and stores token in `tokens.json`. Pairing now enabled by default (`require_pairing = true`).
  - **Secure token storage**: Use restrictive file permissions (0o600) at minimum, prefer OS keyring integration. Tokens should support rotation/refresh under `~/.eyrie/tokens/`.
- [ ] **Stale daemon cleanup**: `runDetached` spawns background processes but doesn't kill existing ones on the same port. Before starting a new daemon, check for and kill any existing process on the target port.
- [ ] **Centralized key vault**: Eyrie-managed API key storage at `~/.eyrie/keys/` with per-provider entries (e.g., `anthropic.key`, `openrouter.key`). Keys stored with 0o600 permissions, optionally encrypted with a master password or OS keyring.
  - **Manual setup**: User adds keys via settings page or `eyrie keys set anthropic <key>` CLI command. Keys are validated against the provider's API before saving.
  - **Framework provisioning**: When a framework completes onboarding but has no API key configured, Eyrie checks the vault for a matching provider key. If found, injects it into the framework's config (env var, config file, or security file depending on framework). The API key prompt dialog (shown after setup) should offer "use key from vault" as a one-click option alongside manual entry.
  - **Instance provisioning**: When creating new talon instances, inject the vault key at start time via environment variable (`ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, etc.) instead of copying encrypted key files. Key never written to instance disk — only lives in the process environment.
  - **Key rotation**: Updating a key in the vault propagates to all running instances on next restart. Dashboard shows which instances use which vault keys and their last rotation date.
  - **API key provisioning for instances**: Currently copies encrypted api_key + .secret_key from parent ZeroClaw installation. Not ideal — shared secret key means one compromised instance exposes all. Let user choose:
    - Shared API key via env var (simplest)
    - Per-instance keys via `zeroclaw onboard` on each instance
    - Centralized key vault in Eyrie 
  - **Per-framework key format**: Each framework stores keys differently. Vault injection must be framework-aware:
    - ZeroClaw: encrypted `api_key` field in config.toml (or env var `ANTHROPIC_API_KEY`)
    - OpenClaw: `.env` file in config dir (or env var)
    - PicoClaw: `.security.yml` in config dir (or env var `PICOCLAW_CHANNELS_*`)
    - Hermes: `.env` file in config dir (or env var)

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
- [x] **Vite proxy buffers SSE responses**: Fixed — Vite proxy configured with `Accept-Encoding: identity` + `timeout: 0` to disable compression buffering. All SSE endpoints stream through the proxy correctly now.

## Code Cleanup

- [x] **SSE_BASE unused in api.ts**: Removed — Vite proxy streams correctly now, no bypass needed.
- [ ] **CORS allowlist from config**: Deferred — no production deployment planned. Current localhost-only restriction is correct for local dashboard. Revisit if Eyrie is deployed to a server or accessed over LAN.
- [x] **SetCaptainDialog error surfacing**: Acceptable — briefing is fire-and-forget by design (dialog closes before callback fires). Captain creation/assignment errors are already surfaced.
- [x] **ProjectDetail reset validation**: Fixed — chat reset now checks `response.ok` and throws on failure.
- [x] **ProjectListPage unmount safety**: Fixed — AbortController stops polling loop on dialog unmount.
- [x] **InstallPage handleManage error overwrite**: Fixed — `handleManage` preserves existing error state instead of overwriting with synthetic success.
- [x] **AgentDetail name editing error feedback**: Fixed — `nameError` state surfaces update failures inline below the agent name.

## UI

- [x] **Extract shared chat component**: ChatPanel.tsx extracted from AgentDetail. ProjectChat imports shared sub-components (PartToolCallCard, StreamingCursor).
- [ ] **Background commander briefing**: Move commander briefing to a background task when assigned on the hierarchy page (no redirect to agent chat). The briefing bootstraps the commander (fetch API ref, save TOOLS.md) — the user doesn't need to watch it.
- [ ] **Hierarchy page**: Show agent status (running/stopped) with live refresh
- [ ] **Project detail**: Add activity timeline showing what each agent is doing
- [ ] **Persona catalog**: Expand with more curated personas and allow community sharing ("Claude Mart" concept)
- [ ] **Session management**: Test session group delete across all frameworks
- [x] **Destroy talons on project reset**: `POST /api/projects/{id}/reset` clears chat, resets commander/captain sessions, stops+deletes talons. Auto-start chat restored.
- [ ] **Hide project sessions from 1:1 chat**: Filter out sessions matching a project ID from the ChatPanel session list. Project conversations should only be accessed via the project chat UI — showing them in 1:1 creates split-brain confusion. Later: clicking a project session could redirect to the project detail page instead.
- [ ] **Re-pair button in dashboard**: When Eyrie gets a 401 from a ZeroClaw gateway, show a "re-pair" button that prompts for the pairing code and updates the stored token.
- [x] **Graceful handling of stale tokens**: Show a clear "authentication expired" state instead of raw 500 error.
- [x] **Rich tool output display**: Detect "Rendered html content to canvas" in tool output, extract frame ID, show inline preview or "view frame" link that navigates to the rendered content. Also HTML preview, image preview, JSON highlighting, file path links and diff display

## Provisioning Config

Known config requirements for provisioned agents, by framework. The provisioner (`internal/instance/provisioner.go`) handles ZeroClaw. Other frameworks need equivalent treatment.

**ZeroClaw** (fixed in provisioner):
- `autonomy.level = "full"` — ZeroClaw rejects "autonomous", expects readonly/supervised/full
- `security.sandbox.backend = "none"` — macOS seatbelt blocks basic commands even inside workspace
- `autonomy.allowed_commands` — must include common utilities (sleep, mkdir, cp, mv, rm, sed, etc.), default list is too restrictive for working agents
- `max_tool_iterations = 50` — default 10 is too low for agents exploring a codebase
- `http_request.enabled = true` + `allowed_private_hosts = ["localhost"]` — agents need to reach Eyrie API
- API key copied from parent ZeroClaw installation with secret key

**OpenClaw** (needs work):
- [ ] Equivalent autonomy/sandbox settings for provisioned OpenClaw instances
- [ ] Verify `sessions.json` handling for provisioned instances
- [ ] Test captain/talon provisioning end-to-end

**PicoClaw** (needs work):
- [ ] Config generation for provisioned PicoClaw instances
- [ ] Verify gateway port allocation and auto-discovery

**Cross-framework**:
- [x] Config migration tool: update existing instance configs when provisioner defaults change (currently requires manual sed per instance)
- [ ] Validation: check provisioned config against framework's schema before starting, surface errors in UI instead of silent daemon crash

## Code Health

- [ ] Extract JSONL append/read into generic utility in `internal/fileutil/` (duplicated in `embedded/sessions.go` and `project/chat.go`) — deferred: different message types make shared extraction low-ROI without generics
- [x] Replace per-request `NewStore()` calls with cached stores on Server struct (38 call sites eliminated across projects.go, instances.go, hierarchy.go)
- [ ] Poll `fetchCommander()` only on project-related routes instead of globally every 30s — deferred: needs route-level context refactor
- [x] Add change-detection guard to DataContext polling — JSON.stringify comparison skips no-op re-renders on 30s poll
- [ ] Remove `ensureMetrics` migration shim in `useAgentMetrics.ts` once old format is obsolete
- [x] Parallelize talon destruction in `handleProjectReset` with sync.WaitGroup — 30s (slowest) instead of 30s×N

## Integrations / Architecture

- [ ] **Telegram bridge for project chat**: Mirror Eyrie project conversations into Telegram groups for mobile access
- [ ] **Discord bridge for project chat**: Same as Telegram bridge for Discord
- [ ] **Slack bridge**: Optional for teams using Slack
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
