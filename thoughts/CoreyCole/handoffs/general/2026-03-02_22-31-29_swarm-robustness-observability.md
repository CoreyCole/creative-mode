---
date: 2026-03-02T22:31:29-08:00
researcher: CoreyCole
git_commit: 8a41da90cbc86e3d26b508eebef256d7401182ac
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm Orchestrator Robustness & Observability Review"
tags: [swarm, robustness, observability, reliability, architecture]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Robustness & Observability Review

## Task(s)

### Completed: Durable Result Files + Remove watchSession Goroutine
Implemented the plan from `thoughts/CoreyCole/plans/2026-03-03_05-35-00_swarm-durable-results-and-session-watching.md`. All three phases complete:

1. **Phase 1 (Durable Result Files)** — COMPLETE. Result files moved from `/tmp/*.txt` to `thoughts/swarm/results/*.md`. Sessions write `RESULT: in_progress` as first step, update during work, finalize at end. `ParseResultFile` treats `in_progress` as `infra_failure` with crash context.

2. **Phase 2 (Remove watchSession Goroutine)** — COMPLETE. Hooks now call `HandleSessionComplete` directly. Removed `watchSession`, `CompletionRegistry`, `RecoverWorkflows`, lifecycle context, and `Shutdown()`. Temporal heartbeat (`SpawnPendingSessions`) is the durable safety net.

3. **Phase 3 (Documentation)** — COMPLETE. Updated `CLAUDE.md` and `harness/CLAUDE.md`.

### Planned: Swarm Robustness & Observability Improvements
Continue reviewing the swarm orchestrator for remaining reliability gaps, observability blind spots, and architectural improvements.

## Critical References
- `harness/CLAUDE.md` — Full swarm architecture docs (hooks, state machine, API, config)
- `harness/internal/swarmorch/manager.go` — Core orchestrator (workflow lifecycle, session spawning, completion handling)
- `harness/internal/swarm/statemachine.go` — State machine for phase transitions and retry logic

## Recent Changes

- `harness/internal/swarmorch/manager.go` — `ResultFilePath()` returns `thoughts/swarm/results/<sessionID>.md`; removed `watchSession`, `CompletionRegistry`, `RecoverWorkflows`, lifecycle context; renamed `handleSessionComplete` → `HandleSessionComplete` (exported); cleanup (start registry, context pressure) moved into `HandleSessionComplete`
- `harness/internal/swarmorch/registry.go` — Removed `CompletionRegistry` and `SessionResult` struct; kept `StartRegistry` for logging
- `harness/internal/server/swarm_hooks.go` — `session-complete` and `session-ended` hooks call `HandleSessionComplete` directly instead of signaling through channels; removed `swarm` import
- `harness/internal/swarm/result.go:83-97` — `ParseResultFile` now handles `in_progress` as crash indicator
- `harness/internal/swarm/prompt/templates/base.md.tmpl:95-112` — Added "Session Initialization" section (write-first pattern)
- `harness/main.go` — Removed `RecoverWorkflows()` call and `Shutdown()` call

## Learnings

- **Hook-driven is simpler and more reliable**: The previous architecture had hooks → `CompletionRegistry` channel → `watchSession` goroutine → `handleSessionComplete`. This indirection existed because the original design wanted goroutines to own session lifecycle. But goroutines die on `air` hot-reload, creating a reliability gap that `RecoverWorkflows` tried to paper over. Making hooks call `HandleSessionComplete` directly eliminates the entire class of "goroutine died" bugs.

- **Result files in /tmp were the root cause of CRE-8 failures**: All 4 CRE-8 child tickets failed at `code_plan` with "result file missing" because the prompt told Claude to write the result file "as the very last step." If Claude crashes or runs out of context, the file is never written. The write-first pattern (write `in_progress` at start) means the file always exists with at least partial progress.

- **Double-fire guard is critical**: `HandleSessionComplete` has a guard at line ~454 (`session.CompletedAt.Valid`) that prevents duplicate processing. This is essential now that multiple callers can trigger it (Stop hook, SessionEnd hook, Temporal heartbeat).

- **`sessionPollInterval` (15s) is still used**: It's used in `activities.go:65` for `RunClaudeSession` activity polling. Don't remove it.

## Artifacts

- `thoughts/CoreyCole/plans/2026-03-03_05-35-00_swarm-durable-results-and-session-watching.md` — Implementation plan (fully executed)
- `thoughts/swarm/results/.gitkeep` — New durable results directory
- `thoughts/swarm/retrospectives/2026-03-03-CRE-8-{1,2,3,4}-code_plan.md` — CRE-8 child failure retrospectives

## Action Items & Next Steps

The swarm has been significantly simplified — no more goroutines for session watching, durable result files, and direct hook-driven completion. The next focus should be identifying remaining robustness and observability gaps:

1. **Audit remaining `/tmp` files**: `LearningFilePath()`, `TokenFilePath()`, and context pressure sentinel files still use `/tmp`. Evaluate whether any of these should be durable. Learning context files are transient inputs (written before session, read by session, cleaned up after), so they're probably fine. But the context pressure sentinel (`/tmp/swarm-context-pressure-<sessionID>`) could be lost on reboot.

2. **Session-level observability**: The JSONL log (`data/logs/<ticketID>/<sessionID>.jsonl`) captures tool use but not token counts mid-session. Consider adding periodic token usage snapshots from transcript parsing.

3. **Stall detection improvements**: `detectAndAlertStalls` fires alerts but doesn't auto-recover. Consider adding auto-cancel for sessions stalled beyond a configurable threshold (e.g., 2x `stallMinutes`).

4. **Error classification enrichment**: `HandleSessionComplete` has smart crash classification (context pressure → `context_limit`). Consider adding more heuristics — e.g., if the result file has `in_progress` but the last progress entry mentions a specific error, extract it for the summary.

5. **Temporal workflow observability**: The Temporal UI (port 8233) provides workflow-level visibility, but there's no dashboard integration showing Temporal schedule health. Consider adding a `/swarm/api/temporal-health` endpoint.

6. **Project workflow wave advancement**: `CheckProjectProgress` and `AdvanceProject` in `activities.go` handle child ticket waves. Review whether the capacity throttling (`maxSessions`) is correctly applied across both standalone and project-child workflows.

7. **Result file rotation**: `thoughts/swarm/results/` will accumulate files over time. Consider adding a retention policy (e.g., archive results older than 30 days) or a cleanup in the heartbeat.

8. **Prompt template regression testing**: The render tests verify section presence but don't validate the actual instructions. Consider snapshot testing for prompt templates to catch unintended changes.

## Other Notes

- **Swarm config** is stored in SQLite: `sqlite3 data/creative-mode.db "SELECT config FROM swarm_config WHERE id = 'default';"`
- **Start a workflow**: `curl -X POST http://localhost:8080/api/swarm/start -H "X-Hook-Secret: $CM_HOOK_SECRET" -H "Content-Type: application/json" -d '{"ticket_id":"CRE-5","workflow_type":"code"}'`
- **Monitor tmux sessions**: `tmux ls | grep cm-swarm`
- **Check Temporal schedules**: `temporal schedule list --namespace swarm`
- **Key test command**: `cd harness && go test ./internal/swarm/... ./internal/swarmorch/... -v`
- The swarm domain types are in `internal/swarm/` (pure, no I/O) and the orchestrator is in `internal/swarmorch/` (DB, HTTP, integrations). This separation is intentional and should be maintained.
