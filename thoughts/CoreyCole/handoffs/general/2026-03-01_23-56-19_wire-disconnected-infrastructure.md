---
date: 2026-03-01T23:56:19-08:00
researcher: CoreyCole
git_commit: 951b81767fa422d8253ba7cd17e05901885bab5b
branch: feature/agent-swarm
repository: creative-mode
topic: "Wire Disconnected Swarm Infrastructure — Phase A Complete"
tags: [implementation, swarm, transcript, prompt-versioning, learnings, tokens]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Wire Disconnected Swarm Infrastructure (Phase A)

## Task(s)

**Phase A: Wire Disconnected Infrastructure — COMPLETED**

Implemented Phase A from the parent plan (`thoughts/CoreyCole/plans/2026-03-01_23-19-48_overstory-additional-swarm-improvements.md`). This phase connects three pieces of fully-built but never-called infrastructure:

1. **Transcript token extraction** — `transcript.ParseFile` + `transcript.DiscoverTranscript` now called from `handleSessionComplete` to populate per-bucket token counts, model, and cost
2. **Prompt version tracking** — `prompt.RenderPrompt` + `UpsertSwarmPromptVersion` now called from `spawnSession` to track prompt content hashes per session
3. **Learning tags** — `deriveLearningTags()` now populates the `tags` column in `swarm_learnings` with comma-separated phase+category

**Status**: All 6 sub-tasks completed. `just check` passes. All tests pass (`swarmorch`, `transcript`, `prompt`).

## Critical References

- Parent plan: `thoughts/CoreyCole/plans/2026-03-01_23-19-48_overstory-additional-swarm-improvements.md` (Phases B–H remain)
- Research: `thoughts/CoreyCole/research/2026-03-01_23-00-00_overstory-additional-improvements.md`

## Recent changes

- `harness/internal/db/queries/swarm.sql:58-63` — Added `CompleteSwarmSessionWithTokens` query (12 bind params)
- `harness/internal/db/sqlc/swarm.sql.go` — Regenerated (sqlc generate)
- `harness/internal/db/sqlc/querier.go` — Regenerated
- `harness/internal/db/sqlc/models.go` — Regenerated (no schema change, already had columns)
- `harness/internal/swarmorch/manager.go:84-87` — Added `promptVersionMu`/`promptVersionIDs` to Manager struct
- `harness/internal/swarmorch/manager.go:107` — Initialized `promptVersionIDs` map in `NewManager`
- `harness/internal/swarmorch/manager.go:22-23` — Added `prompt` and `transcript` imports
- `harness/internal/swarmorch/manager.go:323-351` — Wired prompt version tracking in `spawnSession` (after `buildEnv`, before `WriteHooksConfig`)
- `harness/internal/swarmorch/manager.go:509-608` — Replaced `CompleteSwarmSession` with `CompleteSwarmSessionWithTokens` in `handleSessionComplete`, added transcript discovery/parsing
- `harness/internal/swarmorch/manager.go:622` — Added `clearPromptVersionID` cleanup call
- `harness/internal/swarmorch/manager.go:1199-1215` — Added `setPromptVersionID`, `getPromptVersionID`, `clearPromptVersionID` helpers
- `harness/internal/swarmorch/learnings.go:211-222` — Added `deriveLearningTags` helper
- `harness/internal/swarmorch/learnings.go:40` — Added `Tags` field to `createLearning`
- `harness/internal/swarmorch/manager_test.go:36-54` — Updated test schema: added 7 migration-007 columns to `swarm_sessions`, added `swarm_prompt_versions` table
- `harness/internal/swarmorch/learnings_test.go` — NEW: `TestDeriveLearningTags`
- `harness/internal/swarm/transcript/discover_test.go` — NEW: `TestProjectKeyFromPath`, `TestDiscoverTranscript_NoFiles`, `TestDiscoverTranscript_FindsRecent`

## Learnings

- The `CompleteSwarmSession` sqlc query never actually set `TotalTokens` — it was in the params struct but the query only had 4 `?` placeholders. The new `CompleteSwarmSessionWithTokens` has all 12 columns.
- `startedAt` in `handleSessionComplete` was scoped inside an `if` block — had to hoist it out so it could be reused for transcript discovery.
- The `prompt.RenderPrompt` function only has templates for 6 phases (research, code_plan, plan_review, implement, verify, pr). Terminal/project phases return an error — this is expected and handled with a warn log in `spawnSession`.
- gosec requires `0o750` for `MkdirAll` and `0o600` for `WriteFile` in tests — not `0o755`/`0o644`.
- The linter (golangci-lint via `just check`) auto-reformatted the long struct literals in `manager.go` to break `CacheReadTokens` and `CacheCreationTokens` into multi-line `sql.NullInt64{}` blocks.

## Artifacts

- `harness/internal/db/queries/swarm.sql:58-63` — New `CompleteSwarmSessionWithTokens` query
- `harness/internal/swarmorch/learnings_test.go` — New test file
- `harness/internal/swarm/transcript/discover_test.go` — New test file
- `thoughts/CoreyCole/plans/2026-03-01_23-19-48_overstory-additional-swarm-improvements.md` — Parent plan (Phases B–H)

## Action Items & Next Steps

The parent plan has 7 remaining phases (B–H), all independent except Phase H (depends on Phase A which is now complete):

1. **Phase B: Security Hardening** — Expand bash deny list from 4 to 20+ patterns, add path boundary enforcement for Write/Edit, add secret redaction. Highest safety impact.
2. **Phase C: Health Monitoring** — Per-phase stall thresholds (20min PR, 75min research/implement), tool activity timestamps, tmux liveness verification during SSE ticks. Needs migration `012_activity_tracking.sql`.
3. **Phase D: Learning Enhancements** — Classification tiers with differentiated decay rates. Needs migration `013_learning_classification.sql`.
4. **Phase E: Event Observability** — Filter PostToolUse payloads to ~200 bytes, add `phase` and `file` fields to events, fix SSE handler type assertion that drops typed struct events.
5. **Phase F: Doctor Health Check** — `/api/swarm/doctor` endpoint verifying DB, tmux, Linear, Graphite, Discord, disk.
6. **Phase G: Workflow Overrides** — Per-workflow phase skipping and gate overrides. Needs migration `014_workflow_overrides.sql`.
7. **Phase H: Auto-Insight Extraction** — Auto-analyze transcripts for tool profiles and hot files (depends on Phase A's transcript wiring, now available).

Recommended next: **Phase B** (security) or **Phase E** (event observability, fixes existing dashboard bugs).

## Other Notes

- All changes are on branch `feature/agent-swarm`
- The existing plan from the first research session is at `thoughts/CoreyCole/plans/2026-03-01_21-37-45_overstory-swarm-improvements.md` — this covers a separate set of 6 phases (different from B–H above)
- The swarm orchestrator architecture comparison is at `thoughts/CoreyCole/research/2026-03-01_21-19-53_overstory-vs-swarm-architecture.md`
- Key file locations: `harness/internal/swarmorch/manager.go` (orchestrator), `harness/internal/swarm/transcript/` (token parsing), `harness/internal/swarm/prompt/` (prompt rendering), `harness/internal/swarmorch/learnings.go` (learning capture)
