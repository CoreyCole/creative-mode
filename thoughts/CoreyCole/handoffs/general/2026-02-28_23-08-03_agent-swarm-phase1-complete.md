---
date: 2026-02-28T23:08:03-08:00
researcher: CoreyCole
git_commit: d59646f490f0343da1b647d407e0d6ed4355f787
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives v5 — Phase 1 Complete"
tags: [implementation, agent-swarm, database, state-machine, sqlc, skills]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives v5 — Phase 1 Complete

## Task(s)

1. **Phase 1: Foundation** — COMPLETED. All 14 deliverables from the v5 plan's Phase 1 are implemented:
   - 9 database tables (7 base + 2 learning)
   - sqlc queries + generated Go code
   - 8 typed Go enum types with exhaustive switch coverage
   - State machine with 28 table-driven tests (all passing)
   - Reaper exclusion for `cm-swarm-` prefix
   - `thoughts/swarm/` directory structure (11 subdirs)
   - 2 skills (`swarm-conventions`, `swarm-setup`) + 3 document templates
   - CLAUDE.md updated
   - `just check` passes (all harness, site, and template checks green)

2. **Phase 2: Core Skills** — NOT STARTED. This is the next step.

## Critical References

- **v5 Plan (authoritative)**: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`
- **v4 Plan (orchestration base)**: `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md`
- **Workflow & Context Passing**: `thoughts/CoreyCole/plans/2026-02-28_22-00-00_swarm-workflow-and-context-passing.md`

## Recent changes

All files are new (greenfield implementation). Key changes:

- `harness/internal/swarm/enums.go` — 8 typed enum types (Phase, SessionResult, WorkflowStatus, WorkflowType, EventType, MilestoneStatus, LearningCategory, LearningSeverity)
- `harness/internal/swarm/statemachine.go` — `DetermineNextPhase`, `SkillForPhase`, `SwarmConfig` with named constants
- `harness/internal/swarm/statemachine_test.go` — 28 test cases with `t.Parallel()`
- `harness/internal/db/migrations/006_swarm_tables.sql` — 9 tables with CHECK constraints, indexes, default config row
- `harness/internal/db/queries/swarm.sql` — 24 queries for 7 base tables
- `harness/internal/db/queries/swarm_learnings.sql` — 11 queries for 2 learning tables
- `harness/sqlc.yaml` — 9 column type overrides + 26 rename entries
- `harness/internal/db/sqlc/swarm.sql.go` + `swarm_learnings.sql.go` + updated `models.go` (generated)
- `harness/internal/claude/claude.go:311-315` — added `cm-swarm-` to reaper skip list
- `harness/internal/db/db.go:99` — registered migration 006
- `.claude/skills/swarm-conventions/SKILL.md` + `templates/` (3 files)
- `.claude/skills/swarm-setup/SKILL.md`
- `thoughts/swarm/` — 11 subdirectories with `.gitkeep` files
- `CLAUDE.md` — added `thoughts/swarm/` to project structure table

## Learnings

### Lint Requirements (golangci-lint)

The harness uses strict linting. Key requirements discovered during implementation:

1. **exhaustive**: Every `switch` on a typed enum must have a `default` case, or list all cases explicitly. Missing this causes lint failure.
2. **misspell**: Use `canceled` not `cancelled` (American English). This affected both Go const names and SQL CHECK constraint values.
3. **mnd** (magic number detector): Extract numeric constants — even for config defaults. Use named `const` blocks.
4. **paralleltest**: All test functions AND subtests in `t.Run` must call `t.Parallel()`.
5. **tagliatelle**: JSON struct tags must use camelCase (`maxSessions` not `max_sessions`).
6. **unparam**: Functions that always return the same value get flagged. Inlined `firstPhaseForType` into `DetermineNextPhase`.
7. **revive/early-return**: Nested `if` blocks inside switch cases need restructuring — use `break` or guard clause pattern.
8. **cyclop**: Complex functions need `//nolint:cyclop` pragma with justification.

### SQL Schema Notes

- All datetime columns use `TEXT` type with `DEFAULT (datetime('now'))`, not `TIMESTAMP`. The existing `TIMESTAMP → time.Time` sqlc override would interfere if we used TIMESTAMP.
- The migration uses `canceled` (not `cancelled`) in CHECK constraints to match Go enum values.
- Default config row uses camelCase JSON keys to match Go struct tags: `{"maxSessions":4,"heartbeatSeconds":120,...}`

### sqlc Override Pattern

Use the structured `go_type: {import, type}` format for enum column overrides:
```yaml
- column: "swarm_workflows.phase"
  go_type:
    import: "creative-mode/harness/internal/swarm"
    type: "Phase"
```
The `import` path must be the full Go module path. This gives compile-time type safety — generated query functions accept/return `swarm.Phase` etc.

### State Machine Design

- `DetermineNextPhase` takes primitive args (not struct pointers) for testability
- `ResultContextLimit` is a v5 addition — resumes same phase without incrementing attempt
- `transitionByPhase` is extracted as a separate function to manage cyclop complexity
- All workflow types start at `PhaseResearch` — standalone research workflows transition directly to `PhaseDone`

## Artifacts

- `harness/internal/swarm/enums.go`
- `harness/internal/swarm/statemachine.go`
- `harness/internal/swarm/statemachine_test.go`
- `harness/internal/db/migrations/006_swarm_tables.sql`
- `harness/internal/db/queries/swarm.sql`
- `harness/internal/db/queries/swarm_learnings.sql`
- `harness/sqlc.yaml` (modified)
- `harness/internal/db/db.go` (modified — line 99)
- `harness/internal/claude/claude.go` (modified — line 311-315)
- `harness/internal/db/sqlc/swarm.sql.go` (generated)
- `harness/internal/db/sqlc/swarm_learnings.sql.go` (generated)
- `harness/internal/db/sqlc/models.go` (generated — updated with swarm models)
- `.claude/skills/swarm-conventions/SKILL.md`
- `.claude/skills/swarm-conventions/templates/ticket-description.md`
- `.claude/skills/swarm-conventions/templates/research-doc.md`
- `.claude/skills/swarm-conventions/templates/plan-doc.md`
- `.claude/skills/swarm-setup/SKILL.md`
- `thoughts/swarm/` (11 subdirectories)
- `CLAUDE.md` (modified)

## Action Items & Next Steps

### Phase 2: Core Skills — START HERE

Per the v4 plan (lines 933-1050+), Phase 2 delivers 6 core skills + learning/handoff Go code:

1. **Create `harness/internal/swarm/learnings.go`** — `CapturePlanIssue`, `CaptureCodeBug`, `CaptureTerminalFailure`, `CaptureSuccessPattern`, `GetLearningContext` (assembles top learnings by phase + critical + ticket-specific)
2. **Create `harness/internal/swarm/handoffs.go`** — `resolveHandoffPath` (glob `thoughts/swarm/handoffs-*/*_{ticketID}_*.md`, sort by timestamp, return most recent)
3. **Create `.claude/skills/swarm-research/SKILL.md`** — research primitive (~120 lines)
4. **Create `.claude/skills/swarm-code-plan/SKILL.md`** — code change plan primitive (~120 lines)
5. **Create `.claude/skills/swarm-code/SKILL.md`** — implementation primitive (~100 lines)
6. **Create `.claude/skills/swarm-code-verify/SKILL.md`** — code verification primitive (~100 lines)
7. **Create `.claude/skills/swarm-code-pr/SKILL.md`** — PR creation primitive (~100 lines)
8. **Create `.claude/skills/swarm-plan-review/SKILL.md`** — plan review primitive (~120 lines)

Each skill SKILL.md needs:
- YAML frontmatter (name, description, allowed-tools)
- Preamble: read `$CM_SWARM_HANDOFF_PATH` and `$CM_SWARM_LEARNING_CONTEXT_PATH` if set
- Phase-specific instructions
- RESULT comment format
- Handoff writing instructions

### Subsequent Phases (reference only)

- **Phase 3**: 6 project/support skills (swarm-project, swarm-project-plan, swarm-project-review, swarm-project-verify, swarm-status, swarm-resume)
- **Phase 4**: Temporal workflows + hooks + dashboard + learning loop + observability
- **Phase 5**: Integration testing + documentation

## Other Notes

### Build Constraints (CRITICAL)

- **NEVER run `cargo build/clippy/check`, `go build`, `templ generate`, or `just generate` directly on macOS.** Denied in `.claude/settings.json`.
- **Always use `just check` from the project root** — it uses `CARGO_TARGET_DIR=/tmp/cm-check-target` to isolate from Docker.
- `sqlc generate` IS safe to run on host (only generates Go source files).

### Pre-existing Site Lint Failure

The `site (golangci-lint)` step was failing before this work (it also failed during our `just check` runs alongside the swarm issues). It now passes — no site changes were made.

### Changes NOT Yet Committed

All Phase 1 changes are unstaged. The next agent should commit them or the user should be asked whether to commit.
