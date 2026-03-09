---
summary: "Swarm Temporal orchestration chains stages mostly sequentially with explicit fan-out/fan-in points, while passing data through typed in-memory structs between activities. Agent subprocesses exchange outputs through /tmp/swarm artifact files that are parsed/validated before downstream stages proceed, and final synthesized documents are written under thoughts/swarm and indexed in DB artifacts. Execution handoffs and failures are durably traceable through span trees, task messages, and artifact records keyed by task_id."
---

# Research Document

The swarm Temporal chain is built around two workflows in `harness/internal/swarmorch/workflows.go`: `ResearchWorkflow` and `CodeChangePlanWorkflow`, both primarily sequential with bounded parallel stages (`workflows.go:256-347`, `352-563`).

## Orchestration shape: sequential backbone with explicit fan-out/fan-in

`ResearchWorkflow` runs a single parallel block inside `runResearchSteps(...)`: generate questions, fan out `RunResearchAgent` per question via `workflow.Go`, fan in all findings through a channel, then synthesize (`workflows.go:126-245`, especially `162-206`). Outside that block, task status/event/span setup, document write, artifact persistence, and completion are sequential (`workflows.go:277-347`).

`CodeChangePlanWorkflow` reuses the same inline research block (not a child workflow) and adds a second fan-out/fan-in for domain planners: classify domains, run `RunSpecialistPlanner` per domain in parallel, collect outputs, synthesize plan, then write/persist/finalize (`workflows.go:390-516`, `442-489`, `519-563`).

## How artifacts and intermediate data are passed between stages

Data handoff is hybrid:

- **In-memory typed structs between Temporal activities**: e.g., `QuestionArtifact -> []ResearchFinding -> SynthesizeResult`, and for planning `ClassifyResult -> []PlannerOutput -> PlanSynthesizeResult` (`types.go:57-88`; `workflows.go:206-220`, `454-462`).
- **Filesystem boundary for agent subprocesses**: each agent activity writes to `/tmp/swarm/<taskID>/...`, then the same activity reads/parses/validates and returns typed values (`activities.go:75`, `95-99`, `119`, `138`, `157-161`, `181`, `209-244`).
- **Durable published outputs**: final synthesized document strings are written to `thoughts/swarm/research/<slug>.md` or `thoughts/swarm/project-plans/<slug>.md`, then persisted as DB artifact pointers (`workflows.go:203-206`, `445-448`, `282-302`, `468-488`; `activities.go:269-283`, `401-416`).

So `/tmp/swarm` is subprocess interchange, while `thoughts/swarm` is durable output.

## Validation and schema gates that control progression

Before downstream stages execute, activity results must parse and validate. The ingest path is: format/normalize artifact, unmarshal (front matter or YAML), map to typed struct, validate (`activities.go:229-248`; `artifact.go:29-61`, `108-199`, `204-319`).

Key gates include question count limits, required rationale/findings/files/confidence, min document lengths, planner enum/domain constraints, and non-empty verification/check arrays (`artifact.go:204-319`). Because workflow transitions depend on successful `.Get(...)`, validation errors block progression (`workflows.go:135-146`, `225-236`, `388-403`, `472-503`).

A nuance: unknown fields are tolerated by a permissive fallback unmarshal if strict decode fails (`artifact.go:193-199`), so extra agent keys may pass if required typed fields/validators still succeed.

## API start path and workflow/task identity handoff

From the API layer, both research and code-plan starts go through `startSwarmTask(...)` (`swarm_api.go:19`), which creates a pending `swarm_tasks` row first (with request text and generated short task ID), then starts Temporal and writes back `workflow_id` (`swarm_api.go:35-49`, `67-71`).

Workflow IDs are deterministic from task IDs:

- `swarm-research-<taskID>` for `ResearchWorkflow` (`manager.go:86-108`)
- `swarm-codeplan-<taskID>` for `CodeChangePlanWorkflow` (`manager.go:117-139`)

Typed workflow inputs include `TaskID`, `RequestText`, `RepoRoot` (`manager.go:102-107`, `133-138`). If start fails, task status is set to failed (`swarm_api.go:54-64`).

## Persistence model for inter-stage handoffs and failures

Three persistent record types capture orchestration state:

- **Spans (`swarm_spans`)**: execution graph with `parent_span_id`, status, input/output, errors, timing, metadata (`006_swarm_tables.sql:39-63`; `swarm.sql:50-76`). Agent runtime emits nested `tool_call`, `llm_call`, and `question` spans under agent spans (`agent.go:286-449`), with aggregate usage metadata on completion (`agent.go:178-191`).
- **Task messages (`swarm_task_messages`)**: narrative/user/system timeline entries (`007_swarm_messages.sql:1-7`; `swarm.sql:111-118`; `activities.go:357-389`).
- **Artifacts (`swarm_artifacts`)**: durable index of output docs by `(task_id, artifact_type, file_path)` (`006_swarm_tables.sql:28-35`; `swarm.sql:40-47`; `activities.go:268-279`).

Failures are represented at multiple layers: failed task status, failed spans with error payloads, orphan/stale running span cleanup, and narrative surfacing (`swarm.sql:60-63`, `70-73`, `79-83`, `121-126`; `agent.go:645-691`, `725-750`; `swarm_api.go:52-64`).

## Contradictions, gaps, and confidence

No substantive contradictions were found across the findings; they are mutually reinforcing (all high confidence). A practical gap is that findings describe control/data flow and persistence thoroughly but do not provide empirical runtime examples (sample span trees or concrete payload instances) beyond code-path behavior. Overall confidence remains **high** based on consistent file-level evidence.