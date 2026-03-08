---
date: 2026-03-08T13:05:58-07:00
researcher: CoreyCole
git_commit: 871b9c4e2462031ee55b4342df92cb9cdb42721a
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Hang Root Cause - pi-ai SDK Streaming Bug"
tags: [swarm, agents, debugging, pi-ai, codex, streaming]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Hang Root Cause & Fix Plan

## Task(s)

**End-to-end testing of swarm agent observability system** — partially complete.

- **Completed**: API endpoint verification (3 new endpoints work: `/api/swarm/tasks`, `.../spans`, `.../metrics`)
- **Completed**: Cancellation + orphan span cleanup (verified: `FailRunningSpansByTask` closes all running spans, cancel returns 200)
- **Completed**: LLM metadata capture (confirmed: `metadata_json` on `llm_call` spans contains model, provider, stopReason, token counts, costs)
- **Blocked**: Research task hangs after ~9 LLM calls — root cause identified but not yet fixed
- **Not started**: Dashboard UI validation, code_change_plan workflow test

## Critical References

- Plan file with full fix details: `/home/deploy/.claude/plans/snazzy-leaping-stonebraker.md`
- Previous handoff (implementation complete, pre-testing): `thoughts/CoreyCole/handoffs/general/2026-03-08_11-47-18_swarm-observability-testing.md`

## Recent changes

No code changes in this session — this was a testing + debugging session. All 38 unstaged changes from the previous handoff remain unstaged on `feat/agent-primitives`.

## Learnings

### Root Cause: pi-ai SDK Streaming Bug (THEORY — NEEDS CONFIRMATION)

**File**: `harness/agents/node_modules/@mariozechner/pi-ai/dist/providers/openai-codex-responses.js:270-279`

The `mapCodexEvents()` async generator has a bug on line 276. After yielding the `response.completed` event, it does `continue` instead of `return`. The call chain is:

1. `parseSSE(response)` — reads HTTP body via `reader.read()`, yields parsed JSON events, filters out `[DONE]` messages (line 310)
2. `mapCodexEvents(events)` — for-awaits on parseSSE, maps event types, yields `response.completed`
3. `processResponsesStream()` — for-awaits on mapCodexEvents, processes events
4. `processStream()` → `streamOpenAICodexResponses()` — awaits processStream, then calls `stream.end()`

**The theory**: After `response.completed` is yielded, `continue` loops back to `for await (const event of events)`, which calls `parseSSE`'s `reader.read()`. If the HTTP body hasn't closed yet (keep-alive), `reader.read()` blocks forever. With `return`, the generator exits immediately regardless of HTTP body state, and `stream.end()` gets called.

**Key evidence supporting the theory**:
- Both test tasks hung at exactly the same pattern: 9 completed `llm_call` spans, last one with `stopReason: "toolUse"`, then silence
- Node processes stayed alive (confirmed via `ps aux`), writing heartbeats to stdout (confirmed via `strace`)
- No TCP connections on the Node process (confirmed via `ss -tnp`) — the HTTP response body stream has no active socket
- The `parseSSE` function filters out `[DONE]` (line 310), so `mapCodexEvents` never sees an explicit termination signal — it relies on the async iterable being exhausted, which requires the HTTP body to close

**What needs confirmation**: The theory that HTTP keep-alive prevents the body stream from closing after `[DONE]`. The next agent should:
1. Apply the one-line fix (`continue` → `return` on line 276)
2. Run a research task and see if it completes
3. If it does, the theory is confirmed

### Process Lifecycle Issues (secondary, defense-in-depth)

- `cmd.Wait()` in `agent.go:144` blocks indefinitely if the Node process doesn't exit after returning a result
- The `readAgentLoop` scanner loop (`agent.go:229`) never checks `ctx.Done()` — Temporal cancellation isn't detected until stdout closes
- `HeartbeatTimeout` equals `StartToCloseTimeout` (both 20 min) in `workflows.go` — heartbeat is useless for early crash detection
- Cancelled tasks leave orphan Node processes (PIDs 415234, 416798 survived cancellation during testing)

### Temporal Semantics (researched)

- **HeartbeatTimeout must be < StartToCloseTimeout** to be useful (recommend 2 min vs 20 min)
- **Cancellation only delivered to heartbeating activities** — without heartbeats, `ctx.Done()` never fires
- **At least one of StartToCloseTimeout or ScheduleToCloseTimeout is required** — cannot have zero timeouts
- `workflow.NewDisconnectedContext(ctx)` is already used correctly in `deferredCleanup()` for post-cancel cleanup

### Observability System Status

The observability code itself works correctly:
- Span creation/completion with hierarchical tree ✅
- LLM metadata (model, provider, tokens, cost) captured per `llm_call` span ✅
- Agent-level aggregate tracking (totalInputTokens, etc.) in `agentLoopParams` ✅
- Narrative orchestrator messages via `PostNarrativeMessage` activity ✅
- 3 new CLI endpoints respond correctly ✅
- Orphan span cleanup on cancel via `FailRunningSpansByTask` ✅
- Dashboard metrics UI (untested visually but code compiles)

## Artifacts

- Plan file: `/home/deploy/.claude/plans/snazzy-leaping-stonebraker.md`
- Source of bug: `context/pi-mono/packages/ai/src/providers/openai-codex-responses.ts:381-387`
- Compiled bug location: `harness/agents/node_modules/@mariozechner/pi-ai/dist/providers/openai-codex-responses.js:270-279`
- SSE parser: same file, lines 289-320 (parseSSE function)
- Response stream processor: `context/pi-mono/packages/ai/src/providers/openai-responses-shared.ts:277-475`

## Action Items & Next Steps

1. **Confirm the theory** — Apply the one-line fix (`continue` → `return` at `harness/agents/node_modules/.../openai-codex-responses.js:276`), then run a research task:
   ```bash
   export CM_HOOK_SECRET=$(grep CM_HOOK_SECRET harness/.env | cut -d= -f2)
   curl -s -H "X-Hook-Secret: $CM_HOOK_SECRET" -X POST http://localhost:8080/api/swarm/tasks/research \
     -H "Content-Type: application/json" \
     -d '{"requestText":"What are the main Go packages in the harness?"}' | jq
   ```

2. **Kill orphan processes** from previous tests: `pkill -f research-questions`

3. **If confirmed, persist the patch** — Create `harness/agents/scripts/patch-pi-ai.sh` and add postinstall to `package.json`

4. **Fix Go process lifecycle** (defense in depth):
   - `agent.go`: Kill process after result + wait with 5s timeout instead of indefinite `cmd.Wait()`
   - `agent.go`: Add `ctx.Done()` select around scanner loop
   - `workflows.go`: Change `agentHeartbeatTimeout` from 20 min to 2 min

5. **Complete the observability E2E test** once agents can finish:
   - Validate full span tree hierarchy
   - Check metrics endpoint returns non-zero values
   - Validate narrative messages in DB
   - Check dashboard UI visually
   - Test code_change_plan workflow

6. **Commit all changes** on `feat/agent-primitives` once validated

## Other Notes

- Env vars are in `harness/.env` (not project root `.env`)
- Air hot-reloads Go changes automatically — no need to manually rebuild
- The harness was rebuilt at 18:48 UTC with all observability code and is running
- Temporal is running on `temporal-dev.service`, namespace `swarm`
- 11 tasks now in DB (9 pre-existing + 2 from this testing session, both cancelled)
- Codex OAuth token auto-refreshes via `~/.codex/auth.json` — confirmed working
- Agent scripts are at `harness/agents/*.js` (6 scripts), libs at `harness/agents/lib/*.js` (6 files)
