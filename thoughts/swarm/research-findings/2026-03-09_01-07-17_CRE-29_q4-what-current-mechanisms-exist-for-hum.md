---
question: What current mechanisms exist for human gates and lifecycle state transitions (e.g., awaiting approval/review, resume/retry/cancel actions, parent-child task linkage, and versioned artifact lineage), and how are those states represented in DB schema, Temporal workflow inputs, and UI views?
confidence: high
filesReferenced:
  - harness/agents/skills/swarm-conventions.md
  - harness/agents/skills/temporal-conventions.md
  - harness/internal/db/migrations/006_swarm_tables.sql
  - harness/internal/db/migrations/007_swarm_messages.sql
  - harness/internal/db/queries/swarm.sql
  - harness/internal/db/sqlc/enums.go
  - harness/internal/swarmorch/workflows.go
  - harness/internal/swarmorch/manager.go
  - harness/internal/swarmorch/activities.go
  - harness/internal/swarmorch/agent.go
  - harness/internal/swarmorch/types.go
  - harness/internal/server/swarm_api.go
  - harness/internal/server/swarm_dashboard.go
  - harness/internal/server/server.go
  - harness/internal/auth/auth.go
  - harness/internal/auth/middleware.go
  - harness/internal/db/queries/checkpoints.sql
  - harness/internal/server/server.go
---

Swarm currently models lifecycle state primarily as task/question/span execution status, plus explicit cancel handling; there is no swarm-specific DB state for “awaiting human approval/review” or “resume” transitions.

- Task lifecycle states are constrained in DB to `pending|running|completed|failed|canceled` on `swarm_tasks.status` (006_swarm_tables.sql:6-8), mirrored as typed enums in SQLC (`TaskStatus*`) (sqlc/enums.go:27-35).
- Sub-question lifecycle states are `pending|running|completed|failed` on `swarm_research_questions.status` (006_swarm_tables.sql:21-23), mirrored as `QuestionStatus*` (sqlc/enums.go:63-69).
- Span lifecycle states are `running|completed|failed` on `swarm_spans.status` (006_swarm_tables.sql:51-53), mirrored as `SpanStatus*` (sqlc/enums.go:18-24).

Temporal workflow lifecycle handling (including cancellation):

- Swarm workflows run under Temporal and use task queue `swarm-agents` (temporal-conventions.md; swarm-conventions.md).
- Workflow cleanup logic explicitly maps canceled execution to `TaskStatusCanceled` and emits `task.canceled` events when not in child mode (workflows.go:85-132).
- Child-workflow mode suppresses task-level status/event updates (`isChild` path), with parent responsible for top-level task lifecycle (workflows.go:87-88, 94, 124).
- Manager-level cancel mechanism calls Temporal `CancelWorkflow` by workflow ID (manager.go:164-166).

API/UI cancel mechanisms:

- REST endpoint for cancel exists at `/api/swarm/tasks/:taskID/cancel` (server.go:174; swarm_api.go:184-223).
- Dashboard POST action also exists (`/swarm/cancel`) and invokes the same manager cancel path, then refreshes tabs (server.go:206; swarm_dashboard.go:368-444).
- Cancel precondition in API: task must have `workflow_id` set; otherwise request fails with bad request (swarm_api.go:205).
- API success response is `{ "status": "canceled" }` after Temporal cancel request (swarm_api.go:223).

Resume/retry coverage:

- No task-level “resume” endpoint/state is present in swarm task schema or routes (status enums/route list above).
- Retries are represented at Temporal activity retry-policy level (workflow activity options/policies), not as a separate persisted swarm task state (workflows.go:18-23, 56-58, 67).
- DB status set does not include a retrying/resumable status (006_swarm_tables.sql:6-8; sqlc/enums.go:27-35).

Human gates (“awaiting approval/review”) currently present in app:

- Human approval gating exists at auth/user access level, not as swarm task state: users can be `pending` and routed to a pending-approval page via middleware/handler (auth/middleware.go:58; auth/auth.go:262-263; server.go:78,190).
- Swarm task/message schema includes `swarm_task_messages` with roles `user|orchestrator|system`, representing conversational flow but no dedicated review/approval status column (007_swarm_messages.sql:1-6; sqlc/enums.go:54-60).

Parent-child linkage mechanisms:

1. Span hierarchy (core swarm lineage):

- `swarm_spans.parent_span_id` self-references `swarm_spans(id)` (006_swarm_tables.sql:42).
- `GetSwarmSpanTree` recursive CTE returns depth-annotated parent→child tree for rendering (queries/swarm.sql:95-113).
- Agent protocol includes optional `ParentSpanID`; runtime creates child tool/question/LLM spans under agent spans and includes orphan-child fail handling if parent fails (types.go:223; agent.go refs at 43,73,371,407,461,814-841 from grep results).

2. Workflow-child semantics:

- Workflow input includes `ParentSpanID` for child workflow linkage in tracing context (workflows.go:36,190-198,325).
- Internal stage runners propagate `parentSpanID` across question_generation / parallel_research / synthesis stage spans (workflows.go:401,421,429,471,535).

3. External parent-child linkage (Linear followups):

- Activity `CreateLinearFollowup` creates a new research ticket and links it to a parent issue via relation call (activities.go:503-528).

Versioned artifact lineage:

- Swarm artifact records are append-only rows with `(id, task_id, artifact_type, file_path, created_at)` (006_swarm_tables.sql:28-35; queries/swarm.sql:41-50).
- Artifact types are constrained to `research_doc|plan_doc` (006_swarm_tables.sql:31; sqlc/enums.go:46-51).
- Retrieval is by task, ordered by creation time (`ORDER BY created_at`), which provides chronological artifact history per task (queries/swarm.sql:50).
- There is no explicit artifact version number or parent-artifact reference in swarm schema; lineage is represented by task association + timestamp order.

UI representation of lifecycle and lineage:

- Swarm dashboard is a tabbed view for Chat, Agents, Spans, Artifacts (documented in project docs/skills and served by swarm dashboard handlers in `swarm_dashboard.go`).
- Cancel action is exposed in dashboard handler path and triggers reload of tasks/spans/artifacts/messages after cancellation request (swarm_dashboard.go:368-444).
- Span tree query (`GetSwarmSpanTree`) is designed for hierarchical rendering (“parent before children”, depth field) to support trace/tree UI (queries/swarm.sql:95-113).

Related non-swarm lineage mechanism in product:

- Checkpoint lineage for worlds uses `checkpoints.parent_checkpoint_id` and dedicated lineage view route `/:worldID/lineage/:cpID` (db/queries/checkpoints.sql:2,6,27; server.go:263,781-792). This is separate from swarm artifacts but demonstrates current lineage UI pattern elsewhere in the app.

Overall representation summary:

- DB schema encodes deterministic execution states for tasks/questions/spans and hierarchy via parent span IDs.
- Temporal workflow inputs carry parent span linkage and child-workflow semantics; cancellation is first-class via Temporal cancel path and mapped to `canceled` task status.
- UI/API exposes cancel and status-derived views; approval/review gating currently exists at user-auth level rather than as a swarm task review state.
