---
date: 2026-02-28T21:15:00-08:00
researcher: CoreyCole
git_commit: 35a328e9ffabe7a52efa9eda64dc275702f14dcf
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives v5 — Continuous Learning Layer"
tags: [implementation, strategy, agent-swarm, temporal, sqlc, learning, self-improvement]
status: ready_for_implementation
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives v5 — Continuous Learning Layer

## Task(s)

1. **v4 Plan Review (Temporal + Self-Improvement Focus)** — COMPLETED. Found 3 critical issues, 7 concerns, 5 questions. Review saved to `thoughts/CoreyCole/reviews/2026-02-28_20-52-37_agent-swarm-primitives-v4-temporal-and-self-improvement_review.md`.

2. **v5 Plan Creation** — COMPLETED. Added continuous learning layer on top of v4. Plan saved to `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`. User approved the plan.

3. **Implementation** — NOT STARTED. Plan is ready for Phase 1 implementation.

## Critical References

- **v5 Plan (authoritative)**: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`
- **v4 Plan (orchestration base, still authoritative for non-learning components)**: `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md`
- **v4 Review (Temporal + self-improvement focus)**: `thoughts/CoreyCole/reviews/2026-02-28_20-52-37_agent-swarm-primitives-v4-temporal-and-self-improvement_review.md`
- **v4 Review (original)**: `thoughts/CoreyCole/reviews/2026-02-28_20-18-11_agent-swarm-primitives-v4_review.md`
- **Previous handoff (v4.1)**: `thoughts/CoreyCole/handoffs/general/2026-02-28_20-36-52_agent-swarm-primitives-v4-review-and-fixes.md`

## Recent Changes

No code changes — only new plan and review documents:
- `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md` — new v5 plan
- `thoughts/CoreyCole/reviews/2026-02-28_20-52-37_agent-swarm-primitives-v4-temporal-and-self-improvement_review.md` — focused review

## Learnings

### What v5 Adds to v4

v4 is mechanically sound for orchestration but has no self-improvement. v5 adds:

1. **`swarm_learnings` + `swarm_learning_digests` tables** — SQLite-backed learning store
2. **4 automatic capture points** — plan issues, code bugs, terminal failure retrospectives, success patterns
3. **Learning context injection** — relevant learnings injected into every Claude Code session via `CM_SWARM_LEARNING_CONTEXT_PATH` env var
4. **Relevance scoring** — time decay + reference boosting + auto-archival
5. **Daily digest generation** — deterministic pattern detection + action items
6. **President integration** — autonomous with PR gate: president reads digests, spawns Claude Code sessions to update SKILL.md files, creates PRs for human review
7. **`ContributeSkillImprovement`** — mirrors existing `ContributeLearning()` from `harness/internal/mayor/learning.go:14`

### Key Design Decisions

1. **Autonomous with PR gate** — User confirmed the president should autonomously propose skill improvements but all changes go through PR review. No manual intervention needed for detection→proposal, but humans always approve.

2. **Learning capture is non-fatal** — All capture functions are called from state machine transitions but failures are logged, not propagated. The state machine never blocks on learning capture.

3. **v4 remains authoritative for orchestration** — v5 is an additive layer. All v4 decisions about Temporal, state machine, skills, hook-based completion, and dashboard are unchanged.

4. **`thoughts/shared/swarm/`** — New directory for retrospectives and digests. Uses `shared/` because swarm learnings are system-level, not per-user.

### v4 Review Issues Resolved in v5

1. **Transaction isolation** — `ReadTicketQueue` wrapped in `BEGIN IMMEDIATE` via `WithTx` on store interface
2. **Hook directory conflicts** — Swarm hook written to temp dir, path passed via `--hooks-dir` or `CLAUDE_HOOKS_DIR`
3. **EventBus synthetic key** — Documented that `Subscribe("swarm")` uses a non-worldID key

### Open Questions (Not Blocking)

1. RESULT comment timing — does it exist in Linear when the on-stop hook fires? (v4 open question, still unanswered but likely fine since `linear-cli` is synchronous)
2. `findAvailableSlot` exact implementation — not specified in v4 plan, needs to be defined during Phase 4
3. Temporal `start-dev` in production — acceptable for v1 single-VPS deployment but should bind to localhost only

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md` — v5 plan (approved)
- `thoughts/CoreyCole/reviews/2026-02-28_20-52-37_agent-swarm-primitives-v4-temporal-and-self-improvement_review.md` — focused review (3 critical, 7 concerns, 5 questions)
- `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` — v4 plan (still authoritative for orchestration)
- All prior v1-v4 plans, reviews, and handoffs (see v4.1 handoff for full list)

## Action Items & Next Steps

1. **Begin Phase 1 implementation** — Foundation (v4 deliverables + learning schema). Key deliverables:
   - `harness/internal/db/migrations/006_swarm_tables.sql` — 9 tables (7 from v4 + `swarm_learnings` + `swarm_learning_digests`)
   - `harness/internal/db/db.go` — add migration to `migrationFiles` slice + bootstrap check
   - `harness/internal/db/queries/swarm.sql` — CRUD for 7 v4 tables
   - `harness/internal/db/queries/swarm_learnings.sql` — CRUD for 2 learning tables
   - `harness/internal/swarm/enums.go` — 8 typed enum types (6 from v4 + LearningCategory + LearningSeverity)
   - `harness/sqlc.yaml` — add 9 overrides (7 from v4 + 2 for learnings)
   - `harness/internal/swarm/statemachine.go` — phase transitions
   - `harness/internal/swarm/statemachine_test.go` — table-driven tests
   - `.claude/skills/swarm-conventions/SKILL.md` + 3 templates
   - `.claude/skills/swarm-setup/SKILL.md`

2. **Phase 2** — Core skills (6 skills) + `learnings.go` + learning context preamble + `POST /api/swarm/learnings`

3. **Phase 3** — Project & support skills (6 skills, no learning-specific additions)

4. **Phase 4** — Temporal + completion hooks + dashboard + learning loop (wiring capture functions, digest generation, president skill, dashboard learnings section)

5. **Phase 5** — Integration testing & CLAUDE.md documentation

## Other Notes

### Document Evolution
v1 plan → v1 review → v2 plan → v2 handoff → v3 plan → v3 review → v4 plan → v4 review → v4.1 fixes → **v4 Temporal+self-improvement review → v5 plan (current)**

### Key Codebase Entry Points (same as v4 handoff, still valid)
- Existing reaper (MUST modify): `harness/internal/claude/claude.go:293-337`
- Hook completion pattern: `harness/internal/claude/claude.go:138-253` and `harness/internal/server/server.go:686-711`
- Mayor ContributeLearning (reuse pattern): `harness/internal/mayor/learning.go:14-69`
- President skills (extend): `harness/internal/president/skills.go`
- EventBus: `harness/internal/events/bus.go`
- Migration registration: `harness/internal/db/db.go:93-99`
- sqlc config: `harness/sqlc.yaml`
- Main wiring: `harness/main.go`
- Server struct: `harness/internal/server/server.go:49-66`
