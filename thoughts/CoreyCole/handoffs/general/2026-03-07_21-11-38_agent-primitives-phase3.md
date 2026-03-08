---
date: 2026-03-07T21:11:38-08:00
researcher: CoreyCole
git_commit: 8e544f53c94797181f451b95631b039aeb08274b
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives Phase 3: Temporal Integration"
tags: [implementation, temporal, swarmorch, agent-primitives]
status: in_progress
last_updated: 2026-03-07
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives Phase 3 — Temporal Integration

## Task(s)

Implementing Phase 3 of the agent primitives system, which connects Phase 1 (DB schema, SQLC queries, event constants) and Phase 2 (JS agent scripts, shared libraries, skill files, Go types) by adding Go orchestration code that spawns JS agent subprocesses via bidirectional JSONL, orchestrated by Temporal workflows.

**Working from**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md`

### Sub-phase status:

| Sub-Phase | Status | Description |
|-----------|--------|-------------|
| 3A: Temporal SDK + AgentRunner | **Completed** | `go.temporal.io/sdk` added to go.mod, `runner.go` created |
| 3B: Core runAgent + helpers | **Completed** | `agent.go` and `helpers.go` created with JSONL handling, span helpers, answerQuestion |
| 3C: SwarmActivities | **Completed** | `activities.go` created with agent + infrastructure activities |
| 3D: Temporal Workflows | **Not started** | `workflows.go` — ResearchWorkflow, CodeChangePlanWorkflow |
| 3E: SwarmManager + HTTP API | **Not started** | `manager.go`, `swarm_api.go`, server.go/main.go modifications |

Additionally, a **PreToolUse hook** was created to replace the hard `deny` permission rules for build commands.

## Critical References

- **Implementation plan**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md` — authoritative plan with all sub-phases, code snippets, file summary, and verification steps
- **Existing Go types**: `harness/internal/swarmorch/types.go` — all message/artifact types already defined in Phase 2
- **SQLC queries**: `harness/internal/db/sqlc/swarm.sql.go` — generated DB queries for spans, tasks, artifacts

## Recent changes

All changes are uncommitted on `feat/agent-primitives`:

- `harness/internal/swarmorch/runner.go` — AgentRunner interface + DirectRunner (exec.CommandContext)
- `harness/internal/swarmorch/helpers.go` — truncateJSON, marshal, toNullString, extractKeywords, truncate, sanitizeSlug
- `harness/internal/swarmorch/agent.go` — runAgent(), readAgentLoop(), handleToolEvent(), handleQuestion(), answerQuestion(), grepFiles(), span helpers (createSpan, completeSpan, failSpan, failOrphanedChildSpans)
- `harness/internal/swarmorch/activities.go` — SwarmActivities struct with 6 agent activities (GenerateResearchQuestions, RunResearchAgent, SynthesizeResearchDoc, ClassifyPlanDomains, RunSpecialistPlanner, SynthesizePlanDoc) + 7 infrastructure activities (UpdateTaskStatus, PersistArtifact, EmitEvent, CreateSpanActivity, CompleteSpanActivity, FailSpanActivity, WriteDocument)
- `harness/go.mod` — added `go.temporal.io/sdk v1.40.0`
- `.claude/hooks/recommend-just-check.sh` — PreToolUse hook replacing hard deny rules
- `.claude/settings.json` — removed `deny` rules for go/cargo/templ commands, added PreToolUse hook

## Learnings

1. **PreToolUse hooks vs deny rules**: The `.claude/settings.json` `permissions.deny` rules cause opaque "permission denied" loops where Claude retries the same command indefinitely. PreToolUse hooks with `permissionDecision: "deny"` provide actionable feedback ("use `just check`") so Claude can adapt. The hook is at `.claude/hooks/recommend-just-check.sh`.

2. **Lint considerations**: The golangci-lint config in this project is strict:
   - `nolintlint` — flags unused `//nolint` directives (e.g., `//nolint:gosec` on `exec.CommandContext` is NOT needed — gosec doesn't flag it here)
   - `mnd` — all magic numbers need named constants
   - `perfsprint` — `fmt.Errorf("static string")` → `errors.New("static string")`
   - `govet shadow` — `:=` inside switch cases shadows outer variables; use `=` with pre-declared vars
   - `unused` — all exported AND unexported functions must be referenced (will resolve when all files exist)

3. **JSONL protocol**: JS agents use `protocol.js` for bidirectional stdin/stdout JSONL. Go sends `StartMessage{type:"start", task:..., systemPrompt:...}` and `AnswerMessage{type:"answer", id:..., text:...}`. Agents emit `{type:"event", event:"tool_execution_start"|"tool_execution_end", ...}`, `{type:"question", id:..., text:...}`, and `{type:"result", data:...}`.

4. **Workflow determinism**: Temporal workflows must use `workflow.SideEffect()` for UUID generation (not `uuid.NewString()`) and `workflow.Now(ctx)` instead of `time.Now()`. All I/O via activities only.

5. **Agent scripts location**: `harness/agents/` contains 6 JS scripts + `lib/` (protocol.js, agent-factory.js, orchestrator-tools.js, search-context.js) + `skills/` (7 .md files). Package.json uses `@mariozechner/pi-agent-core` and `@mariozechner/pi-ai`.

## Artifacts

- `harness/internal/swarmorch/runner.go` — new file
- `harness/internal/swarmorch/helpers.go` — new file
- `harness/internal/swarmorch/agent.go` — new file
- `harness/internal/swarmorch/activities.go` — new file
- `.claude/hooks/recommend-just-check.sh` — new file
- `.claude/settings.json` — modified (deny rules → PreToolUse hook)
- `harness/go.mod` / `harness/go.sum` — modified (Temporal SDK added)

## Action Items & Next Steps

1. **Sub-Phase 3D: Create `harness/internal/swarmorch/workflows.go`** — ResearchWorkflow and CodeChangePlanWorkflow. Key details:
   - ResearchWorkflow: create workflow span → UpdateTaskStatus("running") → GenerateResearchQuestions → fan-out RunResearchAgent with `workflow.Go` → SynthesizeResearchDoc → WriteDocument → PersistArtifact → UpdateTaskStatus("completed")
   - CodeChangePlanWorkflow: research phase (inline or child workflow) → ClassifyPlanDomains → fan-out RunSpecialistPlanner → SynthesizePlanDoc → WriteDocument → PersistArtifact
   - Activity options: agent activities 10min StartToClose, 2min heartbeat, retry 3; infra activities 30s, retry 3
   - Use `workflow.SideEffect()` for UUIDs, `workflow.Now(ctx)` for time
   - Output paths: `thoughts/swarm/research/YYYY-MM-DD_<taskID>_<slug>.md` and `thoughts/swarm/plans/...`

2. **Sub-Phase 3E: Create `harness/internal/swarmorch/manager.go`** — SwarmManager with Temporal client/worker, Start/Stop, StartResearch, StartCodePlan, CancelTask. Temporal connection: `localhost:7233`, namespace `swarm`, task queue `swarm-agents`. Node path: `/home/deploy/.nix-profile/bin/node`.

3. **Sub-Phase 3E: Create `harness/internal/server/swarm_api.go`** — HTTP handlers for POST `/api/swarm/tasks/research`, POST `/api/swarm/tasks/code-change-plan`, GET `/api/swarm/tasks/:taskID`, POST `/api/swarm/tasks/:taskID/cancel`. Protected by `hookSecretMiddleware()`.

4. **Sub-Phase 3E: Modify `harness/internal/server/server.go`** — Add `SwarmManager` field to Server struct, register swarm routes in `RegisterRoutes()`.

5. **Sub-Phase 3E: Modify `harness/main.go`** — Init SwarmManager gated on `CM_SWARM_TEMPORAL=true`, wire to Server, add graceful shutdown.

6. **Build verification**: Run `just check` from project root to verify everything compiles and passes lint.

## Other Notes

- The `unused` lint errors (34 of the 45 total errors from the last `just check`) will all resolve once `workflows.go` and `manager.go` are created and reference the functions from `agent.go`, `helpers.go`, and `activities.go`.
- The existing `hookSecretMiddleware()` is at `harness/internal/server/server.go:674` and validates `X-Hook-Secret` header against `CM_HOOK_SECRET` env var.
- EventBus publishes to `"swarm"` channel (not a world ID) for span events — subscribers would use `eventBus.Subscribe("swarm")`.
- The `failOrphanedChildSpans` function currently queries all spans with `GetSwarmSpansByTask(ctx, "")` which passes empty string — this may need a dedicated query or the task ID should be threaded through. Consider adding a `GetRunningSpansByParent` SQLC query if needed.
- The `site/` directory also has lint errors (shown in `just check` output) — those are pre-existing and unrelated to this work.
