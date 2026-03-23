# Eyrie TODO

## Current State (2026-03-23)

**Branch:** `feature/project-orchestrator`

### What's working:
- Commander system: select existing agent, briefing flow, hierarchy page
- Agent instances: provisioning ZeroClaw instances with own config/workspace/port
- Named sessions on ZeroClaw 0.5.7 (upstream merged our PR #4267 as #4275)
- Streaming tool events on ZeroClaw (PR #4350 submitted, passing tests)
- Project CRUD with captain assignment
- Project group chat: backend (SSE streaming, per-agent sessions, @mention routing)
- Project group chat: frontend (messages, @mention autocomplete with keyboard nav)
- Agent lifecycle: start/stop/restart (including provisioned instances)
- Session management: time-gap spacers, most-recent-first tabs, reset/delete
- Chat history from ZeroClaw's SQLite session DB + JSONL enrichment

### Next steps (in order):
1. **Test project chat end-to-end** — clear chess coach chat, click "start project chat", verify commander introduces project and captain takes over
2. **Fix captain not responding to briefing** — captain receives briefing but doesn't respond (may be a ZeroClaw tool execution issue with the kimi model)
3. **OpenClaw observe-group** — use `requireMention: true` for OpenClaw agents in project chat so they silently observe without wasting LLM calls
4. **Commander creating agents via API** — the vision is the commander calling `POST /api/instances` during conversation to provision talons
5. **Cross-agent message sync** — after an agent responds in project chat, forward its response to other agents' sessions

### ZeroClaw PRs:
- **#4275 (named sessions)**: Merged ✅ (our #4267 was superseded)
- **#4350 (streaming tool events)**: Open, all checks pass (3 tests, clippy clean)
- **feature/named-sessions** branch: can be deleted (superseded)
- **feature/streaming-tool-events** branch: active, rebased on 0.5.9

---

## Security

- [ ] **Agent-to-Eyrie API access**: Currently agents use `curl` via `exec` tool to reach Eyrie's API at localhost:7200. OpenClaw's `web_fetch` blocks private IPs (SSRF policy). For production, explore:
  - Eyrie as an MCP server (agents connect via MCP protocol instead of HTTP)
  - Tailscale-based access (Eyrie binds to Tailscale IP, avoids private IP issue)
  - Agent-specific API tokens with scoped permissions
  - mTLS between agents and Eyrie
- [ ] **Auto-pairing for provisioned instances**: Currently provisioned ZeroClaw instances disable pairing (`require_pairing = false`). For production, Eyrie should auto-pair: start the daemon, capture the pairing code from stdout, call `POST /pair`, and save the auth token.
  - **Secure token storage**: Use restrictive file permissions (0o600) at minimum, prefer OS keyring integration. Tokens should support rotation/refresh under `~/.eyrie/tokens/`.
- [ ] **Stale daemon cleanup**: `runDetached` spawns background processes but doesn't kill existing ones on the same port. Before starting a new daemon, check for and kill any existing process on the target port.

## Functionality

- [ ] **Project group chat**: Test end-to-end flow (commander intro → captain takes over → user conversation)
- [ ] **Captain briefing**: Captain should silently bootstrap (fetch API ref, save TOOLS.md) without introducing itself — introduction happens in group chat
- [ ] **Instance provisioning for all frameworks**: Currently only ZeroClaw tested. Need OpenClaw and Hermes instance provisioning (config gen, port alloc, startup)
- [ ] **Commander creating agents**: Commander should be able to call `POST /api/instances` via curl to provision talons during conversation
- [ ] **Cross-agent messaging**: After an agent responds in project chat, sync the response to other participants' sessions
- [ ] **ZeroClaw observe-group**: Cherry-pick or reimplement `observe_group` from closed PR #4328 so ZeroClaw agents can store group history without responding
- [ ] **OpenClaw observe-group**: Use native `requireMention: true` in group config for project chat participants

## Bugs

- [ ] **Config editor corrupts TOML**: Serializes full schema, converting integers to floats, arrays to strings. Breaks `scanZeroClawConfig` parsing and agent discovery.
- [ ] **OpenClaw log noise**: Empty `[info]` lines appear every 30 seconds from HTTP health probe. Low priority but noisy.
- [ ] **Discovery timing**: Newly created instances show "Agent not found" for a few seconds before discovery picks them up. Added yellow provisioning state but race window still exists.

## UI

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
