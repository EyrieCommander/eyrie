# Eyrie

<img width="1328" height="750" alt="image" src="https://github.com/user-attachments/assets/76319b5b-4b76-47ed-9a5e-2bde5f7876a2" />

An agentic factory and control room for the Claw family of AI agent frameworks.

Eyrie orchestrates teams of AI agents into project hierarchies — commanders create projects, captains manage execution, talons specialize — while giving you a real-time dashboard to see everything happening and intervene at any level. Works with ZeroClaw, OpenClaw, Hermes, and others.

## Features

- **Agent hierarchy**: three-tier structure (commander → captain → talons) for organizing agents into project teams
- **Dual control**: agents and users can both create projects, assign agents, and manage lifecycle — same API, same result
- **Project workspace**: split view with agent roster, hierarchy diagram, and @mention chat
- **Real-time visibility**: SSE event streaming so the dashboard updates live whether changes come from the user or an agent
- **Auto-discovery** of running Claw instances on localhost
- **Chat**: talk to any agent with streaming responses and live tool call visibility
- **Agent provisioning**: create new agent instances with custom personas and configuration
- **Session management**: browse, rename, reset, and delete conversation sessions
- **Framework installation**: install new agent frameworks from the dashboard or CLI
- **Lifecycle management**: start, stop, restart any agent from one place
- **Extensible adapter system** — adding new Claw frameworks requires only a new adapter

### In development

- **Mission control**: cross-project dashboard with metrics, agent health, and commander access
- **Activity timeline**: per-project event feeds with tool calls, decisions, and progress tracking
- **Persona picker**: browse and install agent personas from a registry to provision specialist talons
- **Agent profiles**: inspect identity, soul, and memory for any persistent agent

<img width="1321" height="754" alt="Screen Shot 2026-03-17 at 2 08 39 PM" src="https://github.com/user-attachments/assets/0399bff7-9efa-44c1-a1aa-b395419b3c34" />

## Install

```bash
go install github.com/Audacity88/eyrie/cmd/eyrie@latest
```

Or build from source:

```bash
git clone https://github.com/Audacity88/eyrie.git
cd eyrie
make build
```

## Quick Start

```bash
# See all discovered agents and their status
eyrie status

# Get detailed info on a specific agent
eyrie status zeroclaw

# Tail logs from an agent
eyrie logs zeroclaw

# Start the web dashboard
eyrie dashboard

# Install a new framework
eyrie install hermes
```

## CLI Commands

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

<img width="1355" height="781" alt="image" src="https://github.com/user-attachments/assets/a28909f1-6c35-46eb-aa58-4217f7de3b60" />

## Architecture

Eyrie uses an adapter pattern: each Claw framework gets a dedicated adapter that translates the common `Agent` interface into framework-specific gateway calls. ZeroClaw speaks HTTP REST; OpenClaw speaks WebSocket RPC. Eyrie handles both transparently.

Two presentation layers share the same adapter and discovery core:

- **CLI** (`eyrie status`, `eyrie logs`, etc.) — one-shot commands with streaming or tabular output
- **Web dashboard** (`eyrie dashboard`) — React SPA served from the embedded binary

### Framework capabilities

| Feature | ZeroClaw | OpenClaw |
|---------|----------|----------|
| Log streaming | SSE `/api/events` | WebSocket `logs.tail` |
| Chat | WebSocket gateway | WebSocket RPC |
| Session management | SQLite + gateway API | WebSocket `sessions.list` |
| Tool call streaming | via claude-max-api-proxy SSE | WebSocket events |
| Lifecycle (start/stop) | `zeroclaw daemon` | `openclaw` CLI |
| Config format | TOML | JSON |

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

The framework registry (`registry.example.json`) defines available frameworks with their install method, config format, default ports, and binary paths. For production, host the registry at a stable URL; Eyrie caches it locally at `~/.eyrie/cache/registry.json` (24h TTL).

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

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions, coding conventions, and the PR process.

If you're not sure where to start, check the [open issues](https://github.com/Audacity88/eyrie/issues) for anything tagged `good first issue` or `help wanted`.

## License

MIT
