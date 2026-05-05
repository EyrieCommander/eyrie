window.eyrieCommandBoard = {
  "schema_version": 1,
  "kind": "captain-command-board",
  "generated_at": "2026-05-05T20:49:00.313Z",
  "captain": "Magnus/Eyrie",
  "domain": "Eyrie",
  "board_profile": "eyrie",
  "captain_meta": {
    "address": "magnus.eyrie",
    "project_id": "eyrie",
    "canonical_commander_inbox": "Eyrie/Ops"
  },
  "commander_interop": {
    "source_id": "eyrie",
    "summary_items": true,
    "deep_links": true,
    "global_card_id": "eyrie-captain-board",
    "recommended_global_label": "Magnus/Eyrie Captain Board",
    "local_board_url": "status/eyrie-command-board.html",
    "local_manifest_ref": "status/eyrie-command-board.json",
    "canonical_item_source": "status/items/*.md"
  },
  "sources": {
    "items": "status/items/*.md",
    "local_mesh": "docs/agent-mesh/manifest.yaml",
    "runtime_registry": "docs/runtime-registry",
    "commander_inbox": "/Users/dan/Documents/Personal/Commander/Shared/notices/eyrie-inbox.yaml"
  },
  "attention_snapshot": {
    "Commander-visible": [
      "Astra Runtime Watch Officer: Keep Astra one-shot and read-only until Vega or Dan approves scheduler, daemon, runtime-home, or provider changes.",
      "Eyrie Agent Mesh: Route new subordinate work through local inboxes and promote only cross-system or Commander-visible summaries.",
      "Eyrie Captain Board: Maintain the board as Eyrie's durable task surface; add new items there first and expose only Commander-relevant summaries with commander_visible true.",
      "Eyrie Commander Loop: Keep this visible as the post-board implementation lane, but do not start it until the board surface is stable."
    ],
    "Local-only": [
      "Danya Local Queue: Danya should report what is confusing or missing when using the local-only queue and board as daily task intake."
    ],
    "Active or waiting": [
      "Astra Runtime Watch Officer: Astra is registered as Vega's read-only Hermes ACP watch officer with Bedrock Haiku as the working default provider, but remains manual and approval-bound.",
      "Danya Local Queue: Danya has a local-only queue item and should dogfood the Eyrie board from a subordinate-agent perspective.",
      "Eyrie Agent Mesh: Maintain the local Magnus, Danya, Hermes, and Docs file-backed mesh as Eyrie's coordination layer."
    ]
  },
  "agents": [
    {
      "name": "Magnus/Eyrie",
      "address": "magnus.eyrie",
      "role": "Captain and coordinator prototype",
      "status": "active"
    },
    {
      "name": "Danya/Eyrie",
      "address": "danya.eyrie",
      "role": "Companion engineer under Magnus",
      "status": "active"
    },
    {
      "name": "Hermes/Eyrie",
      "address": "hermes.eyrie",
      "role": "Runtime-control agent under Magnus",
      "status": "available"
    },
    {
      "name": "Eyrie Docs",
      "address": "docs.eyrie",
      "role": "Documentation and sync lane under Magnus",
      "status": "available"
    }
  ],
  "items": [
    {
      "id": "astra-runtime-watch-officer",
      "title": "Astra Runtime Watch Officer",
      "status": "waiting",
      "priority": "normal",
      "lane": "monitoring",
      "column": "waiting",
      "captain_column": "monitoring",
      "commander_column": "waiting",
      "owner": "Commander",
      "primary_agent": "Vega/System Command",
      "posted_by": "Magnus/Eyrie",
      "source_label": "Eyrie Runtime Registry",
      "source": "/Users/natalie/Development/eyrie/docs/runtime-registry/astra.vega.yaml",
      "summary": "Astra is registered as Vega's read-only Hermes ACP watch officer with Bedrock Haiku as the working default provider, but remains manual and approval-bound.",
      "next_action": "Keep Astra one-shot and read-only until Vega or Dan approves scheduler, daemon, runtime-home, or provider changes.",
      "task_state": "waiting",
      "assigned_to": "Vega/System Command",
      "accountable_agent": "Magnus/Eyrie",
      "commander_visible": true,
      "source_id": "eyrie",
      "captain": "Magnus/Eyrie",
      "captain_board_profile": "eyrie",
      "linked_item_ref": "/Users/natalie/Development/eyrie/status/items/astra-runtime-watch-officer.md",
      "local_board_url": "status/eyrie-command-board.html",
      "local_item_url": "status/eyrie-command-board.html#item=astra-runtime-watch-officer",
      "local_manifest_ref": "/Users/natalie/Development/eyrie/status/eyrie-command-board.json",
      "updated": "2026-05-06",
      "details": "Astra Runtime Watch Officer\n\nAstra is registered in Eyrie's runtime registry as Vega's watch officer. The\ncurrent mode is a one-shot, read-only Hermes ACP wrapper. Commander-side smoke\ntesting reached a passing Bedrock Haiku response; OpenAI Codex remains a\nfallback after provider limits clear.\n\nCurrent Goal\n\nKeep the registry, provider boundary, and approval boundary explicit while Astra\nstays a supervised runtime option rather than a background service.\n\nNext Action\n\nDo not launch a daemon or scheduler. Use Astra manually for read-only monitoring\nonly when Dan or Vega asks for that runtime path.\n\nApproval Boundary\n\nNo credentials, provider secrets, runtime-home files, launch agents, schedulers,\nexternal calls, commits, pushes, GitHub actions, or public mutations are\napproved by this board item."
    },
    {
      "id": "danya-local-queue",
      "title": "Danya Local Queue",
      "status": "active",
      "priority": "normal",
      "lane": "local",
      "column": "sync-mesh",
      "captain_column": "local",
      "commander_column": "sync-mesh",
      "owner": "Eyrie/Ops",
      "primary_agent": "Danya/Eyrie",
      "posted_by": "Magnus/Eyrie",
      "source_label": "Eyrie Local Mesh",
      "source": "/Users/natalie/Development/eyrie/docs/agent-mesh/inboxes/danya.yaml",
      "summary": "Danya has a local-only queue item and should dogfood the Eyrie board from a subordinate-agent perspective.",
      "next_action": "Danya should report what is confusing or missing when using the local-only queue and board as daily task intake.",
      "task_state": "active",
      "task_id": "eyrie-board-state-dogfood",
      "assigned_to": "Danya/Eyrie",
      "accountable_agent": "Magnus/Eyrie",
      "origin_notice_id": "2026-05-06-vega-eyrie-board-state-and-danya-dogfood-001",
      "notification_refs": [
        "2026-05-06-vega-eyrie-board-state-and-danya-dogfood-001"
      ],
      "commander_visible": false,
      "source_id": "eyrie",
      "captain": "Magnus/Eyrie",
      "captain_board_profile": "eyrie",
      "linked_item_ref": "/Users/natalie/Development/eyrie/status/items/danya-local-queue.md",
      "local_board_url": "status/eyrie-command-board.html",
      "local_item_url": "status/eyrie-command-board.html#item=danya-local-queue",
      "local_manifest_ref": "/Users/natalie/Development/eyrie/status/eyrie-command-board.json",
      "updated": "2026-05-06",
      "details": "Danya Local Queue\n\nDanya's local mesh inbox now contains a board-dogfood assignment. Vega asked\nMagnus to have Danya use the local-only queue and board as an agent using the\nsystem, then report what is confusing or missing. This is a local subordinate\nqueue item, not a Commander-visible Eyrie blocker.\n\nCurrent Goal\n\nKeep subordinate queue state visible on the Eyrie board without inflating\nCommander notification counts.\n\nNext Action\n\nDanya should write a short local report on what fields, filters, or intake cues\nwould make the board easier to use as daily task intake. Magnus should only\nescalate if the findings block Eyrie work or need Dan/Vega attention.\n\nApproval Boundary\n\nDanya may read and acknowledge local Eyrie mesh policy notices. Commits, pushes,\nGitHub actions, credential changes, runtime-home changes, external actions, and\ndestructive cleanup require explicit approval."
    },
    {
      "id": "eyrie-agent-mesh",
      "title": "Eyrie Agent Mesh",
      "status": "active",
      "priority": "normal",
      "lane": "active",
      "column": "sync-mesh",
      "captain_column": "active",
      "commander_column": "sync-mesh",
      "owner": "Eyrie/Ops",
      "primary_agent": "Magnus/Eyrie",
      "posted_by": "Magnus/Eyrie",
      "source_label": "Eyrie",
      "source": "/Users/natalie/Development/eyrie/docs/agent-mesh/README.md",
      "summary": "Maintain the local Magnus, Danya, Hermes, and Docs file-backed mesh as Eyrie's coordination layer.",
      "next_action": "Route new subordinate work through local inboxes and promote only cross-system or Commander-visible summaries.",
      "task_state": "active",
      "assigned_to": "Magnus/Eyrie",
      "accountable_agent": "Magnus/Eyrie",
      "commander_visible": true,
      "source_id": "eyrie",
      "captain": "Magnus/Eyrie",
      "captain_board_profile": "eyrie",
      "linked_item_ref": "/Users/natalie/Development/eyrie/status/items/eyrie-agent-mesh.md",
      "local_board_url": "status/eyrie-command-board.html",
      "local_item_url": "status/eyrie-command-board.html#item=eyrie-agent-mesh",
      "local_manifest_ref": "/Users/natalie/Development/eyrie/status/eyrie-command-board.json",
      "updated": "2026-05-06",
      "details": "Eyrie Agent Mesh\n\nEyrie's local mesh under docs/agent-mesh/ remains the coordination layer for\nMagnus, Danya, Hermes, and Eyrie Docs. Commander Shared notices are for cross-\nsystem routing; local mesh inboxes and reports are for Eyrie-owned work.\n\nCurrent Goal\n\nKeep routine Eyrie traffic local, but preserve clear escalation paths to Vega\nand Commander when work is cross-system, approval-bound, priority-changing, or\npublic/external.\n\nNext Action\n\nUse the board to track durable local work and keep notices as wakeups or\nreceipts. Danya's open local policy-relay acknowledgement remains Danya-owned\nunless Dan or Magnus turns it into a broader Eyrie task.\n\nApproval Boundary\n\nLocal mesh docs and board items may be updated as private project maintenance.\nDo not mutate Commander Shared notices, GitHub, credentials, public services, or\nruntime homes without explicit approval for that action."
    },
    {
      "id": "eyrie-captain-board",
      "title": "Eyrie Captain Board",
      "status": "done",
      "priority": "normal",
      "lane": "monitoring",
      "column": "waiting",
      "captain_column": "monitoring",
      "commander_column": "waiting",
      "owner": "Eyrie/Ops",
      "primary_agent": "Magnus/Eyrie",
      "posted_by": "Magnus/Eyrie",
      "source_label": "Eyrie",
      "source": "/Users/natalie/Development/eyrie/status/items/eyrie-captain-board.md",
      "summary": "Eyrie's first Captain Board slice exists; this item is now a maintenance marker for the durable board surface.",
      "next_action": "Maintain the board as Eyrie's durable task surface; add new items there first and expose only Commander-relevant summaries with commander_visible true.",
      "task_state": "maintenance",
      "assigned_to": "Magnus/Eyrie",
      "accountable_agent": "Magnus/Eyrie",
      "origin_notice_id": "2026-05-05-vega-eyrie-captain-board-handoff-001",
      "notification_refs": [
        "2026-05-05-vega-eyrie-captain-board-handoff-001"
      ],
      "commander_visible": true,
      "source_id": "eyrie",
      "captain": "Magnus/Eyrie",
      "captain_board_profile": "eyrie",
      "linked_item_ref": "/Users/natalie/Development/eyrie/status/items/eyrie-captain-board.md",
      "local_board_url": "status/eyrie-command-board.html",
      "local_item_url": "status/eyrie-command-board.html#item=eyrie-captain-board",
      "local_manifest_ref": "/Users/natalie/Development/eyrie/status/eyrie-command-board.json",
      "updated": "2026-05-06",
      "details": "Eyrie Captain Board\n\nVega accepted the Captain Board model on 2026-05-05: notices wake agents up,\nbut local Captain boards own durable task state. Eyrie's first slice should\nprove that model with local Markdown items, a generated Captain manifest, and a\nlocal board page.\n\nCurrent Goal\n\nMaintain the smallest useful Eyrie board that can show local implementation work,\nsubordinate-agent coordination, and runtime/watch-officer state without copying\nnotice contents into Commander.\n\nNext Action\n\nUse this board as the source of truth for new long-running Eyrie work. Add or\nupdate status/items/*.md, regenerate the manifest, and expose only items that\nDan or Vega should see globally with commander_visible: true.\n\nApproval Boundary\n\nLocal Eyrie docs, status items, and generated local manifests are in scope.\nCommits, pushes, GitHub mutations, credential changes, runtime-home mutation,\nexternal publication, and destructive cleanup still require explicit approval."
    },
    {
      "id": "eyrie-commander-loop",
      "title": "Eyrie Commander Loop",
      "status": "capture",
      "priority": "normal",
      "lane": "backlog",
      "column": "capture",
      "captain_column": "backlog",
      "commander_column": "capture",
      "owner": "Eyrie/Ops",
      "primary_agent": "Magnus/Eyrie",
      "posted_by": "Magnus/Eyrie",
      "source_label": "Eyrie TODO",
      "source": "/Users/natalie/Development/eyrie/docs/TODO.md",
      "summary": "Eyrie's main product lane is the commander LLM loop, provider selection, memory, tools, and autonomy policy.",
      "next_action": "Keep this visible as the post-board implementation lane, but do not start it until the board surface is stable.",
      "task_state": "todo",
      "assigned_to": "Magnus/Eyrie",
      "accountable_agent": "Magnus/Eyrie",
      "commander_visible": true,
      "source_id": "eyrie",
      "captain": "Magnus/Eyrie",
      "captain_board_profile": "eyrie",
      "linked_item_ref": "/Users/natalie/Development/eyrie/status/items/eyrie-commander-loop.md",
      "local_board_url": "status/eyrie-command-board.html",
      "local_item_url": "status/eyrie-command-board.html#item=eyrie-commander-loop",
      "local_manifest_ref": "/Users/natalie/Development/eyrie/status/eyrie-command-board.json",
      "updated": "2026-05-06",
      "details": "Eyrie Commander Loop\n\nThe main Eyrie product backlog still centers on Eyrie itself becoming the\ncommander: a local LLM loop with provider selection, persistent history, memory,\ntools, and explicit autonomy policy.\n\nCurrent Goal\n\nKeep this lane visible without mixing it into the Captain Board implementation\nslice. The board is the current enabling surface; the commander loop comes after\nthe board can track durable work cleanly.\n\nNext Action\n\nAfter the Eyrie board is stable and Commander can import it, choose the smallest\nbackend slice for the commander loop rather than reopening the entire product\nplan.\n\nApproval Boundary\n\nProduct implementation in the Eyrie repo is local until commit or push. Provider\ncalls, credential changes, runtime-home changes, public actions, GitHub actions,\nand destructive cleanup require explicit approval."
    }
  ],
  "counts": {
    "total": 5,
    "commander_visible": 4,
    "local_only": 1,
    "by_status": {
      "waiting": 1,
      "active": 2,
      "done": 1,
      "capture": 1
    },
    "by_priority": {
      "normal": 5
    },
    "by_lane": {
      "monitoring": 2,
      "local": 1,
      "active": 1,
      "backlog": 1
    }
  }
};
