---
date: 2026-03-08T06:57:53+00:00
researcher: CoreyCole
git_commit: e49d41e970eb8416f05561b8435898b74335f4f4
branch: feat/agent-primitives
repository: creative-mode
topic: "Codex Subscription Auth & Temporal Workflow Fixes"
tags: [implementation, temporal, swarm, codex, oauth, agent-primitives]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Codex Subscription Auth & Temporal Workflow Fixes

## Task(s)

**Completed:** Resumed from `thoughts/CoreyCole/handoffs/general/2026-03-07_22-15-42_temporal-best-practices-review.md` which had implemented Temporal best practices improvements. That handoff's action items were to test the workflows end-to-end. Testing revealed three blocking issues which were all fixed:

1. **DBActivities panic (fixed)** — `RegisterActivity(&DBActivities{...})` panicked because `*sqlc.Queries` has a `WithTx()` method whose return signature `(*Queries)` doesn't match Temporal's expected activity pattern `(T, error)`. The handoff's learning that "Temporal only registers methods matching the activity signature" was incorrect — it validates ALL methods and panics on non-conforming ones.

2. **Missing DB tables (fixed)** — `swarm_tasks`, `swarm_artifacts`, `swarm_spans` tables didn't exist despite migration 006 being marked as applied in `_migrations`. Tables were created manually. Root cause unknown — likely dropped by a previous migration or schema change.

3. **OpenAI API key missing (fixed by switching to Codex sub)** — Agent scripts used `getModel('openai', 'gpt-5.3-codex')` requiring `OPENAI_API_KEY`. Switched to `getModel('openai-codex', 'gpt-5.3-codex')` which uses the Codex subscription via OAuth tokens from `~/.codex/auth.json` (shared with the Codex CLI already installed on the VPS).

4. **Heartbeat timeouts during LLM reasoning (fixed)** — Codex model does extended reasoning (5-10+ min) with no stdout output, causing Temporal heartbeat timeout. Added 15s periodic heartbeat from JS agents and increased heartbeat timeout to 10 minutes.

**In progress:** A research workflow (`827c886d`) was kicked off and running at time of handoff. The Temporal infrastructure, Codex auth, and heartbeat keepalive are all working. The agent successfully authenticates, makes tool calls, and explores the codebase. The workflow may complete or may hit the `agentStartToCloseTimeout` (10 min) if Codex reasoning takes too long on any single agent invocation.

## Critical References

- Previous handoff: `thoughts/CoreyCole/handoffs/general/2026-03-07_22-15-42_temporal-best-practices-review.md`
- Codex CLI auth file: `~/.codex/auth.json` (contains `tokens.access_token`, `tokens.refresh_token`, `tokens.account_id`, `last_refresh`)
- pi-mono Codex OAuth module: `harness/agents/node_modules/@mariozechner/pi-ai/dist/utils/oauth/openai-codex.js`

## Recent changes

- `harness/internal/swarmorch/activities.go:21-26` — Changed `DBActivities` to use unexported `Queries *sqlc.Queries` field (not embedded) with comment explaining why embedding panics
- `harness/internal/swarmorch/manager.go:67` — Removed `w.RegisterActivity(&DBActivities{...})` line
- `harness/internal/swarmorch/workflows.go:16` — Changed `agentHeartbeatTimeout` from 2min → 10min
- `harness/agents/lib/agent-factory.js:7` — Added `import { getCodexAccessToken } from './codex-auth.js'`
- `harness/agents/lib/agent-factory.js:31` — Changed `getModel('openai', ...)` to `getModel('openai-codex', 'gpt-5.3-codex')`
- `harness/agents/lib/agent-factory.js:43-45` — Changed `new Agent()` to `new Agent({ getApiKey: () => getCodexAccessToken() })`
- `harness/agents/lib/agent-factory.js:55` — Added `startHeartbeat()` call before `agent.prompt()`
- `harness/agents/lib/codex-auth.js` — New file: reads tokens from `~/.codex/auth.json`, auto-refreshes via `refreshOpenAICodexToken()`, writes refreshed tokens back
- `harness/agents/lib/protocol.js:41-57` — Added `startHeartbeat()`/`stopHeartbeat()` functions (15s interval, `.unref()`)
- `harness/agents/lib/codex-login.js` — New file: interactive PKCE OAuth login script (backup for re-auth)

## Learnings

- **Temporal `RegisterActivity` validates ALL methods on a struct**, not just those matching the activity signature. `WithTx() *Queries` causes a panic because the return type `*Queries` is not `error`. Cannot embed `*sqlc.Queries` as a Temporal activity struct.
- **pi-mono `openai-codex` provider** uses the ChatGPT backend (`chatgpt.com/backend-api/codex/responses`) with JWT OAuth tokens, NOT the standard OpenAI API. The `getEnvApiKey()` function has no entry for `openai-codex` — tokens must be passed via `Agent({ getApiKey })` callback.
- **OpenClaw and Codex CLI share auth** at `~/.codex/auth.json`. Tokens have ~1 hour lifetime (inferred from `last_refresh`). The `refreshOpenAICodexToken()` from `@mariozechner/pi-ai` handles refresh using the `refresh_token`.
- **Codex model reasoning takes 5-10+ minutes** on complex codebase questions. The 2-minute heartbeat timeout was far too short. Even 5 minutes wasn't enough. 10 minutes with 15s heartbeat interval works.
- **Agent heartbeats fire on `scanner.Scan()`** in Go — every line read from stdout triggers `activity.RecordHeartbeat()`. The JS heartbeat timer writes `{"type":"heartbeat"}` lines that the Go side reads (the unknown message type falls through the switch silently, but the `scanner.Scan()` triggers the heartbeat).
- **`agentStartToCloseTimeout` (10 min)** may still be tight for full research workflows where each agent does extended reasoning. Consider increasing to 15-20 min if agents keep timing out.
- **Migration 006 tables disappeared** despite being marked as applied. The `swarm_tasks`, `swarm_artifacts`, `swarm_spans` tables needed to be recreated manually. The `code_change_plan` value was added to the `artifact_type` CHECK constraint (not in the original migration).

## Artifacts

- `harness/agents/lib/codex-auth.js` — Codex OAuth token reader/refresher
- `harness/agents/lib/codex-login.js` — Interactive PKCE OAuth login (for re-auth if tokens expire fully)
- `harness/agents/lib/protocol.js:41-57` — Heartbeat timer implementation
- `harness/agents/lib/agent-factory.js` — Updated to use Codex sub + heartbeat

## Action Items & Next Steps

1. **Monitor workflow `827c886d`** — Check if it completes successfully. If it times out, increase `agentStartToCloseTimeout` from 10min to 20min in `workflows.go:15`.

2. **Increase `agentStartToCloseTimeout`** — 10 minutes may not be enough for multi-step research workflows where each agent does extended Codex reasoning. Consider 20-30 minutes.

3. **Handle `fd` not found** — The `find` tool in `pi-coding-agent` tries to use `fd` which isn't installed on the VPS. This causes tool calls to fail with "fd is not available and could not be downloaded". Either install `fd` via Nix or configure the agent tools differently.

4. **Skills directory path mismatch** — The `search_context` tool returns skill names like `api-conventions.md` and `project-structure.md`, but the agent then tries to read them from the repo root instead of `harness/agents/skills/`. The skill files exist at `harness/agents/skills/` but the `read` tool resolves relative to `cwd` (repo root).

5. **Clean up orphaned spans** — The first two test runs (`ed6c9663`, `2cd50547`, `16f7fd06`) left orphaned "running" agent spans in the DB. The `CleanupOrphanedSpans` query runs on startup but failed earlier because `swarm_spans` didn't exist yet.

6. **Cancellation test** — Still untested from the original handoff. Verify that `POST /api/swarm/tasks/:taskID/cancel` correctly sets task status to `"canceled"` (not `"failed"`) via the `deferredCleanup` helper.

7. **Consider token refresh error handling** — If the Codex refresh token fully expires (e.g., after weeks of inactivity), the agent will crash. Consider catching this and logging a clear error message directing to `codex auth login`.

## Other Notes

- **Swarm API routes**: `POST /api/swarm/tasks/research`, `POST /api/swarm/tasks/code-change-plan`, `GET /api/swarm/tasks/:taskID`, `POST /api/swarm/tasks/:taskID/cancel` — all behind `hookSecretMiddleware`.
- **Test commands**:
  ```bash
  # Start research workflow
  curl -X POST http://localhost:8080/api/swarm/tasks/research \
    -H "X-Hook-Secret: $CM_HOOK_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"requestText":"How does EventBus work?"}'

  # Check task status
  curl http://localhost:8080/api/swarm/tasks/<taskID> -H "X-Hook-Secret: $CM_HOOK_SECRET" | jq '.task.Status'

  # Test agent directly (bypasses Temporal)
  echo '{"type":"start","task":{"taskID":"test","requestText":"...","repoRoot":"/home/deploy/creative-mode","maxQuestions":3}}' | \
    NODE_NO_WARNINGS=1 node harness/agents/research-questions.js
  ```
- **Temporal dashboard**: `http://localhost:8233` — shows workflow executions, can inspect heartbeats and retries
- **Codex token check**: `cat ~/.codex/auth.json | jq '{auth_mode, last_refresh, tokens: (.tokens | keys)}'`
- **The `codex-login.js` script** runs the same PKCE flow that OpenClaw uses. On a headless VPS, it prints an auth URL, you open it in your browser, and paste the redirect URL back. This is only needed if the refresh token fully expires.
