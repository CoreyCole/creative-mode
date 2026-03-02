---
date: 2026-03-02T10:44:56-08:00
researcher: CoreyCole
git_commit: cc3a5255a6106b86779969e0592a78cbabca49f2
branch: feature/agent-swarm
repository: creative-mode
topic: "Linear Project Creation & Agentic Project Plan Review"
tags: [implementation, swarm, linear, state-machine, project-workflow]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Linear Project Creation & Agentic Project Plan Review

## Task(s)

Implementing a 6-phase plan to fix Linear project grouping and improve agent review flow for project workflows. Working from plan at `/home/deploy/.claude/plans/luminous-juggling-wolf.md`.

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Fix UpsertSwarmTicket SQL bug (missing `identifier` in ON CONFLICT) | **Completed** |
| 2 | DB migration — `linear_project_id` column on `swarm_workflows` | **Completed** |
| 3 | Linear client — `CreateProject` method + `projectID` param on ticket creation | **Completed** |
| 4 | Wire up Linear project creation in `CreateProjectTicketsFromPlan` | **Completed** |
| 5 | State machine — agentic review always runs, exhaustion → human gate instead of fail | **Completed** |
| 6 | Update project review skill with simplicity/maintainability criteria | **Completed** |

**All code changes are complete.** `just check` passes (lint + compile). Unit tests for `swarm` and `linear` packages pass. **Swarmorch tests need a fix** — the test helper creates in-memory SQLite without the new `linear_project_id` column.

## Critical References

- Plan: `/home/deploy/.claude/plans/luminous-juggling-wolf.md`
- Harness CLAUDE.md: `harness/CLAUDE.md` (comprehensive architecture reference)

## Recent changes

### Phase 1: SQL bug fix
- `harness/internal/db/queries/swarm.sql:111` — Added `identifier = excluded.identifier` to `UpsertSwarmTicket` ON CONFLICT

### Phase 2: Migration + query updates
- `harness/internal/db/migrations/011_linear_project_id.sql` — New migration: `ALTER TABLE swarm_workflows ADD COLUMN linear_project_id TEXT`
- `harness/internal/db/db.go:115` — Added migration to `migrationFiles` slice
- `harness/internal/db/queries/swarm.sql` — Added `linear_project_id` to all `swarm_workflows` SELECT lists (at end to match model order), added `UpdateSwarmWorkflowLinearProject` query
- `harness/sqlc.yaml:85` — Added `linear_project_id: "LinearProjectID"` rename mapping
- Regenerated sqlc: `harness/internal/db/sqlc/` updated

### Phase 3: Linear client
- `harness/internal/linear/client.go:86-93` — Added `ProjectID` field to `issueCreateInput`
- `harness/internal/linear/client.go:113-127` — Added `ProjectResult`, `projectCreateVars`, `projectCreateInput` types
- `harness/internal/linear/client.go:223-247` — `CreateTicket` now accepts `projectID string` param
- `harness/internal/linear/client.go:308-315` — `CreateTicketWithURL` now accepts `projectID string` param
- `harness/internal/linear/client.go:282-286` — `CreateTicketResult` now includes `ID` (Linear UUID)
- `harness/internal/linear/client.go:371-415` — New `CreateProject` method
- `harness/internal/linear/client_test.go` — Updated `TestCreateTicket` call signature, added `TestCreateProject`
- Updated all callers: `swarmorch/manager.go:116`, `swarmorch/project.go:84`, `swarmorch/project.go:660`

### Phase 4: Project creation wiring
- `harness/internal/swarmorch/project.go:57-76` — `CreateProjectTicketsFromPlan` now creates a Linear Project first, stores `linearProjectID` on workflow via `UpdateSwarmWorkflowLinearProject`
- `harness/internal/swarmorch/project.go:101-108` — Child tickets created with `CreateTicketWithURL` (instead of `CreateTicket`) passing `linearProjectID`
- `harness/internal/swarmorch/project.go:658-662` — `SpawnProjectResearchChildren` reads `wf.LinearProjectID` and passes it to child research ticket creation

### Phase 5: State machine changes
- `harness/internal/swarm/statemachine.go:236-249` — `IsGatedTransition` always returns `true` for `PhaseProjectReview` (ignores config)
- `harness/internal/swarm/statemachine.go:159-174` — Agent exhaustion at `PhaseProjectReview` returns `PhaseProjectVerify` instead of `PhaseFailed`
- `harness/internal/swarm/statemachine.go:29-37` — `DefaultConfig` now sets `GateProjectReview: true`
- `harness/internal/swarmorch/manager.go:734-749` — `advanceWorkflow` intercepts exhaustion path (project_review + logic_failure + project_verify) → enters human gate
- `harness/internal/swarm/statemachine_test.go` — Updated "project_review revise at max" test to expect `PhaseProjectVerify`, updated `IsGatedTransition` tests for always-gated project_review

### Phase 6: Skill update
- `.claude/skills/swarm-project-review/SKILL.md` — Added criteria 8-10 (codebase simplicity, maintainability, dependency minimalism) + Simplicity Principles section + updated verdict criteria

## Learnings

1. **sqlc column order matters for type identity**: When SELECT columns are in a different order than the model (based on table definition), sqlc generates a separate `XxxRow` struct instead of reusing the model struct. Fix: put `linear_project_id` at the END of SELECT lists to match the model's field order (the column was added via `ALTER TABLE ADD COLUMN` which appends it).

2. **golangci-lint nolintlint**: The `//nolint:tagliatelle` directive was flagged as unused on `teamIds` JSON tag — the linter doesn't trigger on `Ids` suffix apparently. Had to remove the nolint directive.

3. **golangci-lint perfsprint**: `fmt.Sprintf("Project: %s", wf.TicketID)` flagged as replaceable with string concatenation. Fixed to `"Project: "+wf.TicketID`.

4. **Test schema gap**: The swarmorch tests create an in-memory SQLite DB with a hardcoded schema that doesn't include the new `linear_project_id` column. This is the remaining work.

## Artifacts

- `harness/internal/db/migrations/011_linear_project_id.sql` — New migration file
- `harness/internal/db/queries/swarm.sql` — Updated queries
- `harness/internal/db/sqlc/` — Regenerated code (models.go, swarm.sql.go, querier.go)
- `harness/internal/linear/client.go` — Extended with `CreateProject`, `projectID` params
- `harness/internal/linear/client_test.go` — New `TestCreateProject`
- `harness/internal/swarm/statemachine.go` — State machine changes
- `harness/internal/swarm/statemachine_test.go` — Updated tests
- `harness/internal/swarmorch/manager.go` — Agent exhaustion → human gate
- `harness/internal/swarmorch/project.go` — Linear project creation + projectID wiring
- `.claude/skills/swarm-project-review/SKILL.md` — Simplicity criteria

## Action Items & Next Steps

1. **Fix swarmorch test schema** — The test helper creates the DB schema inline without the new `linear_project_id` column. Find the schema creation in the test setup (likely `manager_test.go` or a test helper) and add `linear_project_id TEXT` to the `swarm_workflows` CREATE TABLE. The error is: `table swarm_workflows has no column named linear_project_id`. Run `go test -v ./internal/swarmorch/...` to verify.

2. **Check pre-existing test failures** — `TestGetHealthWithActiveWorkflows`, `TestGetHealthRecentCompletions`, and `TestGetMetricsCompletionRate` also failed but may be pre-existing (unrelated to this change). Verify by checking if they fail on the base branch.

3. **Run full verification** after fixing tests:
   - `just check` (already passes)
   - `go test ./internal/swarm/...` (already passes)
   - `go test ./internal/linear/...` (already passes)
   - `go test ./internal/swarmorch/...` (needs test schema fix)

## Other Notes

- The `CreateSwarmWorkflow` INSERT was updated to include `linear_project_id` with a default empty string: `INSERT INTO swarm_workflows (..., linear_project_id) VALUES (..., '')`
- The `UpdateSwarmWorkflowLinearProject` param type uses `sql.NullString` (matching the column type in the generated model)
- Linear's `projectCreate` mutation takes `teamIds` (array) not `teamId` (singular) — different from `issueCreate`
- The `site (golangci-lint)` failure in `just check` output is a pre-existing issue unrelated to these changes
