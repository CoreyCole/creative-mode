---
date: 2026-03-01T15:45:01-08:00
researcher: CoreyCole
git_commit: 1ef883986f25ad1d142d5f972ea99aaabab82dce
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Code Quality — Type Safety & Consistency Fixes"
tags: [implementation, swarm, linear, type-safety, code-quality]
status: in_progress
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Code Quality — Type Safety & Consistency Fixes

## Task(s)

Working from the plan at the top of this conversation (no separate plan file — it was provided inline). The plan has 8 steps. Status:

1. **Fix swarm-setup and swarm-conventions skills** — COMPLETED. Replaced all `linear-cli` references with `curl` + Linear GraphQL API commands.
2. **Add Linear status constants** — COMPLETED. Added `StatusTriage`, `StatusBacklog`, `StatusTodo`, `StatusInProgress`, `StatusInReview`, `StatusDone` to `linear/client.go`. Replaced string literals in `swarmorch/manager.go` and `swarmorch/project.go`.
3. **Type EventBus event payloads** — COMPLETED. Created `swarmorch/events.go` with typed structs (`WorkflowStartedEvent`, `SessionSpawnedEvent`, `SessionCompleteEvent`, `WorkflowCompleteEvent`, `WorkflowFailedEvent`, `SessionJSONLEvent`). Replaced all `map[string]any` EventBus publishes in `manager.go`.
4. **Type Linear GraphQL request/response structs** — COMPLETED. Added typed input structs (`issueCreateInput`, `issueUpdateVars`, `commentCreateVars`, `issueFilterVars`, `stateFilterVars`, etc.) to `linear/client.go`. Replaced all `map[string]any` variable construction. Changed `doQuery` signature from `map[string]any` to `any`.
5. **Move learning operations from swarm to swarmorch** — COMPLETED. Created `swarmorch/learnings.go` with Manager methods (`capturePlanIssue`, `captureCodeBug`, `captureTerminalFailure`, `captureSuccessPattern`, `getLearningContext`, `decayLearningRelevance`). Uses sqlc queries instead of raw SQL. Deleted `swarm/learnings.go` and `swarm/learnings_test.go`. Updated all callers in `manager.go`, `activities.go`, and test files. Added `ArchiveOldLowRelevanceLearnings` sqlc query.
6. **Type JSONL log events** — COMPLETED. Changed `JSONLWriter.Write()` from `map[string]any` to `any`. Also changed `Manager.WriteJSONLEvent()` to `any`.
7. **Add 429 retry to Linear client** — COMPLETED. Extracted `doHTTP()` method that handles single retry on 429 with `Retry-After` header parsing.
8. **Switch heartbeat schedule to LeadFDEWorkflow** — COMPLETED. Changed `ensureHeartbeatSchedule` in `temporal.go`.

**Remaining work**: Lint fixes from `just check` — 26 issues need resolution before this can be committed.

## Critical References
- Plan was provided inline at conversation start (no file path)
- `harness/CLAUDE.md` — build constraints and patterns

## Recent changes

- `.claude/skills/swarm-setup/SKILL.md` — replaced `linear-cli` with `curl` + GraphQL API
- `.claude/skills/swarm-conventions/SKILL.md:111` — updated rate limit description
- `harness/internal/linear/client.go` — added status constants, typed input structs, `doHTTP()` with 429 retry, changed `doQuery` to accept `any`
- `harness/internal/swarmorch/events.go` — NEW: typed EventBus event structs
- `harness/internal/swarmorch/learnings.go` — NEW: Manager-method learning operations using sqlc
- `harness/internal/swarmorch/manager.go` — replaced `map[string]any` EventBus publishes, `swarm.Capture*` → `m.capture*`, `swarm.GetLearningContext` → `m.getLearningContext`, `swarm.DecayLearningRelevance` → `m.decayLearningRelevance`, status string literals → `linear.Status*`
- `harness/internal/swarmorch/project.go` — `"Todo"` → `linear.StatusTodo`, added linear import
- `harness/internal/swarmorch/activities.go:353` — `swarm.DecayLearningRelevance` → `a.mgr.decayLearningRelevance`
- `harness/internal/swarmorch/temporal.go:183` — `HeartbeatWorkflow` → `LeadFDEWorkflow`
- `harness/internal/swarmorch/jsonllog.go` — `Write(map[string]any)` → `Write(any)`
- `harness/internal/swarmorch/manager_test.go` — updated test seeders to use Manager methods
- `harness/internal/swarmorch/metrics_test.go` — same
- `harness/internal/swarmorch/digest_test.go` — same
- `harness/internal/db/queries/swarm_learnings.sql` — added `ArchiveOldLowRelevanceLearnings` query
- `harness/internal/swarm/learnings.go` — DELETED
- `harness/internal/swarm/learnings_test.go` — DELETED

## Learnings

1. **tagliatelle linter**: The project uses `tagliatelle` which enforces camelCase JSON tags. Snake_case tags like `"workflow_id"` get flagged. Need `//nolint:tagliatelle` on struct fields that must use snake_case for EventBus backward compat, OR switch the JSON tags to camelCase if the EventBus consumers don't care about field names.
2. **Linear API JSON field names**: Linear's GraphQL API uses camelCase (`teamId`, `issueId`, `stateId`, `parentId`). The Go linter wants `teamID`, `issueID` etc. These struct fields need `//nolint:tagliatelle` because the JSON must match the API's expected format.
3. **The `swarm` package can't import `sqlc`** due to circular dependency (`sqlc` imports `swarm` for enum types). That's why learning operations used raw SQL. Moving them to `swarmorch` which already imports both packages resolves this cleanly.
4. **DB wrapper embeds `*sqlc.Queries`** — all sqlc methods are directly available on `m.db` without wrapping.

## Artifacts

- `harness/internal/swarmorch/events.go` — new typed event structs
- `harness/internal/swarmorch/learnings.go` — new Manager-method learning operations
- `harness/internal/db/queries/swarm_learnings.sql` — updated with new archive query
- `harness/internal/db/sqlc/swarm_learnings.sql.go` — regenerated by `sqlc generate`

## Action Items & Next Steps

1. **Fix 26 lint issues from `just check`**:
   - **tagliatelle (22 issues)**: Add `//nolint:tagliatelle` to Linear input structs in `client.go` (JSON must match API) and EventBus event structs in `events.go` (JSON must match existing consumers).
   - **unused (1)**: Remove unused `timestamped` struct from `jsonllog.go:39`.
   - **errchkjson (1)**: Fix unchecked `json.Marshal` error in `jsonllog.go:60`.
   - **goconst (1)**: The `"true"` string in `manager.go:896` — add `//nolint:goconst` or extract a constant.
   - **nolintlint (1)**: Remove stale `//nolint:goconst` directive from `temporal.go:41` (no longer needed since the goconst warning moved elsewhere).

2. **Find and migrate remaining hardcoded SQL queries to sqlc**:
   - `harness/internal/swarmorch/metrics.go` — has 5 raw SQL queries (`QueryRowContext`/`QueryContext`) for metrics aggregation
   - `harness/internal/swarmorch/health.go` — has 2 raw SQL queries for health/active workflow checks
   - `harness/internal/swarmorch/manager_test.go:533` — raw SQL query in test (acceptable)
   - These should be converted to sqlc queries in `harness/internal/db/queries/` and regenerated

3. **Run tests**: `cd harness && go test ./internal/swarm/... ./internal/swarmorch/... ./internal/linear/...`

4. **Grep verification**:
   - `map[string]any` in swarmorch — should only exist in `WriteJSONLEvent` callers in `swarm_hooks.go` and test files
   - `"In Progress"`, `"Done"`, `"Todo"` literals in swarmorch — should be none (all via `linear.Status*`)
   - `linear-cli` in skills — should be none
   - Raw SQL in `swarm/` — should be none (learnings.go deleted)

## Other Notes

- The `site (golangci-lint)` failure in `just check` appears to be a pre-existing issue unrelated to this PR.
- The EventBus publishes use `any` as the event type — consumers type-switch or JSON-marshal the events, so changing from `map[string]any` to typed structs is backward compatible as long as the JSON serialization matches.
- `swarm_hooks.go` callers of `WriteJSONLEvent` still pass `map[string]any` — this is fine since `Write()` now accepts `any`. A future pass could type those as well.
