---
date: 2026-03-07T18:16:52-08:00
reviewer: Claude (Staff Eng Review)
git_commit: b06cdd7658f62e68307e979258be6a2b5066784c
branch: feat/agent-primitives
repository: creative-mode
plan_reviewed: thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md
status: complete
type: plan_review
---

# Plan Review: Agent Primitives v3 — Conversational Agents on Temporal + Pi-Mono

### Summary

This is a well-structured plan with clear scope boundaries and solid architectural decisions (Temporal as scheduler not state machine, hierarchical spans, compression at boundaries). The Langfuse-inspired observability redesign is a significant improvement over the flat event model. However, three areas need attention before implementation: the `answerQuestion` mechanism is underspecified pseudocode masquerading as a design, the EventBus can silently drop span events under parallel agent load, and orphaned child spans have no cleanup path on agent crash.

### Critical Issues (Must Address Before Implementation)

1. **`answerQuestion` is pseudocode, not a design**

   - Problem: The plan's `answerQuestion` Go method (lines ~218-240) contains `findRelevantSkills(question)` and `findRelevantFiles(question)` — these are opaque function calls that hide the hardest part of the system. Keyword extraction from natural language questions, mapping keywords to file paths, and scoring skill file relevance are each non-trivial NLP/search problems. The staff summary correctly flags this as "the weakest link" but the plan still ships with pseudocode.
   - Risk: Without a concrete v1 implementation, the first implementer will either build something too simple (exact string match — fails on paraphrased questions) or too complex (embedding-based search — scope creep). Either way, agent quality depends entirely on this function.
   - Suggestion: Define a concrete v1 algorithm. Recommendation: (1) extract nouns/paths from the question via regex (e.g., match `foo/bar.go`, `EventBus`, `migration`), (2) `grep -rl` those terms in `harness/agents/skills/` to find matching skills, (3) `grep -rl` in `harness/` to find matching source files, (4) read and concatenate top-N results. This is ~50 lines of Go, not a research project. Write the algorithm in the plan, not just the signature.

2. **EventBus will silently drop span events during parallel agent runs**

   - Problem: `EventBus.Publish()` uses non-blocking sends with a 100-event channel buffer (`events/bus.go:98-103`). A ResearchWorkflow with 5 parallel agents, each making 20+ tool calls, generates 200+ span events (start + end) in seconds. If the SSE handler experiences any latency (GC pause, TCP backpressure, slow Datastar template rendering), events will be silently dropped.
   - Risk: A dropped `span.completed` event means a span row stays permanently "running" in the dashboard UI — the SSE handler never receives the completion to replace the row. The mayor dashboard avoids this because its event volume is 1-2 events/minute (builds, messages), not 10+ events/second. The plan's granular per-span patching strategy (individual `PatchElementTempl` per span) is particularly vulnerable because each dropped event leaves a visible artifact.
   - Suggestion: Three options (pick one):
     - **Option A (Recommended)**: Adopt the mayor dashboard's fat-morph pattern — on ANY swarm event, re-query `GetSwarmSpansByTask` from DB and replace the entire span tree container. Simpler, self-healing (next event corrects any previously dropped state), proven pattern.
     - **Option B**: Increase EventBus buffer to 1000 for the "swarm" topic (requires making buffer size configurable per-topic).
     - **Option C**: Add a periodic SSE "full state sync" every 5 seconds — re-query DB and replace the full tree, with incremental patches between syncs. More complex but handles both drop recovery and browser refresh.

3. **Orphaned child spans on agent crash**

   - Problem: If a Node.js agent crashes mid-execution (OOM, unhandled Promise rejection, Go context cancellation via `exec.CommandContext`), the `activeToolSpans` map in `runAgent()` (line ~997) holds spanIDs for in-progress tool calls that will never receive `tool_execution_end`. The plan's error path (line ~1038) only calls `failSpan(ctx, agentSpanID, ...)` on the top-level agent span — child tool and question spans remain `status='running'` in `swarm_spans` forever.
   - Risk: Dashboard shows permanently "running" child spans. Over time, the span tree accumulates ghost entries. Queries for "all running spans" become unreliable.
   - Suggestion: In the error path of `runAgent()`, after `failSpan(agentSpanID)`, add: `for _, spanID := range activeToolSpans { a.failSpan(ctx, spanID, "parent agent crashed") }`. Also add a startup cleanup query: `UPDATE swarm_spans SET status='failed', error_message='orphaned' WHERE status='running' AND ended_at IS NULL AND started_at < datetime('now', '-15 minutes')`.

### Concerns (Should Address)

1. **`computeDuration` has no implementation path**

   - Observation: `completeSpan` (line ~1056-1070) calls `computeDuration(spanID, endedAt)` but the plan never shows how it retrieves the span's `started_at`. The `activeToolSpans` map only stores `toolCallId → spanID`, not timestamps. Options: (a) query DB for `started_at` (extra read per span completion), (b) extend `activeToolSpans` to store `{spanID, startedAt}`, (c) maintain a separate `spanStartTimes` map.
   - Suggestion: Use option (b) — change `activeToolSpans` from `map[string]string` to `map[string]spanEntry` where `spanEntry` is `{spanID string, startedAt time.Time}`. Avoids an extra DB query per tool call completion.

2. **`cmd.Env` strips inherited environment — `OPENAI_API_KEY` won't propagate**

   - Observation: The plan says `cmd.Env = env` (line ~1342, ~1364 in `DirectRunner.BuildCommand`). When you set `cmd.Env` in Go, it **replaces** the inherited environment entirely — it does NOT merge. If `env` only contains the values you explicitly pass, `OPENAI_API_KEY`, `PATH`, `HOME`, and other required vars will be missing. The plan never shows where `env` is constructed.
   - Suggestion: Either (a) set `cmd.Env = append(os.Environ(), "EXTRA_VAR=value")` to inherit everything, or (b) explicitly construct the full env: `cmd.Env = []string{"OPENAI_API_KEY=" + ..., "NODE_PATH=" + ..., "HOME=" + ..., "PATH=" + ...}`. For `DirectRunner`, option (a) is appropriate since these are trusted agents on our server.

3. **Divergence from established fat-morph SSE pattern**

   - Observation: The mayor dashboard SSE handler (`mayor_dashboard.go:74-117`) re-queries the entire DB and replaces full tab contents on every event — a proven pattern. The swarm plan proposes granular per-span patching with individual element IDs (`#span-{id}`). This is architecturally different and creates a new pattern in the codebase that's harder to debug and more fragile (depends on correct element IDs, append vs. replace, depth computation).
   - Suggestion: Start with fat-morph (re-render entire span tree on any event). Optimize to per-span patching later only if performance requires it. The span tree query via recursive CTE will be fast for <1000 spans per task.

4. **`readLine` in protocol.js doesn't handle stdin closure**

   - Observation: `readLine()` returns `new Promise(resolve => rl.once('line', resolve))`. If Go crashes or cancels the context while the agent is blocking on `readLine` (waiting for `ask_orchestrator` answer), stdin closes, `readline` emits 'close' but never emits 'line', and the Promise hangs forever. The Node.js process stays alive until the 10-minute context timeout kills it.
   - Suggestion: Add a 'close' handler: `return new Promise((resolve, reject) => { rl.once('line', resolve); rl.once('close', () => reject(new Error('stdin closed'))); })`. This makes agent crash recovery faster (immediate exit vs. 10-minute wait).

5. **No JSONL log rotation or cleanup**

   - Observation: Agent subprocess stdout gets logged to `{dataDir}/logs/` as JSONL files. Each agent generates a log file with every tool call's full input/output (before truncation). With 5 agents per task and multiple tasks per day, these files grow indefinitely. No cleanup mechanism, rotation, or size limits are mentioned.
   - Suggestion: Add a log cleanup strategy in Phase 6 notes at minimum. Quick v1: delete logs older than 7 days via a startup goroutine (same pattern as session cleanup in `main.go:108-122`).

6. **`swarm_spans.duration_ms` is INTEGER but computed from `started_at` TEXT**

   - Observation: `started_at` is `TEXT NOT NULL DEFAULT (datetime('now'))`, which stores second-precision timestamps like `2026-03-08 02:14:32`. Computing `duration_ms` from this gives at best second-precision durations — tool calls that take <1 second will all show `duration_ms=0`. Millisecond-precision tool call timing (the key observability metric) is lost.
   - Suggestion: Change `started_at` default to `(strftime('%Y-%m-%dT%H:%M:%f', 'now'))` which gives millisecond precision (e.g., `2026-03-08T02:14:32.456`). Or use Go's `time.Now()` to set `started_at` and `ended_at` in code rather than relying on SQLite defaults, and compute `duration_ms` in Go.

### Questions (Need Clarification)

1. How will the `/swarm` dashboard authenticate? The plan shows it under "approved users" (`approved.GET("/swarm", ...)`) but doesn't mention whether all approved users should see all tasks, or if there's per-user filtering. Currently the codebase has `admin` vs `approved` roles — should swarm be admin-only?

2. The plan puts agent scripts at `harness/agents/` with its own `package.json`. This creates a second Node.js project inside `harness/` (alongside `harness/package.json` for Tailwind). How will `npm install` be coordinated? Will `just generate` or the build pipeline need to run `cd harness/agents && npm install`?

3. The plan says "symlink or copy `node_modules`" from `/opt/openclaw/`. Which approach will be used? Symlinking is fragile (breaks if OpenClaw is upgraded), copying wastes disk and needs to be re-done on updates. A third option: `package.json` with file-path dependencies (`"@mariozechner/pi-ai": "file:/opt/openclaw/node_modules/@mariozechner/pi-ai"`) — keeps version tracking explicit.

4. When `CodeChangePlanWorkflow` runs `ResearchWorkflow` as a child workflow (line ~570-574), does the child workflow create its own `workflow` span? If so, the span tree for a code-plan task will have a `workflow` → `stage` → `workflow` nesting. Is this the intended hierarchy, or should child workflow spans be of type `stage`?

### Suggestions (Nice to Have)

1. **Add a `span_count` column to `swarm_tasks`** — incrementing on each `createSpan` call. This provides a cheap "is this task active?" check for the task list page without querying `swarm_spans`.

2. **Consider `data-show` for tree view expand/collapse** instead of `<details>` — native `<details>` elements can't be controlled by SSE. A `data-show="$expand_span_{id}"` signal toggle would let SSE handlers expand/collapse nodes programmatically (e.g., auto-expand the currently-running agent).

3. **Add agent script name to span metadata** — store the full script path (e.g., `research-agent.js`) in `metadata_json` on agent spans. This makes it easy to distinguish "which agent is slow" in analytics queries without parsing the `name` field.

4. **Consider making `data` the JSONL field name for tool results** — The plan currently uses `data` for both tool args and results (renamed from `args` in the protocol). Using `args` for tool_execution_start and `result` for tool_execution_end (matching pi-mono's actual field names: `args` and `result`) would be clearer and avoid the rename confusion.

### What's Good

- **Scope discipline is excellent** — "What We're NOT Doing" section is clear and comprehensive. The explicit decision to skip Primitives 2.1-7, code application, PR generation, and gate reviews prevents the scope creep that killed v1.
- **Temporal as durable scheduler, not state machine** — learning from the v1 failure where non-deterministic replay bugs caused issues. State in SQLite is the right call.
- **Bidirectional JSONL protocol with `ask_orchestrator`** — agents that can ask for help instead of hallucinating is a genuine innovation over fire-and-forget patterns.
- **Compression at every pipeline boundary** — each stage produces summaries with `file:line` references, not raw content. Context budget table shows bounded token growth.
- **`AgentRunner` interface abstraction** — clean separation between sandboxing strategy and core protocol logic. `DirectRunner` → `BwrapRunner` → container isolation path is well-reasoned.
- **Hierarchical spans with Langfuse data model** — polymorphic `SpanRow` component that renders by `span_type` is far superior to the original 7 domain-specific event types.
- **`toolCallId` correlation** — verified at the pi-mono source (`types.d.ts:161-177`). Accurate duration tracking on tool call spans is possible because of this.
- **The plan has been through multiple review cycles** — v1 through v5, with staff review summaries and pushback items. The document shows genuine iteration.

### Recommended Next Steps

1. **Resolve Critical Issue #1** — write a concrete `answerQuestion` v1 algorithm (regex keyword extraction + grep-based file/skill matching). Include it in the plan before implementation.
2. **Resolve Critical Issue #2** — decide on fat-morph vs. granular patching for the SSE handler. Recommend fat-morph to start (consistent with mayor dashboard, self-healing on dropped events).
3. **Resolve Critical Issue #3** — add child span cleanup to the `runAgent()` error path and a startup orphan-span cleanup query.
4. **Fix `started_at` precision** — switch to millisecond-precision timestamps (Concern #6) so `duration_ms` is meaningful for sub-second tool calls.
5. **Clarify `cmd.Env` construction** — ensure `OPENAI_API_KEY` and `PATH` are passed to agent subprocesses (Concern #2).
6. **Write the 3 remaining open items** — system prompt text, skill file content, and Temporal heartbeat pattern. These are implementation prerequisites, not deferrals.
7. **Begin Phase 1 implementation** — migration 006, SQLC queries, Temporal SDK, agent libs. The foundation phase has no external dependencies and can start immediately.
