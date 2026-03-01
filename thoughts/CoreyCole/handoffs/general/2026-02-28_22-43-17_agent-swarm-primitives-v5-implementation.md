---
date: 2026-02-28T22:43:17-08:00
researcher: CoreyCole
git_commit: 35a328e9ffabe7a52efa9eda64dc275702f14dcf
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives v5 — Implementation"
tags: [implementation, strategy, agent-swarm, temporal, sqlc, learning, self-improvement, hooks, observability]
status: ready_for_implementation
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives v5 — Implementation

## Task(s)

1. **v5 Plan Finalization** — COMPLETED. The v5 plan has gone through multiple review cycles (v1→v2→v3→v4→v4 review→v5→workflow doc review→observability review) and is ready for implementation. No code has been written yet — this is a greenfield implementation starting from Phase 1.

2. **Phase 1 Implementation** — NOT STARTED. This is the next step. See "Action Items & Next Steps" below for the full Phase 1 deliverable list.

## Critical References

- **v5 Plan (authoritative, start here)**: `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md`
- **v4 Plan (orchestration base — authoritative for non-learning, non-observability components)**: `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md`
- **Workflow & Context Passing companion doc**: `thoughts/CoreyCole/plans/2026-02-28_22-00-00_swarm-workflow-and-context-passing.md`

## Recent Changes

No code changes. All work has been planning and review documents. The v5 plan was updated during this session with:
- Companion doc reference added (line 14)
- `resolveHandoffPath` fixed to glob across ALL handoff directories, not phase-specific (lines 230-241)
- Handoff template notes standalone mode (lines 175-184)
- Learning context file moved to `/tmp/` (line 290)
- `ContributeSkillImprovement` uses git worktree instead of direct checkout (lines 357-364)
- `DecayLearningRelevance` skips if <1hr since last run (line 311)
- Reaper relationship clarified (lines 430-436)
- Full hook system added — 6 hooks with dedicated config per session (lines 438-526)
- Complete observability section added — metrics, structured logging, health API, alerting, JSONL, session inspection, Temporal UI, exact token tracking (lines 549-700+)
- Resolved 4 open questions: UUIDs not slots, RESULT timing verified, completion_test.go added, Temporal UI enabled (lines 660-668)
- Stop hook changed from HTTP to command hook to capture exact token count from tmux status line

## Learnings

### v5 Plan Structure

v5 is an **additive layer** on top of v4. v4 remains authoritative for:
- 14 Claude Code skills (names, SKILL.md structure, invocation patterns)
- 7 base database tables (`swarm_config`, `swarm_workflows`, `swarm_sessions`, `swarm_events`, `swarm_project_milestones`, `swarm_tickets`, `swarm_ticket_comments`)
- State machine phases and transitions
- Temporal workflow types (`HeartbeatWorkflow`, `SessionWorkflow`)
- Dashboard layout

v5 adds on top of v4:
- 2 learning tables (`swarm_learnings`, `swarm_learning_digests`)
- Handoff system (6 trigger points, `thoughts/swarm/` directory structure)
- 6 Claude Code hooks (SessionStart, PreToolUse, PostToolUse, PreCompact, Stop, SessionEnd)
- Full observability (metrics, structured logging, health API, Discord alerting, JSONL logs, Temporal UI, exact token tracking)
- Continuous learning loop (4 capture points, relevance decay, daily digests, president integration, skill improvement PRs)

### Key Design Decisions Made During This Session

1. **UUIDs not slot indices** — Session IDs are UUIDs. `max_sessions` enforced by `COUNT(*)` of active sessions. No `agent_index` column. Tmux names: `cm-swarm-{ticketID}-a{attempt}`.

2. **Stop hook is a command hook, not HTTP** — Because it needs to run `tmux capture-pane` locally to extract the exact token count from Claude Code's status line before POSTing to the harness. The other 5 hooks remain HTTP hooks.

3. **`thoughts/swarm/` committed to git** — Not gitignored. Handoff references work across clones. Growth is acceptable; periodic archival can be added later.

4. **Temporal UI enabled** — `--headless` removed. UI on `127.0.0.1:8233`, accessible via SSH tunnel or Tailscale.

5. **Discord alerting for failures** — Terminal failures, stalls, crash recovery, and high retry rates post to `DISCORD_PRESIDENT_CHANNEL_ID`. Alert dedup: same type + workflow_id within 1-hour window.

6. **Learning context written to `/tmp/`** — `/tmp/swarm-learning-context-{ticketID}.md`, not repo root. Ephemeral per-session file.

7. **`ContributeSkillImprovement` uses git worktree** — Avoids the crash fragility of `ContributeLearning`'s direct-checkout pattern.

### Codebase Entry Points for Implementation

- **Migration registration**: `harness/internal/db/db.go:93-99` — append `migrations/006_swarm_tables.sql` to `migrationFiles` slice
- **Existing migrations embed**: `harness/internal/db/db.go` uses `//go:embed migrations/*.sql`
- **sqlc config**: `harness/sqlc.yaml:55-63` — 2 existing overrides (TIMESTAMP→time.Time, checkpoints.status→string)
- **Existing reaper (MUST modify)**: `harness/internal/claude/claude.go:293-337` — add `cm-swarm-` prefix skip
- **Hook completion pattern to mirror**: `harness/internal/claude/claude.go:138-253` and `harness/internal/server/server.go:686-711`
- **ContributeLearning pattern to mirror**: `harness/internal/mayor/learning.go:14-69` — branch `mayor/{worldID}/learning-{timestamp}`
- **President skills to extend**: `harness/internal/president/skills.go` — 4 existing skills (mayor-status, repo-build, template-update, deploy)
- **EventBus**: `harness/internal/events/bus.go` — `Subscribe(key)` accepts arbitrary strings, `worldSubs` is `map[string][]chan any`
- **Main wiring**: `harness/main.go`
- **Server struct**: `harness/internal/server/server.go:49-66`

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-28_20-52-00_agent-swarm-primitives-v5.md` — v5 plan (approved, updated this session)
- `thoughts/CoreyCole/plans/2026-02-28_22-00-00_swarm-workflow-and-context-passing.md` — workflow companion doc
- `thoughts/CoreyCole/plans/2026-02-28_18-55-00_agent-swarm-primitives-v4.md` — v4 plan (orchestration base)
- `thoughts/CoreyCole/reviews/2026-02-28_20-52-37_agent-swarm-primitives-v4-temporal-and-self-improvement_review.md` — v4 review (3 critical, 7 concerns, 5 questions)
- `thoughts/CoreyCole/handoffs/general/2026-02-28_21-15-00_agent-swarm-primitives-v5-continuous-learning.md` — previous handoff (pre-implementation)

## Action Items & Next Steps

### Phase 1: Foundation — START HERE

This is a greenfield implementation. No swarm code exists yet. Phase 1 deliverables:

**Database & Schema:**
1. Create `harness/internal/db/migrations/006_swarm_tables.sql` — 9 tables (7 from v4 + `swarm_learnings` + `swarm_learning_digests`). Include `duration_sec` and `total_tokens` columns on `swarm_sessions`. See v4 plan for the 7 base table schemas, v5 plan for the 2 learning table schemas.
2. Register migration in `harness/internal/db/db.go` — append to `migrationFiles` slice
3. Create `harness/internal/db/queries/swarm.sql` — CRUD queries for 7 v4 tables
4. Create `harness/internal/db/queries/swarm_learnings.sql` — CRUD queries for 2 learning tables
5. Update `harness/sqlc.yaml` — add 9 column overrides (7 from v4 + 2 for learnings). See v4 plan for the 7 base overrides, v5 for the 2 learning overrides.
6. Run `sqlc generate` (inside Docker or on VPS, never on host directly)

**Go Types & State Machine:**
7. Create `harness/internal/swarm/enums.go` — 8 typed enum types: `Phase`, `SessionResult`, `WorkflowStatus`, `WorkflowType`, `EventType`, `MilestoneStatus` (from v4) + `LearningCategory`, `LearningSeverity` (from v5)
8. Create `harness/internal/swarm/statemachine.go` — `DetermineNextPhase`, `SkillForPhase`, `firstPhaseForType`. Include standalone `research` → `done` transition.
9. Create `harness/internal/swarm/statemachine_test.go` — table-driven tests for all phase transitions including retry loops
10. Add `WithTx(func(tx) error) error` to the store interface for transaction isolation

**Directory Structure & Conventions:**
11. Create `thoughts/swarm/` directory with 11 subdirectories (handoffs-research, handoffs-code, handoffs-plan-reviews, handoffs-code-reviews, handoffs-project, handoffs-project-reviews, plans, research, project-plans, retrospectives, digests)
12. Add `thoughts/swarm/` to CLAUDE.md project structure table
13. Create `.claude/skills/swarm-conventions/SKILL.md` + 3 document templates (ticket-description, research-doc, plan-doc)
14. Create `.claude/skills/swarm-setup/SKILL.md` — idempotent Linear label creation

### Subsequent Phases (for reference, not yet)

- **Phase 2**: 6 core skills + `learnings.go` + `handoffs.go` + learning context preamble
- **Phase 3**: 6 project/support skills
- **Phase 4**: Temporal + hooks + dashboard + learning loop + observability (metrics, alerts, JSONL, health API, Temporal UI)
- **Phase 5**: Integration testing + documentation

## Other Notes

### Document Evolution
v1 plan → v1 review → v2 plan → v2 handoff → v3 plan → v3 review → v4 plan → v4 review → v4.1 fixes → v4 Temporal+self-improvement review → v5 plan → workflow companion doc → **v5 observability + hooks review (current)** → this handoff

### Build Constraints (CRITICAL)
- **NEVER run `cargo build/clippy/check`, `go build`, `templ generate`, or `just generate` directly on macOS host.** These are denied in `.claude/settings.json`. They corrupt Docker's trunk builds.
- **Always use `just check` from the project root** for verification.
- The swarm system itself will run on the VPS (Nix), not macOS. But Phase 1 schema/code can be written on macOS — just verify with `just check`.

### Open Questions (deferred to later phases)
1. Temporal recovery path — document scenarios in Phase 5
2. `thoughts/swarm/` growth — periodic archival, not blocking for v1
