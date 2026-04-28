# Adding a New Framework to Eyrie

When adding a new framework, **every** location below needs updating. Missing any one causes runtime errors that only surface when a user tries to use that framework in a specific context (e.g., starting a provisioned instance, opening a chat, creating a captain).

Long-term goal: make the registry (`registry.json`) the single source of truth so UI dropdowns, chat commands, and lifecycle actions all derive from registry data — adding a framework becomes adding one registry entry. Until then, this checklist is the safety net.

## Registry & Data

- [ ] `registry.json` — add the framework entry (id, name, binary_path, config_path, install_cmd, start_cmd, health_url, config_schema, etc.)

## Backend (Go)

- [ ] `internal/adapter/` — create `<framework>.go` adapter implementing the Adapter interface (health, chat, sessions, streaming)
- [ ] `internal/discovery/discovery.go` — add case in `NewAgent()` factory to create the correct adapter (~line 250)
- [ ] `internal/discovery/probe.go` — add health probe strategy if the framework uses a non-standard health check (~line 25)
- [ ] `internal/manager/manager.go` — **four** switch statements:
  - `Execute()` (~line 31) — top-level lifecycle dispatcher
  - `ExecuteWithConfigEnv()` (~line 328) — lifecycle with vault env injection
  - `ExecuteWithConfig()` (~line 408) — lifecycle with config path
  - `CommandString()` (~line 470) — human-readable command for UI
- [ ] `internal/manager/manager.go` — `serviceInstalled()` (~line 208) — add case if the framework supports launchd/systemd services
- [ ] `internal/instance/provisioner.go` — config format selection (~line 90) and config generation
- [ ] `internal/instance/migrate.go` — add migration case if the framework has config fields that need migration (~line 53)
- [ ] `internal/server/terminal.go` — add case for interactive terminal binary path selection (~line 205)

## Frontend (TypeScript/React)

- [ ] `web/src/lib/types.ts` — add to `FRAMEWORK_EMOJI` map (~line 330)
- [ ] `web/src/components/FrameworkStepPanel.tsx` — add to `CHAT_COMMANDS` map (~line 253)
- [ ] `web/src/components/FrameworkDetail.tsx` — add to `CHAT_COMMANDS` map (~line 18)
- [ ] `web/src/components/FrameworkCompare.tsx` — add to `CAPABILITIES` static data (~line 34)
- [ ] `web/src/components/HierarchyPage.tsx` — add to `FRAMEWORK_PITCHES` map (~line 226)
- [ ] `web/src/components/SetCaptainDialog.tsx` — ensure framework appears in captain dropdown (~line 111)
- [ ] `web/src/components/ProjectListPage.tsx` — ensure framework appears in captain dropdown (~line 228)

## Verification

After adding all the above:

1. **Registry**: `GET /api/registry/frameworks` returns the new framework with correct `installed`/`configured` status
2. **Onboarding flow**: framework appears in the "choose" grid, install/configure/api-key/launch steps work
3. **Instance provisioning**: creating a captain or talon with this framework succeeds via both the project form and the add-agent dialog
4. **Lifecycle**: start/stop/restart work from both the project detail page and the agent detail page
5. **Framework detail page**: `/frameworks/<id>` loads correctly with chat/setup/uninstall actions
6. **Compare page**: framework appears in the comparison grid with correct capabilities
7. **Terminal**: interactive terminal sessions work for the framework

## Notes

- **"embedded" is special**: it runs in-process as goroutines, not as an external process. Many lifecycle/terminal/chat locations correctly have `case "embedded": return nil` or skip it entirely. But it should still have explicit cases rather than falling through to `default: error`.
- **Hard-coded defaults**: avoid defaulting to a specific framework (e.g., "zeroclaw"). Use the first installed framework from the registry instead.
- **Line numbers are approximate** and will drift as the codebase evolves. Search for the surrounding context (function name, switch variable) rather than relying on exact line numbers.
