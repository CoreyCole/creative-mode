---
date: 2026-02-28T15:12:04-0800
researcher: CoreyCole
git_commit: 586d18dd84133dd02672fc99ff516fdf51df1af5
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives v2 Plan Review & Optimization"
tags: [implementation, strategy, agent-swarm, linear-cli, openclaw, skills]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives v2 — Plan Review & Optimization

## Task(s)

1. **v1 Plan Creation** — COMPLETED. Original plan at `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md`.
2. **Staff Engineering Review of v1** — COMPLETED. Review at `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md`. Found 1 critical issue (Project Orchestrator underspecified), 6 concerns, 4 questions.
3. **v2 Plan Creation** — COMPLETED. Addressed all review issues. Plan at `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md`.
4. **v2 Plan Review & Optimization** — PLANNED. Next session should review v2 for optimization opportunities and design a tighter verification loop.

## Critical References

- **v2 Plan**: `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md` — the authoritative plan to review
- **v1 Review**: `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md` — all issues should be addressed in v2
- **Chestnut Flowchart**: `/Users/coreycole/Downloads/chestnut-agent-primitives-flowchart.html` — the original system design this implements

## Recent Changes

No code changes — this session was planning-only. Two new files created:
- `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md` — the v2 implementation plan

## Learnings

### Key Design Decisions Made (user-approved)

1. **Flat primitives**: 11 independent skills (`/swarm-research`, `/swarm-code-change`, etc.) instead of one `/swarm` router. Each SKILL.md is self-contained (~100-150 lines). No primitive loads another. Eliminates context window pressure.
2. **Agent-only plan review**: No `AskUserQuestion` in autonomous sessions. Three human gates only: classification, project kickoff, PR merge.
3. **Dry-run from Phase 1**: `--dry-run` convention; primitives print `[DRY-RUN]` prefix per action without executing. linear-cli has native `--dry-run` support.
4. **Resume primitive**: Reads Linear comment history (structured prefixes like `RESEARCH:`, `PLAN:`, `IMPL:`, `VERIFY:`, `PR:`) to reconstruct state.
5. **Lead FDE spawns via harness API**: `POST /api/lead-fde/spawn` creates tmux sessions `cm-swarm-{ticketID}`. Max 4 concurrent. Health check endpoint.
6. **Discord-only**: Slack acknowledged as future extension.
7. **Full scope**: All 5 phases including Lead FDE OpenClaw agent.

### Technical Discoveries

- **linear-cli label create is NOT idempotent** — errors with "A label with the name already exists" (exit code 1, HTTP 400 INPUT_ERROR). Setup primitive must use check-then-create pattern: `linear-cli l list --output json --compact` → filter → create only missing.
- **linear-cli relations syntax**: `linear-cli rel add FROM -r blocks TO`, `linear-cli rel parent CHILD PARENT`
- **linear-cli exit codes**: 0=Success, 1=General, 2=NotFound, 3=Auth, 4=RateLimited
- **President agent pattern**: Manager struct with `Provision()`, idempotent (checks SOUL.md), writes 6 workspace files, registers via `openclaw agents add`, binds to Discord channel. Auth middleware validates `X-President-Secret` from env var. All API actions are fire-and-forget tmux sessions returning 202.
- **Tmux session naming**: Existing pattern is `cm-{worldID}-{cpID}`. Swarm sessions use `cm-swarm-{ticketID}` to be distinct and identifiable by the reaper.
- **OpenClaw heartbeat**: Timer-based (`context/openclaw/src/infra/heartbeat-runner.ts`), reads HEARTBEAT.md, gives agent full turn with exec/read/write tools, prunes transcript on HEARTBEAT_OK. Active hours configurable per-agent.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-28_15-04-26_agent-swarm-primitives-v2.md` — v2 implementation plan (authoritative)
- `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md` — v1 plan (superseded)
- `thoughts/CoreyCole/reviews/2026-02-28_14-28-09_agent-swarm-primitives_review.md` — v1 review
- `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md` — linear-cli research
- `/Users/coreycole/.claude/plans/vast-wiggling-rocket.md` — plan mode copy (same as v2 plan)

## Action Items & Next Steps

1. **Review v2 plan for optimization** — Look for:
   - Redundant primitives that could be merged
   - Overly complex workflows that could be simplified
   - Missing edge cases or failure modes
   - Whether 11 separate skills is the right granularity vs fewer skills with subcommands

2. **Design tighter verification loop** — Current verification is manual. Explore:
   - Can we automate testing of the primitives themselves (not just `--dry-run`)?
   - Integration test script that creates a test Linear project, runs primitives, verifies outcomes, cleans up
   - How to verify the Lead FDE heartbeat cycle end-to-end
   - Whether `just check` is sufficient or if we need swarm-specific checks

3. **Review the HEARTBEAT.md specification** — This is the most critical part. Verify:
   - The curl commands will work with the auth pattern
   - Linear queries return enough data for stall detection
   - Session lifecycle (spawn → track → reap) handles all edge cases
   - What happens when the Lead FDE itself crashes mid-heartbeat

4. **Consider phased rollout** — Even though user approved all 5 phases, consider whether implementing Phases 1-3 first and validating with real usage before Phase 4 would be more practical.

5. **After review is complete** — Begin implementation starting with Phase 1.

## Other Notes

### Codebase Reference Points
- President agent (reference pattern): `harness/internal/president/president.go`, `president_api.go`, `skills.go`, `workspace.go`
- Mayor agent (comparison): `harness/internal/mayor/mayor.go`, `workspace.go`, `skills.go`
- Tmux session management: `harness/internal/tmux/session.go`
- Claude orchestrator: `harness/internal/claude/claude.go` (spawn, reap, build pipeline)
- Existing skill example: `.claude/skills/playwright-cli/SKILL.md` (YAML frontmatter, references/ subdirectory)
- Linear skills: `.agents/skills/linear-*/SKILL.md` (38 skills, each ~30-50 lines)
- Existing commands composed by swarm: in `context/datastarui/.claude/commands/` — `create_plan.md` (481 lines), `implement_plan.md` (74 lines), `validate_plan.md` (176 lines), `research_codebase.md` (189 lines)

### v1 Review Issues → v2 Resolution Map
| Review Issue | Status | How Addressed |
|---|---|---|
| Project Orchestrator underspecified (CRITICAL) | Resolved | Phase 4 fully specifies spawning via harness API, session naming, reporting via Linear comments, max 4 concurrent, health check |
| Context window pressure | Resolved | Flat primitives, ~100-150 lines each, no nesting |
| Workflow state persistence | Resolved | Structured comment prefixes + resume primitive |
| Slack vs Discord | Resolved | Discord-only, Slack acknowledged as future |
| Label idempotency | Resolved | Check-then-create pattern |
| PresidentManager gaps | Resolved | Health check endpoint, session tracking in memory, reaper |
| No dry-run | Resolved | Convention from Phase 1, all primitives support it |
