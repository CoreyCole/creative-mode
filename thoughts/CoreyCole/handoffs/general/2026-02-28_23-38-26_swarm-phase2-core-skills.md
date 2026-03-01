---
date: 2026-02-28T23:38:26-08:00
researcher: CoreyCole
git_commit: 8cbc9fb0a2a412aed57059b3e4934381867371cb
branch: feature/agent-swarm
repository: creative-mode
topic: "Agent Swarm Phase 2: Core Skills — Learnings, Handoffs, and 6 Skills"
tags: [implementation, swarm, learnings, handoffs, skills, agent-orchestration]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Phase 2 — Core Skills Implementation

## Task(s)

**Phase 2 of the agent swarm system** — all tasks **completed**:

1. **learnings.go + learnings_test.go** — COMPLETED. Learning capture functions (`CapturePlanIssue`, `CaptureCodeBug`, `CaptureTerminalFailure`, `CaptureSuccessPattern`) and context assembly (`GetLearningContext`) with 8 test cases.
2. **handoffs.go + handoffs_test.go** — COMPLETED. Handoff path resolution (`ResolveHandoffPath`), directory mapping (`HandoffDir`), and filename formatting with 6 test cases.
3. **swarm-conventions SKILL.md update** — COMPLETED. Added Preamble, Handoff Writing template, and Swarm Environment Variables sections.
4. **6 skill SKILL.md files** — COMPLETED. Created swarm-research, swarm-code-plan, swarm-plan-review, swarm-code, swarm-code-verify, swarm-code-pr.
5. **Verification** — COMPLETED. `just check` passes (all lint clean), 42 tests pass (28 existing Phase 1 + 14 new Phase 2).

Phase 1 was committed at `8cbc9fb` on `feature/agent-swarm`. Phase 2 changes are **unstaged** — not yet committed.

## Critical References

- `CLAUDE.md` (project root) — Agent system architecture, build constraints, project structure
- `harness/CLAUDE.md` — Harness server patterns, sqlc conventions, lint rules

## Recent changes

- `harness/internal/swarm/learnings.go` — New file: DBTX interface, 5 capture functions, GetLearningContext, SQL queries as constants
- `harness/internal/swarm/learnings_test.go` — New file: 8 test cases with in-memory SQLite, test helpers
- `harness/internal/swarm/handoffs.go` — New file: ResolveHandoffPath, HandoffDir, FormatHandoffFilename, sanitizeFilename
- `harness/internal/swarm/handoffs_test.go` — New file: 6 test cases with temp directories
- `.claude/skills/swarm-conventions/SKILL.md:109-163` — Added 3 new sections (Preamble, Handoff Writing, Env Vars)
- `.claude/skills/swarm-research/SKILL.md` — New skill
- `.claude/skills/swarm-code-plan/SKILL.md` — New skill
- `.claude/skills/swarm-plan-review/SKILL.md` — New skill
- `.claude/skills/swarm-code/SKILL.md` — New skill
- `.claude/skills/swarm-code-verify/SKILL.md` — New skill
- `.claude/skills/swarm-code-pr/SKILL.md` — New skill

## Learnings

1. **Import cycle avoidance**: The plan specified `sqlc.Querier` as the DB interface for learnings.go, but `swarm` → `sqlc` → `swarm` creates a cycle (sqlc imports swarm for enum types like `swarm.Phase`). Solution: defined a local `DBTX` interface matching `*sql.DB`/`*sql.Tx` signatures (`ExecContext`/`QueryContext`) and wrote SQL queries as string constants directly in the swarm package.

2. **Lint rules for this codebase**:
   - `errcheck`: Must handle `rows.Close()` and `db.Close()` return values (use named returns with deferred closure for production code, `t.Logf` in tests)
   - `gosec`: Directory perms must be `0o750` or less, file perms `0o600` or less (even in tests)
   - `noctx`: Must use `ExecContext`/`QueryContext`, never `Exec`/`Query`
   - `usetesting`: Must use `t.Context()` instead of `context.Background()` in tests
   - Linter auto-reformats long function calls to multi-line

3. **Test pattern**: The swarm package uses same-package tests (not `_test` suffix), `t.Parallel()` on all tests and subtests, table-driven tests. For DB tests, create in-memory SQLite with the minimal schema (just the tables needed, no CHECK constraints required for tests).

4. **Phase enum has 11 values**: research, code_plan, plan_review, implement, verify, pr, project_plan, project_review, project_verify, done, failed. All switch statements on Phase need a default case (`exhaustive` linter).

## Artifacts

- `harness/internal/swarm/learnings.go` — Learning capture and context assembly
- `harness/internal/swarm/learnings_test.go` — Learning tests
- `harness/internal/swarm/handoffs.go` — Handoff path utilities
- `harness/internal/swarm/handoffs_test.go` — Handoff tests
- `.claude/skills/swarm-conventions/SKILL.md` — Updated conventions reference
- `.claude/skills/swarm-research/SKILL.md` — Research phase skill
- `.claude/skills/swarm-code-plan/SKILL.md` — Code planning skill
- `.claude/skills/swarm-plan-review/SKILL.md` — Plan review skill (read-only)
- `.claude/skills/swarm-code/SKILL.md` — Implementation skill
- `.claude/skills/swarm-code-verify/SKILL.md` — Verification skill (read-only)
- `.claude/skills/swarm-code-pr/SKILL.md` — PR creation skill

## Action Items & Next Steps

1. **Commit Phase 2** — All changes are unstaged. Stage and commit to `feature/agent-swarm`.
2. **Phase 3: Orchestrator** — The next phase should implement the orchestrator that ties everything together:
   - `harness/internal/swarm/manager.go` — Workflow lifecycle management (start, advance, retry, fail)
   - Wire up the state machine transitions to actually spawn Claude Code sessions with the appropriate skill
   - Set `CM_SWARM_*` environment variables when spawning sessions
   - Call `GetLearningContext` and write to a temp file, set `CM_SWARM_LEARNING_CONTEXT_PATH`
   - Call `ResolveHandoffPath` and set `CM_SWARM_HANDOFF_PATH`
   - Parse RESULT comments from session output to determine next transition
3. **Dashboard integration** — Wire swarm workflow/session data into the mayor dashboard or a new swarm dashboard page
4. **Linear integration** — Connect the swarm skills to actually post comments on Linear tickets (the skills describe the format but the orchestrator needs to handle the API calls)

## Other Notes

- The existing Phase 1 code at `8cbc9fb` includes: 9 DB tables (migration 006), sqlc queries, 8 typed enums (`enums.go`), state machine with 28 tests (`statemachine.go`), and 2 skills (swarm-setup, swarm-conventions).
- The `thoughts/swarm/` directory structure already exists with empty subdirectories for all handoff types (handoffs-code, handoffs-research, handoffs-plan-reviews, etc.).
- The `SkillForPhase()` function in `statemachine.go` maps phases to skill names — these now all have corresponding SKILL.md files.
- The site golangci-lint was failing independently of my changes during early check runs but passed in the final verification. This may be a flaky pre-existing issue.
