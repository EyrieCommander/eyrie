# Unified Onboarding Flow: Eyrie as Commander

## Branch status (updated 2026-04-29)

All work now lives on **`main`**. The former `feature/onboarding-ui` and `feature/eyrie-commander` branches have been merged. The commander backend (`internal/commander/`) is on `main` with read/write tools, structured confirmation, memory store, Anthropic native provider, and context-window usage reporting. The old commander-agent system has been deleted.

This tracked plan now incorporates the still-current architecture notes from the legacy local Claude plan at `~/.claude/plans/majestic-crunching-tiger.md`. Treat that local file as archived reference; this document is the source of truth for onboarding/commander UI direction.

## Context

Three separate surfaces — HierarchyPage (mission control), FrameworkDetail, ProjectDetail — each do part of the onboarding story, and none of them show the full arc. The result is fragmented, and there's no overarching progress indicator tying the pieces together.

The pivot: **Eyrie itself becomes the commander**. The user is guided through a single scrollable flow with three macro phases:

1. **Commander setup** — the backend (`internal/commander/`) exists on `main` with read/write tools, structured confirmation, persistent conversation history, memory, Anthropic native provider support, and context-window reporting. Phase 0's UI is initially a compact "ready" confirmation card (the commander is running; nothing for the user to do). It can grow into provider/model settings, connectivity testing, memory controls, multi-session support, and autonomy policy controls without changing the macro timeline. The macro timeline treats phase 0 as auto-complete as long as the commander endpoint responds.
2. **Frameworks** — pick and install agent runtimes (choose → install → configure → api key → launch).
3. **Projects** — create projects as single-page forms, with captain/talon agents provisioned inline from the team section.

Agents are **not** a macro phase. They're still a first-class concept (the sidebar's `Agents` page remains for 1:1 chat, pool management, and cross-project reuse), but provisioning happens inline inside project creation for the common onboarding path. This collapses the linear flow from 4 phases to 3 and stops forcing users to provision agents in a vacuum without knowing what project they're for.

A persistent **right-side chat panel** lets the user chat with the commander at any step. It's **expanded by default** so the commander is visibly present. The panel supports multiple tabs (one per session — though until Phase 5a adds a multi-session backend, there is just one persistent session at `~/.eyrie/commander/chat.jsonl`). A collapse control shrinks it to a thin vertical strip (~44px) when users want more horizontal room for the main flow; clicking the strip re-expands it. The chat panel talks to `POST /api/commander/chat` for streaming and `GET /api/commander/history` for rehydration; events arrive as SSE with types `delta`, `tool_call`, `tool_result`, `message`, `done`.

The chat panel's content is **context-aware**: the greeting, the commander's opening message, and the "try asking" suggestion chips all reflect what the user is doing on the current page. Examples:
- Phase 1 choose: commander explains trade-offs; chips: "what framework should I pick?", "what's an API key?"
- Phase 2 project form: commander helps scope; chips (purple): "help me scope this project", "suggest talons for this goal", "what should the captain do?"
- Error state: commander analyzes the error immediately; chips (purple): "check my rust version", "clear cargo cache", "try a different framework". Chat header badge turns red ("noticed an error") so the context switch is obvious.

Every major choice point in the main content also surfaces an **"ask commander"** purple CTA — e.g., the "not sure which framework to pick?" box on phase 1, or the "help me resolve this" primary button on the error state. Clicking pre-fills a relevant question in the chat input.

## Architecture strategy

Eyrie is an agentic factory and control room. Agents do the project work; Eyrie provides the governance, project intelligence, cross-framework routing, and visual operating layer above them. Both agents and the user are first-class participants: anything an agent can do through the API should also be visible and overridable in the UI, and anything the user does should leave the same durable project state agents can observe.

The core architectural separation is runtime primitives versus factory judgment. Runtimes such as ZeroClaw, OpenClaw, PicoClaw, and Hermes provide primitives: agent definitions, session management, tool execution, delegation, swarm execution, isolation, and per-framework observability. Eyrie provides judgment: which agent should handle a task, when to hand off, what a project needs next, how to compose agents across frameworks, and when human approval or intervention is required.

The built-in commander validates this split. The commander understands user intent, queries project state, calls direct Go tools, sends messages into project chats, and asks for confirmation before sensitive writes. It does not need a shell, workspace sandbox, pairing flow, or provisioned gateway. Captains and talons remain external framework agents because they do real work in project workspaces.

This means the old commander-agent approach is retired. There is no separate commander ZeroClaw instance, no `commander.json` instance pointer, no commander provisioning/pairing/briefing flow, and no commander participant in project chat. Eyrie itself is the commander; project captains are the first responders inside project chat.

As runtimes gain stronger native multi-agent support, Eyrie's job shifts rather than disappears. Before, Eyrie filled gaps to make multi-agent usable at all. Over time it becomes the project, governance, analytics, and cross-framework composition layer above runtime primitives. Better runtime primitives make the factory layer more powerful, not less relevant.

## Runtime evolution and Eyrie impact

ZeroClaw RFC #5890 is the main near-term runtime change to track. If accepted and implemented, it should give ZeroClaw first-class per-agent identity, gateway CRUD for agents, live reload, `agent_name` observability, and per-agent channel bindings. For Eyrie, that means the ZeroClaw adapter can move from writing config files and restarting daemons toward HTTP lifecycle operations with richer event attribution.

The `adapter.Agent` interface remains the right boundary. RFC #5890 changes how `internal/adapter/zeroclaw.go` implements provisioning, lifecycle, and observability, but it should not force Eyrie to become ZeroClaw-specific. OpenClaw, Hermes, PicoClaw, and embedded EyrieClaw can evolve independently behind the same control-room surface.

Other adapters need their own refresh as their runtimes mature: OpenClaw already has more production multi-agent shape, Hermes has delegation concepts, and PicoClaw's swarm/sub-agent story is still emerging. Eyrie's comparison matrix and provisioning strategies should be updated from current code/docs rather than old assumptions.

The enduring factory-layer work is:
- Cross-framework teams, such as a ZeroClaw developer, Hermes researcher, and PicoClaw monitor in one project.
- Project-level abstractions, such as milestones, quality gates, blueprints, dependencies, and budgets.
- Governance and oversight, including approval workflows, audit trails, escalation policies, and human override.
- Factory analytics, including cost rollups, utilization, bottleneck detection, and project health trends.
- Visual command-center UX, including a spatial/canvas view of agents, workflows, handoffs, state, and batch operations.

## Retrospective: previous plans

The Frameworks-First redesign (reordering frameworks → agents → projects, sidebar, breadcrumbs, collapsible comparisons) is implemented and holding up. Its 3-step mission-control guide was the closest precedent to the unified flow, but only at the macro level — it had no per-framework detail.

The framework-detail redesign (this plan's earlier draft) landed as `1a1e919 feat: terminal-centric framework detail page`. It designed a 4-step timeline (install / configure / api key / launch). Those 4 steps became sub-steps 2–5 of phase 1 in the unified flow; a "choose" sub-step was added in front. The tmux output parser was deferred — the onboarding flow uses polling + filesystem state for step completion, which works well enough.

The legacy commander plan (`majestic-crunching-tiger.md`) is also folded in here. Its stale skeleton implementation details are superseded by the current `internal/commander/` code on `main`; its enduring strategy is preserved above: Eyrie itself is the commander, runtime primitives stay below the adapter boundary, and Eyrie differentiates as the factory/governance layer.

What's being retired:
- `HierarchyPage` 3-step `GuideView` sub-component → absorbed into the unified flow. The mission-control half of `HierarchyPage.tsx` (`SwimLaneTimeline`, metric cards, commander bar, agent hierarchy) stays — it's operational/monitoring, not onboarding. Rather than wholesale deletion, the implementation extracts mission-control into its own component (e.g. `MissionControl.tsx`), keeps `/mission-control` routing it, and deletes the `GuideView` + wrapper logic from `HierarchyPage.tsx` (or renames the remaining file).
- `CommanderSetup` modal dialog → retired outright. With Eyrie-as-commander (not an agent-picker), there's no commander to "set up" — the backend ships configured with OpenRouter + claude-sonnet-4.6. Phase 0's UI replaces the modal with a compact status card.

What's being **kept**:
- Dedicated pages at `/frameworks/:id`, `/agents/:name`, `/projects/:id` — bookmarkable, load-narrow, browser-history-friendly. The onboarding flow lives at `/` and is a separate surface. Deep links from inside the flow (e.g., a framework card linking to its dedicated page) continue to work. The sidebar's `Agents` and `Projects` entries still navigate to their dedicated list pages.
- `AgentsPage`, `AgentDetail`, `AgentCard` — standalone agent surfaces for pool management, 1:1 chat, and cross-project reuse. They just aren't part of the onboarding timeline.

## Layout

Three columns:

| Zone | Default width | Content |
|------|--------------|---------|
| Left sidebar | 220px | Existing Eyrie navigation — frameworks, agents, projects, personas, settings. Unchanged. |
| Main content | flex (~840px at 1440 design width when chat is expanded) | The unified flow: single-line timeline at top, then the currently-active phase's content below. |
| Right chat panel | ~380px (expanded) / ~44px (collapsed) | Commander chat. Expanded by default. Collapse toggle on its left edge shrinks it to a vertical strip. Tabs across the top for multiple sessions. |

Design target is 1440px. At that width the main area is 840px with chat expanded, which comfortably fits the single-line timeline + sub-step pills. On narrower viewports the chat panel can be manually collapsed to free up horizontal room.

## Single-line timeline (expands the active phase inline)

One horizontal timeline at the top of the main content. Completed phases stay visible in a lighter green; pending phases are grayed out. Only the **current** phase is visually "expanded" — it reveals its sub-steps inline on the same row:

**When in phase 0 (commander):**
```
commander · setup    |    frameworks    |    projects
 ●  ───────────              (○ grayed)       (○ grayed)
```

**When in phase 1 (frameworks) — sub-steps appear inline:**
```
commander ✓    |    frameworks · choose → install → configure → api key → launch    |    projects
 (✓ light green)         ●                                                                   (○ grayed)
```

**When in phase 2 (projects):**
```
commander ✓    |    frameworks ✓ · 2 installed    |    projects · describe → team → goal → launch
 (✓ light green)        (✓ light green)                     ●
```

Rules:
- Completed phases: light green, clickable → scrolls you back, still editable.
- Current phase: full color, expanded with its sub-steps shown inline on the same row.
- Pending phases: grayed out, not clickable (tooltip: "complete previous steps first").
- One row, one timeline, no nested rows.

## Phase contents

### Phase 0: Commander setup

The commander backend is available on `main`. The UI for phase 0 can stay minimal at this stage:

- A compact "ready" card: "Eyrie is your commander. Ask me anything via the chat panel →" with a green check.
- Collapsed-by-default details: which provider/model is configured (read from env / server-side config), history count (via `GET /api/commander/history`), "clear history" button (`DELETE /api/commander/history`).
- If the commander fails a health check (e.g., no `OPENROUTER_API_KEY`): dashed-yellow card: "Commander isn't configured. Set `OPENROUTER_API_KEY` and restart Eyrie." with a doc link.

The macro timeline auto-advances to phase 1 when the commander endpoint returns successfully. Phase 0 can later expand into a proper settings card — provider select, model text, connectivity test button, memory controls, context-usage indicator, multi-session selector, and autonomy policy controls — without structural changes to the macro timeline.

### Phase 1: Frameworks (5 sub-steps)

The frameworks phase is its own **5-step mini-flow** nested inline into the single timeline when phase 1 is active:

```
choose → install → configure → api key → launch
```

- **Step 1: Choose** — Framework grid (zeroclaw / openclaw / picoclaw / hermes).
- **Step 2: Install** — auto install vs install in terminal.
- **Step 3: Configure** — fill form / wizard / edit file.
- **Step 4: API key** — inline form with auto-detected provider.
- **Step 5: Launch** — start gateway, launch chat, health check.

**Phase 1 completion:** the macro timeline flips phase 1 to green as soon as **≥1 framework** is fully ready (installed + configured + has-api-key-or-skip). After that, the framework section still shows a persistent `+ framework` affordance so users can add more frameworks at any time — adding doesn't un-complete the phase.

**Sub-step completion is computed from state, not from click-through.** A returning user with an already-installed ZeroClaw, already-configured `~/.zeroclaw/config.toml`, and already-set OpenRouter key in the vault should see choose/install/configure/api-key all as `✓` and launch as the active step. The completion check for each sub-step:

| Sub-step | Completion check |
|----------|------------------|
| choose | A framework has been selected for the current session (UI state) |
| install | `framework.installed === true` (binary exists at `binary_path`) |
| configure | `framework.configured === true` (config file exists at `config_path`) |
| api key | `KeyVault.Get(provider)` is non-empty OR provider is in the local/no-key list |
| launch | Health endpoint returns 200 OR chat command is running in the tmux session |

Clicking any completed sub-step jumps to it for editing. Clicking the first incomplete sub-step is the default "continue" action. Clicking sub-steps beyond the first incomplete one is blocked (tooltip: "complete previous steps first").

The 6 existing Pencil screens (install / configure / api key / installing / ready / binary missing) cover steps 2–5. A new "choose" screen covers step 1.

### Phase 2: Projects (single-page form)

Projects are created via a **single-page form** with four labeled sections. The timeline pills `describe → team → goal → launch` are progress indicators for form sections, not separate wizard pages. Everything is visible and editable at once.

Form sections:

1. **Describe** — project name, framework preference (dropdown, defaults to the first ready framework), description.
2. **Team** — assign captain (required) and talons (optional). Each role slot has:
   - A dropdown of existing agents compatible with the chosen framework.
   - A `+ provision new` button that expands an inline sub-form (persona / name / optional workspace path). Saving adds the new agent to the team immediately and creates it in the background via `POST /api/instances`.
3. **Goal** — the shared objective / success criteria (a free-text field the commander uses when briefing agents).
4. **Launch** — `create project` button. Creates the project via `POST /api/projects`, which triggers captain briefing and kicks off the project chat.

Entry state (when the user first lands on phase 2) shows any existing projects as cards at the top, with a prominent `+ new project` button that opens the form below. After a project is created, the form collapses to a row and the new project is added to the list.

Reuses existing API endpoints (`/api/instances`, `/api/projects`) — no backend changes needed.

## Framework-specific reality check (ZeroClaw)

Looking at `registry.json` to understand what "configure" actually entails for a representative framework:

```
ZeroClaw config_schema.common_fields:
  gateway.port     (number, 1024-65535, default 42617)
  gateway.host     (text, default 127.0.0.1)
  default_provider (select: openrouter/anthropic/openai, default openrouter)
  default_model    (text, default "moonshotai/kimi-k2.5")
  workspace.path   (text)
  memory.backend   (select: sqlite/memory)
api_key_hint: "set OPENROUTER_API_KEY, ANTHROPIC_API_KEY, or OPENAI_API_KEY"
```

Observation: **provider selection lives in the config file, but the API key for that provider lives in the KeyVault (`~/.eyrie/keys.json`) or an env var.** The two are intertwined — you can't meaningfully set the key without first knowing the provider. That's why API key is its own step AFTER configure, and auto-detects the provider from the config file's current value.

For frameworks using local/gateway providers (e.g., ollama, vercel-ai-gateway), the API key step is skipped automatically.

## Badge state semantics

Header badges follow clear, single-meaning labels. No overloaded words.

| Badge | Color | Meaning |
|-------|-------|---------|
| _none_ | — | Fresh framework, nothing started. Timeline alone conveys the state. |
| `installing` | blue | An install operation is currently running (tmux output being parsed). |
| `configuring` | blue | The onboard wizard / config form is actively being completed. |
| `needs setup` | yellow | Binary installed but no config file yet — user action required. |
| `needs api key` | yellow | Installed + configured, but no key set for the chosen provider. |
| `binary missing` | yellow | Config file exists but binary is gone (moved, deleted, or path drift). |
| `install failed` | **red** | Most recent install attempt exited non-zero or raised an error marker. |
| `ready` | green | All steps complete; framework is fully operational. |

"installing" is reserved for active in-progress operations. Yellow states are "action needed from you, but nothing's broken". Red means something genuinely went wrong and the user should look at the error before retrying.

## Active-step panel (phase 1 sub-steps)

The 5 frameworks sub-steps appear inline in the single timeline when phase 1 is active. Below the timeline, a **single active panel** shows options for whichever sub-step the user is on:

- `(✓)` green = done (state-derived; see phase 1 completion logic above)
- `(●)` accent = currently active (the first sub-step whose completion check fails)
- `(○)` grayed = pending

Below the active panel: the persistent tmux terminal for that framework, always visible.

### Sub-step 1: Choose framework

- Grid of framework cards (zeroclaw / openclaw / picoclaw / hermes) — same FrameworkCard component, each showing its current status badge.
- Clicking a card sets it as the active framework and advances to sub-step 2 (or directly to whichever is the first incomplete sub-step for that framework).
- Already-ready frameworks are clickable too — clicking jumps straight to the "ready" state panel for that framework.

### Sub-step 2: Install binary

Two equal-weight options:

- **Auto install with defaults** → runs `eyrie install <id> -y` in the tmux terminal. Runs all 4 CLI phases (binary + config + discovery + adapter) with sensible defaults.
- **Install in terminal** → pastes `cargo install zeroclaw` (or the framework's `install_cmd`) into the tmux shell, user hits Enter.

Inline list of requirements (e.g., `rust>=1.70`) with a warning icon if any are missing from `$PATH`.

### Sub-step 3: Configure

Three equal-weight paths to the same outcome:
- **Fill form here** → renders `config_schema.common_fields` verbatim as a form, using the existing `ConfigForm.tsx`. `common_fields` is already the curated onboarding subset — no second filter needed. Writes config file directly on save.
- **Run wizard in terminal** → pastes `<binary_path> onboard` into tmux. Framework's own wizard handles interactive prompts.
- **Edit config file** → shows the config path to copy, or prints `$EDITOR <path>` hint into the terminal.

Form header says "Default ZeroClaw agent config" with `~/.zeroclaw/config.toml` shown underneath — makes the "framework page editing an agent endpoint" relationship explicit.

### Sub-step 4: API key

- **Detected provider**: read the current value of the field matching `provider`, `default_provider`, or `agents.defaults.provider` from the config, display "For OpenRouter (from your config):".
- **Inline form**: password input (Eye icon toggle) + save button. Writes via existing `PUT /api/keys/{provider}`.
- **Alternatives**: "I set it as an env var" (marks complete if env var detected), "Open Settings page" (links to `/settings`), "Copy from another framework" (if another framework has the same provider's key).
- **Skip if local provider**: if the chosen provider is in a known "no-key" list (ollama, vercel-ai-gateway), show "No key needed for [provider]" with a green check and auto-advance.

### Sub-step 5: Launch

- **Start gateway** → runs `framework.start_cmd` in tmux. For frameworks where `start_cmd` is empty (Hermes), skip directly to chat.
- **Launch chat** → runs the chat command.
- **Health check** → live status from `framework.health_url` if present.

Once complete, the framework's section shifts to a "ready" state: the only prominent actions are "launch chat" and "configure" (with uninstall tucked into a subtle corner).

### Error state (red)

When an install attempt fails (exit code ≠ 0, or tmux parser catches an `error:` / `ERROR` marker):

- Header badge: `install failed` (red, with `⚠` icon).
- The current sub-step's circle turns red with `!`, and the connector into it turns red.
- Above the step panel: a red banner showing the last error line captured from the terminal.
- Inside the step panel (three CTAs):
  - **Help me resolve this** (purple primary) — pre-fills a question in the commander chat and focuses the chat input.
  - **Retry** (red border) — reruns the same command in the terminal.
  - **Try a different method** (border) — switches the user to the other install option.
- Terminal keeps the full failure log visible — user can scroll up, copy the error, paste it elsewhere.

The red state is sticky until the next successful operation clears it. Filesystem checks don't override the error badge — seeing "the binary exists now" isn't proof the user resolved the underlying problem.

## Real-time progress via tmux output parsing

Filesystem polling is kept as a safety net but is no longer the primary signal. Instead:

### Approach

The Terminal component already pipes all PTY bytes from the server WebSocket into xterm. Extend it to **also** run a parser on the incoming bytes, watching for line-delimited patterns. When a pattern matches, it fires a callback up to FrameworksPhase to refresh state (call `getFrameworkDetail(id)` and `fetchKeys()` immediately).

Patterns to detect (per step):

| Step | Pattern (regex on stripped lines) | Trigger |
|------|----------------------------------|---------|
| Install | `/Installed (\S+) (?:to |at )/` (script), `` /`cargo install` .* Installed\b/ ``, `/changed \d+ packages/` (npm) | refetch framework detail |
| Install | `/Phase \d\/\d: (\w+)/` (eyrie CLI phase markers) | update current phase indicator |
| Install | `/✓ Binary installed/` (eyrie CLI) | mark install done immediately |
| Configure | `/✓ Configuration ready/`, `/saving config to /`, `/.* is ready!/` | refetch framework detail |
| API key | (no terminal output — use KeyVault poll or form submission) | direct refetch |
| Launch | `/Gateway started on/`, `/Listening on :\d+/` | mark launch done |

### Implementation

Add an optional `onOutput?: (line: string) => void` prop to `Terminal`. FrameworksPhase passes a handler that:
1. Matches lines against the pattern map for the CURRENT sub-step (not all sub-steps — reduces noise and avoids false positives from stale output).
2. When a match fires, calls `loadFramework()` and `fetchKeys()` immediately.
3. Extracts the phase name from `Phase N/4: X` markers and shows it above the progress bar (e.g., "installing… binary phase").

Tmux output is a byte stream, not line-buffered — buffer incomplete lines in the parser until a `\n` arrives, and strip ANSI escape sequences before matching. A small `lineParser(chunk)` utility in `web/src/lib/terminalParser.ts` handles this.

## Relationship to existing ConfigPage

ConfigPage lives at `/agents/:name/config` and is **per-agent**. Each agent (including multiple provisioned instances of the same framework) has its own config file path.

**Decision: coexist, don't replace — but label clearly.**

| Surface | Scope | Purpose |
|---------|-------|---------|
| Phase 1 sub-step 3 form | Framework's *default* agent config file | Onboarding — guided, opinionated, uses `common_fields`. |
| ConfigPage / AgentDetail config tab | Specific agent/instance | Ongoing editing — full schema, raw mode, format validation, masked-secret handling, per-instance overrides. |

Both read/write the same `/api/agents/{name}/config` endpoint. The phase 1 form is always scoped to the framework's *default* single agent (e.g., `zeroclaw` → `~/.zeroclaw/config.toml`). Both share the same `ConfigForm` component.

### Follow-up: framework-level vs agent-level config (deferred)

Some config fields are logically *framework-level* (e.g., default provider, default model, binary path) while others are *agent-level* / *instance-level*. Right now `common_fields` mixes both. Coordination questions (cascade? per-instance overrides? where does inheritance live?) are tracked as a follow-up — not resolved in this plan.

## Inline API key form

Extract the `ApiKeysSection` provider form from `SettingsPage.tsx` into a reusable `ApiKeyForm` component:

**File**: `web/src/components/ApiKeyForm.tsx` (new)
- Props: `provider: string`, `onSaved?: () => void`
- Renders: password input + Eye toggle + save button + validation feedback
- Uses existing `setKey(provider, key)` and `validateKey()` API functions

SettingsPage updates to use `ApiKeyForm` for each provider slot, so behavior is identical there. Phase 1 sub-step 4 embeds `ApiKeyForm` inside the active panel.

## Files to modify

### Created (steps 1–3, done)
- `web/src/components/OnboardingFlow.tsx` — top-level unified flow; macro timeline + current phase. Home route.
- `web/src/components/MacroTimeline.tsx` — 3-phase outer timeline.
- `web/src/components/phases/CommanderPhase.tsx` — placeholder for phase 0.
- `web/src/components/phases/FrameworksPhase.tsx` — phase 1: framework grid + 5-step timeline + step panel + terminal.
- `web/src/components/phases/ProjectsPhase.tsx` — phase 2: project list + creation form.
- `web/src/components/FrameworkProgressTimeline.tsx` — inner 5-step timeline with state-derived status.
- `web/src/components/FrameworkStepPanel.tsx` — active sub-step content panel.
- `web/src/components/ConfigFieldsForm.tsx` — renders forms from `config_schema.common_fields`.
- `web/src/components/ApiKeyForm.tsx` — extracted reusable API key form.
- `web/src/lib/onboardingStorage.ts` — localStorage persistence for onboarding state.
- `internal/cli/reset.go` — `eyrie reset <id>` command (remove config, keep binary).

### Still to create (steps 4–5)
- `web/src/components/CommanderChat.tsx` — right-side chat panel with expand/collapse + session tabs. SSE streaming from `/api/commander/chat`, rehydration from `/api/commander/history`. Tool-call events as collapsible inline cards. Confirmation cards for write tools.
- `web/src/components/MissionControl.tsx` — extracted from HierarchyPage (SwimLaneTimeline, metric cards, agent hierarchy).

### Modified (steps 1–3, done)
- `web/src/App.tsx` — `/` renders `OnboardingFlow`. Dedicated pages unchanged.
- `web/src/lib/frameworkStatus.ts` — added `hasApiKey`, `needsApiKey`, `apiKeyProvider`, `skipApiKey`. `isReady` redefined.
- `web/src/lib/api.ts` — added `fetchFrameworkConfig`, `patchFrameworkConfig`, `putRawFrameworkConfig`.
- `web/src/components/FrameworkCard.tsx` — added `onSelect` prop, consumes extended status.
- `web/src/components/SettingsPage.tsx` — refactored to use `ApiKeyForm`.
- `web/src/components/FrameworkDetail.tsx` — added "reset config" button, version badge, update button.

### Still to modify (steps 4–5)
- `web/src/App.tsx` — add CommanderChat at `<main>` level for 3-column layout.
- `web/src/components/OnboardingFlow.tsx` — pass phase context to CommanderChat for greeting/chips.
- `web/src/components/phases/CommanderPhase.tsx` — live health check, memory count, history count.
- `web/src/components/phases/FrameworksPhase.tsx` — error state panel, "ask commander" CTAs.
- `web/src/lib/api.ts` — commander API helpers (stream, history, clear, memory, confirm).
- `web/src/lib/types.ts` — commander event types, MemoryEntry.

### Retired
- `web/src/components/CommanderSetup.tsx` — already deleted (commander merge).
- `web/src/components/HierarchyPage.tsx` — to be replaced by `MissionControl.tsx` (step 5).

### Not modified (by design)
- Server-side install/uninstall: `eyrie install -y` remains the automatic install path.
- ConfigPage / AgentDetail config tab: kept for per-agent power editing.
- `AgentsPage`, `AgentDetail`, `AgentCard`: agents remain a first-class sidebar surface.
- `ProjectListPage`, `ProjectDetail`: post-onboarding project management surfaces.

## Reuse

| Need | Existing component | Location |
|------|-------------------|----------|
| Step number/checkmark circles | Inline in HierarchyPage `GuideView` | `web/src/components/HierarchyPage.tsx:264-266,324-325,347-348` |
| Status badges (green/yellow/red) | `FrameworkCard` badge rendering | `web/src/components/FrameworkCard.tsx:37-41` |
| Config form | `ConfigForm` | `web/src/components/ConfigForm.tsx` |
| API key password input + validate | `ApiKeysSection` | `web/src/components/SettingsPage.tsx` |
| Info box pattern | `border-yellow/30 bg-yellow/5` | `FrameworkDetail.tsx:158-168` |
| KeyVault API client | `fetchKeys`, `setKey`, `validateKey` | `web/src/lib/api.ts` |
| Framework status derivation | `getFrameworkStatus` | `web/src/lib/frameworkStatus.ts` |
| SSE stream parsing patterns | `web/src/lib/chat-events.ts`, `ProjectChat.tsx` streaming handlers | Reference for how to shape the commander SSE client. Not directly reusable — commander SSE events (`delta` / `tool_call` / `tool_result` / `message` / `done`) differ from project-chat events. |
| Chat message rendering (markdown, tool-call cards) | `ChatPanel` sub-components (`PartToolCallCard`, `StreamingCursor`, `chat/` folder) | Reusable for rendering commander turns — the in-message tool-call UI pattern is the same. |
| Project creation form pieces | `ProjectListPage` creation form | `web/src/components/ProjectListPage.tsx` |
| Instance provisioning API | `POST /api/instances` | `web/src/lib/api.ts` |

## Implementation order (UI-first)

Each step ships something usable; supporting refactors land in-place with the phase that needs them rather than as standalone pre-work.

1. ~~**Shell + navigation scaffolding.**~~ **DONE** — `OnboardingFlow.tsx`, `MacroTimeline.tsx`, route wiring. Home route renders `OnboardingFlow`.

2. ~~**Phase 1 (`FrameworksPhase`)**~~ **DONE** — All 5 sub-steps implemented:
   - `FrameworkProgressTimeline.tsx` (inner 5-step timeline) with state-derived sub-step status
   - `FrameworkStepPanel.tsx` — choose (framework grid), install (auto/manual), configure (quick setup form / wizard / raw editor), api key (inline form with auto-detected provider), launch (start/health/chat)
   - `ConfigFieldsForm.tsx` — renders forms from `config_schema.common_fields` with provider-keyed model suggestions, grouped layout, advanced field toggle
   - `ApiKeyForm.tsx` extracted from SettingsPage
   - `frameworkStatus.ts` extended with `hasApiKey` / `skipApiKey` / `apiKeyProvider`; `isReady` redefined
   - Phase-1 completion logic: macro timeline flips green when ≥1 framework is ready
   - **Not implemented from original plan:** `terminalParser.ts` / `Terminal.onOutput` for real-time progress detection. Currently uses polling + filesystem state. Deferred — polling works well enough and avoids ANSI parsing complexity.
   - **Additional work not in original plan:** URL-driven step navigation (`?phase=frameworks&fw=picoclaw&step=configure`), localStorage persistence via `onboardingStorage.ts`, health check proxy through backend to avoid CORS, `eyrie reset` CLI command for config-only removal.

3. ~~**Phase 2 (`ProjectsPhase`)**~~ **DONE** — Single-page project creation form. Default project/captain names. Backend-down detection. Framework dropdown from installed frameworks.
   - **Not yet implemented:** inline agent provisioning (`+ provision new` sub-form per role), talon assignment in team section. Currently uses existing captain dropdown only.

4. **Commander chat panel (`CommanderChat.tsx`).** ← **NEXT** — Commander backend is already on `main` (no merge needed). Persistent right-side panel with expand/collapse + session tabs. Context-aware greeting and suggestion chips per phase. Purple `$accent-purple` for "ask commander" CTAs throughout the main content. Chat streams from `/api/commander/chat` (SSE: delta/tool_call/tool_result/message/done), rehydrates from `/api/commander/history` on mount. Tool-call events render as collapsible inline cards. If the commander endpoint returns 5xx, show a red banner with "commander unavailable — check `OPENROUTER_API_KEY`" instead of silently failing.

5. **Error state + polish + cleanup.**
   - Red `install failed` banner in step panel with "help me resolve this" (purple primary), retry (red border), try-a-different-method.
   - Deep-link scroll behavior: visiting `/` with a query param (e.g., `/?phase=frameworks&fw=zeroclaw`) scrolls and expands that framework. **Partially done** — URL params work for phase/fw/step but no scroll-into-view behavior.
   - Extract `MissionControl.tsx` from `HierarchyPage.tsx` (keep the `SwimLaneTimeline`, metric cards, commander bar, agent hierarchy subpage; drop `GuideView` and the wrapper logic). Route `/mission-control` → `MissionControl`. `CommanderSetup.tsx` already deleted by the commander merge.

6. **Follow-ups (deferred, not in this plan).**
   - Framework-level vs agent-level config cascade.
   - Knowledge base for the commander.
   - **Personas for the commander.** Personas (`internal/persona/`, `PersonasPage.tsx`, `PersonaCard.tsx`) already exist as a rich schema (system prompt, preferred model, temperature, reasoning level, tools filter, identity templates, traits) but aren't wired to anything. They'd be a natural fit for commander customization rather than captain/talon provisioning — pick a persona for your Eyrie and its voice / default model / capabilities change. Defers to a follow-up because we want to ship the minimum phase 0 first and let usage clarify what knobs actually matter, and because commander currently uses a hardcoded system prompt that would need a small `internal/commander/` change to read from the selected persona. When this lands, phase 0's "ready" card grows a "persona: default · change" affordance that opens an inline persona picker.

## Verification

1. `make dev` — start servers. Ensure `OPENROUTER_API_KEY` is set (commander backend requires it).
2. Open `/` — lands on the unified OnboardingFlow. See macro timeline: `(✓) commander — (●) frameworks — (○) projects`. Phase 0's "ready" card shows the configured provider/model and a history count. Right chat panel is expanded by default.
3. Ask the commander a question ("what frameworks are available?") — streams from `/api/commander/chat`. Response renders with deltas appearing live. Tool-call card appears inline if the commander fires `list_projects`.
4. Click the commander chat's collapse toggle — panel shrinks to thin strip. Click the strip — expands again.
5. Reload the page — chat history rehydrates from `/api/commander/history` (the JSONL-persisted conversation).
6. Click phase 1's "delete history" in the phase 0 details panel — `DELETE /api/commander/history` clears the conversation, confirms via greeting reset.
7. In phase 1, see the inner 5-step timeline. Sub-step status reflects current state (not click history) — a returning user with ZeroClaw already installed + configured sees those sub-steps already green.
8. Click "auto install with defaults" — tmux runs `eyrie install zeroclaw -y`. Inner timeline's install step turns blue (installing) and flips to green when the output parser detects success. No 5-second polling lag.
9. Inner timeline advances through configure and api-key steps. Paste OpenRouter key inline → validates → ready state.
10. Macro timeline's phase 1 flips to green as soon as one framework is ready. `+ framework` affordance remains.
11. Phase 2 becomes clickable. Click → project form opens. Fill name/framework/description. In team section, click `+ provision new` for the captain role → inline sub-form → pick persona, name, save. Captain agent created via `/api/instances`, added to team. Click `create project` → project created via `/api/projects`.
12. Hit deep link `/frameworks/zeroclaw` — opens the dedicated framework page (not the unified flow). Visit `/mission-control` — still works, renders the extracted `MissionControl.tsx`.
13. Trigger an install failure (e.g., uninstall rust, then attempt install) — red error state shows with "help me resolve this" purple CTA that pre-fills a question in the commander chat input.
14. Negative case: unset `OPENROUTER_API_KEY` and restart — phase 0 shows the yellow "commander isn't configured" card; chat panel shows red unavailable banner instead of streaming errors.

## Non-goals (explicit)

- Minimal changes to server-side install/uninstall logic. (v0.2.1 added `eyrie reset` for config-only removal, health proxy, and config patch endpoints — these were necessary for the onboarding UX but didn't change the core install/uninstall flow.)
- No change to tmux integration or terminal persistence.
- Not retiring dedicated pages at `/frameworks/:id`, `/agents/:name`, `/projects/:id` — they remain bookmarkable and accessible from the sidebar.
- Not retiring the standalone `AgentsPage` / `AgentDetail` surfaces — agents are a first-class concept, just not a macro phase in onboarding.
- Not building a new Settings page — extracting `ApiKeyForm` is a refactor, not a redesign.
- Not replacing ConfigPage / AgentDetail config tab.
- Not resolving framework-level vs agent-level config overlap — tracked as a follow-up.
- Not building the commander knowledge base — tracked as a follow-up.

## Pencil blueprint status

Existing screens in `/Users/dan/Documents/Comp/eyrie/project-design.pen`:

**y=4400 — y=5440 (framework drill-down, 6 screens, reused in phase 1 step panels):**
1. Step 1 active — install with auto/manual options.
2. Step 2 active — configure with three paths.
3. Step 3 active — api key with auto-detected provider.
4. Installing — live tmux with phase marker + blue-bordered terminal.
5. Ready — all inner steps green, launch chat CTA.
6. Binary missing — yellow recovery state.

**y=6600 — y=8680 (unified flow overview, 8 screens):**
7. Unified flow overview (chat expanded) — single-line timeline + right chat panel at 380px.
8. Unified flow overview (chat collapsed) — same layout with chat shrunk to 44px strip.
9. Phase 1 sub-step 1 (choose) — framework grid as active panel.
10. Phase 2 (projects) — project creation form with team section and inline agent provisioning.
11. Install failed (red) — error state with help-me-resolve CTA.
12. Commander phase placeholder — dashed border, "coming soon".
13. End-of-phase-1 summary — `+ framework` and `continue to projects →` after a framework is ready.
14. (Additional variant / detail screens.)

**Note:** the existing agent-phase mockup (y=7640 x=0, `HCtta`) was designed when agents were a separate macro phase. Since we've collapsed agents into project creation, this screen is no longer used in the onboarding flow. Keep it as reference for the standalone `AgentsPage` sidebar surface, which has similar "pool + provision new" content.

---

## Implementation Update (2026-04-29)

Steps 1–3 are **done** and shipped on `main` (v0.2.1). The FrameworkCard click-hijack bug is fixed (`onSelect` prop). Both feature branches have been merged to `main`. The commander backend is fully available — read/write tools, structured confirmation, memory store, Anthropic native provider, context-window usage. This section details the remaining steps 4 and 5.

### Step 4: Commander chat panel + Phase 0

The commander backend is already on `main` — no merge needed.

#### 4a. API client helpers (`web/src/lib/api.ts`)

Add thin wrappers for the 5 commander endpoints:

- `streamCommanderChat(message: string): EventSource` — POST `/api/commander/chat`, returns a streaming reader that emits typed SSE events
- `fetchCommanderHistory(): Promise<Message[]>` — GET `/api/commander/history`
- `clearCommanderHistory(): Promise<void>` — DELETE `/api/commander/history`
- `fetchCommanderMemory(): Promise<MemoryEntry[]>` — GET `/api/commander/memory`
- `confirmCommanderAction(id: string, approved: boolean, reason?: string): EventSource` — POST `/api/commander/confirm/{id}`, returns SSE (the continuation turn streams back)

SSE parsing: the chat and confirm endpoints stream `data: {json}\n\n` lines. Use a shared `parseCommanderSSE(response: Response, handlers: EventHandlers)` utility rather than `EventSource` (which requires GET; our endpoints are POST). This mirrors how `ProjectChat.tsx` already handles SSE from POST endpoints.

#### 4b. Type definitions (`web/src/lib/types.ts`)

```typescript
// Commander SSE event types
interface CommanderDelta    { type: "delta"; text: string }
interface CommanderToolCall { type: "tool_call"; id: string; name: string; args: Record<string, any> }
interface CommanderToolResult { type: "tool_result"; id: string; name: string; output: string; error?: boolean }
interface CommanderMessage  { type: "message"; role: string; content: string }
interface CommanderDone     { type: "done"; input_tokens?: number; output_tokens?: number; context_tokens?: number; context_window?: number }
interface CommanderError    { type: "error"; error: string }
interface CommanderConfirmRequired { type: "confirm_required"; id: string; tool: string; args: Record<string, any>; summary: string }

type CommanderEvent = CommanderDelta | CommanderToolCall | CommanderToolResult | CommanderMessage | CommanderDone | CommanderError | CommanderConfirmRequired;

interface MemoryEntry { key: string; value: string; created_at: string; updated_at: string }
```

#### 4c. `CommanderChat.tsx` — the right-side panel

**New file**: `web/src/components/CommanderChat.tsx`

**Layout** (matches pencil mockups):
- ~380px wide (expanded), ~44px collapsed
- Expand/collapse toggle on its left edge
- Expanded by default
- Chat header: "commander" label + memory badge (count) + context-window usage bar + collapse button
- Message list: scrollable, rehydrated from `/api/commander/history` on mount
- Input area: text input + send button at bottom

**SSE event handling** — each event type maps to a UI update:

| Event | UI behavior |
|-------|-------------|
| `delta` | Append text to the current streaming assistant message. Show `StreamingIndicator`. |
| `tool_call` | Insert a collapsible card into the message stream: tool name + args summary. Card starts "running…" state (blue spinner). |
| `tool_result` | Update the matching tool-call card: show output (truncated), green check or red X based on `error` field. |
| `confirm_required` | Insert a **confirmation card** — shows the tool's `summary`, an "approve" (green) button, a "deny" (red outline) button, and an optional reason text input for deny. Clicking approve POSTs to `/api/commander/confirm/{id}` with `approved: true` and begins streaming the continuation turn. Clicking deny POSTs `approved: false` with the reason. The card transitions to "approved" or "denied" state after the POST. |
| `message` | Finalize the streaming assistant message (replace accumulated deltas with the complete content). |
| `done` | Stop streaming indicator. Update context-window usage from `context_tokens` / `context_window`. |
| `error` | Show an inline error message (red) in the chat stream. Stop streaming. |

**Confirmation flow detail:** When a `confirm_required` event arrives, the turn is paused server-side. The chat panel shows the confirmation card inline in the message stream. The user must approve or deny before the commander continues. On approve/deny, the POST to `/api/commander/confirm/{id}` returns a new SSE stream (the continuation turn) — handle it identically to the original chat stream (deltas, tool calls, done, etc.).

**Context-window usage indicator:** A thin bar in the chat header that fills proportionally to `context_tokens / context_window`. Color: green under 50%, yellow 50–80%, red over 80%. Tooltip shows "N / M tokens (X%)". Updated after each `done` event.

**Memory section:** A collapsible drawer below the chat header, toggled by clicking the memory badge. Lists all entries from `GET /api/commander/memory` as `key: value` rows with timestamps. Read-only — the user manages memory by talking to the commander ("remember that...", "forget X"). Refreshed after each `done` event (the commander may have used remember/recall/forget tools during the turn).

**Context-aware greetings + suggestion chips** (per plan — no changes):
- Phase 1 choose: "what framework should I pick?", "what's an API key?"
- Phase 2 project form: "help me scope this project", "suggest talons for this goal"
- Error state: "check my rust version", "clear cargo cache"

**"Ask commander" CTAs** — purple buttons throughout the main content that pre-fill a question in the chat input:
- Choose step: "not sure which framework to pick?"
- Error state: "help me resolve this"
- API key step: "what's an API key?"

#### 4d. Layout integration (`App.tsx` / `OnboardingFlow.tsx`)

The chat panel lives **outside** the constrained `max-w-5xl` container, as a sibling to the main content area. The layout becomes:

```
┌─────────┬──────────────────────┬────────────┐
│ sidebar │   main content       │ chat panel │
│  220px  │   flex (constrained) │  380/44px  │
└─────────┴──────────────────────┴────────────┘
```

`App.tsx` renders `CommanderChat` at the `<main>` level, next to the Routes container. The chat panel is visible on **all routes** (not just `/`) so the user can talk to the commander while on `/frameworks/zeroclaw` or `/projects/:id`. The expand/collapse state persists via `localStorage`.

#### 4e. Phase 0 (`CommanderPhase.tsx`) — now live

Replace the static placeholder with a live card:

- **Health check:** on mount, fetch `GET /api/commander/history`. If it succeeds, show the green "ready" card. If it fails (5xx or network error), show yellow "commander isn't configured" card with guidance ("Set `OPENROUTER_API_KEY` or `ANTHROPIC_API_KEY` and restart Eyrie").
- **Ready card contents:**
  - "Eyrie is your commander. Ask me anything via the chat panel →"
  - Collapsed-by-default details section:
    - History: "N messages" (from history response length) + "clear history" button
    - Memory: "N entries" (from `GET /api/commander/memory` count)
    - Context window: usage bar matching the chat panel header
  - Provider detection: the backend doesn't expose which provider is configured via an API, so defer this to a follow-up unless we add a `/api/commander/status` endpoint.
- **Auto-advance:** the macro timeline treats phase 0 as complete when the health check succeeds (already implemented in OnboardingFlow.tsx — just wire the actual check instead of hardcoding `commander: "complete"`).

### Step 5 (revised): Error state + polish

#### 5a. Install error state (red)

When `stepStatus.install === "error"` in FrameworksPhase, show the error panel per the pencil mockup ("Unified Flow — Install failed (red)"):

- Red banner above the step panel: `"install failed · {last error line from terminal}"`
- Step panel contents:
  - **"ask the commander"** (purple primary) — pre-fills a question in the chat input describing the error
  - **"retry install"** (red outline) — reruns the same command
  - **"switch to manual install"** (border) — flips to the other install option
- The inner timeline's install circle turns red with `!`; connector into it turns red
- Terminal keeps the full failure log visible below

The error line extraction: `terminalParser.ts` already has install-failure patterns. Extend it to capture the last `error:` / `ERROR` / `error[E` line from the terminal output and surface it via a callback or ref.

#### 5b. End-of-phase-1 celebration

Update the `showReadyActions` banner in FrameworksPhase to match the mockup ("Unified Flow — End of Phase 1"):
- Show the full sub-step summary: `"choose ✓ → install ✓ → configure ✓ → api key ✓ → launch ✓"`
- Two CTAs: **"set up another framework"** (loops back to choose) + **"continue to projects →"** (advances the macro phase)
- The "continue" CTA calls `onSelect("projects")` on the macro timeline

#### 5c. MissionControl extraction

- Extract the mission-control parts of HierarchyPage (SwimLaneTimeline, metric cards, agent summary bar) into `MissionControl.tsx`
- Route `/mission-control` → MissionControl
- HierarchyPage.tsx can be deleted or kept as a thin wrapper that imports MissionControl
- The merge already removes the `CommanderSetup` references and `changingCommander` state, making this extraction cleaner

#### 5d. Cleanup

- Delete empty `web/src/components/mission/` directory
- Verify `CommanderSetup.tsx` is gone (handled by merge)
- Remove any dead imports or stale TODO comments referencing the old commander-agent model

### Revised verification checklist

1–12 from the original plan still apply, plus:

15. Ask the commander "what are my projects?" — tool_call card appears inline (list_projects), result renders as a collapsible summary.
16. Ask the commander "create a project called test" — `confirm_required` event renders an approval card with summary "Create project 'test'". Click approve → project is created, commander confirms.
17. Click deny on a confirmation card with reason "not yet" → commander acknowledges the denial.
18. After a multi-tool turn, the context-window bar in the chat header updates. Hover shows token count.
19. Click the memory badge → drawer opens showing stored entries. Ask the commander "remember that I prefer OpenRouter" → memory entry appears on next refresh.
20. Collapse the chat panel → 44px strip. Navigate to `/frameworks/zeroclaw` → chat panel persists (still collapsed). Click strip → expands with full history.
21. Trigger install failure → red error banner + "ask the commander" CTA → clicking it pre-fills a question in the chat input and focuses it.

### Files to modify/create (steps 4–5)

See the "Still to create" and "Still to modify" sections in "Files to modify" above — those are the consolidated list for steps 4–5.
