---
date: 2026-03-01T21:33:18-08:00
researcher: CoreyCole
git_commit: e5b77bbda1c4083c52d2bc48c41548e0818146cc
branch: feature/agent-swarm
repository: creative-mode
topic: "Self-Directed Project Workflow (Tech Debt Discovery) Implementation"
tags: [implementation, swarm, project-workflow, create-project-api, ticket-description]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Self-Directed Project Workflow Implementation

## Task(s)

**Status: Implementation COMPLETE, needs commit + linter pass + deploy**

Implemented the self-directed project workflow feature that allows the swarm orchestrator to autonomously create and manage projects without pre-existing Linear tickets. The key design: the Linear ticket description serves as the project's dynamic instructions (the "skill"), loaded by the project lead on each phase wake-up.

All code changes are implemented and tests pass. The changes have NOT been committed yet (only the prerequisite `project_decompose` commit was made).

## Critical References

- Plan document that was followed: provided inline by user at start of session (not a file)
- `harness/CLAUDE.md` — full swarm API routes, architecture, configuration details
- `harness/internal/swarmorch/manager.go` — core orchestrator logic (advanceWorkflow, buildEnv, gates)

## Recent changes

### Migration & DB
- `harness/internal/db/migrations/010_project_create.sql` — NEW: adds `description TEXT` to `swarm_tickets`, recreates `swarm_workflows` and `swarm_sessions` tables to include `project_decompose` in phase CHECK constraints
- `harness/internal/db/db.go:114` — registered migration 010 in `migrationFiles` slice
- `harness/internal/db/queries/swarm.sql:108-125` — `UpsertSwarmTicket` now includes `description`, added `UpdateSwarmTicketDescription`
- `harness/internal/db/queries/swarm_dependencies.sql:33-34` — added `DeleteSwarmTicketsByProject`
- All ticket SELECT queries updated to include `description` column (order: after `synced_at` to match model)
- `harness/sqlc.yaml:85` — added `description: "Description"` rename

### SwarmEnv
- `harness/internal/swarm/env.go:33-34` — added `TicketDescriptionPath string` field

### Manager (buildEnv, advanceWorkflow, gates, new methods)
- `harness/internal/swarmorch/manager.go:115-149` — NEW `CreateProjectTicket()` method
- `harness/internal/swarmorch/manager.go:1087-1101` — `buildEnv()` fetches ticket description for project workflows, writes to temp file
- `harness/internal/swarmorch/manager.go:1139-1159` — NEW `resolveTicketDescription()` helper (Linear first, DB fallback)
- `harness/internal/swarmorch/manager.go:864-887` — advanceWorkflow interceptions for `project_plan→project_review` (create tickets) and `project_review→project_plan` retry (reconcile)
- `harness/internal/swarmorch/manager.go:870` — changed `SpawnProjectChildren` → `SpawnProjectWorkflows`
- `harness/internal/swarmorch/manager.go:1513` — same rename in `ApproveGate`
- `harness/internal/swarmorch/manager.go:1615-1622` — `RejectGate` now reconciles tickets on `project_review` rejection

### Project.go (refactored)
- `harness/internal/swarmorch/project.go:35-122` — NEW `CreateProjectTicketsFromPlan()` — creates tickets/milestones from plan, does NOT spawn workflows
- `harness/internal/swarmorch/project.go:124-147` — NEW `ReconcileProjectTickets()` — deletes child tickets/deps on plan rejection
- `harness/internal/swarmorch/project.go:149-195` — NEW `SpawnProjectWorkflows()` — refactored from old `SpawnProjectChildren`, only spawns workflows for existing tickets
- `harness/internal/swarmorch/project.go:197-265` — extracted helpers: `readProjectPlan()`, `createDependencyEdges()`, `createMilestones()`

### API Endpoint
- `harness/internal/server/swarm_api.go:459-512` — NEW `handleSwarmCreateProject` handler
- `harness/internal/server/server.go:168` — registered `POST /create-project` route

### Linear Client
- `harness/internal/linear/client.go:280-347` — NEW `CreateTicketWithURL()` method + `CreateTicketResult` type

### Skill Files
- `.claude/skills/swarm-research/SKILL.md` — added `CM_SWARM_TICKET_DESCRIPTION_PATH` to preamble + env table
- `.claude/skills/swarm-project-decompose/SKILL.md` — same
- `.claude/skills/swarm-project-plan/SKILL.md` — same
- `.claude/skills/swarm-project-review/SKILL.md` — same

### Tests
- `harness/internal/swarmorch/manager_test.go:78-91` — updated test schema: added `description TEXT` to `swarm_tickets`, `revision_target TEXT` to `swarm_gate_reviews`

## Learnings

1. **sqlc column order matters**: When adding a column via `ALTER TABLE ADD COLUMN`, the column is appended to the end. sqlc generates a separate Row type if the SELECT column order doesn't match the model order. Fix: put `description` at the end of SELECT lists to match the model's field order.

2. **Test schema is hardcoded**: `manager_test.go` uses a `swarmFullTestSchema` constant with the full schema. It does NOT use the migration files. Any schema change must be reflected in both the migration file AND the test schema constant.

3. **`CreateTicket` returns identifier only**: The Linear client's `CreateTicket` method returns just the identifier string. To also get the URL, I added `CreateTicketWithURL` rather than changing the existing signature (which has many callers).

4. **Pre-existing test failure**: `internal/linear/TestHTTPError` times out because it triggers rate limiting (60s sleep) with only a 10s test timeout. Not caused by these changes.

5. **Linter auto-formatted**: The linter auto-formatted `manager.go` and `project.go` after edits (line wrapping in `CreateProjectTicket`, `readProjectPlan`, etc.). These changes are intentional.

## Artifacts

- `harness/internal/db/migrations/010_project_create.sql` — new migration
- `harness/internal/db/sqlc/*.go` — regenerated by sqlc
- All files listed in "Recent changes" above

## Action Items & Next Steps

1. **Commit all changes** — All code is implemented and tests pass. Need to `git add` and commit the unstaged changes.

2. **Deploy and test E2E** — After commit:
   ```bash
   # Test the new endpoint:
   curl -X POST http://localhost:8080/api/swarm/create-project \
     -H "X-Hook-Secret: $CM_HOOK_SECRET" \
     -H "Content-Type: application/json" \
     -d '{"title":"Tech Debt Discovery","description":"Explore the codebase for tech debt..."}'
   ```
   Verify: workflow starts, research runs, tickets created during planning, human gate reached.

3. **Optional: Add unit tests** for `CreateProjectTicketsFromPlan`, `ReconcileProjectTickets`, and `SpawnProjectWorkflows` — currently covered by integration flow but no dedicated unit tests.

4. **Optional: Update CLAUDE.md** — Add `POST /api/swarm/create-project` to the Swarm API route table in `harness/CLAUDE.md`.

## Other Notes

- The flow: `POST /api/swarm/create-project` → auto-create Linear ticket → start project workflow → research (autonomous) → project_decompose (autonomous) → child research (autonomous) → project_plan (autonomous, creates child tickets) → project_review (agent review) → HUMAN GATE → human approves → spawn child code workflows → project_verify → done

- `gateProjectReview: true` is already set in the DB config, so the human gate at `project_review` is active. Verify with: `sqlite3 data/creative-mode.db "SELECT config FROM swarm_config WHERE id = 'default';"`

- The `description` field flows: API request → Linear ticket → DB `swarm_tickets.description` → temp file → `CM_SWARM_TICKET_DESCRIPTION_PATH` env var → skill session reads it in preamble
