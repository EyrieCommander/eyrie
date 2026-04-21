# Eyrie — Project Brief

## What it is

A unified management interface for the Claw family of AI agent frameworks (OpenClaw, ZeroClaw, PicoClaw, NanoClaw, IronClaw, and others as they emerge).

## The Problem

There are many different Claw versions and it's not clear which is the best. You have to set them up separately and each has its own peculiarities. This creates the opportunity for a unifying orchestrator that can help simplify installation as well as allow different agent frameworks to use the same personality. 

Developers running multiple Claw instances have no single place to see what's running, manage configuration, install skills, or monitor activity. Everything is done through separate terminal sessions, config files, and framework-specific CLIs. As the Claw ecosystem grows, this friction compounds.

There's also an emerging dimension that no tool addresses yet: agents are increasingly thought of not just as processes but as individuals — with names, personalities, system prompts, memory, and behavioral traits. A developer might want a single unified personality across ZeroClaw and OpenClaw (so they feel like the same assistant regardless of which framework is running), or they might want multiple instances of OpenClaw with deliberately different personalities for different roles. Currently there's no way to manage this at all, let alone across frameworks.

Gas Town (Steve Yegge, Jan 2026) is an adjacent project — a Claude Code orchestrator with a CLI + optional web dashboard — but it manages agents within a single framework rather than across different ones. Eyrie occupies a distinct and currently empty space.

## Three Directions (all in scope)

### 1. Unified Management
Install, configure, and monitor all Claw agents from one place.

- Auto-discover running Claw instances on localhost and optionally remote machines
- Unified status view: health, RAM, uptime, provider, connected channels
- Start, stop, restart any agent
- Edit configuration inline with validation before saving
- Tail logs per agent in real time
- Personality management: define and assign agent identities — name, system prompt, behavioral traits, memory scope — and apply them across frameworks

### 2. Cross-Agent Task Comparison
Send the same prompt to multiple frameworks simultaneously and compare results.

- Parallel execution across selected agents
- Side-by-side response streaming
- Quality, speed, and cost comparison
- Useful for deciding which framework/model combination to trust for a given task type

### 3. Skill / Plugin Marketplace
Browse, install, and manage skills across frameworks from within Eyrie.

- Richer interface to ClawHub than current CLI
- Cross-framework compatibility surfaced clearly
- Install/remove skills per agent
- Potentially: curated/vetted skill lists given known ClawHub security issues

These three directions naturally compose — management is the foundation, comparison and marketplace are features that live on top. Architecture should keep all three viable without forcing a sequence.

## Interface Options (undecided)

### TUI (Terminal UI)
- Frameworks: Rust + Ratatui, Go + Bubble Tea, Python + Textual
- Pros: fits the clanker audience, zero install friction, works natively over SSH, feels like infrastructure
- Cons: constrained for richer features like side-by-side streaming comparison and skill marketplace cards; fighting the medium for visual tasks
```
┌─ Eyrie ────────────────────────────────────────────────┐
│ AGENTS              STATUS   RAM    UPTIME   PROVIDER   │
│ ▶ OpenClaw          ● run    394MB  2d 4h    OpenRouter │
│   ZeroClaw          ● run    7MB    2d 4h    Anthropic  │
│   PicoClaw          ○ stop   -      -        -          │
├─────────────────────────────────────────────────────────┤
│ DETAIL: OpenClaw                                        │
│ Skills: 12 installed  Channels: Telegram, Discord       │
│ Last task: 14m ago    Errors (24h): 2                   │
│ [L]ogs [S]kills [C]onfig [R]estart [X]Stop             │
├─────────────────────────────────────────────────────────┤
│ LOG TAIL: OpenClaw                                      │
│ 14:23:01 Task completed: "summarize inbox"              │
│ 14:19:44 Telegram message received                      │
│ 14:19:44 Routing to skill: email-skill                  │
└─────────────────────────────────────────────────────────┘
 [I]nstall  [C]ompare  [M]arketplace  [Q]uit  [?]Help
```
### Local Web App
- Served from the machine on localhost, opened in browser
- Frameworks: anything — React, Svelte, etc. with a lightweight backend
- Pros: full graphical richness, natural for marketplace and comparison features, remote access via SSH port forwarding or Tailscale, no client install needed
- Cons: slightly more setup, less "native" feel

### Desktop App
- Frameworks: Tauri (Rust-native, lightweight) or Electron
- Pros: richest native experience, best for marketplace and comparison UI
- Cons: no headless/SSH use, more complex distribution, requires a display

### Hybrid CLI + Web Dashboard (Gas Town approach)
- CLI for control (`eyrie start`, `eyrie stop`, `eyrie status`)
- Web dashboard for monitoring and visual features
- Pros: best of both worlds — scriptable control plus rich visuals
- Cons: two things to build and maintain

## Target User

Solo technical developer running multiple Claw agents on personal or dedicated hardware. Design for team use as a secondary consideration — don't close off multi-user scenarios architecturally.

## Context & Opportunity

- The Claw family is fragmented: OpenClaw (191K stars), ZeroClaw (42K), NanoClaw, PicoClaw, IronClaw all exist with no shared management layer
- ClawHub provides skill discovery but only via CLI
- OpenClaw has known security issues (8 critical CVEs as of March 2026); a management layer that surfaces security posture across frameworks has real value
- The acquihire/OSS dynamic noted by swyx (@swyx, Mar 10 2026) suggests category-leading open source AI engineering tools have significant acquisition value right now
- Gas Town demonstrates appetite for multi-agent orchestration UI but doesn't address cross-framework management
- No existing tool does cross-framework Claw management

## Principles

- **Open Source** from day one
- **Zero config to get started** — auto-discovery of running agents on localhost
- **Progressive disclosure** — simple summary view by default, drill down for detail
- **Non-destructive defaults** — no action takes effect without confirmation
- **Extensible** — architecture should accommodate non-Claw frameworks in future

## Name

**Eyrie** — a bird of prey's nest; implies command, elevation, oversight. CLI command: `eyrie`.

Etymology note: considered lobster/oceanic metaphors (Burrow, Reef, Fathom, Shoal) to match the Claw family's crustacean theme, but Eyrie is stronger as a control-plane metaphor and more memorable as a product name.

## Open Questions for Claude Code Session

- What language/framework for the initial build?
- How should Eyrie communicate with running agents — Unix socket, HTTP to gateway ports, direct process inspection, or some combination?
- Should Eyrie manage agent processes directly (start/stop) or just observe them?
- What's the auto-discovery mechanism for finding running Claw instances?
- Open source license (MIT, Apache 2.0, other)?
- What's the MVP — management only, or management + one of the other two directions?
