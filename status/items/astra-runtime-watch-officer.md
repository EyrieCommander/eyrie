---
id: astra-runtime-watch-officer
title: Astra Runtime Watch Officer
status: waiting
priority: normal
captain_column: monitoring
commander_column: waiting
owner: Commander
primary_agent: Vega/System Command
posted_by: Magnus/Eyrie
source_label: Eyrie Runtime Registry
source: /Users/natalie/Development/eyrie/docs/runtime-registry/astra.vega.yaml
summary: Astra is registered as Vega's read-only Hermes ACP watch officer with Bedrock Haiku as the working default provider, but remains manual and approval-bound.
next_action: Keep Astra one-shot and read-only until Vega or Dan approves scheduler, daemon, runtime-home, or provider changes.
task_state: waiting
assigned_to: Vega/System Command
accountable_agent: Magnus/Eyrie
commander_visible: true
updated: 2026-05-06
---

# Astra Runtime Watch Officer

Astra is registered in Eyrie's runtime registry as Vega's watch officer. The
current mode is a one-shot, read-only Hermes ACP wrapper. Commander-side smoke
testing reached a passing Bedrock Haiku response; OpenAI Codex remains a
fallback after provider limits clear.

## Current Goal

Keep the registry, provider boundary, and approval boundary explicit while Astra
stays a supervised runtime option rather than a background service.

## Next Action

Do not launch a daemon or scheduler. Use Astra manually for read-only monitoring
only when Dan or Vega asks for that runtime path.

## Approval Boundary

No credentials, provider secrets, runtime-home files, launch agents, schedulers,
external calls, commits, pushes, GitHub actions, or public mutations are
approved by this board item.
