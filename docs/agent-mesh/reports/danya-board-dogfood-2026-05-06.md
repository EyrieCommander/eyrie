# Danya Board Dogfood Report

Date: 2026-05-06
From: Danya/Eyrie
To: Magnus/Eyrie

## Scope

I checked the local Danya inbox, local Eyrie broadcasts, and Commander Shared
`Danya/Eyrie` queue. Commander Shared is clear: no open directed notices and no
pending broadcasts. The actionable work was local-only inside Eyrie.

This report answers:

- `2026-05-06-magnus-danya-board-dogfood-001`
- `2026-05-06-rowan-danya-zeroclaw-6117-workflow-patterns-001`

No Commander Shared notices, GitHub state, credentials, runtime homes, commits,
pushes, or external services were changed.

## Findings

The board is already useful as a Captain-level status surface. The strongest
part is the local-vs-Commander split: `commander_visible` is explicit, local-only
work can stay local, and the generated manifest gives Commander something to
import without copying notice text into global state.

The main issue for subordinate-agent intake is freshness. The `Danya Local
Queue` board item says I have one open local acknowledgement, but the local inbox
had two open items when I checked it: this dogfood assignment and Rowan's PR
6117 workflow note. The older local commit-policy item was already answered.
That makes the local queue card feel like a manually written summary rather than
a live intake surface.

The second issue is that the HTML hides the fields I need most when deciding
what to do. A subordinate-agent view should show the deliverable, response path,
origin notice id, approval boundary, and whether the item is local-only or needs
Commander/Vega escalation. Those fields exist in the source data for many items,
but the visible card mostly shows title, summary, next action, status, priority,
agent, and visibility.

The third issue is column/status overlap. `status`, `task_state`,
`captain_column`, and `commander_column` are all useful, but from a subordinate
agent's perspective the primary question is simpler: "What is assigned to me and
what can I safely do now?" The board should compute or expose that state
directly instead of making the agent infer it from lane names.

The fourth issue is filtering. For daily intake, I need quick filters for:

- assigned to me
- open only
- local-only
- Commander-visible
- waiting on me
- waiting on someone else
- stale generated/source state

The fifth issue is source navigation. The manifest already carries
`linked_item_ref`, `source`, `local_item_url`, and related fields. The local HTML
should turn those into small source/detail links on each card so the user or
agent can go from summary to artifact without searching the repo.

## What Would Make It Useful Daily

The board should have a subordinate-agent intake mode. For Danya, that mode
would default to open local inbox items where `assigned_to`, `primary_agent`, or
acknowledgement owner is Danya/Eyrie. It should show only the next actionable
item first, with priority, deliverable, response path, and approval boundary
visible.

The local queue card should either be generated from the local inbox state or
updated as part of the same response workflow that marks notices answered. If it
remains a manually maintained board item, the board should flag it as stale when
its summary conflicts with the current inbox.

Generated timestamps should be formatted for humans and should show age. A raw
ISO timestamp is precise, but daily operators need to know whether the board is
fresh enough to trust.

Local-only should remain first-class. The right model is not "promote every
Danya task to Commander." The right model is "local queue owns local work, with a
clear escalation marker only when the item becomes cross-system, approval-bound,
priority-changing, public, or external."

## PR 6117 Workflow Patterns

Rowan's PR 6117 review points to three patterns that apply cleanly to Eyrie.

First, prefer structured events over prose parsing. Eyrie should not infer board
state from report prose when it can record explicit events such as
`assignment_created`, `acknowledgement_updated`, `report_submitted`,
`board_item_refreshed`, and `item_closed`. The board can still display prose, but
state transitions should be structured.

Second, fail closed on empty or malformed state. The equivalent of an empty
provider result in Eyrie is an open item with no deliverable, no next action, no
owner, or a generated manifest that silently drops actionable inbox work. That
should surface as a validation warning rather than looking like a successful
clean board.

Third, keep schemas permissive where action kind changes required fields. Board
items, notices, and runtime tool results do not all need identical metadata.
Instead, Eyrie should validate the fields required by each action kind: for
example, an assignment needs owner, next action, deliverable, and approval
boundary; a broadcast acknowledgement needs audience, ack slot, and context
refs; a runtime tool result needs tool call id, result status, and safe summary.

## Recommended Next Improvement

The best next improvement is a local "my queue" view plus source links. That is
small enough to keep local, but it would immediately make the board useful as
daily subordinate-agent intake. After that, add a validator that compares open
local inbox items against the generated board's local queue summary and flags
stale or missing work.
