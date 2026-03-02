---
date: 2026-03-01T20:46:48-08:00
researcher: CoreyCole
git_commit: 7a96614b77723db351ff78a37cfe95f4a70a1600
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Prompt Composition, Transcript Parsing, and Prompt Versioning — Lint Fixes"
tags: [implementation, swarm, prompt-composition, transcript-parsing, lint]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Prompt Composition Lint Fixes

## Task(s)

- **Implement the "Swarm Prompt Composition, Transcript Parsing, and Prompt Versioning" plan** — **completed** (all 6 phases were already implemented before this session began).
- **Fix golangci-lint failures in `just check`** — **superseded**. Four lint issues were found and fixed in the older code, but the remote branch diverged significantly (human gates, Linear integration, Graphite stacking, hooks, JSONL logging, metrics, etc.). The fixes were stashed, but could not be cleanly applied after pulling. The newer remote code already handles some of these differently (e.g., uses `swarm.EnvKey()` and `//nolint:goconst` inline comments).

The plan document was provided inline in the user prompt. It described 6 phases: DB migration, transcript parser, prompt templates, orchestrator wiring, dashboard updates, and SKILL.md cleanup. All phases were already committed on the `feature/agent-swarm` branch prior to this session. The lint fixes were applied but could not be merged cleanly after the remote branch diverged.

## Critical References

- `harness/internal/swarmorch/manager.go` — the orchestrator that wires together prompt rendering, session spawning, transcript parsing, and workflow advancement
- `harness/internal/swarm/prompt/` — Go `text/template` prompt composition system (context.go, render.go, templates/*.md.tmpl)
- `harness/internal/swarm/transcript/` — JSONL transcript parser for token/cost tracking (parse.go, pricing.go, discover.go)

## Recent changes

- `harness/internal/swarmorch/manager.go:33` — Added `envTrue = "true"` constant to fix goconst lint (string `"true"` had 3 occurrences)
- `harness/internal/swarmorch/manager.go:748-750` — Changed `"true"` literals to `envTrue` constant
- `harness/internal/swarmorch/manager.go:850` — Changed `"true"` literal to `envTrue` constant
- `harness/internal/swarmorch/manager.go:837` — Removed unused `ctx context.Context` parameter from `buildPromptContext` (unparam lint)
- `harness/internal/swarmorch/manager.go:225` — Updated call site to remove `ctx` argument
- `harness/internal/swarmorch/manager.go:862` — Wrapped `os.ReadFile(handoffPath)` in `filepath.Clean()` to satisfy gosec G304
- `harness/internal/swarmorch/manager.go:875` — Wrapped `os.ReadFile(learningPath)` in `filepath.Clean()` to satisfy gosec G304

A linter hook also auto-added `//nolint:tagliatelle` comments to `harness/internal/swarm/transcript/parse.go:40-43` for the Claude API JSON field names and `//nolint:gosec` on `parse.go:48`.

## Learnings

- **gosec G304 + gofmt interaction**: `//nolint:gosec` directives must be on the same line as the `os.ReadFile` call. If `gofmt` reformats a long line across multiple lines, the directive ends up on a different line and becomes "unused" (triggering nolintlint). The clean fix is to use `filepath.Clean(path)` instead, which satisfies gosec without needing nolint.
- **All 6 plan phases were already committed**: The prompt templates (`harness/internal/swarm/prompt/templates/`), transcript parser (`harness/internal/swarm/transcript/`), migration 007, sqlc queries, dashboard token/cost display, and SKILL.md cleanup were all done in prior commits (`b2f7d3c` and `68aa6ba`).
- **`SkillForPhase` is kept**: The plan allowed keeping it for logging. It's still in `harness/internal/swarm/statemachine.go:182-207` and is used in `manager.go:214` to get the skill name for session DB records.

## Artifacts

- `harness/internal/swarmorch/manager.go` — lint fixes applied
- `harness/internal/swarm/transcript/parse.go` — auto-fixed nolint comments by linter hook

## Action Items & Next Steps

- **All plan phases are complete.** The prompt composition, transcript parsing, and prompt versioning features are committed.
- **Lint fixes need to be re-applied to the current code.** The remote branch diverged significantly (commits `9652321` through `da7ea2e` added human gates, Linear integration, hooks, etc.). The lint fixes from this session were based on the older code and don't apply cleanly. Run `just check` on the current code to see if the newer codebase has its own lint issues.
- All 15 unit tests pass across transcript (8 tests) and prompt (7 tests) packages
- Consider running a dry-run swarm workflow end-to-end to validate the full pipeline
- The SKILL.md files (`swarm-code-plan`, `swarm-code-pr`, `swarm-code-verify`, `swarm-conventions`, `swarm-research`) still exist in the newer remote code — may need to be re-evaluated for deletion depending on current usage

## Other Notes

- The swarm prompt system uses Go `text/template` (not templ) with `//go:embed` for markdown prompt composition. Base template at `harness/internal/swarm/prompt/templates/base.md.tmpl` provides shared sections; each phase defines a `{{ define "process" }}` block.
- Transcript parsing discovers JSONL files under `~/.claude/projects/{projectKey}/` (overridable via `CLAUDE_PROJECTS_DIR`), filtering by modification time after session start.
- Pricing uses per-million-token rates: Opus ($15/$75 I/O), Sonnet ($3/$15), Haiku ($0.80/$4). Unknown models default to Sonnet.
- The `site (golangci-lint)` failure seen in some runs is transient — running it independently shows 0 issues.
