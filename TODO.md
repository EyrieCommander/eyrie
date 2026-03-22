# Eyrie TODO

## Security

- [ ] **Agent-to-Eyrie API access**: Currently requires adding `allowedHostnames: ["127.0.0.1"]` to OpenClaw's SSRF policy. This is a manual config change that weakens OpenClaw's security boundary. For production, explore:
  - Eyrie as an MCP server (agents connect via MCP protocol instead of HTTP)
  - Tailscale-based access (Eyrie binds to Tailscale IP, avoids private IP issue)
  - Agent-specific API tokens with scoped permissions
  - mTLS between agents and Eyrie
  - A dedicated Eyrie CLI tool that agents invoke instead of web_fetch
- [ ] **Auto-pairing for provisioned instances**: Currently provisioned ZeroClaw instances disable pairing (`require_pairing = false`) for simplicity. For production, Eyrie should auto-pair: start the daemon, capture the pairing code from stdout, call `POST /pair`, and save the auth token. This keeps the security model intact while automating the handshake.
- [ ] **Stale daemon cleanup**: `runDetached` spawns background processes but doesn't kill existing ones on the same port. This can lead to dozens of stale daemons accumulating. Before starting a new daemon, check for and kill any existing process on the target port.

## Functionality

- [ ] **Commander briefing session**: When the agent can't fetch localhost, the briefing includes all data inline. Need a cleaner solution for frameworks that block private network access.
- [ ] **Instance provisioning**: Test creating actual agent instances via the API — verify config generation, port allocation, and process startup work end-to-end for each framework.
- [ ] **Captain flow**: After Commander creates a project and Captain, the Captain should receive its own briefing with project context.
- [ ] **Talon management**: Captains need a way to communicate tasks to their Talons and track progress.
- [ ] **Cross-agent messaging**: Agents in the same project should be able to send messages to each other through Eyrie.

## UI

- [ ] **Hierarchy page**: Show agent status (running/stopped) with live refresh.
- [ ] **Project detail**: Add activity timeline showing what each agent is doing.
- [ ] **Persona catalog**: Expand with more curated personas and allow community sharing ("Claude Mart" concept).
- [ ] **Session management**: The session group delete button needs testing across all frameworks (currently only OpenClaw supports DestroySession).

## Architecture

- [ ] **PicoClaw support**: Add as a fourth framework option — even lighter than ZeroClaw for simple Talon roles.
- [ ] **Project templates**: Pre-built team compositions (e.g., "SaaS Launch" = Captain + dev Talon + marketing Talon + research Talon).
- [ ] **Agent-to-agent protocol**: Define how agents in a project coordinate (shared context, task handoffs, status updates).
