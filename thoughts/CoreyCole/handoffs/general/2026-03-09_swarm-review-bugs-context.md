# Handoff: Swarm System Review — Bugs, Quality Improvements, Context Injection

**Date**: 2026-03-09
**Branch**: `feat/agent-primitives`

## Changes Made

### Phase 1: Bug Fixes

1. **`workflows.go:627`** — Changed `research.Synthesize.Summary` → `research.Synthesize.Document` so specialist planners receive the full research synthesis (2000-6000 chars with file:line references) instead of a 2-3 sentence summary.

2. **`skills/temporal-conventions.md`** — Complete rewrite. Removed stale "Go SDK not integrated" claim and "Planned Phase 3" section. Documented actual state: workflows, activity struct, key patterns (deterministic workflows, disconnected context, fan-out).

### Phase 2: Deterministic Project Context Injection

3. **`types.go`** — Added `ProjectContext string` field to `StartMessage`.

4. **`context.go`** (new) — `loadProjectContext(repoRoot)` reads root `CLAUDE.md` and skill frontmatter manifest. ~3200 tokens total.

5. **`agent.go`** — Added `projectContext` field to `runAgentParams`, wired to `StartMessage.ProjectContext`.

6. **`activities.go`** — `SwarmActivities` caches `projectContext` on construction via `loadProjectContext()`. Passes to every `runAgentParams`.

7. **`agent-factory.js`** — Prepends `startMsg.projectContext` before the agent's system prompt with `---` separator.

### Phase 3: New Skill + Search Scope

8. **`skills/swarm-conventions.md`** (new) — Documents JSONL protocol, agent scripts, output format, Temporal integration, key source files.

9. **`agent.go:answerQuestion()`** — Added `templates/` directory to grep search scope. Added `.rs` to `--include` flags. Restructured loop to iterate over both `harness/` and `templates/` dirs.

10. **`agent.go:grepFiles()`** — Added `--include=*.rs` to grep command.

## Verification

- `just check` passes (Go lint, Rust clippy, ESLint, formatting)
- Service needs restart: `sudo systemctl restart creative-mode`

## Follow-up Items

- Run a research workflow and verify projectContext appears in agent system prompts
- Run a code_change_plan workflow and compare specialist planner output quality (should be richer with full research doc)
- Monitor token usage — projectContext adds ~3200 tokens per agent invocation
