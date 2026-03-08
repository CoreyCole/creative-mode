---
date: 2026-03-07T20:35:28-08:00
researcher: CoreyCole
git_commit: e0f70579366f9f97c765ed5b7842afb33c319161
branch: feat/agent-primitives
repository: creative-mode
topic: "Agent Scripts Phase 2: Review Complete, Ready for Phase 3"
tags: [implementation, agents, pi-mono, swarm, eslint, review]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Agent Scripts Phase 2 Review & Fixes Complete

## Task(s)

**Phase 2 review (completed)**: Spawned 4 sub-agents to review all Phase 2 artifacts in parallel — shared libraries, agent scripts, skill files, and Go types. Found and fixed a critical TypeBox/Go field name mismatch plus skill file accuracy issues.

**ESLint setup (completed)**: Added ESLint v9 to `harness/agents/`, integrated into `scripts/check.sh` and `scripts/fmt.sh`. Fixed golangci-lint v2 config issues in both `harness/.golangci.yml` and `site/.golangci.yml`.

**Next task (planned)**: Final review of Phase 2 implementation against the v3 plan before proceeding to Phase 3 (Temporal integration, Go `runAgent()`, workflow activities).

## Critical References

- **Authoritative v3 plan**: `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md`
- **Phase 2 implementation plan**: `thoughts/CoreyCole/plans/2026-03-08_03-42-05_agent-scripts-phase2.md`
- **Previous handoff**: `thoughts/CoreyCole/handoffs/general/2026-03-07_20-08-08_agent-scripts-phase2-impl.md`

## Recent changes

All changes are uncommitted on `feat/agent-primitives`:

### TypeBox field name fixes (snake_case → camelCase to match Go JSON tags)
- `harness/agents/research-questions.js:15` — `suggested_files` → `suggestedFiles`
- `harness/agents/research-agent.js:14,25,37` — `files_referenced` → `filesReferenced` (schema + validation + system prompt)
- `harness/agents/research-synthesizer.js:10,16` — `output_path` → `outputPath` (schema + validation)
- `harness/agents/specialist-planner.js:13-15,22-24` — `plan_section` → `planSection`, `files_affected` → `filesAffected`, `verification_checks` → `verificationChecks`
- `harness/agents/plan-synthesizer.js:10-11,17-18` — `phase_order` → `phaseOrder`, `output_path` → `outputPath`

### Skill file fixes
- `harness/agents/skills/api-conventions.md` — Rewrote: `hookSecretMiddleware()` takes no args (was wrong `hookSecretMiddleware(secret)`), inline middleware pattern (not groups), admin group off `authed` not `approved`, correct handler names
- `harness/agents/skills/temporal-conventions.md` — Rewrote: added status section clarifying Temporal Go SDK is not yet integrated, marked workflow rules as "Phase 3 planned"
- `harness/agents/skills/database-conventions.md:20` — Fixed migration filename `001_init.sql` → `001_initial.sql`

### ESLint setup
- `harness/agents/package.json` — Added `eslint`, `@eslint/js`, `globals` devDeps + lint scripts
- `harness/agents/eslint.config.js` — New: ESLint v9 flat config (recommended rules, node globals, ignore node_modules/skills)
- `harness/agents/lib/search-context.js:25,65,102` — Fixed 3 lint issues (useless escape, unused catch vars)

### golangci-lint v2 config fixes
- `harness/.golangci.yml` — Moved top-level `exclusions` under `linters:` (v2 format), removed invalid `print-linter-name`/`print-issued-lines`, added `node_modules` exclusion path
- `site/.golangci.yml` — Same v2 config fixes (exclusions nesting, output keys)
- `harness/internal/world/manager.go:707-708` — Removed blank line (whitespace lint)
- 13 stale `//nolint:gosec` directives auto-removed across `harness/` (builder, claude/memory, logging, server, world/manager, main)

### Integration
- `scripts/check.sh` — Added JS eslint step running in parallel with Go/Rust lints
- `scripts/fmt.sh` — Added `eslint --fix` step running in parallel with other formatters

## Learnings

### golangci-lint v2 config migration
- **`exclusions` must be under `linters:`, not top-level** — v2 changed the schema. `golangci-lint config verify` catches this but `golangci-lint run` silently ignores invalid top-level blocks
- **`print-linter-name` and `print-issued-lines` removed** from `output` in v2 — use `golangci-lint config verify` to check
- **Moving exclusions properly activates presets** like `common-false-positives`, which can make existing `//nolint` directives stale (triggering `nolintlint`). Fix with `golangci-lint run --fix ./...`
- **`exclusions.paths` uses regex matching** against reported file paths — `node_modules` matches as a substring

### TypeBox schema field names = LLM artifact field names
- The field names in `Type.Object({...})` are the exact JSON keys the LLM will produce when calling `submit_artifact`. These must match the Go struct JSON tags exactly, or `encoding/json` will silently drop them during deserialization.
- The `tagliatelle` linter enforces `goCamel` for JSON tags (`harness/.golangci.yml:252`), so all artifact field names must be camelCase.

### ESLint v9 flat config
- `no-unused-vars` needs `caughtErrorsIgnorePattern: '^_'` in addition to `argsIgnorePattern: '^_'` to allow `catch (_e)` pattern
- `node_modules` is auto-ignored by eslint but `skills/` (markdown files) needs explicit ignore

## Artifacts

- `harness/agents/eslint.config.js` (new)
- `harness/agents/package.json` (updated — devDeps + scripts)
- `harness/agents/lib/search-context.js` (lint fixes)
- `harness/agents/research-questions.js` (camelCase fix)
- `harness/agents/research-agent.js` (camelCase fix)
- `harness/agents/research-synthesizer.js` (camelCase fix)
- `harness/agents/specialist-planner.js` (camelCase fix)
- `harness/agents/plan-synthesizer.js` (camelCase fix)
- `harness/agents/skills/api-conventions.md` (rewritten)
- `harness/agents/skills/temporal-conventions.md` (rewritten)
- `harness/agents/skills/database-conventions.md` (filename fix)
- `harness/.golangci.yml` (v2 config fix + node_modules exclusion)
- `site/.golangci.yml` (v2 config fix)
- `scripts/check.sh` (eslint integration)
- `scripts/fmt.sh` (eslint --fix integration)
- `harness/internal/world/manager.go` (whitespace fix)
- Multiple files: 13 stale `//nolint:gosec` removals

## Action Items & Next Steps

1. **Final review of Phase 2 against v3 plan**: Before proceeding to Phase 3, do a comprehensive review ensuring all Phase 2 deliverables match the v3 plan at `thoughts/coreycole/plans/2026-03-08_agent-primitives-v3-final.md`. Specifically verify:
   - All 6 agent scripts match the design (system prompts, artifact schemas, validation)
   - All 4 shared libraries match the design (protocol, tools, search-context, factory)
   - Go types in `harness/internal/swarmorch/types.go` are complete
   - The 7 skill files are accurate (3 were fixed, 4 have minor gaps noted below)

2. **Address remaining skill file gaps** (minor, noted by review agents):
   - `project-structure.md` — missing `thoughts/`, `context/` dirs, missing 4+ packages (tmux, president, swarmorch, logging)
   - `ui-conventions.md` — missing `datastar.GetSSE()`/`PostSSE()` templ helpers
   - `build-system.md` — missing `just generate`, `just build`, `air` hot-reload
   - `agent-hierarchy.md` — missing president workspace details, production-disabled note

3. **Commit Phase 2**: After final review, stage and commit all Phase 2 files (agent scripts, libraries, skills, Go types, eslint setup, golangci config fixes)

4. **Proceed to Phase 3**: Temporal integration — Go `runAgent()` function, workflow activities, worker setup. The v3 plan has extensive Go code examples for this phase.

## Other Notes

- `just check` now passes fully clean (all Go, Rust, and JS linting)
- The sub-agent review identified no blocking issues in shared libraries or agent scripts beyond the TypeBox naming (now fixed)
- The `node_modules/flatted` npm package ships a `.go` file which was being picked up by golangci-lint — this was a pre-existing issue masked by the invalid v2 config, now properly excluded
- Phase 1 (DB schema, SQLC, events) was committed as `93e0184` — Phase 2 files are all uncommitted
