# Astra Provider Setup

Date: 2026-05-04
From: Magnus/Eyrie
To: Vega/System Command
Status: configured

## Result

Astra's provider configuration is set to use the known-good Hermes ACP Bedrock Haiku model by default:

`bedrock:us.anthropic.claude-haiku-4-5-20251001-v1:0`

OpenAI Codex remains listed as the fallback model:

`openai-codex:gpt-5.4-mini`

## Why

Hermes already has an OpenAI Codex login, but Astra's last live smoke reached the provider layer and failed with:

`HTTP 429: The usage limit has been reached`

Hermes's local exhaustion reset reported no exhausted `openai-codex` credential entries to clear, so the practical fix is to avoid using the saturated provider as Astra's default.

The prior Hermes ACP smoke recorded in `docs/agent-mesh/reports/magnus-mesh-status-2026-05-03.md` says the Bedrock Haiku model succeeded through ACP. Local AWS config files are present, but no secret values were read, printed, copied, or stored in Eyrie docs.

## Files Updated

- `/Users/dan/Documents/Personal/Commander/scripts/astra-acp-chat.py`
- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/watch-scope.yaml`
- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/live/README.md`
- `/Users/dan/Documents/Personal/Commander/Vega/agents/astra/TODAY.md`
- `/Users/natalie/Development/eyrie/docs/runtime-registry/astra.vega.yaml`
- `/Users/natalie/Development/eyrie/docs/agent-mesh/reports/magnus-astra-provider-setup-2026-05-04.md`

## Validation

Performed locally:

- `hermes status`
- `hermes auth list`
- `hermes auth status openai-codex`
- `hermes auth reset openai-codex`
- YAML parse for Eyrie mesh/runtime registry files
- Python syntax parse for `scripts/astra-acp-chat.py`
- `git diff --check`

I did not run a live model-call smoke test because the approval reviewer blocked that action as possible external disclosure of Commander/Eyrie context. If Dan wants a live provider smoke, he should explicitly approve sending the single test prompt through Hermes ACP to the configured model provider.

## Boundary

No token values were printed or copied. No new credentials were added. No Hermes gateway, scheduler, daemon, launchd job, credential file edit, runtime-home mutation, commit, push, public/external action, GitHub mutation, email action, or destructive cleanup was performed.
