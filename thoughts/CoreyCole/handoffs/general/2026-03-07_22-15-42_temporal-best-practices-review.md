---
date: 2026-03-07T22:15:42-08:00
researcher: CoreyCole
git_commit: adaa1c7ffa8f3ea1a97708f581a399003c0b44d1
branch: feat/agent-primitives
repository: creative-mode
topic: "Temporal Workflows Best Practices Review & Improvements"
tags: [implementation, temporal, swarm, workflows, review]
status: complete
last_updated: 2026-03-07
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Temporal Workflows Best Practices Review & Improvements

## Task(s)

**Completed:** Reviewed the Phase 3D+3E Temporal workflows implementation (from previous handoff `thoughts/CoreyCole/handoffs/general/2026-03-07_21-50-29_temporal-workflows-review.md`) against Temporal SDK best practices and implemented four improvements.

### Changes implemented:
1. **DBActivities pattern** — Registered `*sqlc.Queries` as a Temporal activity struct, making every sqlc query automatically available as an activity with type safety and go-to-definition support.
2. **WorkflowExecutionTimeout** — Added 1h timeout for research workflows, 2h for code plan workflows.
3. **Fan-out context fix** — Changed `runResearchSteps` to create `WithActivityOptions` from goroutine's `gCtx` instead of parent `ctx`.
4. **Cancellation-aware cleanup** — Extracted `deferredCleanup` helper. Uses `ctx.Err()` to distinguish canceled vs failed workflows, setting appropriate task status and event type.

## Critical References

- Previous handoff that prompted this review: `thoughts/CoreyCole/handoffs/general/2026-03-07_21-50-29_temporal-workflows-review.md`
- Temporal SDK v1.40.0: `go.temporal.io/sdk` in `harness/go.mod:26`

## Recent changes

- `harness/internal/swarmorch/activities.go:22-26` — Added `DBActivities` struct wrapping `*sqlc.Queries`
- `harness/internal/swarmorch/manager.go:8` — Added `time` import
- `harness/internal/swarmorch/manager.go:23-24` — Added `researchWorkflowTimeout` and `codePlanWorkflowTimeout` constants
- `harness/internal/swarmorch/manager.go:65` — Registered `DBActivities` on the worker
- `harness/internal/swarmorch/manager.go:93` — Added `WorkflowExecutionTimeout` to research `StartWorkflowOptions`
- `harness/internal/swarmorch/manager.go:124` — Added `WorkflowExecutionTimeout` to code plan `StartWorkflowOptions`
- `harness/internal/swarmorch/workflows.go:72-100` — New `deferredCleanup` helper function with cancellation awareness
- `harness/internal/swarmorch/workflows.go:107-134` — `runResearchSteps` fan-out now uses `gCtx` for `WithActivityOptions`
- `harness/internal/swarmorch/workflows.go:196,300` — Both workflows now use `deferredCleanup` instead of inline deferred functions

## Learnings

- **`var a *SwarmActivities` nil pointer pattern**: Standard Temporal Go SDK pattern. The SDK uses reflection to extract the method name — it never invokes through the nil pointer. This gives go-to-definition and compile-time method name verification. Discussed alternatives (string names, standalone functions) but nil pointer is the pragmatic choice.
- **DBActivities pattern**: Register `*sqlc.Queries` directly as an activity struct. Temporal only registers methods matching the activity signature (`func(context.Context, ...) (T, error)` or `func(context.Context, ...) error`), so `WithTx()` and other non-query methods are automatically skipped.
- **`db.DB` embeds `*sqlc.Queries`**: Access the embedded queries via `database.Queries` (field name is the unqualified type name). See `harness/internal/db/db.go:18-21`.
- **misspell linter**: Uses American English — `canceled` not `cancelled`.
- **Fan-out context**: Using `gCtx` vs parent `ctx` for `WithActivityOptions` — in practice both work since all coroutines share the workflow lifecycle, but `gCtx` is more idiomatic per Temporal examples.
- **Cancellation detection**: `ctx.Err() != nil` in a deferred function reliably detects workflow cancellation. The disconnected context (`workflow.NewDisconnectedContext`) ensures cleanup activities still run.

## Artifacts

- `harness/internal/swarmorch/activities.go:22-26` — `DBActivities` type definition
- `harness/internal/swarmorch/manager.go` — Timeout constants, DBActivities registration, WorkflowExecutionTimeout
- `harness/internal/swarmorch/workflows.go:72-100` — `deferredCleanup` helper

## Action Items & Next Steps

1. **Manual testing** — Start a research workflow via the API and verify end-to-end:
   ```bash
   curl -X POST http://localhost:8080/api/swarm/tasks/research \
     -H "X-Hook-Secret: $CM_HOOK_SECRET" \
     -H "Content-Type: application/json" \
     -d '{"requestText":"How does the EventBus work?"}'
   ```

2. **Use DBActivities in new workflow code** — When adding new workflows or workflow steps that need DB access, use `var db *DBActivities` and call sqlc methods directly instead of writing wrapper activities. Existing wrapper activities (`UpdateTaskStatus`, `PersistArtifact`, etc.) are kept since they add timestamp/UUID business logic.

3. **Consider cancellation test** — Verify that cancelling a running workflow (via `POST /api/swarm/tasks/:taskID/cancel`) correctly sets task status to `"canceled"` instead of `"failed"`.

## Other Notes

- **Temporal best practices validated as correct**: `workflow.SideEffect` for UUIDs, `workflow.Now(ctx)` for timestamps, `workflow.NewDisconnectedContext` for cleanup, `worker.Stop()` before `client.Close()`, activity heartbeats on JSONL reads, proper variable capture in goroutine closures.
- **Not changed (acceptable as-is)**: No `WorkflowIDReusePolicy` (task IDs are per-request UUIDs so collisions are negligible). No `workflow.GetLogger()` in workflow functions (workflows don't log directly; all logging happens in activities).
- **Swarm API routes**: `POST /api/swarm/tasks/research`, `POST /api/swarm/tasks/code-change-plan`, `GET /api/swarm/tasks/:taskID`, `POST /api/swarm/tasks/:taskID/cancel` — all behind `hookSecretMiddleware`.
