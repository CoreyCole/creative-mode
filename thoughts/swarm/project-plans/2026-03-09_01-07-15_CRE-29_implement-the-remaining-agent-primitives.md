# Implementation Plan

## Dependency-aware phase summary

The critical dependency chain is: **DB schema/state contract → workflow/signal semantics → API/dashboard gate controls → end-to-end lifecycle loops**. PR creation additionally depends on net-new GitHub integration, and project orchestration should be enabled last due history/complexity risk. Old entrypoints remain as compatibility shims until classifier routing is stable.

## Phase 1: Data model and lifecycle contract foundation

**Goal:** Introduce durable lifecycle, gate, revision, and PR persistence required by all downstream workflows.

### Scope

- Add an additive migration (do not rewrite historical migration in place) for lifecycle/lineage persistence.
- Extend status support to include human-wait states (minimum: `awaiting_plan_review`/`awaiting_human_plan_approval`, `awaiting_pr_review`, revision-related state).
- Add dedicated tables/fields for:
  - lifecycle phase/substate and gate metadata,
  - revision lineage (attempt number, root linkage, predecessor linkage, trigger type),
  - gate decisions (approved/revision/full_restart/merged + feedback),
  - PR linkage per revision,
  - artifact version/attempt linkage.
- Update `harness/internal/db/queries/swarm.sql` and regenerate sqlc.
- Register new migration in `harness/internal/db/db.go`.
- Backfill existing swarm tasks/artifacts with revision=1 defaults.

### Key files

- `harness/internal/db/migrations/00X_swarm_lifecycle_lineage.sql`
- `harness/internal/db/db.go`
- `harness/internal/db/queries/swarm.sql`
- generated sqlc files under `harness/internal/db/sqlc/*`

### Commit boundary

- Commit includes migration + queries + sqlc regeneration + compile-safe no-op call-site updates.

### Verification

**Automated**

- `cd harness && sqlc generate`
- `cd harness && go test ./internal/db/... ./internal/swarmorch/... ./internal/server/...`
- `just check`

**Manual**

- Confirm existing tasks remain readable in dashboard/API after migration.
- Create a new task and verify waiting statuses are queryable as active states.

### Risks / cross-domain concerns

- Enum/status expansion can break old writers unless API/workflow updates land quickly after migration.
- Dual source-of-truth risk between task status and lifecycle table must be documented with transition rules.

______________________________________________________________________

## Phase 2: Temporal workflow primitives and manager wiring

**Goal:** Add missing workflow entrypoints and signal-driven waits while preserving existing orchestration patterns.

### Scope

- Implement/register workflows using existing span/fanout/child patterns:
  - `TaskRouterWorkflow`
  - `CodeChangeLifecycleWorkflow`
  - `PlanRevisionLoopWorkflow`
  - `ImplementationVerificationLoopWorkflow`
  - `PRCreationWorkflow` (stage or child)
  - `HumanReviewGateWorkflow` (or signal-driven stage)
  - `ProjectWorkflow`
- Add/extend activities in `activities.go` for new agent scripts and persistence transitions.
- Add manager methods for signaling decisions (`approve`, `request revision`, `restart`, `resume`) and lifecycle starts.
- Ensure deterministic signal handling and continue-as-new strategy for long waits/high history growth.

### Key files

- `harness/internal/swarmorch/workflows.go`
- `harness/internal/swarmorch/activities.go`
- `harness/internal/swarmorch/types.go`
- `harness/internal/swarmorch/manager.go`

### Commit boundary

- Commit contains workflow/activity scaffolding + manager registrations behind feature flag.

### Verification

**Automated**

- `cd harness && go test ./internal/swarmorch/...`
- `temporal workflow list --namespace swarm`

**Manual**

- Start a test workflow and verify signal-driven wait/resume works without polling.

### Risks / cross-domain concerns

- Determinism hazards if decision context is read outside activity/signal payloads.
- Human wait states can bloat history unless continue-as-new is applied.

______________________________________________________________________

## Phase 3: Unified task classification and routing endpoints

**Goal:** Replace manual primitive selection with classification-first routing while keeping compatibility.

### Scope

- Add `agents/task-classifier.js` and classifier activity/types.
- Introduce unified API route `POST /api/swarm/tasks` that starts router workflow.
- Refactor dashboard start flow to use the same resolver path (remove manual `new_task_type` branch).
- Preserve legacy endpoints (`/research`, `/code-change-plan`) as deprecated wrappers.
- Persist and surface classifier rationale/confidence in task artifacts/messages.
- Update active-task logic in dashboard/SSE to include new waiting states.

### Key files

- `harness/internal/server/server.go`
- `harness/internal/server/swarm_api.go`
- `harness/internal/server/swarm_dashboard.go`
- `harness/internal/swarmorch/*` (resolver integration)
- `harness/agents/task-classifier.js`

### Commit boundary

- Commit includes new route + shared resolver + backward-compatible aliases.

### Verification

**Automated**

- `just check`
- `go test ./harness/internal/server/... ./harness/internal/swarmorch/... ./harness/internal/db/...`

**Manual**

- Submit ambiguous idea text via dashboard/API and verify routing to question/code_change/project.
- Validate hook-secret/session auth behavior unchanged.

### Risks / cross-domain concerns

- Misrouting risk; mitigated by explicit persisted classification report and UI visibility.

______________________________________________________________________

## Phase 4: Plan Revision Loop with human plan gate

**Goal:** Implement iterative plan approval/revision with Linear-linked lineage (`v1`, `v2`, ...).

### Scope

- Extract/reuse existing code-change planning pipeline in loop form.
- Add `agents/plan-reviewer.js` for completeness review.
- Per attempt: generate plan → reviewer output → set awaiting-plan state → wait for human decision.
- On revision request: create linked follow-up Linear ticket via existing `linear.Client` wrapper, persist feedback + increment revision.
- Encode attempt version in artifact metadata/path and span naming.

### Key files

- `harness/internal/swarmorch/workflows.go`
- `harness/internal/swarmorch/activities.go`
- `harness/agents/plan-reviewer.js`
- DB query call-sites for revision/gate writes

### Commit boundary

- Commit enables complete plan loop for code-change lifecycle while implementation loop can remain stubbed behind next phase.

### Verification

**Automated**

- `go test ./harness/internal/swarmorch/... ./harness/internal/server/... ./harness/internal/db/...`

**Manual**

- Request revision at plan gate; verify v2 attempt starts and Linear follow-up ticket is linked to parent.

### Risks / cross-domain concerns

- Ticket tree noise from repeated revisions; enforce naming/link conventions.

______________________________________________________________________

## Phase 5: Implementation & Verification Loop (Max’s Bolt)

**Goal:** Add bounded implement→verify retries with structured failure context.

### Scope

- Add `agents/implementation-agent.js` and `agents/verification-agent.js`.
- Implement iterative workflow stage:
  - implementation pass,
  - verification fan-out (unit/integration/E2E/manual checklist artifact),
  - aggregate pass/fail,
  - retry with failure bundle until max attempts.
- Add config for retry cap/backoff and terminal failure behavior.

### Key files

- `harness/internal/swarmorch/workflows.go`
- `harness/internal/swarmorch/activities.go`
- `harness/agents/implementation-agent.js`
- `harness/agents/verification-agent.js`

### Commit boundary

- Commit is independently deployable with PR stage still feature-flagged off.

### Verification

**Automated**

- `just check`
- `go test ./harness/internal/swarmorch/... ./harness/internal/server/...`

**Manual**

- Force failing verification and confirm retry includes prior failure context.
- Confirm pass path emits verification summary artifact.

### Risks / cross-domain concerns

- Cost/timeout pressure from heavy verification; requires activity timeouts and retry policies.

______________________________________________________________________

## Phase 6: PR creation integration

**Goal:** Automatically open PR after verification pass and persist linkage.

### Scope

- Implement GitHub wrapper (parallel to existing Linear wrapper) with config/env wiring.
- Add PR drafting agent (`pr-drafter.js`/`pr-summarizer.js`) to generate title/body from plan + implementation + verification artifacts.
- Add PR creation activity and persist PR metadata.
- Post PR details back to Linear via existing wrapper helpers (non-fatal).

### Key files

- `harness/internal/github/*` (new wrapper)
- `harness/internal/swarmorch/activities.go`
- `harness/main.go` (config)
- `harness/agents/pr-drafter.js` (or `pr-summarizer.js`)

### Commit boundary

- Commit includes GitHub integration behind feature flag and graceful degradation on auth/API failures.

### Verification

**Automated**

- `go test ./harness/internal/linear/... ./harness/internal/swarmorch/... ./harness/internal/server/...`
- `just check`

**Manual**

- Verify PR URL/number artifact persistence and Linear comment linkage after successful verification.

### Risks / cross-domain concerns

- External API auth/rate limits; must avoid blocking lifecycle completion semantics.

______________________________________________________________________

## Phase 7: Human PR review gate and restart paths

**Goal:** Add post-PR human decision gate with three outcomes: merge, revision needed, full restart.

### Scope

- Add API endpoints + dashboard actions for review decisions.
- Signal workflow with durable payloads.
- Outcome handling:
  - merge → complete task + Linear done update,
  - revision needed → return to plan revision loop with feedback,
  - full restart → create new Linear ticket referencing previous attempt and start fresh lifecycle lineage.
- Enforce idempotent decision processing.

### Key files

- `harness/internal/server/swarm_api.go`
- `harness/internal/server/swarm_dashboard.go`
- `harness/internal/server/server.go`
- `harness/internal/swarmorch/manager.go`
- `harness/internal/swarmorch/workflows.go`

### Commit boundary

- Commit fully closes lifecycle after PR with human-in-the-loop controls.

### Verification

**Automated**

- `go test ./harness/internal/server/... ./harness/internal/swarmorch/... ./harness/internal/db/...`

**Manual**

- Exercise merge / revision_needed / full_restart and verify state transitions, lineage rows, and Linear links.

### Risks / cross-domain concerns

- Race/double-submit on gate endpoints; require one-way transition guards.

______________________________________________________________________

## Phase 8: Project workflow and dependency orchestration

**Goal:** Implement project primitive producing decomposition, dependency graph, and execution orchestration.

### Scope

- Add project agents (`project-decomposer.js`/`project-orchestrator.js`, `project-dependency-planner.js`).
- Implement stages for decomposition, compressed summary, dependency DAG and execution plan.
- Orchestrate child workflows in parallel/sequential order by DAG.
- Persist Graphite stack plan and Linear dependency graph via existing Linear wrapper.

### Key files

- `harness/internal/swarmorch/workflows.go`
- `harness/internal/swarmorch/activities.go`
- `harness/agents/project-*.js`

### Commit boundary

- Commit can ship with feature flag disabled by default, then enabled after soak.

### Verification

**Automated**

- `go test ./harness/internal/swarmorch/... ./harness/internal/server/...`
- `just check`

**Manual**

- Run project task and validate dependency ordering, artifacts, and Linear dependency links.

### Risks / cross-domain concerns

- Highest complexity/history growth area; enable last and monitor.

______________________________________________________________________

## Phase 9: Docs, rollout controls, and deprecation cleanup

**Goal:** Safely roll out and stabilize while preventing breaking changes.

### Scope

- Update docs/runbooks (`harness/CLAUDE.md`, swarm skill docs) for new lifecycle and gate ops.
- Rollout sequence:
  1. DB migrations,
  1. workflow registration (flagged),
  1. unified routing endpoint,
  1. gate UI controls,
  1. project primitive enablement.
- Deprecate/remove manual start UI path after classifier stability.
- Retain legacy API aliases until downstream clients migrate.

### Verification

**Automated**

- `just check`
- `go test ./...`

**Manual**

- End-to-end smoke across all primitive paths in dashboard and API.

______________________________________________________________________

## Cross-domain risk register (combined)

- **Schema/API/workflow skew:** status and query updates must land before gate handlers are enabled.
- **Temporal replay determinism:** all gate/restart decisions must come from signals/activities, not nondeterministic runtime reads.
- **Workflow history growth:** human waits + loops require continue-as-new policy.
- **Integration fragility:** GitHub failures should degrade gracefully; Linear follow-up creation should be retried with clear audit trail.
- **Operational thrash:** verification loop must enforce attempt caps/backoff and terminal failure state.

## What We’re NOT Doing

- Full dashboard UX redesign beyond required controls/status visibility for new gates.
- Deep prompt-quality tuning for classifier/reviewer/implementation agents beyond functional contracts.
- Detailed Graphite execution automation internals beyond producing stack/dependency planning artifacts.
- Replacing the existing `linear.Client` wrapper (all Linear operations remain on current wrapper).
- Removing legacy research/code-change-plan start endpoints immediately; deprecation is staged.