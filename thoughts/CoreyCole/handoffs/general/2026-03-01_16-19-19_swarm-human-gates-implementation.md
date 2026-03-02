---
date: 2026-03-01T16:19:19-08:00
researcher: CoreyCole
git_commit: 3dfc781a0ff01ac34e05c1c214b279a06204308b
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Human Gates & Post-PR Lifecycle Implementation"
tags: [implementation, swarm, human-gates, workflow-orchestration]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Human Gates & Post-PR Lifecycle Implementation

## Task(s)

**Implement the Swarm Human Gates & Post-PR Lifecycle feature** — a 7-phase plan adding human review gates to the fully-autonomous swarm agent workflow.

| Phase | Description | Status |
|-------|-------------|--------|
| Phase 1 | DB Migration & Types | Planned |
| Phase 2 | Enum & State Machine Changes | Planned |
| Phase 3 | Manager Gate Logic | Planned |
| Phase 4 | API Endpoints | Planned |
| Phase 5 | Dashboard UI | Planned |
| Phase 6 | Health & Stall Detection | Planned |
| Phase 7 | Tests | Planned |

**No code has been written yet.** The session was spent reading the full codebase to understand patterns, then writing the implementation plan. All phases are ready for implementation.

## Critical References

1. **Implementation Plan**: `thoughts/CoreyCole/plans/2026-03-02_00-16-57_swarm-human-gates-post-pr-lifecycle.md` — the complete 7-phase plan with code snippets, file targets, and success criteria.
2. **State Machine**: `harness/internal/swarm/statemachine.go` — the core transition logic that needs `PhasePR` → `PhaseHumanReview` change.
3. **Manager**: `harness/internal/swarmorch/manager.go` — the `advanceWorkflow()` method (line 570) where gate interception must be added.

## Recent changes

No code changes were made. Only the plan document was created.

## Learnings

**Key architectural patterns the implementer must follow:**

1. **SQLite migration pattern**: The existing schema uses `CREATE TABLE IF NOT EXISTS` with CHECK constraints. To add new enum values to CHECK constraints, you must recreate the table (SQLite limitation). See `harness/internal/db/migrations/006_swarm_tables.sql` for the pattern.

2. **sqlc workflow**: Queries go in `harness/internal/db/queries/*.sql`, then run `cd harness && sqlc generate` to regenerate `harness/internal/db/sqlc/`. The `harness/sqlc.yaml` file has type overrides that map DB columns to Go types (e.g., `swarm_workflows.phase` → `swarm.Phase`). New columns like `gate_phase` and `review_feedback` need overrides added to `sqlc.yaml`.

3. **Manager mutex**: `advanceWorkflow()` (manager.go:575) holds `m.mu` mutex and re-reads the workflow under lock. Gate interception must happen inside this locked section.

4. **`toNullString()` lives in `learnings.go:210`**, `nowUTC()` in `project.go:449` — both are package-level helpers in `swarmorch`.

5. **`SkillForPhase()` returns `""` for terminal phases** and `spawnSession()` errors on empty skill (manager.go:296-298). `PhaseHumanReview` must return `""` from `SkillForPhase` and the code path must never call `spawnSession` for it — it calls `enterGate()` instead.

6. **EventBus events** use typed structs (see `swarmorch/events.go`) published via `m.eventBus.PublishGlobal()`. Dashboard SSE handler catches any event with `strings.HasPrefix(eventType, "swarm.")` prefix.

7. **CancelWorkflow** (manager.go:209) only accepts `running` or `pending`. Must add `awaiting_review` to the accepted statuses.

8. **Dashboard SSE** (`swarm_dashboard.go:90-160`): The existing SSE handler already refreshes workflow/event tabs on any `swarm.*` event. New gate events will automatically trigger refreshes since they use the `swarm.gate_*` prefix.

9. **Linear integration**: `linearUpdateStatus()` uses constants from `linear/client.go`. `StatusInReview = "In Review"` already exists — use it when entering a gate.

10. **templ views**: The dashboard uses templ components in `harness/views/swarm/dashboard.templ`. The `statusClass()` function (line 512) maps `WorkflowStatus` → CSS classes. Add `StatusAwaitingReview` → purple styling.

## Artifacts

- `thoughts/CoreyCole/plans/2026-03-02_00-16-57_swarm-human-gates-post-pr-lifecycle.md` — Full implementation plan (7 phases)

## Action Items & Next Steps

Implement all 7 phases in order. Each phase has explicit success criteria in the plan. The recommended execution order:

1. **Phase 1**: Create `harness/internal/db/migrations/008_human_gates.sql`, add queries to `harness/internal/db/queries/swarm.sql`, create `harness/internal/db/queries/swarm_gate_reviews.sql`, add overrides to `harness/sqlc.yaml`, run `cd harness && sqlc generate`. Verify: `cd harness && go build ./...`

2. **Phase 2**: Edit `harness/internal/swarm/enums.go` (add `PhaseHumanReview`, `StatusAwaitingReview`, 3 gate event types). Edit `harness/internal/swarm/statemachine.go` (config flags, PR→HumanReview transition, gate helpers). Edit `harness/internal/events/types.go` (3 new constants). Edit `harness/internal/swarmorch/events.go` (2 new event structs). Verify: `cd harness && go test ./internal/swarm/...`

3. **Phase 3**: Edit `harness/internal/swarmorch/manager.go` (gate interception in `advanceWorkflow`, `enterGate`, `ApproveGate`, `RejectGate`, `CancelWorkflow` update, `buildEnv` review feedback). Edit `harness/internal/swarm/env.go` (add `ReviewFeedback` field). Edit `harness/internal/swarmorch/alerts.go` (add `FireGateReached`). Verify: `cd harness && go build ./...`

4. **Phase 4**: Edit `harness/internal/server/swarm_api.go` (3 new handlers). Edit `harness/internal/server/swarm_dashboard.go` (2 new dashboard handlers). Edit `harness/internal/server/server.go` (register routes at lines 166-182 and 211-218). Verify: `cd harness && go build ./...`

5. **Phase 5**: Edit `harness/views/swarm/dashboard.templ` (purple badge, gate review panel, review history). Verify: `cd harness && go build ./...`

6. **Phase 6**: Edit `harness/internal/swarmorch/health.go` (include `awaiting_review` in active query, exclude from stall). Edit `harness/internal/swarmorch/metrics.go` (count `awaiting_review` separately). Verify: `cd harness && go build ./...`

7. **Phase 7**: Edit `harness/internal/swarm/statemachine_test.go` (new transition tests). Add manager gate tests. Verify: `cd harness && go test ./internal/swarm/... ./internal/swarmorch/...`

## Other Notes

- The VPS is the production environment. Build directly with `just vps-build` from `harness/`.
- **Never run `cargo build` or `go build` on macOS** — only in Docker. But this is a VPS (Linux), so direct builds are fine.
- `just check` from project root verifies Go + Rust + WASM all compile.
- The swarm dashboard is at `/swarm` (approved users) and the API is at `/api/swarm/*` (hookSecret auth).
- Existing tests: `harness/internal/swarm/statemachine_test.go` has comprehensive table-driven tests — follow the same pattern for new tests.
- The `swarmorch` package has existing test files: `harness/internal/swarmorch/digest_test.go`, `manager_test.go`, `metrics_test.go` — check their patterns for manager-level tests.
