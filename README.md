# Eyrie
<img width="1318" height="766" alt="Screen Shot 2026-03-29 at 2 19 23 PM" src="https://github.com/user-attachments/assets/abb67d06-20c7-4c55-b41b-3aee3a23c456" />

An agentic factory and control room for the Claw family of AI agent frameworks.

Eyrie orchestrates teams of AI agents into project hierarchies — commanders create projects, captains manage execution, talons specialize — while giving you a real-time dashboard to see everything happening and intervene at any level. Works with ZeroClaw, OpenClaw, Hermes, PicoClaw, and others to come.

> **v0.2.0** — This is an early alpha release intended for local development and experimentation. It binds to localhost only and has no authentication. Known security limitations:
> - No request body size limits (large payloads can consume memory)
> - No authentication or authorization (anyone on localhost can access the API)
> - Agent-generated HTML previews are sandboxed but not sanitized
>
> Do not expose Eyrie to untrusted networks. See [TODO.md](TODO.md) for the full list of known issues.

## Features

- **Framework installation**: install new agent frameworks from the dashboard or CLI
- **Agent provisioning**: create new agent instances with custom personas and configuration
- **Lifecycle management**: start, stop, restart any agent from one place
- **Session management**: browse, rename, reset, and delete conversation sessions
- **Chat**: talk to any agent with streaming responses and live tool call visibility
- **Project workspace**: split view with agent roster, hierarchy diagram, and @mention chat
- **Agent hierarchy**: three-tier structure (commander → captain → talons) for organizing agents into project teams
- **Dual control**: agents and users can both create projects, assign agents, and manage lifecycle — same API, same result
- **Real-time visibility**: SSE event streaming so the dashboard updates live whether changes come from the user or an agent
- **Reliable connections** — SSE streaming per-request instead of persistent WebSockets. Survives sleep/wake, network drops, and browser tab restores without losing messages or state. Agent responses are persisted incrementally so nothing is lost even if the connection drops mid-stream.
- **Extensible adapter system** — adding new Claw frameworks requires only a new adapter

### Project Orchestration

- **Three-tier hierarchy**: Commander (strategy) → Captain (execution) → Talons (specialists)
- **Project chat**: multi-agent group conversations with @mention routing and automatic agent-to-agent handoff
- **Mission control**: dashboard with metrics, swim-lane timeline, and commander bar
- **Project workspace**: split view with agent roster, hierarchy diagram, and live chat
- **Dual control**: anything an agent can do, the user can also do via UI, and vice versa
- **Persona catalog**: browse and install agent personalities from a curated registry

### In development

- **Activity timeline**: per-project event feeds with tool calls, decisions, and progress tracking
- **Agent profiles**: inspect identity, soul, and memory for any persistent agent

<img width="1324" height="759" alt="Screen Shot 2026-03-29 at 2 19 34 PM" src="https://github.com/user-attachments/assets/ad151ea2-0195-472f-be07-bf694f0413a5" />

## Prerequisites

- **Go 1.21+** — [install instructions](https://go.dev/doc/install) or `brew install go`
- **Node.js 22+** — required for the web UI (`nvm install 22` or [nodejs.org](https://nodejs.org))
- **tmux** — persistent terminal sessions (`brew install tmux` or `apt install tmux`)

Depending on which frameworks you install, you may also need:
- **Rust** — for ZeroClaw (`curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`)
- **Python 3.11+** — for Hermes (`brew install python` or [python.org](https://python.org))

## Install

```bash
git clone https://github.com/Audacity88/eyrie.git
cd eyrie
cd web && npm install && cd ..   # install frontend dependencies
make build                       # builds React frontend + Go binary
make install                     # adds to PATH; installs to ~/.local/bin/
```

## Quick Start

Using web dashboard (recommended):

```bash
# Start the web dashboard
eyrie dashboard
```

Using terminal:

```bash
# See all discovered agents and their status
eyrie status

# Get detailed info on a specific agent
eyrie status zeroclaw

# Tail logs from an agent
eyrie logs zeroclaw

# Install a new framework
eyrie install hermes
```

## Additional CLI Commands

| Command | Description |
|---------|-------------|
| `eyrie status` | Show all discovered agents and their health |
| `eyrie status <name>` | Detailed status for one agent |
| `eyrie start <name>` | Start an agent |
| `eyrie stop <name>` | Stop an agent |
| `eyrie restart <name>` | Restart an agent |
| `eyrie logs <name>` | Tail logs in terminal |
| `eyrie activity <name>` | Stream activity events (tool calls, LLM requests) |
| `eyrie history <name>` | View conversation sessions and chat history |
| `eyrie config <name>` | View agent configuration |
| `eyrie discover` | Run discovery and show results |
| `eyrie dashboard` | Start web dashboard |
| `eyrie install` | List or install available frameworks |
| `eyrie version` | Version info |

## Configuration

Eyrie's config lives at `~/.eyrie/config.toml`. It's optional — Eyrie works out of the box by auto-discovering agents from their standard config file locations.

```toml
[dashboard]
port = 7200
host = "127.0.0.1"

[discovery]
interval_seconds = 30

# Manually register remote agents
[[agents]]
name = "remote-zeroclaw"
framework = "zeroclaw"
url = "http://192.168.1.50:42617"
```

<img width="1323" height="749" alt="Screen Shot 2026-03-29 at 2 18 31 PM" src="https://github.com/user-attachments/assets/cdeab567-150e-48d4-b6dd-add402953a59" />

## Architecture

Eyrie uses an adapter pattern: each Claw framework gets a dedicated adapter that translates the common `Agent` interface into framework-specific gateway calls. ZeroClaw speaks HTTP REST; OpenClaw speaks WebSocket RPC; PicoClaw uses a hybrid of REST and the Pico Protocol WebSocket. Eyrie handles all transparently.

Two presentation layers share the same adapter and discovery core:

- **CLI** (`eyrie status`, `eyrie logs`, etc.) — one-shot commands with streaming or tabular output
- **Web dashboard** (`eyrie dashboard`) — React SPA served from the embedded binary

### Framework capabilities

| Feature | ZeroClaw | OpenClaw | PicoClaw |
|---------|----------|----------|----------|
| Log streaming | SSE `/api/events` | WebSocket `logs.tail` | API polling `/api/gateway/logs` |
| Chat | WebSocket gateway | WebSocket RPC | Pico Protocol WebSocket |
| Session management | SQLite + gateway API | WebSocket `sessions.list` | REST `/api/sessions` + JSONL |
| Tool call streaming | via claude-max-api-proxy SSE | WebSocket events | Not exposed (server-side) |
| Lifecycle (start/stop) | `zeroclaw daemon` | `openclaw` CLI | REST `/api/gateway/{start,stop}` |
| Config format | TOML | JSON | JSON |

## Framework Installation

Eyrie can install new agent frameworks from the CLI or web dashboard.

```bash
eyrie install                         # List available frameworks
eyrie install hermes                  # Install Hermes agent
eyrie install hermes --from zeroclaw  # Install and copy config from existing agent
```

Installation proceeds through five phases:

1. **Binary** (25%) — Download/build via cargo, npm, or install script
2. **Config** (50%) — Scaffold default configuration or copy from an existing agent
3. **Discovery** (75%) — Wire config path into Eyrie's discovery system
4. **Adapter** (90%) — Set up the communication adapter (HTTP/WebSocket/CLI)
5. **Complete** (100%) — Framework ready to use

The web dashboard shows real-time progress via SSE streaming. Installed frameworks show a purple "already installed" badge; available ones show a white install button.

The framework registry (`registry.json`) defines available frameworks with their install method, config format, default ports, and binary paths. `make install` copies it to `~/.eyrie/registry.json`. For production, host the registry at a stable URL; Eyrie caches it locally at `~/.eyrie/cache/registry.json` (24h TTL).

## Development

```bash
# Full-stack development (Go + React hot reload)
make dev

# Backend only
make dev-go

# Frontend only
make dev-web

# Production build
make build

# Install to ~/.local/bin
make install
```

### Testing the commander from the terminal

The commander is the built-in LLM-driven orchestrator you chat with to manage projects and agents. It exposes a streaming SSE endpoint at `POST /api/commander/chat`. For quick terminal testing without the UI, use the `commander-test` CLI:

```bash
# Install to a directory on your PATH
go build -o ~/.local/bin/commander-test ./cmd/commander-test

# Send a prompt and stream the reply
commander-test "what projects do I have?"

# Print the saved conversation
commander-test -history

# Start a fresh conversation
commander-test -clear
```

Requires an `openrouter` key in the Eyrie vault (`~/.eyrie/keys.json`) or the `OPENROUTER_API_KEY` environment variable. The default model is `anthropic/claude-sonnet-4.6`.

<img width="1334" height="772" alt="Screen Shot 2026-03-29 at 2 19 12 PM" src="https://github.com/user-attachments/assets/0cde0fdf-a030-434f-828a-0fa9dd03fa9a" />

## Troubleshooting

### Command not found
- **ZeroClaw**: Ensure `~/.cargo/bin` is in PATH (cargo sets this up automatically)
- **OpenClaw**: Check `/usr/local/bin` is in PATH (standard on macOS)
- **OpenClaw dyld errors**: Switch to Node.js v22 for compatibility on older macOS (`nvm use 22`)
- **Eyrie**: Run `make install` to copy the binary to `~/.local/bin/`, which must be in PATH

### Port conflicts
All services use different ports and can run simultaneously:
- ZeroClaw gateway: 42617
- OpenClaw gateway: 18789
- Eyrie dashboard: 7200
- Provisioned instances: 43000-43999

### Config issues
- ZeroClaw: `~/.zeroclaw/config.toml` (TOML syntax)
- OpenClaw: `~/.openclaw/openclaw.json` (JSON syntax)
- Eyrie: `~/.eyrie/config.toml` (optional — auto-discovery works without it)

## Uninstall

```bash
make uninstall          # remove the binary from ~/.local/bin/
rm -rf ~/.eyrie         # optional: remove config and data
```

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions, coding conventions, and the PR process.

If you're not sure where to start, check the [open issues](https://github.com/Audacity88/eyrie/issues) for anything tagged `good first issue` or `help wanted`.

## License

MIT
