---
date: 2026-03-01T02:40:41-08:00
researcher: CoreyCole
git_commit: 3ce5f93a935f112ce6d32c5bf64d608464810e68
branch: feature/agent-swarm
repository: creative-mode
topic: "Agent Swarm Phase 4D Alerts + Learning Digest Complete"
tags: [implementation, swarm, orchestrator, alerts, digest, learnings, observability]
status: complete
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Phase 4D Alerts + Learning Digest Loop Complete

## Task(s)

Implementing Phase 4D of the Agent Swarm Phase 4 completion plan — Discord alerts, learning relevance decay, digest generation, and periodic maintenance tickers.

| Sub-Phase | Description | Status |
|-----------|-------------|--------|
| **4A: Structured Logging + JSONL** | SessionLog wrapper, per-session JSONL files, log API endpoint | **Completed** (previous) |
| **4B: Hook System + Completion Registry** | CompletionRegistry, StartRegistry, WriteHooksConfig, 6 hook endpoints | **Completed** (previous) |
| **4C: Metrics + Health** | SQL aggregation with cache, health endpoint, session status | **Completed** (previous) |
| **4D: Alerts + Learning Digest Loop** | Discord alerts, daily digest, relevance decay, periodic tickers | **Completed** (this session) |
| **4E: Dashboard Enhancements** | Metrics/Learnings tabs, live tool activity feed | **Not started** (next) |
| **4F: President Skill Integration** | swarm-learnings skill for president | Planned |
| **4G: Temporal Integration** | Feature-flagged Temporal workflow engine | Planned |

Working from full plan: `thoughts/CoreyCole/plans/2026-03-01_01-27-27_swarm-phase4-completion.md`

## Critical References

1. **Full implementation plan**: `thoughts/CoreyCole/plans/2026-03-01_01-27-27_swarm-phase4-completion.md` — detailed specs for all 7 sub-phases including Phase 4E-4G.
2. **Previous handoff (Phase 4C)**: `thoughts/CoreyCole/handoffs/general/2026-03-01_02-16-15_swarm-phase4c-metrics-health.md`
3. **Existing orchestrator**: `harness/internal/swarmorch/manager.go` — the core Manager struct with alerts, maintenance loop, hook-driven completion, registries, JSONL logging, and metrics cache.

## Recent changes

All on branch `feature/agent-swarm`, uncommitted changes on top of commit `3ce5f93` (Phase 4C commit):

**Phase 4D — new files:**
- `harness/internal/swarmorch/alerts.go` — NEW: `AlertManager` struct with `DiscordSender` interface, `FireTerminalFailure`, `FireCrashRecovery`, `FireStallDetected` methods. Fire-and-forget goroutines with 1hr dedup via `shouldFire()` method. `DiscordSender` interface decouples from `worldchannel.Client` for testability.
- `harness/internal/swarmorch/digest.go` — NEW: `GenerateDigest` function that queries learnings since last digest, groups by category, runs `DetectPatterns` (3 rules: repeated code bugs → update verify skill, repeated plan issues → update plan skill, post-mortems → review config), writes markdown to `thoughts/swarm/digests/{date}_digest.md`, stores in `swarm_learning_digests` table.
- `harness/internal/swarmorch/alerts_test.go` — NEW: 4 tests for dedup, different alert types, nil discord safety, and expiry.
- `harness/internal/swarmorch/digest_test.go` — NEW: 7 tests covering pattern detection for each rule type, no-pattern case, empty digest, and digest with seeded learnings.

**Phase 4D — modified files:**
- `harness/internal/swarm/learnings.go:288-307` — Added `DecayLearningRelevance(ctx, db)` with 0.95 multiplier on active learnings with score > 0.1, plus auto-archive of learnings >60 days old with relevance < 0.1.
- `harness/internal/swarm/learnings_test.go` — Added 3 tests for decay (basic decay, skip low score, archive old).
- `harness/internal/swarmorch/manager.go:37` — Added `stallCheckInterval` (2min), `decayInterval` (1hr), `digestInterval` (24hr) constants. Added `alertMgr *AlertManager` and `maintenanceCancel context.CancelFunc` fields to Manager struct. Added `SetAlertManager()`, `StartMaintenance()`/`StopMaintenance()`, `maintenanceLoop()`, and `detectAndAlertStalls()` methods. Wired `alertMgr.FireTerminalFailure` into `advanceWorkflow` terminal failure path. Wired `alertMgr.FireCrashRecovery` into `watchSession` tmux fallback path.
- `harness/internal/swarmorch/manager_test.go:95-104` — Added `swarm_learning_digests` table to test schema.
- `harness/internal/server/swarm_api.go:15,242-362` — Added `defaultLearningsLimit` const, 3 new handlers: `handleSwarmLearnings` (GET with ticket/phase filters), `handleSwarmCreateLearning` (POST), `handleSwarmLatestDigest` (GET latest digest).
- `harness/internal/server/server.go:175-177,215-216` — Registered 3 learning/digest routes under both `swarmGroup` (hookSecret) and `approved` group (dashboard).

## Learnings

- **Import cycle prevention**: `digest.go` was initially placed in `harness/internal/swarm/` but caused an import cycle (`swarm` → `db` → `sqlc` → `swarm`). Moved to `harness/internal/swarmorch/` which already imports both `db` and `swarm`. Any new code that needs both `swarm` types and `db.DB` methods must go in `swarmorch` or a new package, not `swarm`.
- **`DiscordSender` interface for testability**: Rather than depending on `*worldchannel.Client` directly, `AlertManager` takes a `DiscordSender` interface with just `SendMessage(channelID, content string) (string, error)`. This enables mock-based testing without Discord credentials.
- **Existing sqlc queries already cover most needs**: `DecaySwarmLearningRelevance`, `ListRecentSwarmLearnings`, `CreateSwarmLearningDigest`, `GetLatestSwarmLearningDigest`, and `ArchiveSwarmLearning` were already defined in `harness/internal/db/queries/swarm_learnings.sql`. The raw SQL in `DecayLearningRelevance` is used for the archive step (which has a date filter the sqlc query doesn't have), while the simpler decay uses the same logic as the sqlc query.
- **Linter requirements**: `perfsprint` requires string concatenation instead of `fmt.Sprintf` for simple `"prefix:" + var` cases. `gosec` requires 0o750/0o600 for directory/file permissions. `revive` requires early-return patterns (invert condition + continue). `mnd` flags magic numbers — extract to named constants.
- **93 tests now pass** across `swarm/` (25) and `swarmorch/` (68) — up from 77 before Phase 4D.
- **`SetAlertManager` is a setter rather than constructor param**: This avoids changing the `NewManager` signature which would break all existing test callsites. The manager works without alerts (nil check guards).

## Artifacts

- `harness/internal/swarmorch/alerts.go` — AlertManager + Discord alerts
- `harness/internal/swarmorch/digest.go` — GenerateDigest + DetectPatterns
- `harness/internal/swarmorch/alerts_test.go` — 4 alert tests
- `harness/internal/swarmorch/digest_test.go` — 7 digest/pattern tests
- `harness/internal/swarm/learnings.go` — DecayLearningRelevance
- `harness/internal/server/swarm_api.go` — 3 new learning/digest API handlers
- `thoughts/CoreyCole/plans/2026-03-01_01-27-27_swarm-phase4-completion.md` — Full implementation plan (READ THIS for Phases 4E-4G specs)

## Action Items & Next Steps

1. **Commit Phase 4D changes** — all changes are currently uncommitted. Tests pass, lint is clean.

2. **Phase 4E: Dashboard Enhancements** — Next phase. Per the plan:
   - `harness/views/swarm/dashboard.templ` — Add `MetricsCards(metrics)`, `HealthStatus(health)`, `LearningsSection(learnings)` components. Add "Metrics" and "Learnings" tabs.
   - `harness/internal/server/swarm_dashboard.go` — Fetch metrics + health data for dashboard. Handle `EventSwarmToolUse` in SSE for live tool activity feed. Show handoff paths, context pressure indicator, and token counts per session in workflow detail.

3. **Wire `SetAlertManager` in `main.go`** — Currently `AlertManager` is created but not wired into the running server. In `main.go` where `SwarmManager` is initialized, call `swarmMgr.SetAlertManager(alertMgr)` with a `worldchannel.Client` and `DISCORD_PRESIDENT_CHANNEL_ID`. Also call `swarmMgr.StartMaintenance()` on startup and `swarmMgr.StopMaintenance()` on shutdown.

4. **Continue with Phases 4F through 4G** sequentially per the plan document.

5. **Run `just check` after each phase** — harness lint must be clean.

## Other Notes

- **New API endpoints added this session**:
  | Method | Route | Auth | Purpose |
  |--------|-------|------|---------|
  | GET | `/api/swarm/learnings?ticket=&phase=` | hookSecret | Filtered learnings query |
  | POST | `/api/swarm/learnings` | hookSecret | Create learning from skill |
  | GET | `/api/swarm/learnings/digest/latest` | hookSecret | Latest digest |
  | GET | `/swarm/api/learnings` | approved | Dashboard learnings |
  | GET | `/swarm/api/learnings/digest/latest` | approved | Dashboard digest |
- **Maintenance loop intervals**: Stall detection every 2min (uses existing `GetHealth` which already detects stalls via 45min threshold), decay every 1hr, digest every 24hr.
- **Pattern detection is deterministic**: No LLM calls — uses simple counting rules. Same tag in >=2 code bugs, >=2 plan issues, any post-mortems. This makes digest generation fast and predictable.
- **DB schema note**: `swarm_learning_digests` table must be in the test schema (`manager_test.go:95-104`). This was added during this session.
