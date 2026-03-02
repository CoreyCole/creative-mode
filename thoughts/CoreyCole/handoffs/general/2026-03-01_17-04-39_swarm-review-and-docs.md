---
date: 2026-03-01T17:04:39-08:00
researcher: CoreyCole
git_commit: eeb40a97a945a6c7b5d9846fc07bf116aa454d6b
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Code Review, Bug Fix, and Documentation"
tags: [review, documentation, swarm, human-gates, simplify]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Code Review, Bug Fix, and Documentation

## Task(s)

1. **`/simplify` review of human gates implementation** — Complete. Ran three parallel review agents (code reuse, code quality, efficiency) against the `932fc85` commit. Found 1 bug and 6 quality issues, all fixed.
2. **Documentation updates for swarm + harness** — Complete. Added comprehensive swarm documentation to both `CLAUDE.md` and `harness/CLAUDE.md`.
3. **Next: top-to-bottom swarm review against v5 plan + Chestnut flowchart** — Planned. The user wants to review the full swarm implementation against the original v5 plan and the Chestnut Agent Primitives flowchart (HTML document provided inline) to identify gaps, missing features, and alignment issues.

## Critical References

1. **Chestnut Agent Primitives Flowchart** — An HTML document provided inline by the user (not saved to disk). Defines the target architecture with 7 primitives: (1) Research, (2) Code Change lifecycle with plan revision loops + implement/verify loops + human review with 3 outcomes (merge/revision/full restart), (3) Project decomposition into dependency graphs, (4) Parent tickets, (5) Project plan & dependency graph, (6) Project Orchestrator heartbeat, (7) Lead FDE heartbeat. Key design principles: don't wait for human input, agents answer own questions first, Linear is source of truth.
2. **v5 plan** — Not yet located. The user referenced "the original v5 plan" — this likely lives in `thoughts/` somewhere. The next session should search for it (e.g., `thoughts/swarm/` or `thoughts/CoreyCole/plans/` with "v5" in the name).
3. **Human gates implementation plan**: `thoughts/CoreyCole/plans/2026-03-02_00-16-57_swarm-human-gates-post-pr-lifecycle.md`

## Recent changes

All changes are uncommitted on `feature/agent-swarm`:

**Bug fix — `review_feedback` cleared immediately after being set:**
- `harness/internal/db/queries/swarm.sql:183-184` — Removed `review_feedback = NULL` from `ClearSwarmWorkflowGate` query so rejection feedback survives for `buildEnv` to pass to the next session
- `harness/internal/swarmorch/manager.go:1410-1415` — Reordered calls: `ClearSwarmWorkflowGate` now runs before `UpdateSwarmWorkflowReviewFeedback` (was after, which nullified the feedback)

**Code quality refactors:**
- `harness/internal/swarm/enums.go:163-169` — Added `GateAction` typed enum (`GateActionApprove`, `GateActionReject`) replacing raw `"approve"`/`"reject"` strings
- `harness/internal/swarmorch/manager.go:1258-1283` — Extracted `requireAwaitingReview()` helper (deduplicates gate preamble from `ApproveGate` and `RejectGate`)
- `harness/internal/swarmorch/manager.go:1468-1507` — Extracted `completeWorkflow()` helper (deduplicates ~30 lines between `advanceWorkflow` and `ApproveGate`)
- `harness/internal/server/swarm_dashboard.go:78-99` — Extracted `fetchWorkflowDetailData()` helper (deduplicates 5-query data-fetching from `handleSwarmWorkflowDetail` and `renderWorkflowDetail`)
- `harness/internal/server/swarm_dashboard.go:196-201` — Extracted `reviewerFromContext()` helper (deduplicates reviewer extraction in dashboard handlers)
- `harness/views/swarm/dashboard.templ:508` — Uses `swarm.GateActionApprove` constant instead of raw string
- `harness/internal/db/sqlc/` — Regenerated after query change

**Documentation:**
- `CLAUDE.md` — Added Swarm Orchestrator section, updated Agent Hierarchy diagram, added swarm env vars
- `harness/CLAUDE.md` — Added 10 new sections: Swarm overview, workflow types, state machine, human gates, API routes (13), hooks (6), dashboard (10 routes), configuration, integrations, DB schema, session environment vars. Added swarm packages to Key Packages table.

## Learnings

1. **`ClearSwarmWorkflowGate` was a data-destroying query**: The original SQL included `review_feedback = NULL`, which meant `RejectGate`'s call to `UpdateSwarmWorkflowReviewFeedback` was immediately undone by the next line calling `ClearSwarmWorkflowGate`. Fix: remove `review_feedback = NULL` from the clear query and reorder calls.

2. **`swarm` vs `swarmorch` separation**: `internal/swarm/` is pure domain (enums, state machine, env config — no I/O). `internal/swarmorch/` is the orchestrator (Manager with DB, HTTP, Linear, Graphite, Discord, Temporal integrations). This is a deliberate architectural choice.

3. **Gate architecture has two mechanisms**: (a) Configurable gates via `IsGatedTransition()` — intercepted before state machine computation. (b) Always-on `PhaseHumanReview` — built into state machine transitions, intercepted after computation. Both call `enterGate()` but arrive through different paths.

4. **`just check` runs from project root** (`/home/deploy/creative-mode`), not from `harness/`. The harness justfile doesn't have a `check` recipe — that's in the root justfile which delegates to `scripts/check.sh`.

5. **sqlc regeneration** needed after query changes: `cd harness && sqlc generate`. templ regeneration happens automatically via `just check` (through the check script).

## Artifacts

- `CLAUDE.md` — Updated with swarm sections
- `harness/CLAUDE.md` — Updated with comprehensive swarm documentation
- `harness/internal/db/queries/swarm.sql:183-184` — Fixed `ClearSwarmWorkflowGate`
- `harness/internal/swarm/enums.go:163-169` — New `GateAction` type
- `harness/internal/swarmorch/manager.go` — `requireAwaitingReview()`, `completeWorkflow()` helpers, string literal replacements
- `harness/internal/server/swarm_dashboard.go` — `fetchWorkflowDetailData()`, `reviewerFromContext()` helpers
- `harness/views/swarm/dashboard.templ:508` — Typed constant usage
- `harness/internal/db/sqlc/swarm.sql.go` — Regenerated

## Action Items & Next Steps

The next agent should:

1. **Find the v5 plan** — Search `thoughts/` for the "v5" plan document referenced by the user. Likely in `thoughts/swarm/` or `thoughts/CoreyCole/plans/`.
2. **Save the Chestnut flowchart** — The HTML flowchart was provided inline. Consider saving it to `thoughts/swarm/chestnut-agent-primitives-flowchart.html` for reference.
3. **Review swarm implementation top-to-bottom against v5 plan + Chestnut flowchart** — Systematically compare:
   - Task classification & routing (flowchart section 1) vs `internal/swarm/classify.go`
   - Code change lifecycle (flowchart section 2) vs state machine + manager
   - Plan revision loop vs `MaxPlanRevisions` + `PhasePlanReview` → `PhaseCodePlan` retry
   - Implement & verify loop vs `MaxVerifyRetries` + `PhaseVerify` → `PhaseImplement` retry
   - Human review 3 outcomes (merge/revision/full restart) vs gate approve/reject + `previousWorkflowID`
   - Project lifecycle (flowchart section 3) vs project workflow type + `SpawnProjectChildren`
   - Project plan verification (flowchart section 5.1) vs `PhaseProjectReview`
   - Orchestration heartbeats (flowchart sections 6+7) vs `StartMaintenance()` + health/stall detection
   - Design principles alignment
4. **Identify gaps** — What does the Chestnut flowchart specify that isn't implemented? What's implemented that diverges from the flowchart?
5. **Commit all changes** — The current session's changes (bug fix, refactors, docs) are uncommitted.

## Other Notes

- All tests pass: `go test ./internal/swarm/... ./internal/swarmorch/...` and `just check` both clean.
- The Chestnut flowchart mentions concepts not yet in the codebase: "Lead FDE" (maps roughly to the swarm Manager's maintenance loop), "OpenClaw" as human interface to Lead FDE (partially maps to the president agent), code change sub-types (feature/bugfix/prototype/refactor — `classify.go` exists but sub-type routing isn't in the state machine), Slack integration (not implemented, Discord is used instead).
- The flowchart shows plan revision creating a "new Linear ticket" linked to parent — current implementation retries on the same ticket with attempt increment. This is a potential gap.
