# Contributing to Eyrie

Thanks for your interest in contributing! Eyrie is a project orchestrator for AI agent teams, and all contributions are welcome — bug fixes, features, new adapters, docs improvements, or just tidying things up.

## Getting Started

### Prerequisites

- Go 1.26+
- Node.js >= 22 (for the web dashboard)
- npm >= 9
- At least one Claw framework installed ([ZeroClaw](https://github.com/zeroclaw-labs/zeroclaw), [OpenClaw](https://github.com/nicholasbester/openclaw), [PicoClaw](https://github.com/sipeed/picoclaw), or Hermes)

### Local Development

```bash
git clone https://github.com/natalie/eyrie.git
cd eyrie
npm install --prefix web
make dev
```

This starts both the Go backend (with hot-reload via air) and the React frontend (with Vite HMR). Access the dashboard at http://localhost:5173.

### Available Commands

| Command | Description |
|---|---|
| `make dev` | Full-stack dev mode (Go + React with hot-reload) |
| `make dev-go` | Backend only (Go with air, serves on port 7200) |
| `make dev-web` | Frontend only (Vite on port 5173, proxies to 7200) |
| `make build` | Production build (embeds React SPA into Go binary) |
| `make install` | Install binary to ~/.local/bin/eyrie |
| `go build ./...` | Build Go without embedding web assets |
| `go vet ./...` | Run Go linter |
| `cd web && npx tsc --noEmit` | TypeScript type checking |
| `cd web && npx vite build` | Production build of React frontend |

## Making Changes

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Run `go build ./...` and `go vet ./...` to make sure Go compiles clean
4. Run `cd web && npx tsc --noEmit` to check TypeScript types
5. Commit using [Conventional Commits](https://www.conventionalcommits.org/) format:
   - `feat:` — new feature
   - `fix:` — bug fix
   - `docs:` — documentation only
   - `refactor:` — code change that neither fixes a bug nor adds a feature
   - `test:` — adding or updating tests
   - `chore:` — maintenance tasks
6. Push to your fork and open a PR against `main`

## Pull Request Process

- Keep PRs focused — one feature or fix per PR
- Include a clear description of what changed and why
- Make sure both Go and TypeScript compile without errors
- Link any related issues (e.g., "Closes #12")

## Code Style

### Go (Backend)

- Follow standard Go conventions (`gofmt`, `go vet`)
- Adapter pattern: new frameworks go in `internal/adapter/`
- File-based storage: JSON files with mutex-protected access (see `internal/persona/store.go` for the pattern)
- SSE streaming: use `startSSE()` from `internal/server/stream.go`
- Atomic config writes: use helpers from `internal/config/write.go`

### React (Frontend)

- TypeScript throughout — no `any` unless unavoidable
- React functional components with hooks
- Tailwind CSS for styling
- Lowercase UI text (buttons, labels, messages) — except brand names
- Purple/green color theme

## Project Structure

```
cmd/eyrie/          # CLI entry point
internal/
  adapter/          # Framework adapters (ZeroClaw HTTP, OpenClaw WebSocket, PicoClaw hybrid, Hermes CLI)
  cli/              # Cobra commands
  config/           # Config loading, token store, atomic writes
  discovery/        # Auto-discovery of running agents
  instance/         # Agent instance provisioning
  manager/          # Lifecycle management (start/stop/restart)
  persona/          # Persona catalog and store
  project/          # Project management
  registry/         # Framework registry client
  server/           # HTTP handlers, SSE streaming, web dashboard
  tui/              # Bubble Tea interactive terminal UI
web/
  src/
    components/     # React components (AgentDetail, HierarchyPage, Sidebar, etc.)
    lib/            # API client (api.ts), types (types.ts)
    pages/          # Page components
```

## Adding a New Framework Adapter

1. Create `internal/adapter/myframework.go`
2. Implement the `Agent` interface from `internal/adapter/agent.go`
3. Add a case in `internal/discovery/discovery.go` `NewAgent()` to instantiate your adapter
4. Add config scanning in `internal/discovery/config.go` if your framework has a standard config path
5. Add lifecycle commands in `internal/manager/manager.go`

## Where to Start

Check the [open issues](https://github.com/natalie/eyrie/issues) — look for anything tagged `good first issue` or `help wanted`. If you're unsure about an approach, open an issue to discuss before starting.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
