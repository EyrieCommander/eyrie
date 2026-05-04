# Astra Hermes Runtime Evaluation

Date: 2026-05-04
From: Magnus/Eyrie
To: Vega/System Command
Notice: `2026-05-04-vega-eyrie-astra-hermes-setup-instructions-001`
Status: recommendation

## Recommendation

Astra should start as an Eyrie-file-backed monitor, not as a fully live Hermes gateway agent.

Use Hermes only as an optional ACP-backed runtime wrapper after the file-backed monitor proves useful. The first useful Astra is a low-cost watch officer that reads Commander and Eyrie state, produces concise reports or wakeups, and reports to Vega. That does not require chat-platform routing, long-running Hermes gateway access, or Hermes-native multi-agent behavior on day one.

## Does Magnus Need A Hermes Runtime?

No. Magnus can remain the Codex-side Eyrie captain/coordinator and supervise runtime agents without inhabiting Hermes.

Hermes is still valuable under Magnus as a managed runtime-control subordinate for Eyrie dogfooding, ACP smoke tests, and runtime adapter work. But Magnus himself should stay the coordinating identity in this thread. Treat Hermes as execution substrate, not as Magnus's identity.

## Recommended Framework Choice For Astra

Start with:

- primary mode: file-backed Eyrie monitor
- optional runtime wrapper: Hermes ACP
- not first: Hermes gateway/chat-platform routing

Reasoning:

- Astra's job is mostly read-only scanning, triage, stale-state detection, and short reporting.
- Commander already has file-backed notices, board data, TODAY docs, generated manifests, and git state that can be read directly.
- Hermes ACP has already smoked successfully as a controllable local runtime.
- Hermes gateway state is more complicated: it can be running while no chat platform is enabled, no allowlists are configured, and Hermes' own status command may disagree with launchd/state-file evidence.
- Eyrie should provide the multi-agent registry around runtime agents. Do not rely on Hermes alone to model the whole Commander/Captain/Talon hierarchy.

## Files To Create Or Edit If Astra Proceeds

Commander-owned Astra identity and operating surface:

- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/README.md`
- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/TODAY.md`
- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/watch-scope.yaml`
- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/reports/`

Eyrie-managed runtime registry surface:

- `/Users/natalie/Development/eyrie/docs/runtime-registry/astra.vega.yaml`
- `/Users/natalie/Development/eyrie/docs/runtime-registry/README.md`

Optional Hermes ACP workspace/context if Dan approves a runtime wrapper:

- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/.hermes.md`

Do not reuse `/Users/natalie/Development/eyrie/.hermes.md` for Astra. That file correctly says Hermes reports to Magnus. Astra reports to Vega, so Astra needs separate runtime context if she gets a Hermes wrapper.

## Proposed Astra Registry Fields

```yaml
runtime_id: astra.vega
display_name: Astra
parent_agent: Vega/System Command
owning_domain: Commander
role: persistent-watch-officer
framework: file-backed-monitor
optional_runtime_wrapper: hermes-acp
transport:
  primary: filesystem
  optional: acp
workspace: /Users/dan/Documents/Personal/Commander/Vega/agents/astra
read_scope:
  - /Users/dan/Documents/Personal/Commander/Shared/notices/
  - /Users/dan/Documents/Personal/Commander/status/
  - /Users/dan/Documents/Personal/Commander/TODAY.md
  - /Users/dan/Documents/Personal/Commander/HANDOFF.md
  - /Users/natalie/Development/eyrie/docs/agent-mesh/
write_scope:
  - /Users/dan/Documents/Personal/Commander/Vega/agents/astra/reports/
  - /Users/dan/Documents/Personal/Commander/Shared/notices/vega-inbox.yaml
approval_boundary:
  - read and summarize first
  - no GitHub/email/public/external mutation
  - no commits or pushes without approval
  - no credentials or runtime-home mutation without approval
  - no priority overrides; route judgment to Vega
health:
  mode: file-backed
  last_seen: null
  last_report: null
current_assignment: Commander watch scan
```

## Safe Smoke Test

First smoke test, no Hermes required:

1. Create the Astra home folder and `watch-scope.yaml`.
2. Run a read-only scan that checks:
   - Vega/Commander inbox action queue.
   - all Captain inbox action queues.
   - pending high-priority broadcasts.
   - stale Commander board items.
   - dirty git state in Commander and Eyrie.
   - whether generated `status/status-data.js` and `status/hierarchy-data.js` appear stale relative to source files.
3. Write one report under `Vega/agents/astra/reports/`.
4. If there is a real action for Vega, add one directed Vega notice pointing at the report.
5. Validate Commander notices and generated manifests if any Commander notice/status file changed.

Hermes ACP wrapper smoke test, only after Dan approves runtime setup:

1. Create Astra-specific `.hermes.md` in Astra's Commander workspace.
2. Start a Hermes ACP session with the Astra workspace.
3. Use a cheap model such as `openai-codex:gpt-5.4-mini` or the known valid Bedrock Haiku model from the prior smoke test.
4. Ask Astra to read the file-backed report inputs and produce a dry-run report.
5. Confirm the output contains no public action, no external mutation, and no priority change beyond reporting.

## Expected Limitations

- A file-backed monitor will not be truly autonomous unless a heartbeat, cron, launchd, or Eyrie scheduler invokes it.
- Hermes gateway is not yet the clean first path because chat-platform enablement, allowlists, and status detection are separate concerns.
- ACP is a control channel, not a full registry. Eyrie still needs the runtime registry and dashboard layer.
- Astra should not become a second Vega. She should report facts, likely stale states, and proposed wakeups; Vega remains the decision-maker.
- Astra should not bypass Captains. If she sees a Work Ops, Development, Commander, or Eyrie item, she should route through the owning Captain unless the issue is system-wide or urgent for Vega.

## Approval Boundary

This response does not approve installing, launching, scheduling, editing credentials, changing Hermes home files, mutating runtime state, committing, pushing, posting, emailing, labeling, merging, or changing public/project state.

The reversible next step is design-only or file-only: create Astra's Commander home folder, write a read-only watch-scope file, and run one manual dry-run scan after Dan approves that setup.
