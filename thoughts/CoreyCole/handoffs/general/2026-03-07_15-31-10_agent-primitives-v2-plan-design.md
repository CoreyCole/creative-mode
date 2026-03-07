---
date: 2026-03-07T15:31:10-08:00
researcher: CoreyCole
git_commit: cae7638b69b6411aee5e8d4f2de5f28ce864d189
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives v2 — Research & Code Plan Design"
tags: [implementation, strategy, swarm, temporal, pi-mono, agent-primitives]
status: in_progress
last_updated: 2026-03-07
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives v2 — Research & Code Plan System Design

## Task(s)

**Designing an agent swarm system on top of Temporal, Linear, and SQLite** — focusing on Primitive 1 (Research) and Primitive 2 (Code Change Plan). Status: **plan written, continuing to refine**.

- **Plan document created** (`thoughts/coreycole/plans/2026-03-07_23-08-53_agent-primitives-v2-research-and-code-plan.md`) — this is the primary artifact. It covers 5 implementation phases: DB schema, pi-mono agent scripts, Temporal workflows, HTTP API, and Datastar dashboard.
- **No code written yet** — this session was entirely design/planning.
- The user wants to continue refining the plan before implementation begins.

## Critical References

1. **The plan itself**: `thoughts/coreycole/plans/2026-03-07_23-08-53_agent-primitives-v2-research-and-code-plan.md` — READ THIS FIRST, it's the complete design document.
2. **Previous failed attempt**: `git show origin/feature/agent-swarm` — has working dashboard templ, state machine, prompt templates, and agent-primitives HTML flowchart that we reference but simplify.
3. **Existing skills that define prompt contracts**: `.claude/skills/research_codebase.md` and `.claude/skills/create_plan.md`

## Recent changes

- `thoughts/coreycole/plans/2026-03-07_23-08-53_agent-primitives-v2-research-and-code-plan.md` — new plan document (entire file)

## Learnings

### The 7 Primitives
1. Research, 2. Code Change, 3. Project, 4. Parent Ticket, 5. Project Plan, 6. Project Orchestrator, 7. Lead Orchestrator. We only build 1 and 2 now.

### Pi-Mono Architecture
- Pi-mono (`github.com/badlogic/pi-mono`) is the TypeScript agent framework powering OpenClaw. Already installed at `/opt/openclaw/` (v0.54.0).
- Key packages: `@mariozechner/pi-ai` (unified multi-provider LLM API), `@mariozechner/pi-agent-core` (agent loop with tools), `@mariozechner/pi-coding-agent` (production CLI).
- Supports OpenAI Codex via `openai-codex-responses` provider type. The user wants **Codex 5.3** as the model.
- Agent execution model: **Node.js subprocesses** spawned by Go activities. Scripts at `harness/agents/`, receive JSON on stdin, return JSON on stdout.

### Specialized Planner Agents (Key Design Decision)
The plan synthesizer uses **fan-out/fan-in** like research: a plan orchestrator classifies the change, fans out to specialist planners (`planner-database.js`, `planner-api.js`, `planner-temporal.js`, `planner-ui.js`, `planner-general.js`), then a plan synthesizer merges outputs. Each specialist has baked-in domain context (conventions, patterns, file locations). New specialists are added by dropping a file + updating orchestrator classification.

### Previous Branch Lessons
The `feature/agent-swarm` branch (350+ files, all 7 primitives) collapsed under scope. Key reusable pieces:
- `harness/internal/swarm/statemachine.go` — solid phase transition logic
- `harness/internal/swarm/enums.go` — typed enums for phases, statuses, results
- `harness/views/swarm/dashboard.templ` — working dashboard with workflow list, detail page, SSE, gate reviews
- `harness/internal/swarm/prompt/templates/*.md.tmpl` — research and code_plan prompt contracts
- `harness/internal/swarm/agent-primatives.html` — visual flowchart of all 7 primitives

### Infrastructure Already Running
- Temporal dev server: `systemctl status temporal-dev` (ports 7233/8233, namespace `swarm`, SQLite-backed)
- `.env` has `CM_SWARM_TEMPORAL=true`, `LINEAR_API_KEY`, `LINEAR_TEAM_KEY=CRE`, `DISCORD_SWARM_CHANNEL_ID`
- But: `go.temporal.io` is NOT in `go.mod` yet, no swarm Go code exists on this branch

## Artifacts

- `thoughts/coreycole/plans/2026-03-07_23-08-53_agent-primitives-v2-research-and-code-plan.md` — **THE PLAN** (read this)
- `thoughts/coreycole/plans/2026-03-07_agent-swarm-primitives-phase1-research-and-code-plan.md` — earlier draft (same session day, less detailed)
- `thoughts/coreycole/plans/2026-03-07_22-54-03_swarm-create-plan-skill.md` — create_plan skill contract for swarm

## Action Items & Next Steps

1. **Continue refining the plan** — the user explicitly said they want to keep iterating before implementation. Areas to consider:
   - Are there additional specialist planners needed? (e.g., `planner-discord.js`, `planner-bevy.js`)
   - Should the research question generator also classify question domains for specialized research agents?
   - Error handling and retry semantics for agent subprocess failures
   - Exact system prompts for each agent script (currently described conceptually, not written)
   - Linear integration depth — just optional linkage, or post status comments?

2. **When ready to implement**, follow the 5 phases in order:
   - Phase 1: DB schema + Temporal SDK
   - Phase 2: Pi-mono agent scripts
   - Phase 3: Temporal workflows + activities
   - Phase 4: HTTP API + EventBus integration
   - Phase 5: Dashboard (Datastar + templ)

3. **Verify pi-mono/Codex compatibility** — check if `codex-5.3` is a valid model string in the installed pi-mono version (v0.54.0 at `/opt/openclaw/`). May need to update to v0.57.0+.

## Other Notes

### Key Files in Existing Codebase (for context when implementing)
- `harness/internal/events/bus.go` — EventBus pattern to extend for swarm events
- `harness/internal/events/types.go` — event type constants to add swarm types to
- `harness/internal/server/server.go:107-230` — `RegisterRoutes()` where swarm routes will be added
- `harness/internal/server/mayor_dashboard.go` — SSE dashboard pattern to follow
- `harness/internal/db/db.go` — migration registration (`migrationFiles` slice)
- `harness/internal/claude/claude.go` — existing orchestrator pattern (we're NOT using this for swarm, but it shows the tmux pattern we're replacing with pi-mono)
- `harness/views/mayor/dashboard.templ` — templ dashboard pattern to follow

### Branch Context
- Working on `feat/agent-primitives` branch
- The old implementation lives on `origin/feature/agent-swarm` (remote only, not checked out locally)
- Use `git show origin/feature/agent-swarm:<path>` to pull reference code from the old branch
