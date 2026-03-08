---
date: 2026-03-07T19:52:12-08:00
researcher: CoreyCole
git_commit: f6324ee2dadd5b7018525bba73f75468dce08f12
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Scripts Phase 2: JS Execution Layer with Self-Discovering Context"
tags: [implementation, agents, pi-mono, swarm, skills, search-context]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Scripts Phase 2 — JS Execution Layer

## Task(s)

**Planning (completed)**: Created implementation plan for Phase 2 of the agent primitives system. Phase 1 (Go infrastructure — migration 006, SQLC queries, event constants, cleanup routines) is already complete on `feat/agent-primitives` but NOT yet committed. Phase 2 creates the JS agent execution layer.

**Implementation (planned, not started)**: The plan covers 9 phases — scaffolding, shared libraries, skill files, agent scripts, and Go types. No code has been written for Phase 2 yet.

## Critical References

- **Authoritative v3 plan**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md` — contains exact schemas, protocol spec, workflow structure, Go code patterns
- **Phase 2 implementation plan**: `thoughts/CoreyCole/plans/2026-03-08_03-42-05_agent-scripts-phase2.md` — the plan created in this session
- **Pi-mono reference code**: `context/pi-mono/` (v0.57.1) — Agent class, tools, skill discovery patterns

## Recent changes

No code changes were made in this session. Only plan documents were created.

## Learnings

### Pi-mono API (verified against source)
- `Agent` class at `context/pi-mono/packages/agent/src/agent.ts:102` — `setModel()`, `setSystemPrompt()`, `setTools()`, `subscribe()`, `prompt(string)`
- `getModel('openai', 'gpt-5.3-codex')` from `@mariozechner/pi-ai` — NOT `getModel('openai-codex', ...)` (that routes through ChatGPT OAuth)
- `createReadOnlyTools(cwd)` from `@mariozechner/pi-coding-agent` returns `[read, grep, find, ls]` — `context/pi-mono/packages/coding-agent/src/core/tools/index.ts:122`
- `Type` re-exported from `@mariozechner/pi-ai` (wraps `@sinclair/typebox`)
- `AgentTool` interface: `name`, `label`, `description`, `parameters` (TypeBox schema), `execute(toolCallId, params, signal?, onUpdate?)` → `Promise<{content: [{type, text}], details}>`
- Events: `tool_execution_start { toolCallId, toolName, args }`, `tool_execution_end { toolCallId, toolName, result, isError }`

### Package availability
- `@mariozechner/pi-*` packages are on npm (from `github.com/badlogic/pi-mono`). Use standard `npm install` — do NOT symlink to `/opt/openclaw/node_modules`. This was a design decision made during the session.

### Phase 1 state (uncommitted)
- Migration 006: `harness/internal/db/migrations/006_swarm_tables.sql`
- SQLC queries: `harness/internal/db/queries/swarm.sql`, generated `harness/internal/db/sqlc/swarm.sql.go`
- Modified: `harness/internal/db/db.go`, `harness/internal/db/sqlc/models.go`, `harness/internal/db/sqlc/querier.go`, `harness/internal/events/types.go`, `harness/main.go`, `harness/sqlc.yaml`, `flake.nix`
- All Phase 1 changes should be committed BEFORE starting Phase 2 work

## Artifacts

- `thoughts/CoreyCole/plans/2026-03-08_03-42-05_agent-scripts-phase2.md` — the Phase 2 implementation plan (9 phases)
- `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md` — authoritative v3 plan (pre-existing, not created in this session)

## Action Items & Next Steps

1. **Commit Phase 1**: Stage and commit all uncommitted Phase 1 files on `feat/agent-primitives` before starting Phase 2
2. **Execute Phase 2 plan** (in order):
   - Phase 1: Create `harness/agents/package.json` + `npm install`
   - Phase 2: `harness/agents/lib/protocol.js` — JSONL stdin/stdout protocol
   - Phase 3: `harness/agents/lib/orchestrator-tools.js` — `ask_orchestrator` + `submit_artifact`
   - Phase 4: `harness/agents/lib/search-context.js` — deterministic keyword grep (the self-discovery tool)
   - Phase 5: `harness/agents/lib/agent-factory.js` — `runAgent()` entry point
   - Phase 6: 7 skill files in `harness/agents/skills/` with YAML frontmatter
   - Phase 7: 6 agent scripts (research-questions, research-agent, research-synthesizer, plan-orchestrator, specialist-planner, plan-synthesizer)
   - Phase 8: `harness/internal/swarmorch/types.go` — Go struct definitions
3. **Verify**: `node --check` all JS, module resolution, `go build ./internal/swarmorch/`, `just check`

## Other Notes

- The `search_context` tool is the key design innovation — agents call it with natural language queries, it does deterministic keyword matching against skill file frontmatter (YAML `description` + `tags`), returns file paths + descriptions. Agents then use `read` to load full content. No extra LLM call.
- Skill index is cached per process (agent scripts are short-lived, minutes each)
- The `.gitignore` already has a `node_modules/` glob, so `harness/agents/node_modules` is already excluded
- `readLine()` in protocol.js must reject pending promises on stdin `close` event — critical for handling Go orchestrator crashes
- The v3 plan has extensive code examples for the Go side (`runAgent()`, span helpers, workflow structure) — those are Phase 3 (Temporal integration), not Phase 2
