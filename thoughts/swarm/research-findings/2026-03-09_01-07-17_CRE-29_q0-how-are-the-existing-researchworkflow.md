---
question: How are the existing ResearchWorkflow and CodeChangePlanWorkflow implemented end-to-end in internal/swarmorch (workflow stages, activity calls, status transitions, artifact writes, and span/event emission), and which reusable orchestration patterns already exist for adding new primitives?
confidence: high
filesReferenced:
  - harness/agents/skills/swarm-conventions.md
  - harness/agents/skills/temporal-conventions.md
  - harness/internal/swarmorch/workflows.go
  - harness/internal/swarmorch/activities.go
  - harness/internal/swarmorch/agent.go
---

Both workflows are Temporal workflows in `internal/swarmorch/workflows.go`, using two activity-option profiles (`agentActivityOpts` with 20m/60s heartbeat + retries, and `infraActivityOpts` with 30s + retries) to separate long agent runs from short infra/DB calls (`workflows.go:46-66`).

## End-to-end: `ResearchWorkflow`

- Entry and root span:

  - Creates a workflow span (`SpanTypeWorkflow`, name `research`) via `CreateSpanActivity`, with deterministic UUID via `workflow.SideEffect` (`workflows.go:68-79, 607-635`).
  - Supports child mode (`ParentSpanID` set) by attaching workflow span under parent and skipping task-level status/event handling (`workflows.go:599-606, 620-633, 646-683, 771-821`).

- Failure/cancel cleanup contract:

  - `deferredCleanup` is always deferred; on error it fails root span, fails all running task spans, and (if standalone) sets task status to `failed` or `canceled` and emits `task.failed`/`task.canceled` (`workflows.go:81-140, 638-641`).
  - Uses `workflow.NewDisconnectedContext` so cleanup still runs when workflow context is canceled (`workflows.go:95-96`).

- Status/event transitions (standalone path):

  - `UpdateTaskStatus(...running)` then `EmitEvent("research.started")` (`workflows.go:645-667`).
  - On success: `UpdateTaskStatus(...completed)` then `EmitEvent("task.completed")` (`workflows.go:802-819`).

- Stage pipeline (`runResearchSteps`):

  1. `question_generation` stage span (`createStageSpan`), then `GenerateResearchQuestions` activity writes a question artifact path under `thoughts/swarm/research-questions/...yaml` (`workflows.go:393-426`).
  1. Narrative message announcing decomposition (`PostNarrativeMessage`) (`workflows.go:428-445`).
  1. `parallel_research` stage span; one `workflow.Go` per question runs `RunResearchAgent` with per-question output path `thoughts/swarm/research-findings/...md`; fan-in via `workflow.Channel` (`workflows.go:447-499, 501-509`).
  1. Narrative message announcing synthesis (`workflows.go:514-529`).
  1. `synthesis` stage span; `SynthesizeResearchDoc` generates combined doc with output path `thoughts/swarm/research/...md` (`workflows.go:531-560`).
  1. Each stage completes via `CompleteSpanActivity` through `completeStageSpan` (`workflows.go:186-203, 427, 511, 563`).

- Artifact/document persistence:

  - `WriteDocument(outputPath, synth.Document)` writes final research markdown into repo-root-relative path (`workflows.go:713-722`, `activities.go:381-397`).
  - `PersistArtifact(taskID, ArtifactTypeResearchDoc, outputPath)` stores artifact row and returns artifact ID (`workflows.go:725-734`, `activities.go:260-277`).
  - Emits narrative completion message with file path (`workflows.go:737-748`).

- Span completion and eventing:

  - Completes workflow span with truncated synthesized JSON (`CompleteSpanActivity`) (`workflows.go:751-760`).
  - DB span lifecycle activity methods are in `activities.go` (`CreateSpanActivity`, `CompleteSpanActivity`, `FailSpanActivity`) (`activities.go:300-359`).

## End-to-end: `CodeChangePlanWorkflow`

- Entry/root setup:

  - Creates workflow span (`SpanTypeWorkflow`, name `code_change_plan`) (`workflows.go:833-856`).
  - Defers same cleanup helper (non-child mode) (`workflows.go:862-863`).
  - Sets task running and emits `code_plan.started` (`workflows.go:865-886`).

- Research-first composition:

  - Fetches ticket context once for prompt injection (`fetchTicketContext`) (`workflows.go:888-892, 239-261`).
  - Starts Linear status/labels via fire-and-forget helper (`runLinearActivity`) (`workflows.go:894-903, 220-237`).
  - Executes `ResearchWorkflow` as a child workflow with `ParentSpanID=plan root span` and shared `TaskID`, so research span tree nests under plan root and research task-level status is skipped (`workflows.go:906-927`, `workflows.go:599-606, 620-633, 646-683`).

- Planning stage:

  1. Creates `planning` stage span (`workflows.go:949-952`).
  1. `ClassifyPlanDomains` activity (artifact path `thoughts/swarm/plan-classifications/...yaml`) (`workflows.go:954-975`).
  1. Narrative message listing planner domains (`workflows.go:978-1001`).
  1. Fan-out specialist planners with `workflow.Go` + channel fan-in; each planner writes `thoughts/swarm/specialist-plans/...md` (`workflows.go:1003-1050, 1052-1060`).
  1. `SynthesizePlanDoc` creates final project plan (`thoughts/swarm/project-plans/...md`) (`workflows.go:1062-1086`).
  1. Completes planning stage span (`workflows.go:1088`).

- Artifact/document persistence:

  - `WriteDocument(planOutputPath, planResult.Document)` then `PersistArtifact(...ArtifactTypePlanDoc...)` (`workflows.go:1091-1113`).

- Task closeout:

  - Narrative completion message (`workflows.go:1142-1161`).
  - `UpdateTaskStatus(...completed)`, `EmitEvent("task.completed")`, and root `CompleteSpanActivity` (`workflows.go:1163-1192`).

## Agent activity execution and span/event emission path

- Workflow agent activities (`GenerateResearchQuestions`, `RunResearchAgent`, `SynthesizeResearchDoc`, `ClassifyPlanDomains`, `RunSpecialistPlanner`, `SynthesizePlanDoc`) are thin wrappers over generic `runAgentActivity[T]` with script name, output path, optional markdown body field mapping, and validator (`activities.go:64-177`).
- `runAgentActivity` resolves output path under `repoRoot`, ensures directories, invokes `runAgent`, formats artifact file, unmarshals YAML+markdown artifact, and validates typed output (`activities.go:195-258`).
- `runAgent` in `agent.go` creates an agent span, runs JS subprocess, writes `start` JSONL, processes stdout JSONL protocol, reads output file, and completes/fails span with aggregate metadata (`agent.go:57-223`).
- JSONL loop emits nested spans:
  - Tool events: `tool_call` spans on start/end (`agent.go:319-372`).
  - Inference events: `llm_call` spans start/end with token/cost metadata accumulation (`agent.go:383-438`).
  - Question events: `question` span + grep-based answer + `answer` message back to agent (`agent.go:449-490, 492-579`).
- Span helper functions publish EventBus events (`span.started`, `span.completed`, `span.failed`) while updating DB rows (`agent.go:626-793`).

## Reusable orchestration patterns already present

1. **Deterministic workflow scaffolding**: side-effect UUID generation and workflow-time timestamps for replay safety (`workflows.go:68-79, 171-184`).
1. **Two-tier activity profiles**: `agent` vs `infra` options reused across all stages (`workflows.go:46-66`).
1. **Standard root span lifecycle**: create root workflow span, defer centralized cleanup, complete root span on success (`workflows.go:607-641, 751-760, 833-863, 1183-1192`).
1. **Stage-span wrappers**: `createStageSpan`/`completeStageSpan` utility for phase demarcation (`workflows.go:171-203`).
1. **Fan-out/fan-in concurrency**: `workflow.Go` + `workflow.Channel` used in both research and planning parallelism (`workflows.go:451-499, 1003-1060`).
1. **Generic agent-activity adapter**: `runAgentActivity[T]` with per-script config and validation (`activities.go:179-258`).
1. **Artifact path convention helper**: `swarmOutputPath` with PST timestamp + optional ticket prefix + slug sanitation (`workflows.go:158-169`).
1. **Task-level narrative stream**: `PostNarrativeMessage` inserts orchestrator messages and emits swarm narrative events (`activities.go:361-379`; calls in `workflows.go:432-445, 517-529, 739-748, 981-1001, 1145-1161`).
1. **Status/event transition pattern**: running → completed with explicit event emission; failed/canceled via deferred cleanup path (`workflows.go:645-667, 802-819, 865-886, 1163-1179, 81-140`).
1. **Child workflow composition**: parent workflow nests child span trees via `ParentSpanID` and child-mode status suppression (`workflows.go:599-606, 620-633, 645-683, 906-927`).
1. **Artifact persistence pattern**: write file then persist DB artifact reference (ID used for downstream links) (`workflows.go:713-734, 1091-1113`; `activities.go:260-277, 381-397`).
1. **Fire-and-forget external integration wrapper**: `runLinearActivity` used for non-fatal side integrations (`workflows.go:220-237` and multiple call sites).
