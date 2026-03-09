---
question: How do current JS swarm agent scripts and the shared agent runtime contract work (input schema, JSONL protocol events/questions, output file schema/paths), and what script interfaces would be required to support plan-review/revision, implementation-verification looping, PR generation, and project decomposition agents?
confidence: high
filesReferenced:
  - harness/agents/skills/swarm-conventions.md
  - harness/agents/skills/temporal-conventions.md
  - harness/internal/swarmorch/types.go
  - harness/internal/swarmorch/agent.go
  - harness/internal/swarmorch/workflows.go
  - harness/agents/
---

Current swarm JS agents run as subprocesses managed by `runAgent` with a shared JSONL stdin/stdout contract and file-based artifact return path.

## Current JS agent script surface

Registered script set (in `harness/agents/`): `research-questions.js`, `research-agent.js`, `research-synthesizer.js`, `plan-orchestrator.js`, `specialist-planner.js`, `plan-synthesizer.js`, plus `linear-context-processor.js` for post-processing (`harness/agents/`, `harness/agents/skills/swarm-conventions.md`).

Workflow usage currently maps as:

- Research workflow: question generation → per-question research agents (fan-out) → synthesis (`workflows.go` runResearchSteps).
- Code change plan workflow: child research workflow → plan classification (`plan-orchestrator`) → specialist planners (fan-out) → plan synthesis (`workflows.go` CodeChangePlanWorkflow).

## Shared runtime contract (Go \<-> JS)

### Start/input envelope (Go -> agent)

`StartMessage` is written once at process start (`types.go`, `agent.go`):

- `type: "start"`
- `task`: raw JSON payload for the concrete script input type
- `systemPrompt` (optional)
- `projectContext` (optional)
- `config` with optional `model` (`AgentConfig`)

Concrete task payload structs currently defined in `types.go`:

- `GenerateQuestionsInput`
- `ResearchAgentInput`
- `SynthesizeInput`
- `ClassifyInput`
- `SpecialistInput`
- `PlanSynthesizeInput`
- `LinearContextInput`

Common fields across most script interfaces: `taskID`, `requestText`/domain-specific prompt fields, `repoRoot`, `outputPath`, and optional `ticketContext` on context-aware steps (`types.go`).

### Streaming protocol (agent -> Go)

`AgentMessage` supports three message families (`types.go`, parsed in `agent.go:readAgentLoop`):

1. `type:"event"` with optional:
   - `event`: one of `tool_execution_start`, `tool_execution_end`, `inference_start`, `inference_end`
   - `tool`, `data`, `toolCallID`
1. `type:"question"`:
   - `id`, `text` (agent asks orchestrator for context)
1. `type:"heartbeat"`

Go handles these as:

- Tool events create/complete `tool_call` spans, counted against call limits (`agent.go:handleToolEvent`).
- Inference events create/complete `llm_call` spans and aggregate usage/cost metadata from `data` into agent totals (`agent.go:handleToolEvent`, `types.go` `SpanMetadata`/`LLMUsage`).
- Questions create `question` spans; Go answers by keyword-grep over `harness/agents/skills`, `harness/`, and `templates/`, then sends `AnswerMessage {type:"answer", id, text}` back on stdin (`agent.go:handleQuestion`, `answerQuestion`).

### Completion contract

There is no terminal JSONL “result” message; the subprocess exits and Go reads artifact bytes from `outputPath` (`agent.go` after `cmd.Wait`). That file content is treated as returned artifact JSON/raw text depending on downstream parser/activity.

## Output schemas and paths

Pathing is centralized in `swarmOutputPath` (`workflows.go`):

- Root: `thoughts/swarm/<category>/`
- Filename prefix: PST timestamp `YYYY-MM-DD_HH-MM-SS`
- Optional `_TICKETID`
- Slugified suffix from request/domain/question
- Extension varies (`.yaml`/`.md`)

Observed categories in workflow code:

- `research-questions` (`.yaml`)
- `research-findings` (`.md`)
- `research` (`.md`)
- `plan-classifications` (`.yaml`)
- `specialist-plans` (`.md`)
- `project-plans` (`.md`)
- `linear-context` (`.yaml`)

Artifact structs in `types.go` define parsed content contracts:

- Question decomposition: `QuestionArtifact { questions: []SubQuestion }`
- Research finding: `ResearchFinding { question, findings, filesReferenced, confidence }`
- Research synthesis: `SynthesizeResult { document, summary, outputPath }`
- Plan classification: `ClassifyResult { planners: []PlannerSpec{type, focus} }`
- Specialist output: `PlannerOutput { domain, planSection, filesAffected, automatedVerification, manualVerification, risks, dependencies }`
- Plan synthesis: `PlanSynthesizeResult { document, summary, phaseOrder, outputPath }`
- Linear post-processing: `LinearContextOutput { comment, followups[] }`

## Script interfaces required for additional agent roles (based on existing contract patterns)

Following existing swarm conventions, additional roles would require:

1. A dedicated input struct in `types.go` with `taskID`, domain payload, `repoRoot`, `outputPath`, optional `ticketContext` as needed.
1. A corresponding output struct in `types.go` matching artifact parsing needs.
1. Workflow/activity wiring that allocates `thoughts/swarm/<new-category>/...` via `swarmOutputPath`.
1. Runtime behavior identical to existing scripts: consume `start.task`, emit JSONL `event/question/heartbeat`, write final artifact to `outputPath`, exit.

Applied to requested roles, required interface families are:

- **Plan-review/revision agents**: review-input + review-output structs; revision-input + revised-plan artifact structs; categories analogous to `plan-reviews` / `plan-revisions`.
- **Implementation-verification loop agents**: verification-input capturing plan/implementation references and iteration context; verification-output carrying pass/fail findings and checks; loop orchestration in workflow with repeated agent invocations.
- **PR generation agents**: PR-input containing implementation summary/diff metadata/branch context; PR-output containing PR title/body/checklist artifacts and any links/identifiers produced by integration activities.
- **Project decomposition agents**: decomposition-input from request/research/plan docs; decomposition-output for work packages/dependencies/ordering suitable for downstream fan-out.

These interface requirements derive from the already-established swarm model: typed task payload + JSONL telemetry/questions + file artifact handoff + workflow-managed pathing, status, spans, and persistence (`types.go`, `agent.go`, `workflows.go`).
