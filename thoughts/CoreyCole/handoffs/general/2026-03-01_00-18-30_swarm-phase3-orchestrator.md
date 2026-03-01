---
date: 2026-03-01T00:18:30-08:00
researcher: CoreyCole
git_commit: 15d73b1a8d2a1963d400d350589b4516c365a8db
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Phase 3: Orchestrator Manager Implementation"
tags: [implementation, swarm, orchestrator, manager, phase3]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Phase 3 Orchestrator Manager

## Task(s)

Implementing Phase 3 of the agent swarm system — the **orchestrator Manager** that ties everything together: starting workflows, spawning Claude Code sessions per-phase, detecting completion, parsing results, advancing through phases, and capturing learnings.

Working from the plan provided inline by the user (no separate plan document — it was given directly in the prompt).

### Status of each sub-task:

1. **result.go — Result file parsing** ✅ COMPLETED
2. **result_test.go — Result parsing tests** ✅ COMPLETED
3. **SqlDB() accessor on db.DB** ✅ COMPLETED
4. **NewForTest() constructor on db.DB** ✅ COMPLETED
5. **Swarm events in events/types.go** ✅ COMPLETED
6. **manager.go — Core orchestrator** ✅ COMPLETED (moved to `swarmorch` package due to import cycle)
7. **manager_test.go — Orchestrator tests** ✅ COMPLETED (moved to `swarmorch` package)
8. **server.go + main.go wiring** ✅ COMPLETED (uses `swarmorch` package)
9. **swarm_api.go — HTTP handlers** ✅ COMPLETED but **NEEDS IMPORT FIX** (still references `swarm` package, needs `swarm` for WorkflowType only)
10. **Skill SKILL.md updates** ✅ COMPLETED (all 6 skills updated with `CM_SWARM_RESULT_PATH`)
11. **Tests pass** ❌ NOT YET VERIFIED — interrupted before running tests

## Critical References

- `thoughts/swarm/` — Swarm agent plans, handoffs, research, retrospectives
- Phase 1 commit `8cbc9fb` — Foundation: DB tables, sqlc queries, enums, state machine, skills
- Phase 2 (unstaged) — `harness/internal/swarm/learnings.go`, `harness/internal/swarm/handoffs.go`

## Recent changes

### New files created:
- `harness/internal/swarm/result.go` — `SessionResultData` struct + `ParseResultFile()` function
- `harness/internal/swarm/result_test.go` — Table-driven tests for result parsing
- `harness/internal/swarmorch/manager.go` — Full Manager orchestrator (was originally `swarm/manager.go`, moved to break import cycle)
- `harness/internal/swarmorch/manager_test.go` — Tests for buildEnv, advanceWorkflow, captureLearnings, loadConfig, emitEvent, concurrent advances
- `harness/internal/server/swarm_api.go` — 3 HTTP handlers: handleSwarmStart, handleSwarmStatus, handleSwarmCancel

### Modified files:
- `harness/internal/db/db.go` — Added `SqlDB() *sql.DB` and `NewForTest(rawDB, queries) *DB` methods
- `harness/internal/events/types.go` — Added 5 swarm event constants
- `harness/internal/server/server.go` — Added `SwarmManager *swarmorch.Manager` field, 3 routes under hookSecretMiddleware
- `harness/main.go` — Import `swarmorch`, create manager, wire to server, call RecoverWorkflows on startup
- `.claude/skills/swarm-research/SKILL.md` — Added `CM_SWARM_RESULT_PATH` env var + result file output section
- `.claude/skills/swarm-code-plan/SKILL.md` — Same
- `.claude/skills/swarm-plan-review/SKILL.md` — Same
- `.claude/skills/swarm-code/SKILL.md` — Same
- `.claude/skills/swarm-code-verify/SKILL.md` — Same
- `.claude/skills/swarm-code-pr/SKILL.md` — Same

## Learnings

### Import cycle: `swarm` → `db` → `db/sqlc` → `swarm`
The `swarm` package defines typed enums (`Phase`, `SessionResult`, etc.) that are imported by `db/sqlc` (sqlc-generated models). If the Manager lived in `swarm` and imported `db`, it creates a cycle. **Solution**: Move the Manager to a separate `swarmorch` package that imports both `swarm` and `db`.

### db.DB wrapper hides *sql.DB
The `db.DB` struct has an unexported `db *sql.DB` field. Learning functions (`CapturePlanIssue`, `GetLearningContext`, etc.) need a `DBTX` interface (`*sql.DB` or `*sql.Tx`). Added `SqlDB()` accessor.

### Test DB pattern
Tests in the swarm package use `newTestDB(t)` with an inline `swarmTestSchema` DDL string (not the real migration files). For the Manager tests in `swarmorch`, I created `newManagerTestDB(t)` with `swarmFullTestSchema` that includes all necessary tables (workflows, sessions, events, tickets, config, learnings) and uses `db.NewForTest()` to create a proper `db.DB` wrapper.

### Session naming
Swarm tmux sessions use `cm-swarm-{ticketID}-{phase}` naming. Already excluded from `ReapOrphanedSessions()` in `claude.go:311-315`.

## Artifacts

- `harness/internal/swarm/result.go` — Result file parsing
- `harness/internal/swarm/result_test.go` — Result parsing tests
- `harness/internal/swarmorch/manager.go` — Core orchestrator Manager
- `harness/internal/swarmorch/manager_test.go` — Manager tests
- `harness/internal/server/swarm_api.go` — HTTP handlers for swarm API
- `harness/internal/db/db.go:68-71` — `SqlDB()` method
- `harness/internal/db/db.go:73-77` — `NewForTest()` method
- `harness/internal/events/types.go:16-21` — Swarm event constants

## Action Items & Next Steps

1. **Fix `swarm_api.go` import** — Currently imports `creative-mode/harness/internal/swarm` for `swarm.WorkflowType()`. This is correct and should work, but verify the import compiles.

2. **Run tests** — `cd harness && go test ./internal/swarm/... ./internal/swarmorch/... -v -count=1` to verify all tests pass. Fix any compilation errors.

3. **Run `just check`** from project root to verify full lint/build passes. Key linters to watch: `errcheck`, `gosec`, `noctx`, `usetesting`, `exhaustive`.

4. **Verify `main.go` baseDir** — The Manager is created with `baseDir: ".."` (since harness runs from `harness/` dir). This means `ResolveHandoffPath` will look at `../thoughts/swarm/handoffs-*/`. Verify this is correct vs the VPS deployment where `air` runs from `harness/`.

5. **Manual dry-run test** (from plan):
   - Start harness (Docker or VPS)
   - `curl -X POST localhost:8080/api/swarm/start -d '{"ticket_id":"TEST-1","workflow_type":"research","ticket_url":"https://example.com"}'`
   - Verify: workflow record created, tmux session `cm-swarm-TEST-1-research` exists, Claude Code launched with skill prompt

## Other Notes

### Architecture: Why `swarmorch` package?
The clean separation is: `swarm` = types, enums, state machine, learnings, handoffs (no DB import). `swarmorch` = Manager orchestrator that imports both `swarm` and `db`. This parallels how `claude` orchestrator is separate from `world` manager.

### Key function signatures in swarmorch.Manager:
- `NewManager(db, logger, eventBus, baseDir, logsDir, harnessURL) *Manager`
- `StartWorkflow(ctx, ticketID, workflowType, ticketURL) (string, error)`
- `CancelWorkflow(ctx, workflowID) error`
- `RecoverWorkflows(ctx) error`
- `GetWorkflow(ctx, workflowID) (SwarmWorkflow, *SwarmSession, error)`

### Swarm API routes (under hookSecretMiddleware):
- `POST /api/swarm/start` — `{ticket_id, workflow_type, ticket_url}`
- `GET /api/swarm/status/:id` — Returns workflow + latest session
- `POST /api/swarm/cancel` — `{workflow_id}`

### EventBus usage:
Swarm events are published globally via `PublishGlobal()` with event type strings from `events.EventSwarmWorkflow*` / `events.EventSwarmSession*`. The `swarm.EventType` enum values (used in DB) are separate from the `events` package constants (used for EventBus).
