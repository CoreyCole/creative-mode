---
date: 2026-03-01T20:49:14-08:00
researcher: CoreyCole
git_commit: 611f9d826f9ab76f6c413a4d889aad397e111966
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Project Decompose Phase Implementation"
tags: [implementation, swarm, project-workflow, decompose, state-machine]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Project Decompose Phase Implementation

## Task(s)

**Completed**: Inserted a `project_decompose` phase into the project workflow between `research` and `project_plan`. This phase reads the initial research output, identifies child research topics, and the orchestrator spawns independent research workflows for each. When all child research completes, aggregated findings feed into the project_plan phase.

New project workflow flow:
```
research → project_decompose → [child research workflows] → project_plan → project_review → [child code workflows] → project_verify → done
```

This mirrors the existing pattern where `project_verify` waits for child code workflows — we added the same pattern earlier in the pipeline for research children.

## Critical References

- Plan document: The plan was provided inline in the user message (no separate file). It was titled "Plan: Swarm Workflow Routing Redesign — Align with Chestnut Agent Primitives".
- Chestnut vision: `thoughts/swarm/chestnut-agent-primitives-flowchart.html` — the multi-research decomposition concept this implements.

## Recent changes

All changes are uncommitted on `feature/agent-swarm`:

- `harness/internal/swarm/enums.go:14` — Added `PhaseProjectDecompose` enum constant
- `harness/internal/swarm/enums.go:29` — Added to `Valid()` switch
- `harness/internal/swarm/statemachine.go:103` — Changed project research success route from `PhaseProjectPlan` to `PhaseProjectDecompose`
- `harness/internal/swarm/statemachine.go:149-153` — Added `PhaseProjectDecompose` case: success → `PhaseProjectPlan`, logic_failure → `PhaseFailed`
- `harness/internal/swarm/statemachine.go:206-207` — Added `SkillForPhase` mapping: `PhaseProjectDecompose` → `"swarm-project-decompose"`
- `harness/internal/swarm/handoffs.go:48-73` — Added `ResolveDecomposePath()` function (globs `thoughts/swarm/decompose/*_{ticketID}_*.md`)
- `harness/internal/swarm/handoffs.go:130` — Added `PhaseProjectDecompose` to `HandoffDir()` → `"handoffs-project"`
- `harness/internal/swarm/env.go:32-33` — Added `AggregatedResearchPath` field with `CM_SWARM_AGGREGATED_RESEARCH_PATH` envconfig tag
- `harness/internal/swarmorch/project.go:269-370` — Refactored `advanceProject()` into `advanceProjectDecompose()` and `advanceProjectVerify()`. Decompose path aggregates child research findings, advances to project_plan, and spawns the plan session.
- `harness/internal/swarmorch/project.go:259-261` — Updated `CheckProjectProgress()` to also monitor `project_decompose` phase
- `harness/internal/swarmorch/project.go:487-516` — Added `ParseDecomposeOutput()` — parses markdown table of research topics
- `harness/internal/swarmorch/project.go:524-623` — Added `SpawnProjectResearchChildren()` — creates child research tickets and spawns workflows
- `harness/internal/swarmorch/project.go:625-630` — Added `hasResearchChildren()` helper
- `harness/internal/swarmorch/project.go:634-703` — Added `aggregateResearchFindings()` — reads all child research outputs, concatenates into single aggregated document in `thoughts/swarm/research-aggregated/`
- `harness/internal/swarmorch/manager.go:847-864` — Added decompose→project_plan interception in `advanceWorkflow()` (spawns research children, heartbeat monitors)
- `harness/internal/swarmorch/manager.go:1006-1025` — Added aggregated research path resolution in `buildEnv()` for `project_plan` phase
- `harness/internal/swarm/statemachine_test.go:182-199` — Updated project research test, added decompose success/failure tests
- `harness/internal/swarm/statemachine_test.go:268` — Added `PhaseProjectDecompose` to `TestSkillForPhase` action phases
- `harness/internal/swarmorch/project_test.go` — **New file** — tests for `ParseDecomposeOutput()`
- `.claude/skills/swarm-project-decompose/SKILL.md` — **New file** — skill for the decompose phase
- `.claude/skills/swarm-project-plan/SKILL.md` — Added `CM_SWARM_AGGREGATED_RESEARCH_PATH` env var and updated research reading instructions
- `harness/CLAUDE.md:111` — Updated project workflow type description
- `harness/CLAUDE.md:249` — Added `CM_SWARM_AGGREGATED_RESEARCH_PATH` to env var table
- `CLAUDE.md:44,70` — Updated workflow types and skills list

## Learnings

1. **Linter strictness**: gosec requires directory permissions `0o750` or less and file permissions `0o600` or less. The `perfsprint` linter flags `fmt.Sprintf` when simple string concatenation suffices. The `unparam` linter catches unused function parameters — use `_` for context params that are kept for interface consistency.

2. **Pattern reuse**: The decompose→research children pattern closely mirrors the existing project_review→project_verify→code children pattern. Both use `buildProjectGraph()` and `completedChildTickets()` for progress monitoring, and both intercept the state machine transition in `advanceWorkflow()` to spawn children instead of immediately advancing.

3. **Child ticket identifiers**: Research children use `{ticketID}-r{num}` format (e.g., `CRE-5-r1`) to distinguish from code children which use `{ticketID}-{num}` format.

4. **Aggregated research**: Written to `thoughts/swarm/research-aggregated/{timestamp}_{ticketID}_aggregated.md`, resolved via `filepath.Glob` in `buildEnv()` when the phase is `project_plan` and workflow type is `project`.

## Artifacts

- `harness/internal/swarm/enums.go` — Updated enum
- `harness/internal/swarm/statemachine.go` — Updated state machine
- `harness/internal/swarm/handoffs.go` — Updated handoffs + new `ResolveDecomposePath()`
- `harness/internal/swarm/env.go` — Updated env struct
- `harness/internal/swarmorch/project.go` — Core decompose orchestration logic
- `harness/internal/swarmorch/manager.go` — Updated workflow advancement + env building
- `harness/internal/swarm/statemachine_test.go` — Updated tests
- `harness/internal/swarmorch/project_test.go` — New test file
- `.claude/skills/swarm-project-decompose/SKILL.md` — New skill
- `.claude/skills/swarm-project-plan/SKILL.md` — Updated skill
- `harness/CLAUDE.md` — Updated docs
- `CLAUDE.md` — Updated docs

## Action Items & Next Steps

1. **Commit changes** — All changes are uncommitted. Review and commit to `feature/agent-swarm`.
2. **Manual E2E test** — Start a project workflow and verify it goes through `research → project_decompose → spawns research children → aggregates → project_plan`. Can use dry-run mode.
3. **Create `thoughts/swarm/decompose/` directory** — The skill writes to this directory but it won't exist until the first decompose session runs. The `ResolveDecomposePath()` will return empty (no error) if the directory doesn't exist, which is fine.
4. **Consider recursive research** — The plan explicitly defers recursive research spawning (research workflows creating child research) as a future enhancement.
5. **Dashboard updates** — The swarm dashboard at `/swarm/:id` should already render `project_decompose` in the phase timeline since it reads phases dynamically, but verify visually.

## Other Notes

- The `PhaseProjectDecompose` was added between `PhaseProjectPlan` and `PhaseProjectReview` in the const block, but the linter reformatted it to be between `PhaseProjectPlan` and `PhaseProjectReview` with consistent alignment. This is cosmetic only.
- The existing `TestLogicFailureFallthroughPhases` test does NOT include `PhaseProjectDecompose` because decompose has explicit logic_failure handling (it falls through to the default `PhaseFailed` via the switch default, same as other phases without special logic_failure handling like `PhaseCodePlan`). This is correct behavior.
- The swarm dashboard phase timeline rendering at `views/swarm/` reads phases dynamically so the new phase should appear without template changes.
