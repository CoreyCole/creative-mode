# Research Document

## Top-line synthesis

The current system is well-positioned for extension: existing `ResearchWorkflow` and `CodeChangePlanWorkflow` already implement reusable orchestration patterns (deferred cleanup, deterministic IDs, stage spans, child workflow composition, agent script adapters, artifact writes, and Linear updates) (`harness/internal/swarmorch/workflows.go:46-66`, `:81-140`, `:171-203`, `:906-927`; `harness/internal/swarmorch/activities.go:179-258`). However, the requested primitives require new lifecycle semantics and integrations that are not present today: no swarm task states for awaiting human approval/review, no resume/revision workflow states, and no GitHub PR integration wrapper/activity path (`harness/internal/db/migrations/006_swarm_tables.sql:6-8`; `harness/internal/db/sqlc/enums.go:27-35`; `harness/internal/linear/cli.go:12`; GitHub absence noted in findings).

## Orchestration foundations to reuse across all new primitives

A consistent implementation strategy can leverage the existing Temporal + activity model instead of introducing new execution infrastructure:

- Two activity option classes already separate long-running agent work from infra calls (`harness/internal/swarmorch/workflows.go:46-66`).
- Root workflow span + deferred cleanup pattern already handles fail/cancel deterministically (`harness/internal/swarmorch/workflows.go:81-140`).
- Stage span wrappers (`createStageSpan` / `completeStageSpan`) provide natural boundaries for plan review, implementation, verification, PR creation, and human gate waits (`harness/internal/swarmorch/workflows.go:171-203`).
- Fan-out/fan-in (`workflow.Go` + channel) can support parallel verification suites (unit/integration/E2E/manual CLI checks) (`harness/internal/swarmorch/workflows.go:451-499`, `:1003-1060`).
- Child-workflow composition with `ParentSpanID` supports nesting plan-revision and restart attempts under a parent attempt/thread (`harness/internal/swarmorch/workflows.go:906-927`, `:599-606`).
- Agent runtime contract is stable and extensible: `start` envelope, JSONL telemetry (`event/question/heartbeat`), file output handoff via `outputPath` (`harness/internal/swarmorch/agent.go`; `harness/internal/swarmorch/types.go`).

## Lifecycle gating, approval, and revision lineage

Current lifecycle representation is insufficient for requested human gates and iterative revisions:

- `swarm_tasks.status` supports only `pending|running|completed|failed|canceled` (`harness/internal/db/migrations/006_swarm_tables.sql:6-8`; `harness/internal/db/sqlc/enums.go:27-35`).
- No swarm-native “awaiting approval/review,” “revision requested,” or “resumable” status exists (same refs).
- Existing cancel path is first-class via Temporal cancel and API/dashboard handlers (`harness/internal/server/swarm_api.go:184-223`; `harness/internal/server/swarm_dashboard.go:368-444`; `harness/internal/swarmorch/manager.go:164-166`).

Lineage exists but is partial for revision-loop semantics:

- Span lineage is strong (`swarm_spans.parent_span_id`, tree query) (`harness/internal/db/migrations/006_swarm_tables.sql:42`; `harness/internal/db/queries/swarm.sql:95-113`).
- Artifact lineage is chronological only (append-only by `created_at`), without explicit version linkage (`harness/internal/db/migrations/006_swarm_tables.sql:28-35`; `harness/internal/db/queries/swarm.sql:50`).
- Linear follow-up linkage exists (`CreateLinearFollowup`) and can model v1/v2/v3 relationships externally (`harness/internal/swarmorch/activities.go:503-528`).

Implication: Plan Revision Loop and Human Review Gate can reuse existing Temporal/state-update/event patterns, but require new persisted state semantics and/or explicit event-driven gate representation beyond current status enum coverage.

## Routing and task classification integration points

Automatic classification/routing cleanly fits existing startup choke points:

- API currently hard-splits by endpoint (`/tasks/research`, `/tasks/code-change-plan`) and passes fixed primitive type/starter (`harness/internal/server/server.go:167-174`; `harness/internal/server/swarm_api.go:117-144`).
- Dashboard currently reads `new_task_type` and branches between research/code-plan (`harness/internal/server/swarm_dashboard.go:234-239`, `:267-281`).
- Primitive type selected at this point is persisted and drives Linear labels/workflow dispatch (`harness/internal/server/swarm_api.go:67-71`, `:319-327`; `harness/internal/server/swarm_dashboard.go:247-251`).

Implication: classification should be inserted before `CreateSwarmTask` and starter dispatch in both API and dashboard paths, replacing/manual-defaulting the current primitive selection branch.

## Integration surface: Linear is mature; GitHub PR is a hard gap

Linear capabilities are production-ready and already threaded through manager, activities, and workflows:

- Wrapper supports issue create/update/comment/attachments/relations/search (`harness/internal/linear/cli.go:63-184`).
- Shared client injected into manager and server (`harness/main.go:336-354`; `harness/internal/swarmorch/manager.go:67`).
- Activities bridge workflow to Linear (`FetchLinearTicket`, `UpdateLinearStatus`, `AddLinearComment`, `CreateLinearFollowup`, etc.) (`harness/internal/swarmorch/activities.go:436-557`).
- Workflows already do non-fatal Linear side effects with `runLinearActivity` (`harness/internal/swarmorch/workflows.go:220-237`).

By contrast, findings report no GitHub wrapper, auth wiring, or PR helper/activity path in inspected code.

Implication: PR Creation primitive depends on introducing a new GitHub integration layer analogous to `internal/linear` plus workflow activities and config wiring.

## Agent-script contract readiness for new primitives

Current JS/Go contract supports adding new agent roles without protocol changes:

- Add typed inputs/outputs in `types.go`.
- Register new script(s) in `agents/` using same JSONL telemetry and file-output conventions.
- Invoke via `runAgentActivity[T]` wrappers and `swarmOutputPath` categories (`harness/internal/swarmorch/activities.go:179-258`; `harness/internal/swarmorch/workflows.go:158-169`).

This directly supports requested additions:

- plan review/revision agents,
- implementation/verification agents,
- PR drafting agents,
- project decomposition/planning/dependency agents.

## Primitive-by-primitive implementation fit and constraints

### 1) Plan Revision Loop

Fit: strong for orchestration and Linear follow-up linking; weak for explicit approval states/version lineage in DB.

- Reuse stage spans + child workflows for iterative plan attempts (`workflows.go:171-203`, `:906-927`).
- Reuse `CreateLinearFollowup` for revision ticket chain (`activities.go:503-528`).
- Gap: no explicit task status for “awaiting approval/revision requested” (`006_swarm_tables.sql:6-8`).

### 2) Implementation & Verification Loop

Fit: very strong for loop orchestration, telemetry, and parallel checks.

- Reuse iterative Temporal loop pattern with agent activities and failure-context feedback.
- Parallel verification can use fan-out/fan-in primitive (`workflows.go:451-499`).
- Existing span/event model already captures nested tool/LLM/check traces (`agent.go:319-438`).

### 3) PR Creation

Fit: orchestration yes; integration no.

- Workflow stage and artifact generation are straightforward with existing patterns.
- Hard gap is absent GitHub client/config/activity plumbing (finding: no existing GitHub wrapper/helpers).

### 4) Human Review Gate

Fit: partial.

- Waiting/gating can be modeled at workflow layer, but swarm DB/API/UI currently lacks explicit review states and resume actions (status enum and endpoints lack resume/review).
- Existing human gating is auth-level pending-user approval, not task review (`harness/internal/auth/middleware.go:58`; `harness/internal/auth/auth.go:262-263`).

### 5) Project Workflow

Fit: high.

- Existing decomposition/research/synthesis and planning fan-out patterns are close analogs.
- Need new scripts/artifacts for dependency graph + Graphite/Linear dependency outputs; orchestration scaffolding exists.

### 6) Task Classification & Routing

Fit: high and localized.

- Hook into API/dashboard primitive selection points before DB insert and workflow start (`swarm_api.go:117-144`; `swarm_dashboard.go:234-239`).

## Contradictions / dual perspectives

- **State modeling sufficiency**: one perspective is that workflow-layer events could represent human gates without schema changes; opposing evidence is that current persisted statuses/endpoints cannot natively express awaiting-review/resume semantics (`006_swarm_tables.sql:6-8`; route surface in `server.go:167-174`, `:203-208`).
- **Version lineage sufficiency**: one perspective is chronological artifacts + Linear relations may be enough for v1/v2/v3; opposing evidence is no explicit artifact version/parent linkage in swarm schema (`006_swarm_tables.sql:28-35`).

## Gaps and low-confidence areas

High confidence overall across findings. Remaining uncertainty is implementation-choice, not repository fact:

- Whether to extend `swarm_tasks.status` vs represent human gates via separate review tables/events (not resolved by findings).
- Exact GitHub PR integration shape (none exists today; design is open).
- Exact persistence model for revision lineage (artifact-level version refs vs task/ticket linkage).