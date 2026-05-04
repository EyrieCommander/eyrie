# Eyrie Runtime Registry

Status: provisional
Owner: Magnus/Eyrie
Updated: 2026-05-04

This directory tracks runtime agents that Eyrie knows how to supervise or represent.
It is a registry, not a process launcher.

Each runtime entry should name:

- stable `runtime_id`
- display name
- parent agent
- owning domain
- role
- framework or adapter
- transport
- workspace
- read and write scopes
- allowed actions
- approval boundary
- health mode
- current assignment

The registry can include file-backed monitors before they have a live runtime wrapper.
That lets Eyrie model agents such as Astra without launching Hermes, scheduling jobs,
or mutating runtime home files.

## Current Entries

- `astra.vega.yaml`: Vega's file-backed Commander watch officer.

## Operating Rules

- Runtime agents operate under a parent Captain, Talon, or command agent; they are
  not free-floating peers.
- Eyrie tracks runtime ownership, scope, and health, but it does not override the
  parent agent's judgment.
- File-backed registration does not approve installs, launches, schedules, commits,
  pushes, public actions, GitHub mutations, credential changes, runtime-home changes,
  or destructive cleanup.
- ACP, MCP, CLI, launchd, gateway, or API transports should be added only after the
  file-backed registry entry is clear and Dan approves the runtime step.
