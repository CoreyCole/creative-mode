# Agent Swarm Phase 1 Plan: Primitive 1 (Research) + Primitive 2 (Code Change Planning)

## Goal
Design a Temporal-orchestrated swarm that can:
1. Run a **Research primitive** and output a detailed research document in `thoughts/swarm/research/`.
2. Run a **Code Change primitive (planning-only)** that always performs research first, then outputs:
- A research document in `thoughts/swarm/research/`.
- A code-change plan document in `thoughts/swarm/plans/`.

No code implementation in this phase. No actual code edits yet.

## Scope
In scope:
- Temporal workflow design for primitive 1 and 2.
- HTTP entrypoints from orchestrator/harness.
- SQLite persistence model for runs, tasks, and artifact paths.
- Linear linkage for traceability.
- Prompt contracts for separate context windows (research vs planning).
- SSE dashboard behavior with Datastar + templ.

Out of scope:
- Primitive 2.1 verification (deferred).
- Applying code changes.
- PR generation and merge automation.
- OpenClaw/Discord runtime integration (future).

## High-Level Architecture
1. Human sends request to orchestrator.
2. Orchestrator classifies intent:
- `question` -> Primitive 1 (`research`).
- `change request` -> Primitive 2 (`code_change_plan`) which internally runs research then plan.
3. Orchestrator POSTs to harness HTTP API.
4. Harness starts Temporal workflow.
5. Temporal executes fan-out/fan-in research and optional planning stage.
6. Workflow stores state in SQLite and writes artifacts to `thoughts/swarm/*`.
7. Harness emits task/run updates via SSE for Datastar dashboard.
8. Workflow completion callback gives orchestrator artifact paths.

## Primitive Definitions

### Primitive 1: `research`
Input:
- User question.
- Repo/workspace context.
- Optional constraints (areas, files, depth).

Execution:
- Main research agent generates sub-questions.
- Parallel research agents investigate sub-questions in separate context windows.
- Main agent compresses findings into one research document.

Output:
- `thoughts/swarm/research/<timestamp>_<task_id>_<slug>.md`

### Primitive 2: `code_change_plan`
Input:
- Requested change outcome.
- Acceptance criteria (if provided).

Execution:
1. Mandatory embedded research step (same model as Primitive 1).
2. Planner agent consumes research artifact.
3. Planner writes implementation plan (no code changes).

Output:
- `thoughts/swarm/research/<timestamp>_<task_id>_<slug>.md`
- `thoughts/swarm/plans/<timestamp>_<task_id>_<slug>.md`

## Temporal Design

### Workflows
1. `ResearchWorkflow`
- Validate request.
- Generate research sub-questions.
- Fan-out `RunResearchAgentActivity` in parallel.
- Fan-in results.
- Run synthesis activity to produce final research document.
- Persist artifact metadata + final status.

2. `CodeChangePlanWorkflow`
- Validate request.
- Call reusable child `ResearchWorkflow` (or equivalent shared activity set).
- Run `PlanSynthesisActivity` using research artifact.
- Write plan document.
- Persist both artifact metadata + final status.

### Activities (Phase 1 design)
- `CreateTaskRecord`
- `GenerateResearchQuestions`
- `RunResearchAgent`
- `SynthesizeResearchDoc`
- `SynthesizeCodePlanDoc`
- `PersistArtifactRecord`
- `EmitRunEvent`

### Queueing/Concurrency
- Dedicated queue for research worker pool (parallelizable).
- Dedicated queue for synthesis/planning (lower concurrency).
- Per-task max parallel researchers configurable.

## HTTP API Contracts (Harness)

### Start Research
`POST /api/swarm/tasks/research`
- Body: `{ request, requester, repo_context, constraints? }`
- Returns: `{ task_id, run_id, workflow_id, status: "queued" }`

### Start Code Change Plan
`POST /api/swarm/tasks/code-change-plan`
- Body: `{ request, requester, repo_context, acceptance_criteria? }`
- Returns: `{ task_id, run_id, workflow_id, status: "queued" }`

### Task Status
`GET /api/swarm/tasks/:task_id`
- Returns latest status + artifact paths when complete.

### SSE Stream
`GET /api/swarm/stream`
- Emits: task queued, workflow started, research fan-out progress, synthesis started, artifact created, completed/failed.

## SQLite Data Model (Phase 1)
Core tables:
1. `swarm_tasks`
- `id`, `primitive_type` (`research` | `code_change_plan`), `request_text`, `status`, `created_at`, `updated_at`, `linear_issue_id?`

2. `swarm_task_runs`
- `id`, `task_id`, `workflow_id`, `attempt`, `status`, `started_at`, `ended_at`, `error_summary?`

3. `swarm_research_questions`
- `id`, `run_id`, `question_text`, `agent_index`, `status`, `result_summary?`, `artifact_path?`

4. `swarm_artifacts`
- `id`, `task_id`, `run_id`, `artifact_type` (`research_doc` | `plan_doc`), `path`, `checksum?`, `created_at`

5. `swarm_events`
- `id`, `task_id`, `run_id`, `event_type`, `payload_json`, `created_at`

## Artifact Contracts

### Research Document (`thoughts/swarm/research`)
Required sections:
1. Request + intent.
2. Research questions asked.
3. Findings by subsystem/file.
4. Risks/unknowns.
5. Recommendations.
6. References (files, commands, external docs if any).

### Code Plan Document (`thoughts/swarm/plans`)
Required sections:
1. Requested change summary.
2. Research-informed understanding.
3. Proposed implementation approach.
4. File-by-file change plan.
5. Risks + mitigations.
6. Open questions.
7. Deferred verification strategy (placeholder for primitive 2.1).

## Prompt Architecture (Critical)
Design principle: separate context windows with explicit input/output contracts.

1. `Research Question Generator Prompt`
- Input: user request + repo context.
- Output: prioritized set of concrete, answerable sub-questions.

2. `Research Agent Prompt`
- Input: single sub-question + scoped context.
- Output: concise evidence-backed findings + referenced files.

3. `Research Synthesizer Prompt`
- Input: all parallel findings.
- Output: normalized research document (artifact contract above).

4. `Code Plan Synthesizer Prompt`
- Input: original change request + research artifact.
- Output: normalized plan document (artifact contract above), no code patches.

Prompt guardrails:
- Must separate facts vs assumptions.
- Must include unknowns and confidence.
- Must not claim code changes were applied.
- Must produce deterministic markdown section headers for downstream parsing.

## Dashboard (Datastar + templ + SSE)
Phase 1 dashboard goals:
1. Buttons/forms:
- “Start Research Task”
- “Start Code Change Plan Task”
2. Live task table via SSE:
- task id, primitive, status, started, last event, artifact links.
3. Task detail panel:
- run timeline, fan-out research question progress, final artifact paths.
4. Result actions:
- open research doc
- open plan doc (if code change plan)

## Linear Integration (Minimal for Phase 1)
- Optional `linear_issue_id` attached at task creation.
- Post status transitions as comments or labels (queued/running/completed/failed).
- Store artifact paths in final Linear update for traceability.

## Error Handling
- Retry policy for transient worker errors.
- Partial research failures allowed up to threshold, with synthesis documenting gaps.
- Hard fail if synthesis cannot produce required artifact sections.
- All failures emit SSE events and persist terminal run state.

## Milestones (Design-to-Implementation Handoff)
1. Freeze API and artifact schemas.
2. Finalize Temporal workflow signatures and activity boundaries.
3. Finalize prompt templates and output validators.
4. Implement dashboard skeleton with mocked SSE events.
5. Implement live Temporal-backed execution.

## Acceptance Criteria for This Plan
1. A human question can map to a research task that outputs one research doc in `thoughts/swarm/research/`.
2. A code change request can map to a code-change-plan task that outputs:
- one research doc in `thoughts/swarm/research/`
- one plan doc in `thoughts/swarm/plans/`
3. Dashboard can trigger both tasks and display real-time progression over SSE.
4. Orchestrator receives completion payload with artifact paths.
5. No code modifications are performed by this phase.

## Future Extensions (Not Now)
- Primitive 2.1 verification workflow.
- Multi-plan option generation and ranking.
- OpenClaw + Discord as primary front-end.
- Continuous learning loop from review/verification outcomes.
