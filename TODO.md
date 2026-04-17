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
- **Commander**: Eyrie itself — an LLM loop inside the Eyrie process with tools that directly call projectStore/instanceStore/chatStore/provisioner. The user talks to the commander; the commander dispatches captains. No separate agent process, no provisioning, no briefing, no subprocess sandboxing concerns. See "Phase 5: Eyrie as Commander" below.
- **Captain**: project lead. First responder in project chat. Owns planning, execution, coordination. Creates and manages talons. User can also add talons via persona picker (dual control).
- **Talon**: specialist agent (researcher, developer, writer, etc.). Created by captain or user.

### Next steps (by phase):

**Phase 3 — Control Room UI (frontend)**
9. [x] **Project workspace** — split view: agent roster sidebar + hierarchy diagram + chat workspace
10. [x] **Real-time SSE streaming** — project chat streams messages, tool calls, and deltas in real-time
11. [x] **Project chat routing** — captain is first responder, @mention forwarding with chaining, [LISTENING] follow-up for agent responses
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
- **#4852 (composite session backend)**: Merged then reverted in batch rollback. Re-submitted as **#5147** — closed (pre-microkernel paths)
- **#5148 (http_request per-host allowlist)**: Closed (pre-microkernel paths) — needs redo on new crate layout
- **#5696 (session reset/delete tools)**: Open, ready for review — `SessionResetTool` + `SessionDeleteTool`, not registered by default (destructive). Labels: `tool`, `security`. Supersedes #5147.
- **#5705 (session abort + incremental persistence)**: Open, ready for review — `POST /api/sessions/{id}/abort` + streaming responses saved every 500ms. Auto-routed to @jordanthejet by review bot. Eyrie's stop button and crash resilience depend on this.
- **#5701 (clear_messages issue)**: Open issue — `clear_messages` trait method for O(1) session reset. Follow-up optimization for #5696.
- **#5791 (When to Supersede docs)**: Open, ready for review — adds guidance on when to push fixups vs supersede contributor PRs. Closes #4363.
- **#4363 (push fixups instead of superseding)**: Closed in favor of docs PR (#5791).

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
- [x] **Centralized key vault**: `config/vault.go` — flat JSON store at `~/.eyrie/keys.json` (0600 permissions) with singleton accessor. REST API (`GET/PUT/DELETE /api/keys`, `POST /api/keys/{provider}/validate`). Keys injected into framework processes via env vars (`EnvSlice()`) through `ExecuteWithConfigEnv`. Settings page UI for add/edit/delete with provider validation. Embedded agents use vault directly via `SetVault()`. Pending improvements:
  - [ ] **Encryption at rest**: Keys stored as plain JSON. Add ChaCha20-Poly1305 encryption (like ZeroClaw's SecretStore) with a master key in `~/.eyrie/.vault_key` (0600).
  - [ ] **Per-instance key overrides**: Currently one key per provider globally. Add optional per-instance overrides for multi-tenant setups (e.g., different OpenRouter keys for different projects).
  - [ ] **Custom env var names**: Provider-to-env-var mapping is hardcoded. Add optional `env_var` field per key for frameworks with non-standard env var names (e.g., `PICOCLAW_CHANNELS_*`).
  - [ ] **CLI command**: `eyrie keys set <provider> <key>` — API + UI are sufficient for now.
  - [ ] **Key rotation dashboard**: Show which instances use which vault keys, last rotation date, and "restart required" indicator when a key changes.

## Functionality

- [x] **Project group chat**: Real-time SSE streaming with @mention routing — captain is first responder, [LISTENING] follow-up for delegated work
- [x] **Captain briefing**: Runs in background at captain assignment, not at chat start
- [x] **Captain creating talons**: Captain calls `POST /api/instances` via curl — tested end-to-end
- [x] **Cross-agent messaging**: Retry with backoff, failures surfaced as system messages
- [ ] **Instance provisioning for all frameworks**: ZeroClaw and PicoClaw provisioning implemented. Need OpenClaw and Hermes instance provisioning testing (config gen, port alloc, startup)

### Phase 5: Eyrie as Commander (primary focus)

Eyrie itself becomes the commander — the user chats directly with Eyrie. No separate agent instance, no provisioning, no briefing. Eyrie's commander has its own LLM loop and tools that directly read/write the project, instance, and chat data.

**Backend (do first):**
- [ ] Build the commander's LLM loop so it can hold a conversation, call tools, and stream responses back to the UI
- [ ] Support multiple LLM providers (Anthropic, OpenAI, and OpenAI-compatible endpoints like the Claude Max proxy, Ollama, OpenRouter) with the user choosing a default; keys come from the existing vault
- [ ] Give the commander a persistent conversation history that survives restarts
- [ ] Give the commander its own memory store so it can remember user preferences and project context across conversations
  - **Later — recall strategy beyond flat JSON**: MVP injects all entries into the system prompt each turn. Options when that breaks down (too many entries, token cost, or need for semantic lookup):
    - SQLite with FTS5 for keyword/prefix search — mirrors ZeroClaw's session storage (`claws/zeroclaw/`) and gives fast `recall(query)` without loading everything
    - Vector embeddings (local model, e.g. via `text-embedding-3-small` through OpenAI-compat endpoint, or a Go-native embedder) for semantic recall — LLM says "what did I say about mobile releases?" and we search by meaning, not exact key
    - Tag/namespace support (`project:X/*`, `user-pref/*`) for scoped recall and bulk forget
    - TTL-based pruning and "last-accessed" ordering so stale notes fall out naturally
    - Cross-reference how EyrieClaw, OpenClaw, and PicoClaw structure their agent memory (`claws/*/`) — pick conventions rather than invent new ones
  - **Later — UI surface for memory**: list/view/edit/delete via Settings page (backend beyond skeleton needs PUT/DELETE endpoints)
- [ ] Implement an initial tool set: listing and getting project details, creating projects, listing personas and running agents, assigning captains (with full provisioning and briefing), reading a project's chat, sending messages into a project chat on the user's behalf, querying recent activity, and restarting agents
- [ ] Autonomy policy: read-only tools run automatically; write tools (create, assign, send, restart) require user confirmation
- [ ] Surface context-window usage to the UI so the user can see when a conversation is getting long (summarization deferred)

**Frontend (happens in parallel on another machine):**
- Commander chat page as the primary user-facing surface
- Settings for provider and model selection with a connectivity test
- Visible context-usage indicator

**Features that emerge from having tools plus memory:**
- Autonomous project creation from a single user request
- Cross-project oversight and status summarization
- Daily sync that walks each project and produces one summary for the user
- Reassigning talons between projects
- Turning high-level goals into concrete projects

**Cleanup (no backward compatibility — no existing users):**
- [ ] Delete the old commander-agent concept everywhere: the stored pointer to a commander instance, the set/get commander endpoints, the frontend setup page, and any remaining participant/discovery paths that assumed the commander was an agent
- [ ] When the commander sends a message into a project chat, it appears as a distinct sender (not "user") so the captain and user can see who initiated it

**Deferred (project-chat observation parity):**
- [ ] Let ZeroClaw agents observe project chats without responding (Cherry-pick or reimplement `observe_group` from closed PR #4328 so ZeroClaw agents can store group history without responding
- [ ] Let OpenClaw agents observe project chats without responding (Use native `requireMention: true` in group config for project chat participants)

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
- [ ] **Unify streaming into messages array**: Currently ProjectChat has two rendering paths — `messages` (stored) and `streamingParts` (live). This causes duplication on done/poll, state loss on transitions, and complex filtering to avoid showing both. Refactor to build agent responses directly in the `messages` array with a temporary ID, updating in place as deltas arrive. One source of truth eliminates the dual-render problem entirely. Both ChatPanel and ProjectChat could share this approach.
- [ ] **Background commander briefing**: Move commander briefing to a background task when assigned on the hierarchy page (no redirect to agent chat). The briefing bootstraps the commander (fetch API ref, save TOOLS.md) — the user doesn't need to watch it.
- [ ] **Hierarchy page**: Show agent status (running/stopped) with live refresh
- [ ] **Project detail**: Add activity timeline showing what each agent is doing
- [ ] **Persona catalog**: Expand with more curated personas and allow community sharing ("Claude Mart" concept)
- [ ] **Session management**: Test session group delete across all frameworks
- [x] **Destroy talons on project reset**: `POST /api/projects/{id}/reset` clears chat, resets commander/captain sessions, stops+deletes talons. Auto-start chat restored.
- [ ] **Hide project sessions from 1:1 chat**: Filter out sessions matching a project ID from the ChatPanel session list. Project conversations should only be accessed via the project chat UI — showing them in 1:1 creates split-brain confusion. Later: clicking a project session could redirect to the project detail page instead.
- [ ] **Bulk project selection + delete in UI**: Project list page needs multi-select (checkboxes or shift-click) with a bulk delete action. Currently deleting test projects requires per-row action or filesystem cleanup. Should destroy same-UUID workspace directory alongside the `.json` metadata, matching the single-delete path.
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
- [x] Extract `dedupMessages` helper in chat.go (was duplicated between Messages and Compact)
- [x] Extract `consumeAgentStream` + `storeAgentResponse` helpers in orchestrate.go (was duplicated 3x)
- [x] Extract `DefaultAllowedCommands` shared constant (was duplicated between provisioner and migrator)
- [x] Use `strings.Builder` for streamed text accumulation (was O(n²) string concat)
- [ ] Add `?since=` parameter to `GET /api/projects/{id}/chat` to avoid fetching full history on every poll
- [ ] Extract main respondent streaming into `consumeAgentStream` (currently separate due to incremental persistence)
- [ ] Use `reflect.DeepEqual` in migrate.go `setNestedValue` instead of `fmt.Sprintf("%v")` comparison
- [ ] Extract `briefingTemplateForRole(role) string` helper (switch duplicated in orchestrate.go)
- [ ] Extract `"eyrie-captain-briefing"` session key as a named constant

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
- [x] **EyrieClaw (embedded agent)**: Go-native agent loop (1,874 lines) running inside the Eyrie process as goroutines. OpenAI-compatible provider, 5 built-in tools (workspace-sandboxed), ring buffer logging, JSONL sessions. Strong default for talons. Pending improvements:
  - [ ] **Native Anthropic provider**: Use `anthropic-sdk-go` for direct Anthropic API with extended thinking support.
  - [ ] **MCP client integration**: Use official `modelcontextprotocol/go-sdk` for skill-based tool injection (e.g., Remotion video authoring skill).
  - [ ] **Skill package format**: Define the format for reusable knowledge packages that teach agents specific technologies. Skills = API reference + patterns + scaffolding knowledge + optional MCP tools.
  - [ ] **Automatic context summarization**: V1 uses hard truncation when token budget exceeded. Add LLM-powered summarization of older messages as a follow-up.
- [ ] **Project templates**: Pre-built team compositions (e.g., "SaaS Launch" = Captain + dev + marketing + research Talons)
- [ ] **Agent-to-agent protocol**: Define coordination patterns (shared context, task handoffs, status updates)
- [ ] **Server middleware layer**: Request logging, panic recovery, and rate limiting middleware — per PLAN.md `internal/server/middleware.go`. Currently all 52 routes are registered bare with no central error handling or observability.
