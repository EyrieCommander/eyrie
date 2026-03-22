# Eyrie

A unified management interface for the Claw family of AI agent frameworks.

Eyrie gives you a single CLI, interactive TUI, and web dashboard to discover, monitor, and control all your Claw agents — OpenClaw, ZeroClaw, PicoClaw, NanoClaw, IronClaw, and others — regardless of which framework they run on.

## Features

- **Auto-discovery** of running Claw instances on localhost
- **Unified status view**: health, RAM, uptime, provider, connected channels
- **Start, stop, restart** any agent from one place
- **Log tailing** per agent in real time
- **Live activity stream**: see agent sessions, tool calls, LLM requests as they happen
- **Conversation history**: browse sessions and chat messages (OpenClaw)
- **Interactive TUI** with tabbed detail pane — Status, Logs, Activity, History (Bubble Tea)
- **Web dashboard** for visual monitoring (served from the same binary)
- **Extensible adapter system** — adding new Claw frameworks requires only a new adapter

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
eyrie status openclaw

# Tail logs from an agent
eyrie logs zeroclaw

# Watch agent activity (tool calls, LLM requests, sessions)
eyrie activity zeroclaw

# View conversation history (OpenClaw)
eyrie history openclaw

# Launch the interactive TUI
eyrie tui

# Start the web dashboard
eyrie dashboard
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `eyrie status` | Show all discovered agents and their health |
| `eyrie status <name>` | Detailed status for one agent |
| `eyrie start <name>` | Start an agent (delegates to framework CLI) |
| `eyrie stop <name>` | Stop an agent |
| `eyrie restart <name>` | Restart an agent |
| `eyrie logs <name>` | Tail logs in terminal |
| `eyrie activity <name>` | Stream activity events (tool calls, LLM requests) |
| `eyrie history <name>` | View conversation sessions and chat history |
| `eyrie config <name>` | View agent configuration |
| `eyrie discover` | Run discovery and show results |
| `eyrie tui` | Launch interactive terminal UI |
| `eyrie dashboard` | Start web dashboard |
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

## TUI Mode

Run `eyrie tui` to launch a full-screen interactive terminal interface. The TUI shows a split-pane layout with an agent list on the left and a tabbed detail pane on the right.

**Detail pane tabs:**

| Tab | Content |
|-----|---------|
| `1` Status | Health, framework, port, memory, uptime, provider, model, channels |
| `2` Logs | Live-streaming log entries with level coloring and scroll |
| `3` Activity | Real-time activity events: agent sessions, tool calls, LLM requests |
| `4` History | Conversation sessions and chat messages (OpenClaw only) |

**Keyboard shortcuts:**

| Key | Action |
|-----|--------|
| `1`-`4` | Switch detail tab |
| `j` / `k` or arrows | Navigate agent list, scroll logs/activity, or browse sessions |
| `Enter` | In History tab: view messages for selected session |
| `Esc` | In History tab: back to session list |
| `r` | Force refresh agent status |
| `s` | Stop selected agent |
| `R` | Restart selected agent |
| `q` | Quit |

## Architecture

Eyrie uses an adapter pattern: each Claw framework gets a dedicated adapter that translates the common `Agent` interface into framework-specific gateway calls. ZeroClaw speaks HTTP REST; OpenClaw speaks WebSocket RPC. Eyrie handles both transparently.

Three presentation layers share the same adapter and discovery core:

- **CLI** (`eyrie status`, `eyrie logs`, `eyrie activity`, `eyrie history`) — one-shot commands with streaming or tabular output
- **TUI** (`eyrie tui`) — interactive Bubble Tea terminal UI with tabbed detail pane (Status, Logs, Activity, History)
- **Web dashboard** (`eyrie dashboard`) — React SPA served from the embedded binary

### Framework capabilities

| Feature | ZeroClaw | OpenClaw |
|---------|----------|----------|
| Log streaming | SSE `/api/events` | WebSocket `logs.tail` |
| Activity events | SSE (agent_start, tool_call, llm_request, etc.) | WebSocket `agent`/`chat` events |
| Session list | Not supported | WebSocket `sessions.list` |
| Chat history | Not supported | WebSocket `chat.history` |

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions, coding conventions, and the PR process.

If you're not sure where to start, check the [open issues](https://github.com/natalie/eyrie/issues) for anything tagged `good first issue` or `help wanted`.

## License

MIT
