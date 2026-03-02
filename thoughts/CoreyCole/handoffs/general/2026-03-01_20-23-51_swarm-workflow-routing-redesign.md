---
date: 2026-03-01T20:23:51-08:00
researcher: CoreyCole
git_commit: fbdff4dadf1adc733cfda06981d41fbfdff59a07
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Workflow Routing Redesign — Align with Chestnut Agent Primitives Vision"
tags: [swarm, architecture, routing, workflow-types, chestnut-primitives]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Workflow Routing Redesign

## Task(s)

### Completed
1. **Swarm first-run infrastructure fixes** — Fixed 6 critical bugs blocking swarm execution:
   - Hooks auth (`--settings` flag instead of `CLAUDE_CONFIG_DIR`)
   - CLI compat (positional prompt, `--input-file` removed in v2.1.63)
   - Localhost for hooks (Claude Code blocks private/link-local IPs)
   - Migrations 007-009 not registered in runner
   - Linear `GetTicket` using invalid `identifier` filter field (fixed to `number` + `team.key`)
   - Discord bot permissions for swarm alerts channel
2. **First successful E2E workflow** — Workflow `83f30594` for CRE-5 ran research → project_plan → project_review → gate (`awaiting_review`). All hooks fire correctly, phases advance, Discord alerts work, Linear comments posted.
3. **Committed all fixes** — 3 commits on `feature/agent-swarm`: `a0804f7`, `8f049c4`, `fbdff4d`.

### Planned — The Real Task for Next Session
4. **Redesign the swarm's workflow routing to match the Chestnut Agent Primitives vision.** The current system treats `project` as a single workflow type that does research → project_plan → project_review → project_verify. The vision document (`thoughts/swarm/chestnut-agent-primitives-flowchart.html`) describes a fundamentally different flow where the lead orchestrator classifies ideas into 3 distinct processes at the top of the funnel:
   - **Research** — standalone research that can spawn further research tickets
   - **Code Change** — always starts with research before planning
   - **Project** — decomposes into multiple research topics first, then creates a project plan from the aggregated research

   Key gaps between current implementation and the vision:
   - **Current**: A single ticket with `type: project` goes through a linear pipeline. Research is one phase, not a first-class workflow type that spawns children.
   - **Vision**: Projects should kick off *multiple* research tickets first. The project plan should be created from the *aggregated* research summaries. Research can recursively spawn more research.
   - **Current**: Code changes jump straight to `code_plan` after research. No human gate between research and planning.
   - **Vision**: Research is always a prerequisite for code changes. Research completion may reveal new questions that need their own tickets.
   - **Current**: The state machine (`internal/swarm/statemachine.go`) hardcodes phase sequences per workflow type.
   - **Vision**: The routing should be more dynamic — research outcomes determine what happens next (more research, code change, or project decomposition).

## Critical References

1. `thoughts/swarm/chestnut-agent-primitives-flowchart.html` — **THE** vision document. An interactive HTML flowchart showing the desired agent primitives architecture. Must be read and understood before any changes.
2. `harness/internal/swarm/statemachine.go` — Current state machine with `DetermineNextPhase()`. This is where workflow type → phase routing is defined.
3. `harness/CLAUDE.md` — Full swarm architecture documentation (Workflow Types section, State Machine section, Swarm Configuration section).

## Recent changes

- `harness/internal/db/db.go:110-112` — Added migrations 007-009 to runner
- `harness/internal/swarmorch/manager.go:319-332` — `--settings` flag instead of `CLAUDE_CONFIG_DIR`
- `harness/internal/swarmorch/manager.go:1109-1130` — Positional prompt instead of `--input-file`
- `harness/internal/swarmorch/manager.go:322` — `http://localhost:8080` for hooks
- `harness/internal/linear/client.go:402-416` — `parseIdentifier()` + `GetTicket` fix (number + team.key filter)
- `harness/internal/linear/client.go:135-147` — New filter types (`numberEqFilter`, `teamKeyFilter`)
- `harness/main.go:253-270` — `DISCORD_SWARM_CHANNEL_ID` env var support

## Learnings

- **The swarm's current 3 workflow types (research, code, project) are too rigid.** The Chestnut vision treats research as a first-class recursive process, not just a phase within another workflow. Research should be able to spawn child research tickets, and projects should aggregate multiple completed research outputs before planning.
- **The state machine is the bottleneck for routing changes.** `DetermineNextPhase()` in `statemachine.go` uses a static `phaseOrder` map per workflow type. Making routing dynamic (e.g., research outcomes determine next steps) requires rethinking this.
- **Linear integration works but sessions don't know the team key.** The harness-level Linear client uses `LINEAR_TEAM_KEY=CRE` correctly, but individual Claude Code sessions that try to post comments directly use `CM` as default. The `CM_SWARM_LINEAR_TEAM_KEY` env var should be passed to sessions.
- **The project workflow that just ran (83f30594) produced a solid plan but through the wrong process.** It did research + project_plan + project_review as 3 sequential phases in one workflow. The vision wants: multiple independent research workflows first → aggregate → then project plan. The plan itself (8 child tickets in 4 waves) is good content, but it was produced by a single research session, not by a proper multi-research decomposition.

## Artifacts

- `thoughts/swarm/chestnut-agent-primitives-flowchart.html` — Vision flowchart (must-read)
- `thoughts/swarm/research/2026-03-02_02-48-02_CRE-5_tech-debt-audit.md` — Research output from swarm's first successful run
- `thoughts/swarm/project-plans/2026-03-02_03-46-03_CRE-5_tech-debt-audit_v1.md` — Project plan output (8 tickets, 4 waves)
- `harness/internal/swarm/statemachine.go` — Current state machine (needs redesign)
- `harness/internal/swarm/env.go` — Session environment variables
- `harness/internal/swarmorch/manager.go` — Orchestrator (advanceWorkflow, spawnSession)
- `harness/CLAUDE.md` — Swarm docs (updated this session)
- `/home/deploy/.claude/projects/-home-deploy-creative-mode/memory/MEMORY.md` — Session memory with operational details

## Action Items & Next Steps

1. **Read and deeply understand `thoughts/swarm/chestnut-agent-primitives-flowchart.html`** — This is the target architecture. Open it or read the HTML to understand the 7 primitives, the 3 routing paths, and the orchestration heartbeat model.

2. **Audit the current routing system against the vision** — Map every current phase/transition in `statemachine.go` against the flowchart. Identify:
   - What's missing (recursive research, multi-research aggregation for projects, research-before-code-plan)
   - What's wrong (project is too linear, research doesn't spawn children)
   - What can be reused (the code change implement/verify/PR loop is close to correct)

3. **Design the new routing architecture** — Key design decisions:
   - Should "research" be a standalone workflow type that can spawn child research workflows?
   - How does a project workflow know when all its research children are done?
   - How does the orchestrator aggregate multiple research outputs into a project plan prompt?
   - Should the state machine become event-driven rather than phase-sequential?
   - How do the 7 primitives from the flowchart map to the current skill system?

4. **Consider the orchestration model** — The flowchart describes two orchestration layers: a "Lead FDE" heartbeat and per-project orchestrators. The current system has a single `Manager`. How should this evolve?

5. **The CRE-5 workflow is paused at `awaiting_review`** — It can be canceled (`POST /api/swarm/cancel` with `{"workflow_id":"83f30594"}`) since the routing redesign may change how projects work. Or it can be approved if you want to see the current project_verify phase work.

## Other Notes

- **Swarm operational details** are in `/home/deploy/.claude/projects/-home-deploy-creative-mode/memory/MEMORY.md` — hook secret, API endpoints, monitoring commands.
- **The site golangci-lint failure** in `just check` output is pre-existing and unrelated to swarm changes.
- **Current workflow 83f30594** sessions: research (108s), project_plan (246s), project_review (390s). All completed successfully. The system works mechanically — the issue is architectural (the routing doesn't match the vision).
- **The flowchart mentions "OpenClaw" as the human interface to the Lead FDE** — this maps to the existing president agent concept but with a more active role in task classification and routing.
- **DB location**: `data/creative-mode.db`. Service: `creative-mode` (systemd). Hot-reload via `air`.
