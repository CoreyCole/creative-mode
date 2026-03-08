---
date: 2026-03-08T11:47:18-07:00
researcher: CoreyCole
git_commit: 8834248e13c0cfc8f8adb01c09af167405136ab8
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Observability System - Implementation Complete, Ready for Testing"
tags: [swarm, observability, tokens, metrics, testing]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Observability System - Test & Validate

## Task(s)

**Completed: Full implementation of swarm observability system (7 changes)**

All code changes are implemented, lint passes, build succeeds. The system needs end-to-end testing by running actual research and code change plan workflows.

## Critical References

- Implementation plan (read in plan mode): `thoughts/` — plan was discussed in conversation, not persisted as a file
- CLAUDE.md swarm section: project root `CLAUDE.md` (search "Swarm" section for architecture)
- Memory notes on swarm operations: `/home/deploy/.claude/projects/-home-deploy-creative-mode/memory/MEMORY.md`

## Recent changes

All changes are unstaged on branch `feat/agent-primitives`:

1. **`harness/agents/lib/agent-factory.js:54-58`** — `inference_start` now sends `{model}`, `inference_end` sends `{model, provider, stopReason, usage}` from pi-ai SDK's `message_end` event

2. **`harness/internal/swarmorch/types.go:143-178`** — Added `LLMUsage`, `LLMCost`, `SpanMetadata` types for per-call and aggregate metadata

3. **`harness/internal/db/queries/swarm.sql:59-70`** — Added 3 new queries: `CompleteSwarmSpanWithMetadata`, `FailSwarmSpanWithMetadata`, `FailRunningSpansByTask`

4. **`harness/internal/swarmorch/agent.go`** — Major changes:
   - `agentLoopParams` now tracks running totals (tokens, cost, tool/LLM call counts) — `:173-178`
   - `readAgentLoop` accepts `*agentLoopParams` (pointer for mutation) — `:198`
   - `handleToolEvent` accepts `*agentLoopParams`, increments `p.toolCallCount` on tool start — `:291`
   - `AgentEventInferenceStart`: extracts model name from data, uses as span name — `:330-345`
   - `AgentEventInferenceEnd`: parses `SpanMetadata`, accumulates token totals, calls `completeSpanWithMetadata` — `:354-381`
   - `completeSpanWithMetadata()` helper — `:585-628`
   - `failSpanWithMetadata()` helper — stores stderr + aggregates in metadata — `:631-668`
   - Agent span completion writes aggregate `SpanMetadata` — `:179-186`

5. **`harness/internal/swarmorch/activities.go`** — Added `PostNarrativeMessage` (inserts orchestrator message + publishes to EventBus) and `FailRunningSpansByTask` activities

6. **`harness/internal/swarmorch/workflows.go`** — Added narrative messages at 5 stage transitions:
   - After question generation: "Decomposed into N research questions..."
   - After all research agents: "All N research agents finished..."
   - After domain classification: "Identified N specialist domains..."
   - Research complete: "Research complete. Document written to ..."
   - Code plan complete: "Code change plan complete. Document written to ..."
   - `deferredCleanup` now calls `FailRunningSpansByTask` to close orphaned spans

7. **`harness/internal/server/swarm_api.go`** — 3 new CLI-friendly endpoints:
   - `GET /api/swarm/tasks` — list all tasks
   - `GET /api/swarm/tasks/:taskID/spans` — full span tree
   - `GET /api/swarm/tasks/:taskID/metrics` — aggregated metrics

8. **`harness/views/swarm/dashboard.templ`** — Dashboard enhancements:
   - `computeTaskMetrics()` / `parseSpanMetadata()` / `formatTokens()` / `formatCost()` helpers
   - `taskMetricsBar` component: aggregate bar in task header
   - `SpanRow`: token badges for llm_call and agent spans
   - `AgentSpanCard`: aggregate tokens, cost, call count
   - Chat tab: llm_call messages include token count

9. **`harness/internal/server/server.go:165-170`** — Registered 3 new routes

## Learnings

- The pi-ai SDK provides full `Usage` data on every `message_end` event with fields: `input`, `output`, `cacheRead`, `cacheWrite`, `totalTokens`, `cost` (object with `input`, `output`, `cacheRead`, `cacheWrite`, `total`)
- `metadata_json` column already existed on `swarm_spans` and was included in `CreateSwarmSpan` INSERT — just never populated
- The `readAgentLoop` needed to be changed to accept a pointer (`*agentLoopParams`) so aggregates could be mutated during the loop
- Linter requires early-return patterns (`if !c { break }` instead of `if c { ... }`) and string concatenation instead of `fmt.Sprintf` for simple cases
- `just vps-build` from harness/ is the correct build command on VPS (not direct `go build` or `templ generate`)
- `./scripts/check.sh` runs the full lint suite

## Artifacts

- `harness/agents/lib/agent-factory.js` — JS agent token forwarding
- `harness/internal/swarmorch/types.go:143-178` — SpanMetadata types
- `harness/internal/db/queries/swarm.sql:59-70` — New SQL queries
- `harness/internal/db/sqlc/swarm.sql.go` — Generated (sqlc)
- `harness/internal/swarmorch/agent.go` — Core metadata tracking
- `harness/internal/swarmorch/activities.go` — New activities
- `harness/internal/swarmorch/workflows.go` — Narrative messages + orphan cleanup
- `harness/internal/server/swarm_api.go` — CLI endpoints
- `harness/internal/server/server.go` — Route registration
- `harness/views/swarm/dashboard.templ` — Dashboard metrics UI
- `harness/views/swarm/dashboard_templ.go` — Generated (templ)

## Action Items & Next Steps

1. **Restart the harness service** to pick up changes: `sudo systemctl restart creative-mode`

2. **Run a research task** to test the full pipeline:
   ```bash
   curl -s -H "X-Hook-Secret: $CM_HOOK_SECRET" -X POST http://localhost:8080/api/swarm/tasks/research \
     -H "Content-Type: application/json" \
     -d '{"requestText":"How does the checkpoint system work in creative-mode?"}' | jq
   ```

3. **Monitor with new CLI endpoints**:
   ```bash
   # List tasks
   curl -s -H "X-Hook-Secret: $CM_HOOK_SECRET" http://localhost:8080/api/swarm/tasks | jq
   # Get spans for a task
   curl -s -H "X-Hook-Secret: $CM_HOOK_SECRET" http://localhost:8080/api/swarm/tasks/<ID>/spans | jq
   # Get metrics
   curl -s -H "X-Hook-Secret: $CM_HOOK_SECRET" http://localhost:8080/api/swarm/tasks/<ID>/metrics | jq
   ```

4. **Check the dashboard** at `/swarm` — verify:
   - Task header shows aggregate metrics bar (agents, tools, LLM calls, tokens, cost, duration)
   - Spans tab shows token badges on llm_call and agent spans
   - Chat tab shows orchestrator narrative messages at stage transitions
   - Chat tab shows token counts on "Thought complete" messages

5. **Test failure/cancel scenarios**:
   - Cancel a running task and verify all spans close (no more stuck "running")
   - Check stderr appears in metadata when an agent fails

6. **Verify DB storage**:
   ```bash
   sqlite3 data/creative-mode.db "SELECT metadata_json FROM swarm_spans WHERE span_type='llm_call' AND metadata_json IS NOT NULL LIMIT 1;"
   ```

7. **Run a code_change_plan task** to test the specialist domain narrative messages

8. **Commit changes** once validated

## Other Notes

- The `CM_HOOK_SECRET` value is in `.env` — needed for all `/api/swarm/*` endpoints
- Temporal must be running: `sudo systemctl status temporal-dev`
- `CM_SWARM_TEMPORAL=true` must be set in `.env`
- Swarm workflows use OpenAI Codex (`gpt-5.3-codex`) via pi-ai SDK — token costs will reflect that model's pricing
- The `HARNESS_HOOK_URL` must be `http://localhost:8080` (not Tailscale URL) for Claude Code hooks
- All changes are unstaged — nothing committed yet
