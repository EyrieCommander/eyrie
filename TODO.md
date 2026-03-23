# Eyrie TODO

## Security

- [ ] **Agent-to-Eyrie API access**: Currently requires adding `allowedHostnames: ["127.0.0.1"]` to OpenClaw's SSRF policy. This is a manual config change that weakens OpenClaw's security boundary. For production, explore:
  - Eyrie as an MCP server (agents connect via MCP protocol instead of HTTP)
  - Tailscale-based access (Eyrie binds to Tailscale IP, avoids private IP issue)
  - Agent-specific API tokens with scoped permissions
  - mTLS between agents and Eyrie
  - A dedicated Eyrie CLI tool that agents invoke instead of web_fetch
- [ ] **Auto-pairing for provisioned instances**: Currently provisioned ZeroClaw instances disable pairing (`require_pairing = false`) for simplicity. For production, Eyrie should auto-pair: start the daemon, capture the pairing code from stdout, call `POST /pair`, and save the auth token. This keeps the security model intact while automating the handshake.
  - **Secure token storage**: The saved auth token must be stored securely — use restrictive file permissions (0o600) at minimum, and prefer OS keyring integration (macOS Keychain, Linux secret-service) or encrypted file storage when available. Tokens should support rotation/refresh and the storage path should be under `~/.eyrie/tokens/` (separate from instance configs). When agent-specific API token middleware is implemented, use the same secure storage for those tokens.
- [ ] **Stale daemon cleanup**: `runDetached` spawns background processes but doesn't kill existing ones on the same port. This can lead to dozens of stale daemons accumulating. Before starting a new daemon, check for and kill any existing process on the target port.

## Functionality

- [ ] **Commander briefing session**: When the agent can't fetch localhost, the briefing includes all data inline. Need a cleaner solution for frameworks that block private network access.
- [ ] **Instance provisioning**: Test creating actual agent instances via the API — verify config generation, port allocation, and process startup work end-to-end for each framework.
- [ ] **Captain flow**: After Commander creates a project and Captain, the Captain should receive its own briefing with project context.
- [ ] **Talon management**: Captains need a way to communicate tasks to their Talons and track progress.
- [ ] **Cross-agent messaging**: Agents in the same project should be able to send messages to each other through Eyrie.

## Bugs

- [ ] **Config editor corrupts TOML**: The config editor serializes the full ZeroClaw schema, converting integers to floats (`port = 43000.0`), arrays to strings (`allowed_commands = "["`), and expanding every field. This breaks `scanZeroClawConfig` parsing and agent discovery. The editor should preserve the original format or only write changed fields.

## UI

- [ ] **Hierarchy page**: Show agent status (running/stopped) with live refresh.
- [ ] **Project detail**: Add activity timeline showing what each agent is doing.
- [ ] **Persona catalog**: Expand with more curated personas and allow community sharing ("Claude Mart" concept).
- [ ] **Session management**: The session group delete button needs testing across all frameworks (currently only OpenClaw supports DestroySession).

## Integrations

- [ ] **Telegram bridge for project chat**: Mirror Eyrie project conversations into Telegram groups for mobile access. Each project maps to a Telegram group; agents respond via their existing Telegram channel support (`mention_only` mode). Eyrie remains the source of truth.
- [ ] **Discord bridge for project chat**: Same as Telegram bridge but for Discord servers/channels. Leverage ZeroClaw's existing Discord channel support with per-channel-ID sessions.
- [ ] **Slack bridge**: Optional integration for teams already using Slack for coordination.

## Architecture

- [ ] **Eyrie virtual channel**: Register Eyrie as a native channel in ZeroClaw/OpenClaw/Hermes (like Telegram or Discord). Agents would see project messages via their `Channel` trait with native group behavior (`mention_only`, per-chat sessions, `ChannelMessage` struct). Builds on top of the current WebSocket-based project chat — not a replacement, but a deeper integration.
- [ ] **PicoClaw support**: Add as a fourth framework option — even lighter than ZeroClaw for simple Talon roles.
- [ ] **Project templates**: Pre-built team compositions (e.g., "SaaS Launch" = Captain + dev Talon + marketing Talon + research Talon).
- [ ] **Agent-to-agent protocol**: Define how agents in a project coordinate (shared context, task handoffs, status updates).
