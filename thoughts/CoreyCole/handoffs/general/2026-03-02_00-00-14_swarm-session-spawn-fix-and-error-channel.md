---
date: 2026-03-02T00:00:14-08:00
researcher: CoreyCole
git_commit: 94a664271a6e81fe2104163096427836d908bc8e
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Session Spawn Fix & Error Channel"
tags: [implementation, temporal, swarm, bug-fix, discord, linear, error-reporting]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Session Spawn Fix, Error Channel, Linear Key Rotation

## Task(s)

1. **Fix broken Temporal fire-and-forget session spawning** — **Completed.** `LeadFDEWorkflow` used fire-and-forget child workflows (`workflow.ExecuteChildWorkflow` without `.Get()`) to spawn sessions. The parent workflow completed before the child start was confirmed by Temporal, causing child workflows to silently never start. Zero `SessionWorkflow` instances ever existed in Temporal despite being scheduled every 2 minutes for over an hour. CRE-8's `project_plan` session was stuck as a result.

2. **Add Discord swarm-errors channel** — **Completed.** Added `FireError()` method to `AlertManager` that posts to a dedicated `#swarm-errors` Discord channel (falls back to main alerts channel). Wired into `linearComment`, `linearUpdateStatus`, and `SpawnPendingSessions` so integration failures are no longer silent.

3. **Rotate Linear API key** — **Completed.** Old key `lin_api_NTk1s...` was expired (401). Replaced with new key `lin_api_ICwxW...` in `.env`.

4. **Test tech debt project workflow end-to-end** — **Completed.** CRE-8 project workflow successfully advanced through `project_plan` → `project_review` → `awaiting_review` gate. Project plan written to `thoughts/swarm/project-plans/2026-03-02_07-51-45_CRE-8_tech-debt-cleanup_v1.md`.

## Critical References

- `harness/internal/swarmorch/manager.go` — Core spawn and advance logic, Linear integration error reporting
- `harness/internal/swarmorch/workflows.go` — `LeadFDEWorkflow` (replaced child workflows with `SpawnPendingSessions` activity)
- `harness/internal/swarmorch/alerts.go` — `AlertManager` with new `FireError` and `errChannelID`

## Recent changes

- `harness/internal/swarmorch/manager.go:316-327` — Added idempotency guard to `spawnSession` (skip if active session exists for workflow+phase)
- `harness/internal/swarmorch/manager.go:929-940` — Added direct `spawnSession` call at end of `advanceWorkflow` after phase update
- `harness/internal/swarmorch/manager.go:227-237` — Added direct `spawnSession` call in `StartWorkflow` for initial research session
- `harness/internal/swarmorch/manager.go:1741-1751` — Added direct `spawnSession` call in `advanceFromGate`
- `harness/internal/swarmorch/manager.go:1679-1693` — Added direct `spawnSession` call in `RejectGate`
- `harness/internal/swarmorch/manager.go:1335-1365` — Wired `FireError` into `linearComment` for API failures
- `harness/internal/swarmorch/manager.go:1367-1412` — Wired `FireError` into `linearUpdateStatus` for API failures
- `harness/internal/swarmorch/workflows.go:216-225` — Replaced fire-and-forget child workflows with single `SpawnPendingSessions` activity call
- `harness/internal/swarmorch/activities.go:170-205` — New `SpawnPendingSessions` activity that reads ticket queue and spawns sessions directly
- `harness/internal/swarmorch/alerts.go:24-45` — Added `errChannelID` field and updated `NewAlertManager` signature
- `harness/internal/swarmorch/alerts.go:118-140` — New `FireError` method (no dedup, posts to error channel)
- `harness/internal/swarmorch/workflows_test.go:85-140` — Updated `LeadFDEWorkflow` tests: replaced `ReadTicketQueue`+`SessionWorkflow` mocks with `SpawnPendingSessions` mock
- `harness/main.go:250-281` — Wired `DISCORD_SWARM_ERRORS_CHANNEL_ID` env var into `NewAlertManager`
- `harness/.env:36-37` — Added `DISCORD_SWARM_ERRORS_CHANNEL_ID`, rotated `LINEAR_API_KEY`

## Learnings

- **Temporal fire-and-forget child workflows are unreliable**: `workflow.ExecuteChildWorkflow` without `.Get()` causes the parent to complete before the child workflow start is confirmed. With `PARENT_CLOSE_POLICY_ABANDON`, the child should theoretically survive, but in practice (SQLite-backed dev server) zero child workflows ever materialized. The fix: use a direct activity-based spawn instead of child workflows.
- **`spawnSession` idempotency is critical**: With both `advanceWorkflow` and the heartbeat's `SpawnPendingSessions` trying to spawn sessions, the idempotency guard (check if active session exists for workflow+phase) prevents double-spawning.
- **Air hot-reload doesn't reload env vars**: Changes to `.env` require a full `systemctl restart creative-mode`, not just an air rebuild.
- **CRE-13 Claude CLI stuck at interactive prompt**: When a Claude Code session completes its task but doesn't exit, the tmux session stays alive and the `RunClaudeSession` activity keeps polling. The session must be killed externally (tmux kill-session) and the reaper or recovery handles completion.

## Artifacts

- `harness/internal/swarmorch/manager.go` — Idempotency guard, direct spawn calls, error reporting
- `harness/internal/swarmorch/workflows.go` — Replaced child workflow pattern with `SpawnPendingSessions`
- `harness/internal/swarmorch/activities.go` — New `SpawnPendingSessions` activity
- `harness/internal/swarmorch/alerts.go` — `FireError` method, `errChannelID` support
- `harness/internal/swarmorch/workflows_test.go` — Updated tests
- `harness/internal/swarmorch/alerts_test.go` — Updated test constructors
- `harness/main.go` — Error channel wiring
- `harness/.env` — New Linear key, error channel ID
- `thoughts/swarm/project-plans/2026-03-02_07-51-45_CRE-8_tech-debt-cleanup_v1.md` — Tech debt project plan (generated by swarm)

## Action Items & Next Steps

1. **Review and approve CRE-8 project plan** — The workflow is at `awaiting_review` gate for `project_review`. Review the plan at `thoughts/swarm/project-plans/2026-03-02_07-51-45_CRE-8_tech-debt-cleanup_v1.md` and approve/reject via the swarm dashboard (`/swarm/e8c72519`) or API (`POST /api/swarm/gate/e8c72519/approve`). On approval, the orchestrator will create child tickets and spawn code workflows.

2. **Clean up duplicate Discord channels** — There are reportedly 3 `swarm-alerts` channels in Discord. Only `1477874879704465428` is used. The new `swarm-errors` channel is `1477938374005358607`. Delete the unused duplicates.

3. **Consider adding `FireError` to more failure points** — Currently wired into Linear API calls and `SpawnPendingSessions`. Could also add to: Discord alert send failures (currently just logged), Temporal client errors, hook delivery failures.

4. **Commit changes** — All changes are uncommitted on `feature/agent-swarm`. Consider committing the session spawn fix, error channel, and Linear key rotation.

5. **Remove dead `CheckProjectProgress` activity** — `activities.go:220-225` still exists but is no longer called by any workflow (noted in previous handoff).

## Other Notes

- **Discord channels**: Alerts → `1477874879704465428`, Errors → `1477938374005358607`
- **Linear API key**: Rotated 2026-03-02. New key starts with `lin_api_ICwxW...`
- **Temporal UI**: `http://localhost:8233` — can verify no more orphaned SessionWorkflow instances
- **CRE-5 project workflow**: Still at `project_review` / `awaiting_review` from earlier testing. Can be canceled if superseded by CRE-8.
- **All checks pass**: `just check` green, `go test ./internal/swarmorch/` and `go test ./internal/swarm/` both pass
- **Pre-existing test issues** (not related): `views/*` packages have `non-constant format string` build errors, `internal/linear` TestHTTPError times out
