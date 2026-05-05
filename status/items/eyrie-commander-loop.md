---
id: eyrie-commander-loop
title: Eyrie Commander Loop
status: capture
priority: normal
captain_column: backlog
commander_column: capture
owner: Eyrie/Ops
primary_agent: Magnus/Eyrie
posted_by: Magnus/Eyrie
source_label: Eyrie TODO
source: /Users/natalie/Development/eyrie/docs/TODO.md
summary: Eyrie's main product lane is the commander LLM loop, provider selection, memory, tools, and autonomy policy.
next_action: Keep this visible as the post-board implementation lane, but do not start it until the board surface is stable.
task_state: todo
assigned_to: Magnus/Eyrie
accountable_agent: Magnus/Eyrie
commander_visible: true
updated: 2026-05-06
---

# Eyrie Commander Loop

The main Eyrie product backlog still centers on Eyrie itself becoming the
commander: a local LLM loop with provider selection, persistent history, memory,
tools, and explicit autonomy policy.

## Current Goal

Keep this lane visible without mixing it into the Captain Board implementation
slice. The board is the current enabling surface; the commander loop comes after
the board can track durable work cleanly.

## Next Action

After the Eyrie board is stable and Commander can import it, choose the smallest
backend slice for the commander loop rather than reopening the entire product
plan.

## Approval Boundary

Product implementation in the Eyrie repo is local until commit or push. Provider
calls, credential changes, runtime-home changes, public actions, GitHub actions,
and destructive cleanup require explicit approval.
