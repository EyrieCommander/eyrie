# Eyrie

A unified management interface for the Claw family of AI agent frameworks.

Eyrie gives you a single CLI and web dashboard to discover, monitor, and control all your Claw agents — ZeroClaw, OpenClaw, Hermes, and others — regardless of which framework they run on.

## Features

- **Auto-discovery** of running Claw instances on localhost
- **Unified status view**: health, uptime, provider status, connected channels
- **Start, stop, restart** any agent from one place
- **Log tailing** per agent in real time
- **Chat**: talk to any agent from the dashboard with streaming responses and live tool call visibility
- **Session management**: browse, rename, reset, and delete conversation sessions
- **Streaming tool events**: see tool calls with arguments and output as they happen (supports ZeroClaw with [claude-max-api-proxy](https://github.com/nichochar/claude-max-api-proxy))
- **Framework installation**: install new agent frameworks (ZeroClaw, OpenClaw, Hermes) from the dashboard or CLI
- **Agent provisioning**: create new agent instances with custom personas and configuration
- **Provider health probing**: verify LLM provider connectivity from the status view
- **Web dashboard** for visual monitoring (served from the same binary)
- **Extensible adapter system** — adding new Claw frameworks requires only a new adapter

### In development

- **Project orchestration**: organize agents into project teams with commander/captain/talon hierarchy
- **Project chat**: multi-agent group conversations with @mention routing
- **Persona catalog**: browse and install agent personalities from a registry

## Install

```bash
go install github.com/natalie/eyrie/cmd/eyrie@latest
```

Or build from source:

```bash
git clone https://github.com/natalie/eyrie.git
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

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions, coding conventions, and the PR process.

If you're not sure where to start, check the [open issues](https://github.com/natalie/eyrie/issues) for anything tagged `good first issue` or `help wanted`.

## License

MIT
