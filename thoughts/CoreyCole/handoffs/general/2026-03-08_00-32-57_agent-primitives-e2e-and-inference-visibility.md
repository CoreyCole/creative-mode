---
date: 2026-03-08T00:32:57-08:00
researcher: CoreyCole
git_commit: 3b1ae89467219c1f039f896b21a9c0e2f892d000
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives E2E Testing & Inference Visibility"
tags: [swarm, e2e-testing, inference-visibility, agent-primitives, codex]
status: in_progress
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives E2E Testing & LLM Inference Visibility

## Task(s)

### 1. Code review of feat/agent-primitives branch — COMPLETED
Full code review done via subagents. Key findings:
- Architecture is solid — Temporal workflows, JSONL agent protocol, span-based observability, Datastar SSE dashboard all wire together correctly
- No XSS vulnerabilities (templ auto-escapes)
- Notable issues (non-blocking): heartbeat timeout = start-to-close timeout (both 20m) defeats stall detection; SSE auto-selects first active task overriding user selection; 5 DB queries per SSE event

### 2. E2E testing research primitive — IN PROGRESS
- Successfully started research workflows via `POST /api/swarm/tasks/research` (field is `requestText` camelCase)
- Verified dashboard renders: sidebar with task list, all 4 tabs (Chat, Agents, Spans, Artifacts), span tree hierarchy, chat message building from spans
- **Problem discovered**: During long LLM inference pauses (Codex gpt-5.3-codex), the dashboard shows NO activity — no spans, no status, nothing between tool calls. The agent only forwards `tool_execution_start/end` events to Go, ignoring `message_start/end` and `turn_start/end` from pi-agent-core.

### 3. Adding inference visibility — IN PROGRESS (partially implemented, needs rethinking)
Added `inference_start`/`inference_end` events using pi-agent-core's `message_start`/`message_end` (assistant role). Changes in 3 files but **not yet verified working** — debug logging added but events still not appearing in span tree. The issue may be deeper (Codex WebSocket streaming flow, event ordering, etc.)

**User feedback**: This approach may be overcomplicated. Should investigate how `context/openclaw` handles Codex CLI auth to make requests with the Codex CLI pro subscription token, rather than debugging the pi-ai library's event pipeline.

### 4. E2E testing code_change_plan primitive — NOT STARTED

## Critical References
- `thoughts/CoreyCole/handoffs/general/2026-03-08_00-10-46_agent-primitives-branch-review.md` — Previous handoff with full branch review
- `harness/CLAUDE.md` — Swarm API routes, dashboard docs, dev login testing
- `context/openclaw/` — OpenClaw reference code (should be checked for Codex auth patterns)

## Recent changes

**Modified files (uncommitted on `feat/agent-primitives`):**

- `harness/agents/lib/agent-factory.js` — Added `message_start`/`message_end` event forwarding as `inference_start`/`inference_end`. Also added stderr debug logging (`process.stderr.write`) for all event types.
- `harness/internal/swarmorch/agent.go` — Added `inference` span handling in `handleToolEvent` (now returns `inferenceSpan *toolSpanInfo`). Added debug slog for every agent message received. Changed `handleToolEvent` signature from `(int, error)` to `(int, *toolSpanInfo, error)`.
- `harness/views/swarm/dashboard.templ` — Added `inference` span type to `spanIcon` (merged with `llm_call`), added `inference` case to `spansToChatMessages` (shows "Thinking..." when running, "Thought complete (Xms)" when done)
- `harness/views/swarm/dashboard_templ.go` — Auto-generated from templ

## Learnings

### Swarm API details
- Route: `POST /api/swarm/tasks/research` and `POST /api/swarm/tasks/code-change-plan`
- Auth: `X-Hook-Secret` header matching `CM_HOOK_SECRET` env var
- Request body: `{"requestText": "..."}` (camelCase, not snake_case)
- Response: `{"taskID":"abc12345","workflowID":"swarm-research-abc12345"}` (202)
- Task status: `GET /api/swarm/tasks/:taskID` with same auth header

### pi-agent-core event types available
From `@mariozechner/pi-agent-core/dist/types.d.ts`: `agent_start`, `agent_end`, `turn_start`, `turn_end`, `message_start`, `message_update`, `message_end`, `tool_execution_start`, `tool_execution_update`, `tool_execution_end`. Currently only `tool_execution_start/end` are forwarded to Go.

### Event flow for a turn
`turn_start` → LLM API call → `message_start` (streaming begins) → `message_update` × N → `message_end` → `tool_execution_start` × N → `tool_execution_end` × N → `turn_end`. The `message_start/end` bracket the LLM inference, tool execution happens after.

### Model details
- Agent uses `getModel('openai-codex', 'gpt-5.3-codex')` via `@mariozechner/pi-ai`
- Provider: `azure-openai-responses` (uses WebSocket streaming)
- Auth: Codex OAuth token via `harness/agents/lib/codex-auth.js` → `codex-login.js`
- LLM calls are very slow (3+ minutes with no visible progress)

### Air doesn't watch JS files
`.air.toml` only watches `.go`, `.templ`, `.css` — JS agent changes take effect on next subprocess spawn (new task), not air reload. BUT the Go binary needs rebuild for Go changes, which air handles.

### Dashboard testing
- Dev login: `POST /dev/auth/login -d 'username=test&role=admin'` (form data)
- playwright-cli is installed on VPS (`/home/deploy/.npm-global/bin/playwright-cli`)
- Commands: `open`, `goto`, `snapshot`, `screenshot`, `click`, `fill`, `run-code` (NOT `navigate`)
- Must use `run-code` with `page.evaluate(fetch(...))` for Datastar POST endpoints

### Agent stderr not visible
Go runner captures stderr to `strings.Builder` but never logs it — stderr debug output from agents is invisible in journalctl. Only surfaces in error messages when the process exits non-zero.

## Artifacts

### Code changes (uncommitted)
- `harness/agents/lib/agent-factory.js:48-67` — Event forwarding + debug logging
- `harness/internal/swarmorch/agent.go:190-193` — `inferenceSpan` tracking variable
- `harness/internal/swarmorch/agent.go:219-228` — Updated result handling to close inference span
- `harness/internal/swarmorch/agent.go:246-310` — Updated `handleToolEvent` with inference span support
- `harness/views/swarm/dashboard.templ:487-504` — `inference` case in `spansToChatMessages`
- `harness/views/swarm/dashboard.templ:616` — `inference` in `spanIcon`

### Screenshots taken (in `.playwright-cli/`)
- Dashboard chat tab with running research task
- Dashboard spans tab showing hierarchical span tree
- Dashboard agents tab showing agent activity
- Dashboard artifacts tab (empty — "No artifacts yet")

## Action Items & Next Steps

1. **Investigate `context/openclaw/` for Codex auth patterns** — The user wants to understand how OpenClaw handles Codex CLI auth and uses the pro subscription token for API requests. This might simplify the agent infrastructure significantly compared to the current pi-ai library approach.

2. **Decide: keep or revert inference visibility changes** — The 3 modified files add inference span tracking but it's unverified. Either debug why `message_start` events aren't reaching Go (could be Codex WebSocket stream timing), or revert and take a simpler approach.

3. **Make agent stderr visible** — Quick win: pipe agent stderr to slog in `agent.go` instead of capturing to a silent `strings.Builder`. This would make all agent debug output visible in journalctl.

4. **E2E test code_change_plan primitive** — Not yet tested. Same API pattern but `POST /api/swarm/tasks/code-change-plan`. Has additional stages: classify domains → parallel specialist planners → synthesize plan.

5. **Let a research workflow run to completion** — All test workflows were cancelled early due to slow Codex LLM. Need to let one complete fully to verify the full pipeline: questions → parallel research → synthesis → artifact → dashboard display.

## Other Notes

### Useful commands for testing
```bash
# Start research task
curl -s -X POST http://localhost:8080/api/swarm/tasks/research \
  -H "X-Hook-Secret: $(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)" \
  -H "Content-Type: application/json" \
  -d '{"requestText":"your question here"}'

# Check task status
curl -s http://localhost:8080/api/swarm/tasks/<TASK_ID> \
  -H "X-Hook-Secret: $(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)" | python3 -m json.tool

# Cancel task
curl -s -X POST http://localhost:8080/api/swarm/tasks/<TASK_ID>/cancel \
  -H "X-Hook-Secret: $(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)"

# Check Temporal workflow
temporal workflow describe --workflow-id swarm-research-<TASK_ID> --namespace swarm

# Playwright dashboard testing
playwright-cli open http://localhost:8080 --persistent
playwright-cli run-code "async page => { await page.evaluate(async () => { await fetch('/dev/auth/login', {method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded'}, body: 'username=test&role=admin', redirect: 'manual'}); }); await page.goto('http://localhost:8080/swarm'); }"
```

### Running tasks to cancel
There may be a running research task `7c3845fd` that should be cancelled if still alive.
