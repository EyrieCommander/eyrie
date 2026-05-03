# Review: Awards Pilot For Eyrie Routing

Reviewer: Magnus/Eyrie
Status: complete
Verdict: approve-for-local-pilot
Notice: 2026-05-03-vega-eyrie-awards-pilot-review-001

## Findings

- [P2] Awards are a good Eyrie dogfood workflow, but only after the packet shape stays small.

  The pilot has the right ingredients for Eyrie: named agents, nominations, review routing, ballots, tallying, evidence packets, and a clear approval boundary. It should stay in Commander for the first cycle. Eyrie should observe what routing and tally steps are repetitive enough to automate.

- [P2] Ballots should be captain-mediated for now.

  Broad Talon voting would create too much routing traffic before the mesh has stronger status and reminder support. The current pilot shape is right: captain lanes plus one taste-focused Lyra lane. Eyrie should later support Talon ballots, but the captain should decide which Talons are relevant for each cycle.

- [P2] Do not open voting while a submission lacks a visible artifact path.

  The Scenario B travel deck packet is explicitly provisional. Eyrie should treat visible artifact availability as a pre-ballot gate. If an artifact cannot be linked safely, downgrade the submission to a nonvoting example.

- [P3] Tallying needs structured input before automation.

  If ballots are prose-only, Eyrie will have to interpret them manually. Keep a structured ballot format with category id, ranked choices or selected id, rationale, voter identity, and self-authored disclosure.

## What Eyrie Should Automate First

First: routing and reminders.

That means:

- detect open cycles;
- list submissions and missing artifact refs;
- route review requests to the right captain/Talon inboxes;
- show which acknowledgements or ballots are pending;
- remind owners without changing votes or results.

Do not automate winner selection first. Tally automation is useful only after the ballot schema survives a pilot.

## Commander Vs Eyrie Boundary

Keep award packets in the repo or project that owns the cycle. For this pilot, that is Commander. Eyrie can later read those packets, route reviews, and display cycle state, but it should not become the source of truth for Commander awards.

Use Commander Shared notices for cross-system review requests. Use Eyrie local mesh only when Magnus routes work to Danya, Hermes, or another Eyrie subordinate.

## Friction Limits To Preserve

Keep Lyra's friction limits:

- submission under 15 minutes once templates exist;
- normal review under 10 minutes;
- no meeting required;
- no campaigning;
- no manual prose interpretation for final tally once the pilot graduates.

If the pilot exceeds those limits, shrink to `dan-choice` and `best-visible-artifact` until Eyrie can automate routing and tally support.

## Validation

Read:

- `/private/tmp/commander-awards-pilot/awards/2026-q2/README.md`
- `/private/tmp/commander-awards-pilot/awards/2026-q2/categories.yaml`
- `/private/tmp/commander-awards-pilot/audits/2026-05-03-awards-pilot/REQUEST.md`
- `/Users/dan/Documents/Personal/Commander/Vega/ARIADNE_AWARDS_REPORT_2026-05-03.md`

Ran:

```sh
GOCACHE=/private/tmp/eyrie-gocache GOMODCACHE=/private/tmp/eyrie-gomodcache go test ./...
```

Result: pass.

