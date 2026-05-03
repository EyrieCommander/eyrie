# Review: 2026-05-03 Mesh Status Surface

Reviewer: Rowan/Development
Status: complete
Verdict: approve-with-nits

## Findings

- [P2] Split the commit scope before asking Dan for commit approval. The read-only mesh status surface is intertwined with a dirty Eyrie tree that includes `personas.json`, untracked `docs/agent-mesh/**`, Danya lore/response docs, and the new server/UI files. Some of that is required fixture/state for the feature, and some appears to be pre-existing mesh/persona work. Before commit, make the scope explicit: either include the mesh docs as reviewed fixtures for this surface, or commit the code surface separately and accept that `/api/mesh/status` will return unavailable until the docs land.

- [P2] Decide how production/local launch should locate the mesh root. `readMeshStatus` is read-only and handles missing mesh docs gracefully, but by default it finds `docs/agent-mesh` by walking upward from the server process working directory. That is fine for repo-local dashboard work, but an installed Eyrie launched from another cwd will not find the mesh unless `EYRIE_AGENT_MESH_DIR` is set. Before treating this as more than a local prototype, document or configure the mesh root explicitly.

- [P3] The dashboard route is small and consistent with the existing app, but it exposes only summaries and paths. That is the right first slice. Do not add write/ack controls, posting, or mesh mutation from this UI until the audit/approval boundary is clearer.

## Open Questions

- Should `docs/agent-mesh/**` be treated as seed data for the Eyrie feature, or as separately reviewed agent-state docs that happen to be read by the feature?

- Should Eyrie eventually read Commander Shared notices directly, or should it only display Commander refs found inside Eyrie mesh reports? For this slice, the current ref-only approach is safer.

## Validation

- Read the Rowan-directed notice `2026-05-03-eyrie-rowan-mesh-status-audit-review-001`.
- Read `/Users/dan/Documents/Personal/Commander/AUDIT.md` and Eyrie mesh context files under `docs/agent-mesh/`.
- Inspected `internal/server/mesh_status.go`, `internal/server/mesh_status_test.go`, `internal/server/server.go`, `internal/server/reference.go`, `web/src/components/MeshStatusPage.tsx`, `web/src/App.tsx`, `web/src/components/Sidebar.tsx`, `web/src/lib/api.ts`, and `web/src/lib/types.ts`.
- Ran `GOCACHE=/private/tmp/eyrie-go-cache GOMODCACHE=/private/tmp/eyrie-go-mod go test ./internal/server`; passed.
- Ran `git diff --check`; passed.
- Ran `npm run build` in `web/`; passed with existing Vite chunk-size/dynamic-import warnings.
- Parsed the current dirty/untracked file set with `git status --short --branch`.

## Summary

The mesh status surface is safe to move forward as a read-only local prototype. The backend only reads local YAML/Markdown files and returns summaries; the frontend only displays the result and refreshes it. I would not commit from the current dirty tree without first making the code/docs/persona scope explicit, but the implementation itself is coherent and the validation passed.
