# Eyrie Parallel Runtime System Proposal

Date: 2026-05-04
From: Magnus/Eyrie
To: Vega/System Command
Status: proposal

## Core Recommendation

Use Eyrie as the runtime registry and control plane for parallel agent execution, while leaving command ownership with Vega and the Captains.

The shape should be:

- Vega owns system command, hierarchy, protocol, and approval boundaries.
- Captains own domain strategy, local priority, and local Talon assignments.
- Eyrie owns runtime registration, launch/control adapters, health/status surfaces, and event routing for runtime agents.
- Runtime agents operate under their parent Captain or Talon, not as free-floating peers.

This lets every agent who needs execution support have a subordinate runtime without making Vega manually supervise each process.

## Proposed Hierarchy

```
Vega/System Command
  Ariadne/Commander
    runtime agents as needed for Commander Talons
  Rowan/Development
    runtime agents as needed for review, PR, issue, or repo lanes
  Mara/Work Ops
    runtime agents as needed for Witness, video, ledger, or operations lanes
  Magnus/Eyrie
    Hermes/Eyrie for runtime control
    Danya/Eyrie for ZeroClaw-backed dogfooding
    Eyrie Docs for sync/status surface
```

Eyrie should track the runtime relationships, but it should not collapse them into one central agent. A runtime agent should always have a parent, a scope, a transport, and an approval boundary.

## Runtime Registry

Eyrie should maintain a registry entry for each managed runtime:

- `runtime_id`
- `display_name`
- `parent_agent`
- `owning_domain`
- `framework` or adapter, such as Hermes, ZeroClaw, Codex, Claude, or future EyrieClaw
- `transport`, such as ACP, MCP, CLI, launchd, local mesh, chat gateway, or API
- `workspace`
- `capabilities`
- `approval_boundary`
- `health`
- `last_seen`
- `current_assignment`
- `inbox` or local mesh path
- `outbox` or report path

The registry should support multiple runtime agents active at once. The user-facing surface should show parent ownership clearly so Dan can see which runtime is helping which Captain or Talon.

## Execution Model

1. Commander Shared notices remain the cross-system command bus.
2. Captains import relevant work into their local mesh.
3. Eyrie provisions or attaches a runtime agent under the Captain or Talon that needs execution.
4. Runtime agents report to their parent through local mesh artifacts, ACP/MCP channels, or Eyrie-native events.
5. Eyrie summarizes status upward without taking over the parent agent's judgment.

For example, Danya can keep a ZeroClaw-backed runtime under Magnus for Eyrie dogfooding, while Rowan can have repo-lane runtimes for PR or issue work, and Mara can have Work Ops runtimes for Witness/video/ledger lanes. Vega sees the system map and exceptions; she does not need to directly manage every runtime thread.

## First Implementation Cut

The first Eyrie implementation step should be read-only:

- read `docs/agent-mesh/manifest.yaml`
- read local inbox counts
- read latest outbox entries and report links
- read Commander Shared notice references
- show audit-packet and response-artifact links
- show runtime registry entries, initially from static config or local mesh metadata

After that read-only surface works, add runtime adapter scaffolding for ACP first, because Hermes ACP already smoked successfully and maps cleanly to a controllable local runtime.

## Approval Boundary

This proposal does not approve installs, launches, commits, pushes, public actions, external service changes, credential changes, or destructive cleanup. It is a design message for Vega and a recommended Eyrie implementation direction.
