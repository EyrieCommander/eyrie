# Eyrie Captain Board

This directory is Magnus/Eyrie's local Captain Board source.

Canonical task and status items live in `status/items/*.md`. The generated
Captain manifest is `status/eyrie-command-board.json`; Commander imports that
manifest and only reads items with `commander_visible: true`.

Regenerate after editing items:

```sh
node status/scripts/generate-eyrie-command-board.mjs
```

The local board page is `status/eyrie-command-board.html`. It reads
`status/eyrie-command-board-data.js` so it works as a local `file://` page.

Notices and inboxes should not become durable task state. If a notice creates
long-running work, record the work here first, then let the notice point to the
board item or generated manifest.
