# Swarm `/create_plan` Skill Plan (Prompt-First, No Code Yet)

## Goal
Create a swarm-ready `/create_plan` skill design that uses `.claude/skills` as the canonical prompt basis, so Primitive 2 (`code_change_plan`) consistently produces high-quality planning artifacts after mandatory research.

## Source Skills (Prompt Baseline)
Primary:
- `.claude/skills/create_plan.md`
- `.claude/skills/research_codebase.md`

Supporting:
- `.claude/skills/codebase-locator.md`
- `.claude/skills/codebase-analyzer.md`
- `.claude/skills/codebase-pattern-finder.md`
- `.claude/skills/thoughts-locator.md`
- `.claude/skills/thoughts-analyzer.md`
- `.claude/skills/review_plan.md`

## Scope
In scope:
- Define the `/create_plan` prompt contract for swarm orchestration.
- Define how research outputs are consumed by the planner context window.
- Define deterministic markdown schema for plan artifacts in `thoughts/swarm/plans/`.
- Define Temporal task boundaries, inputs, outputs, and handoff payloads.

Out of scope:
- Implementing skill files or workflow code.
- Running plan review/verification execution loops.
- Applying code changes.

## Design Principles
1. Research-first is mandatory for all code-change planning tasks.
2. Planner context is isolated from raw user text after normalization.
3. Prompts must produce parseable, deterministic section headers.
4. Facts must be evidence-backed with file references.
5. Open questions are allowed only when explicitly blocking.

## `/create_plan` Skill Contract (Swarm Version)

### Input
- `task_id`
- `request_text`
- `research_doc_path` (required)
- `repo_context` (branch, commit, root)
- `constraints` (optional)
- `acceptance_criteria` (optional)

### Output
- `plan_doc_path` in `thoughts/swarm/plans/`
- `plan_summary` (short)
- `open_questions` (array)
- `risk_flags` (array)
- `status` (`complete` | `blocked`)

## Prompt Stack

### 1. Planner System Prompt
Built from `.claude/skills/create_plan.md`, but adapted for swarm:
- Remove interactive back-and-forth requirements.
- Enforce non-interactive completion in one workflow step.
- Keep skepticism and codebase verification behavior.
- Require citing evidence from research document and direct file reads.

### 2. Planner Pre-Flight Prompt
Built from `.claude/skills/research_codebase.md`:
- Re-validate critical claims from research.
- Resolve file paths and conventions in current branch state.
- Mark stale assumptions explicitly.

### 3. Plan Writer Prompt
Produces deterministic artifact schema (below) with no code patches.

### 4. Plan Quality Gate Prompt
Derived from `.claude/skills/review_plan.md` as self-check rubric:
- Completeness
- Technical correctness
- Sequencing
- Risk coverage
- Test/verification strategy placeholders

## Required Plan Artifact Schema
Path pattern:
- `thoughts/swarm/plans/YYYY-MM-DD_HH-MM-SS_<task_id>_<slug>.md`

Required sections:
1. `# Code Change Plan: <title>`
2. `## Request Summary`
3. `## Research Inputs`
4. `## Current State`
5. `## Desired End State`
6. `## Out of Scope`
7. `## Implementation Phases`
8. `## File-by-File Change Plan`
9. `## Risks and Mitigations`
10. `## Verification Strategy (Deferred to Primitive 2.1)`
11. `## Open Questions / Blockers`
12. `## Confidence`

## Temporal Orchestration Mapping

### Workflow: `CodeChangePlanWorkflow`
1. Validate request payload.
2. Execute child `ResearchWorkflow` (or require completed research artifact id).
3. Run `CreatePlanTask` with prompt stack above.
4. Persist artifact metadata.
5. Emit SSE completion event with both research+plan paths.

### Activities
- `LoadResearchArtifact`
- `RunPlannerPreflight`
- `RunPlanSynthesis`
- `RunPlanQualityGate`
- `PersistPlanArtifact`
- `PublishPlanEvent`

## SQLite Additions (Planning-Focused)
1. `swarm_plan_runs`
- `id`, `task_id`, `run_id`, `research_artifact_id`, `status`, `confidence`, `created_at`

2. `swarm_plan_findings`
- `id`, `plan_run_id`, `finding_type` (`risk`|`blocker`|`assumption`), `content`, `severity`

3. `swarm_plan_sections`
- `id`, `plan_run_id`, `section_name`, `present`, `quality_score`

## SSE + Dashboard Behavior
Emit specific events for `/create_plan` flow:
- `plan.preflight.started`
- `plan.preflight.completed`
- `plan.synthesis.started`
- `plan.synthesis.completed`
- `plan.quality_gate.completed`
- `plan.artifact.persisted`
- `task.completed`

Datastar views:
- Task row status chips for each plan stage.
- Artifact links to research doc and plan doc.
- Inline blockers panel when `status=blocked`.

## Prompt Safety Rules
1. Never claim code was changed.
2. Distinguish facts vs assumptions.
3. If evidence is missing, emit blocker instead of inventing detail.
4. Keep section headers exact for parser compatibility.
5. Limit prose; prioritize actionable implementation steps.

## Migration Path
1. Freeze this plan schema and prompt contracts.
2. Create swarm-specific `/create_plan` prompt templates under harness prompt assets.
3. Wire Temporal `CodeChangePlanWorkflow` to these templates.
4. Add artifact validator for required section headers.
5. Add dashboard stage rendering for plan lifecycle events.

## Acceptance Criteria
1. Given a code-change request, swarm always generates research first.
2. `/create_plan` consumes research and emits one deterministic plan artifact.
3. Plan artifact is parseable and contains all required sections.
4. SSE dashboard displays per-stage planning progress and artifact links.
5. No code changes are performed in this phase.

## Notes
- In this workspace, skills were discovered at `.claude/skills/*`; if `.clade/skills` is intended, mirror/symlink later.
- This plan is intentionally non-implementation and prompt-contract focused.
