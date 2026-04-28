package server

import "net/http"

func (s *Server) handleAPIReference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(apiReference))
}

const apiReference = `# Eyrie API Reference

Base URL: http://localhost:7200

IMPORTANT: Use the exec tool with curl to call these endpoints. Do NOT use web_fetch — it blocks localhost.
Example: exec: curl -s http://localhost:7200/api/projects
Example: exec: curl -s -X POST http://localhost:7200/api/projects -H "Content-Type: application/json" -d '{"name":"my project","goal":"build a SaaS"}'

## Projects

### Create a project
POST /api/projects
Content-Type: application/json

Body:
- name (string, required): project name
- description (string): what this project is about
- goal (string): the desired outcome

### List all projects
GET /api/projects

### Get project details
GET /api/projects/{id}

### Update a project
PUT /api/projects/{id}
Content-Type: application/json

Body (all fields optional):
- name (string)
- description (string)
- goal (string)
- status (string): "active", "paused", "completed", "archived"
- orchestrator_id (string): instance ID or agent name of the Captain

### Delete a project
DELETE /api/projects/{id}

### Add an agent to a project
POST /api/projects/{id}/agents
Body: {"instance_id": "..."}

### Remove an agent from a project
DELETE /api/projects/{id}/agents/{instanceId}

### Reset a project
POST /api/projects/{id}/reset
Clears project chat history, resets commander and captain session state (the
instances themselves are preserved), and stops + deletes all talon instances.
Use when a project needs a fresh start.

## Review Ops Tasks (draft-first, local stub runner)

### Create task
POST /api/review-tasks
Content-Type: application/json
Body:
- project_id (string, required)
- domain (string, required): currently "github"
- kind (string, required): triage_issue | review_pr | rereview_pr | respond_reviewer
- repo (string, required): owner/repo
- target_number (number, required): issue or PR number
- runner_kind (string, optional)

### List tasks
GET /api/review-tasks?project_id={id}

### Get task
GET /api/review-tasks/{id}

### Run task (stub + source context)
POST /api/review-tasks/{id}/run
Transitions task to running. For GitHub-domain tasks, fetches read-only source
context (issue/PR metadata + bounded comments) and persists a source_context
artifact. Then writes the local stub draft artifact and marks task draft_ready.
If the GitHub fetch fails, the draft is still created with a note about the
failure. No GitHub writes are performed.

### List task artifacts
GET /api/review-tasks/{id}/artifacts

## Agent Instances

### Create an agent instance
POST /api/instances
Content-Type: application/json

Body:
- name (string, required): slug like "researcher-riley"
- framework (string, required): "zeroclaw", "openclaw", "hermes", "picoclaw", or "embedded"
- persona_id (string): ID of a persona from the registry
- hierarchy_role (string): "commander", "captain", or "talon"
- project_id (string): assign to a project on creation
- model (string): override the persona's default model
- auto_start (bool): start the agent immediately after creation

### List all instances
GET /api/instances

### Get instance details
GET /api/instances/{id}

### Update an instance
PUT /api/instances/{id}
Body: {"name": "...", "display_name": "..."}

### Delete an instance
DELETE /api/instances/{id}

### Start / stop / restart an instance
POST /api/instances/{id}/start
POST /api/instances/{id}/stop
POST /api/instances/{id}/restart

### Migrate instance configs
POST /api/instances/migrate
Updates all provisioned instance configs to current defaults (autonomy level,
sandbox settings, allowed commands, tool iteration limits). Idempotent — safe
to call repeatedly. Returns per-instance results with applied changes.

## Hierarchy

### Get the full hierarchy tree
GET /api/hierarchy

Returns: commander, projects with their captains and talons.

### Set the commander
POST /api/hierarchy/commander
Body: {"instance_id": "..."} or {"agent_name": "..."}

## Registry

### List available personas
GET /api/registry/personas

Returns persona definitions with ID, name, role, description, preferred model, framework affinity, and traits.

### List available frameworks
GET /api/registry/frameworks

Returns framework definitions with ID, name, description, language, install method, requirements, config format, and default port.

## Agents (discovered)

These endpoints work with all agents — both provisioned instances and legacy discovered agents.

### List all agents
GET /api/agents

### Agent chat (streaming, SSE)
POST /api/agents/{name}/chat
Body: {"message": "...", "session_key": "..."}

Response: Server-Sent Events stream with delta, tool_start, tool_result, done, and error events.

### List sessions
GET /api/agents/{name}/sessions

### Create a session
POST /api/agents/{name}/sessions
Body: {"name": "session-name"}

### Get chat history
GET /api/agents/{name}/sessions/{session}/messages?limit=50

### Agent config
GET /api/agents/{name}/config
PUT /api/agents/{name}/config

### Agent logs (streaming, SSE)
GET /api/agents/{name}/logs

### Agent activity (streaming, SSE)
GET /api/agents/{name}/activity

### Agent lifecycle
POST /api/agents/{name}/start
POST /api/agents/{name}/stop
POST /api/agents/{name}/restart
`
