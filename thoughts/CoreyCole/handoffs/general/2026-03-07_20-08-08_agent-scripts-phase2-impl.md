---
date: 2026-03-07T20:08:08-08:00
researcher: CoreyCole
git_commit: 93e0184832cc4ac5611ea784ba57a7f42e5c57e3
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Scripts Phase 2: JS Execution Layer Implementation Complete"
tags: [implementation, agents, pi-mono, swarm, skills, search-context]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Scripts Phase 2 Implementation Complete

## Task(s)

**Phase 1 commit (completed)**: Committed all Go infrastructure (migration 006, SQLC queries, event constants, cleanup routines) as `93e0184` on `feat/agent-primitives`.

**Phase 2 implementation (completed)**: Created the entire JS agent execution layer — all files written, syntax-validated, lint-passing, module resolution verified. All code is **uncommitted** and ready for review.

**Next task (planned)**: Spawn sub-agents to review the implementation — verify all 7 skill files are accurate and complete, review agent scripts for correctness, and identify potential improvements before committing.

## Critical References

- **Authoritative v3 plan**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md`
- **Phase 2 implementation plan**: `thoughts/CoreyCole/plans/2026-03-08_03-42-05_agent-scripts-phase2.md`
- **Pi-mono reference code**: `context/pi-mono/` (v0.57.1)

## Recent changes

All changes are uncommitted on `feat/agent-primitives`:

### Scaffolding
- `harness/agents/package.json` — npm package with `@mariozechner/pi-ai`, `pi-agent-core`, `pi-coding-agent` deps
- `harness/agents/node_modules/` — installed (269 packages, gitignored)

### Shared Libraries (`harness/agents/lib/`)
- `protocol.js` — JSONL stdin/stdout protocol with readline, stdin close rejection
- `orchestrator-tools.js` — `createAskOrchestratorTool()` + `createSubmitArtifactTool(schema, validate)`
- `search-context.js` — keyword extraction, YAML frontmatter parsing, skill index caching, grep-based code search, result formatting
- `agent-factory.js` — `runAgent()` entry point: reads start message, creates pi-mono Agent with model/tools/prompt, streams tool events

### Agent Scripts (`harness/agents/`)
- `research-questions.js` — decomposes question into 2-5 sub-questions
- `research-agent.js` — investigates one sub-question with file tools
- `research-synthesizer.js` — combines findings into research doc (no file tools)
- `plan-orchestrator.js` — classifies which specialist domains need plans
- `specialist-planner.js` — detailed plan for one domain
- `plan-synthesizer.js` — merges specialist plans into unified plan (no file tools)

### Skill Files (`harness/agents/skills/`)
- `project-structure.md`, `database-conventions.md`, `api-conventions.md`, `ui-conventions.md`, `temporal-conventions.md`, `build-system.md`, `agent-hierarchy.md`

### Go Types
- `harness/internal/swarmorch/types.go` — JSONL protocol messages, all 6 input/output struct pairs, span helpers

## Learnings

### Linter requires camelCase JSON tags
- The `tagliatelle` linter enforces camelCase for JSON struct tags — e.g., `json:"taskID"` not `json:"task_id"`, `json:"toolCallID"` not `json:"toolCallId"`
- This affects both Go types AND JS agent scripts (since they read/write the same JSON)
- All JS `task.*` field references must use camelCase: `task.requestText`, `task.repoRoot`, `task.maxQuestions`, `task.outputPath`, `task.researchDocPath`, `task.researchDoc`, `task.researchDocSummary`, `task.plannerOutputs`
- Protocol `sendEvent` uses `toolCallID` (not `toolCallId`) in the JSON output

### gci formatter
- Go files need `gci` import formatting — run `golangci-lint fmt` to auto-fix

### Pi-mono package names on npm
- `@mariozechner/pi-ai`, `@mariozechner/pi-agent-core`, `@mariozechner/pi-coding-agent`
- `Agent` class from `pi-agent-core`, `getModel`/`Type` from `pi-ai`, `createReadOnlyTools` from `pi-coding-agent`

## Artifacts

- `harness/agents/package.json`
- `harness/agents/lib/protocol.js`
- `harness/agents/lib/orchestrator-tools.js`
- `harness/agents/lib/search-context.js`
- `harness/agents/lib/agent-factory.js`
- `harness/agents/research-questions.js`
- `harness/agents/research-agent.js`
- `harness/agents/research-synthesizer.js`
- `harness/agents/plan-orchestrator.js`
- `harness/agents/specialist-planner.js`
- `harness/agents/plan-synthesizer.js`
- `harness/agents/skills/project-structure.md`
- `harness/agents/skills/database-conventions.md`
- `harness/agents/skills/api-conventions.md`
- `harness/agents/skills/ui-conventions.md`
- `harness/agents/skills/temporal-conventions.md`
- `harness/agents/skills/build-system.md`
- `harness/agents/skills/agent-hierarchy.md`
- `harness/internal/swarmorch/types.go`

## Action Items & Next Steps

1. **Review skill files**: Spawn sub-agents to verify each skill file's content is accurate against the actual codebase (CLAUDE.md files, source code). Check for missing conventions, outdated info, or gaps that would make agents less effective.

2. **Review agent scripts**: Verify each agent script correctly implements the design from the v3 plan — system prompts, artifact schemas, validation logic, prompt construction.

3. **Review shared libraries**: Verify protocol handling, tool interfaces match pi-mono's `AgentTool` spec, search-context keyword matching quality.

4. **Identify improvements**: Are there missing skills? Should any agent's system prompt be more specific? Is the validation logic sufficient?

5. **Commit Phase 2**: After review, stage and commit all Phase 2 files.

6. **Verify with `just check`**: Run full project lint/build verification.

## Other Notes

- All 10 JS files pass `node --check` syntax validation
- Module resolution works: `cd harness/agents && node -e "import('./lib/agent-factory.js')"` succeeds
- Go types pass `go vet` and `golangci-lint` with 0 issues
- The `search_context` tool was kept (not simplified away) because the system is designed to scale to many more skills
- Skill files are < 3KB each with YAML frontmatter (`name`, `description`, `tags`) for search indexing
- `node_modules/` already covered by root `.gitignore` glob
- The v3 plan has extensive Go code examples for Phase 3 (Temporal integration) — not implemented yet
