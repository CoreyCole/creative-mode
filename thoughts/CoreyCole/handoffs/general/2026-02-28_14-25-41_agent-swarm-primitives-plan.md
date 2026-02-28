---
date: 2026-02-28T14:25:41-08:00
researcher: CoreyCole
git_commit: 055cdfdc9a74da5dfdcc3be8ffe65d38343e5a33
branch: main
repository: creative-mode
topic: "Agent Swarm Primitives — Plan Design & Review"
tags: [implementation, strategy, agent-swarm, linear-cli, orchestration, openclaw]
status: complete
last_updated: 2026-02-28
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Swarm Primitives Plan Design

## Task(s)

**Plan creation — COMPLETE**: Designed a comprehensive implementation plan for the "Chestnut Agent Primitives" system — a general-purpose agent swarm that turns high-level ideas into Linear-tracked, implemented software. The plan went through two iterations:

1. **v1**: Used `.claude/commands/swarm-*.md` (10 separate command files). Written in full detail.
2. **v2 (current)**: Consolidated into `.claude/skills/swarm/` with a single `SKILL.md` entry point and `primitives/*.md` sub-files loaded on demand. This matches the proven `playwright-cli` skill pattern already in the codebase.

**Plan review — NOT STARTED**: The user wants to continue reviewing the plan before implementation begins. No code has been written yet.

## Critical References

- **The plan**: `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md` — the full implementation plan (v2, consolidated skill structure)
- **Chestnut flowchart**: `/Users/coreycole/Downloads/chestnut-agent-primitives-flowchart.html` — the visual reference showing the 7 primitives, lifecycle loops, and two-level orchestration model
- **Linear CLI research**: `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md` — architecture of linear-cli (38 skills, agent-friendly flags, command patterns)

## Recent changes

No code changes were made. Only the plan document was created:
- `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md` — new file (full plan)

## Learnings

1. **Skills vs Commands**: `.claude/skills/<name>/SKILL.md` is the modern pattern. Supports subdirectories with markdown-linked sub-files loaded on demand. Commands (`.claude/commands/*.md`) still work but are legacy. The `playwright-cli` skill at `.claude/skills/playwright-cli/SKILL.md` with `references/*.md` is the proven pattern to follow.

2. **Single skill, argument routing**: One `/swarm` command routes via first argument (`/swarm research`, `/swarm code-change`, `/swarm project`, etc.) rather than separate `/swarm-research`, `/swarm-code-change` commands. Claude parses the first argument and loads the appropriate primitive sub-file.

3. **linear-cli capabilities**: The CLI supports `--id-only` for chaining commands, `--output json --compact --fields a,b,c` for machine parsing, `--quiet` for agent use, and stdin piping (`cat desc.md | linear-cli i create "Title" -t CM -d -`). Exit codes: 0=Success, 2=NotFound, 3=Auth, 4=RateLimited.

4. **Existing commands compose**: The swarm primitives should COMPOSE existing commands (`/create_plan`, `/implement_plan`, `/validate_plan`, `/research_codebase`, `/describe_pr`) rather than reimplementing them. Each primitive adds Linear tracking around the existing command.

5. **OpenClaw is per-turn**: Agents are NOT persistent processes — they're invoked per-message. Heartbeat is triggered by HEARTBEAT.md configuration, not a cron job. The president agent pattern (`harness/internal/president/`) is the reference implementation for the Lead FDE orchestration agent.

6. **User decisions made**:
   - Runtime for orchestration: **OpenClaw agents** (extends president pattern)
   - Primitives implementation: **Claude Code skills** (`.claude/skills/swarm/`)
   - Scope: **General-purpose** (portable to any codebase with CLAUDE.md)
   - Linear team key: **CM**

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md` — the full implementation plan (5 phases, 15 new files, 2 modified files)
- `thoughts/CoreyCole/research/2026-02-28_13-42-58_linear-cli-architecture.md` — linear-cli architecture research (pre-existing, read during planning)
- `/Users/coreycole/Downloads/chestnut-agent-primitives-flowchart.html` — Chestnut Agent Primitives visual flowchart (user-provided, read during planning)

## Action Items & Next Steps

1. **Continue plan review** — the user wants to review the plan further before implementation. Read the plan at `thoughts/CoreyCole/plans/2026-02-28_14-06-46_agent-swarm-primitives.md` and be prepared for feedback/revisions.

2. **When approved, implement Phase 1 first**:
   - Create `.claude/skills/swarm/SKILL.md` (entry point with routing)
   - Create `primitives/conventions.md` (Linear label taxonomy, ticket format, comment conventions)
   - Create `primitives/setup.md` (one-time label creation)
   - Create `templates/ticket-description.md` and `templates/research-doc.md`

3. **Then Phase 2** (core primitives): `research.md`, `code-change.md`, `project.md`

4. **Then Phase 3** (review/orchestration): `verify.md`, `plan-review.md`, `project-verify.md`, `heartbeat.md`, `status.md`

5. **Phase 4** (Lead FDE OpenClaw agent): `harness/internal/leadfde/leadfde.go`, `harness/internal/server/leadfde_api.go`

6. **Phase 5** (integration testing + CLAUDE.md docs)

## Other Notes

- The **7 composable primitives** map to numbered references from the Chestnut flowchart: 1️⃣ Research, 2️⃣ Code Change, 2️⃣.1️⃣ Verification, 3️⃣ Project, 3️⃣.1️⃣ Project Verification, 5️⃣.1️⃣ Plan Review, 6️⃣/7️⃣ Orchestration

- The **code change lifecycle** has three nested loops visible in the flowchart: plan revision loop (v1->v2->v3 until approved), implement/verify loop (Max's Bolt), and full restart capability (human rejects -> new ticket referencing old)

- The **two-level orchestration model**: Lead FDE (7️⃣) checks all projects cross-cutting; Project Orchestrators (6️⃣) are ephemeral Claude Code sessions spawned per-project per heartbeat tick

- **Design principles** from the flowchart: don't wait for human input, context changes put tickets back in progress with new research, agents answer own questions first, defer to humans for company context & trade-offs, Linear is source of truth, Slack/Discord only for critical human input needs

- The existing agent system (mayors + president) at `harness/internal/mayor/` and `harness/internal/president/` is separate from the swarm system. The Lead FDE would coexist alongside the president agent.

- The orchestration research at `thoughts/CoreyCole/research/2026-02-27_18-52-19_master-plan-orchestration-openclaw-agents.md` has detailed findings on OpenClaw streaming, Discord binding, and multi-agent isolation that are relevant for Phase 4.

- The master plan for the existing agent hierarchy is at `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md` — this is the reference for how the president was implemented (7 phases, all complete in code).
