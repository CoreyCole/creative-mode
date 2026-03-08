---
date: 2026-03-08T01:53:55-08:00
researcher: CoreyCole
git_commit: 773d305ee138bceac6e3b412f627dbf37bc5374f
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Primitives — Final System Prompts & Skill Files Plan"
tags: [implementation, strategy, swarm, agent-primitives, pi-mono, system-prompts, skills, plan-review]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Primitives — Final Plan Written + Review Complete

## Task(s)

1. **Review the system prompts & skill files plan** — Status: **complete**. Conducted staff-level review with independent codebase verification (12/13 file:line references confirmed accurate). Identified 3 critical issues, 5 concerns, 4 questions.

2. **Write final version of plan incorporating review feedback** — Status: **complete**. All 3 critical issues resolved, all 5 concerns addressed, new Go struct definitions added for artifact schema alignment.

3. **Langfuse observability research** — Status: **research document written by parallel session**. Document exists at `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md`. Not yet incorporated into the agent primitives plan — may inform SSE dashboard observability design.

## Critical References

1. **Final plan (authoritative, ready for implementation)**: `thoughts/CoreyCole/plans/2026-03-07_17-36-13_agent-primitives-system-prompts-and-skills-final.md`
2. **v3 architecture plan**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-conversational-agents.md`
3. **Review document**: `thoughts/CoreyCole/reviews/2026-03-07_17-29-01_agent-primitives-system-prompts-and-skills_review.md`

## Recent changes

No code changes this session. Three new thought documents created:
- `thoughts/CoreyCole/reviews/2026-03-07_17-29-01_agent-primitives-system-prompts-and-skills_review.md` — staff review
- `thoughts/CoreyCole/plans/2026-03-07_17-36-13_agent-primitives-system-prompts-and-skills-final.md` — final plan
- `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md` — Langfuse research (written by parallel session)

## Learnings

### Review Findings (resolved in final plan)
- **`temporal-conventions.md` describes code that doesn't exist**: Temporal infrastructure (dev server, namespace) exists but zero Go workflow/activity code exists in the harness. Skill file now has `STATUS: TARGET PATTERNS` header so agents treat it as specification, not discoverable code.
- **Array index tool selection is fragile**: `createReadOnlyTools(cwd)[0]` depends on undocumented return order from pi-mono. Final plan uses `tools.find(t => t.name === 'read')` instead.
- **Artifact schemas must match Go structs**: The JSONL protocol sends `{ type: 'result', data: <artifact> }` — Go unmarshals `msg.Data` into typed structs. Final plan adds explicit Go struct definitions in `harness/internal/swarmorch/types.go` for all 6 agent artifacts.
- **Line numbers in skill files are unstable**: Adding swarm routes, migrations, etc. shifts line numbers. Final plan uses function/method names (`RegisterRoutes()`, `ProvisionFromWebhook()`) instead.

### Codebase Verification Results
- `harness/internal/server/server.go` routes at `RegisterRoutes()` — confirmed lines 107-229
- `harness/internal/db/db.go` migration slice at `runMigrations()` — confirmed lines 93-99
- `harness/internal/events/bus.go` Subscribe/Publish with 100-event buffer — confirmed
- `createReadOnlyTools(cwd)` returns `[readTool, grepTool, findTool, lsTool]` — verified at `/opt/openclaw/node_modules/@mariozechner/pi-coding-agent/dist/core/tools/index.js:44-46`
- `@sinclair/typebox` v0.34.48 available at `/opt/openclaw/node_modules/`
- Pi-mono `AgentEvent` types include `tool_execution_start` with fields `toolCallId`, `toolName`, `args` — verified at `/opt/openclaw/node_modules/@mariozechner/pi-agent-core/dist/types.d.ts:161-177`

### Langfuse Research
- Research document written to `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md`
- May inform how we structure SSE dashboard observability (trace/span model vs. flat event stream)
- Not yet integrated into the agent primitives plan

## Artifacts

- `thoughts/CoreyCole/plans/2026-03-07_17-36-13_agent-primitives-system-prompts-and-skills-final.md` — **the final plan to implement**
- `thoughts/CoreyCole/reviews/2026-03-07_17-29-01_agent-primitives-system-prompts-and-skills_review.md` — staff review
- `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md` — Langfuse observability research

## Action Items & Next Steps

1. **Review the Langfuse research** — read `thoughts/CoreyCole/research/2026-03-07_17-40-48_langfuse-agent-observability.md` and decide if trace/span patterns should influence SSE event design or dashboard observability.

2. **Implement Phase 1 (skill files)** — create `harness/agents/skills/` directory and write 7 `.md` files from the final plan. Content is near-verbatim in the plan.

3. **Implement Phase 2 (Go constants)** — create `harness/internal/swarmorch/` directory and write:
   - `types.go` — Go struct definitions for all 6 artifact types
   - `prompts.go` — 6 Go string constants with system prompts
   - Verify with `go build ./internal/swarmorch/`

4. **Continue to Phase 1 of v3 plan** — foundation work:
   - Migration 006 + register in `db.go` `migrationFiles` slice
   - SQLC queries for swarm tables
   - `go get go.temporal.io/sdk` + go mod tidy
   - `harness/agents/package.json` + `npm install`
   - Shared agent libraries (`lib/protocol.js`, `lib/orchestrator-tools.js`, `lib/agent-factory.js`)

## Other Notes

### Document Evolution Chain
v1 → v1 review → v2 → v2 handoff → v3 → v3 handoff → v3 refinement → v3 refinement handoff → draft prompts plan → **staff review → final plan (this session)**

### VPS State
- Temporal dev server running (7233/8233, namespace `swarm`)
- bubblewrap + temporal-cli in flake.nix (verified installed)
- No `OPENAI_API_KEY` in `.env` yet
- No swarm Go code exists on this branch
- No `harness/agents/` directory exists yet

### Key Design Decisions in Final Plan
- **Prompts as Go constants** in `prompts.go`, not `.md` files — short, tightly coupled to artifact schemas
- **Skill files use function/method names** not line numbers — stable across edits
- **`last_verified` frontmatter** on all skill files for staleness tracking
- **Go struct definitions** in `types.go` ensure artifact schemas match at compile time
- **Plan orchestrator uses named tool lookup** (`find(t => t.name === 'read')`) not array index
- **`temporal-conventions.md`** marked as target patterns, not existing code
- **Synthesizer prompts** explicitly flag contradictions instead of silently resolving
