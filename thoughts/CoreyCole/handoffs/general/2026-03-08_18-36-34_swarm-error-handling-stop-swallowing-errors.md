---
date: 2026-03-08T18:36:34-07:00
researcher: CoreyCole
git_commit: 3c15d001d56a6fe57fc8efaa05c6cbb89225d05e
branch: feat/agent-primitives
repository: creative-mode
topic: "Swarm Error Handling — Stop Swallowing Errors"
tags: [implementation, swarm, error-handling, observability]
status: complete
last_updated: 2026-03-08
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm Error Handling — Stop Swallowing Errors

## Task(s)

**Completed:** Replaced pervasive `_ =` error-swallowing patterns across the swarm codebase with proper error handling (return errors where callers can act, log where they can't). All changes pass `just check` (lint + build).

**Next:** End-to-end testing of research and code_change_plan workflows to verify logs appear correctly, dashboard renders properly, and output artifacts are created.

## Critical References

- `harness/CLAUDE.md` — Swarm API/Dashboard architecture, testing with dev login
- Memory file `MEMORY.md` — Swarm workflow operations (start, cancel, status, gate, tmux monitoring, logs)

## Recent changes

All changes are unstaged on `feat/agent-primitives`:

- `harness/internal/server/swarm_api.go` — 4 fixes: workflow failure cleanup logs error; workflow ID persistence returns 500; GetTask artifacts/spans return 500; GetTaskMetrics spans return 500
- `harness/internal/server/swarm_dashboard.go` — Extracted `loadTaskDetail()` helper to reduce nesting complexity. Initial page load, SSE event, and SSE heartbeat DB reads all log warnings. Dashboard task creation/cancel: workflow ID persistence returns 500; status cleanup logs error; post-action DB reads log warnings. SSE patch `_ =` left unchanged (client disconnect is normal).
- `harness/internal/swarmorch/workflows.go` — 17 fixes: `newSpanID` logs SideEffect failure; `deferredCleanup` logs all 4 activity failures; `createStageSpan`/`completeStageSpan` log failures; all narrative messages, event emissions, workflow span completions log warnings
- `harness/internal/swarmorch/agent.go` — Added `logger *slog.Logger` parameter to all 6 span helper functions (`createSpan`, `completeSpan`, `completeSpanWithMetadata`, `failSpan`, `failSpanWithMetadata`, `failOrphanedChildSpans`); each logs DB errors and skips event publish on failure; ~20 call site updates
- `harness/main.go` — 3 fixes: room seeding write, swarm log dir, discord listener stop

## Learnings

- **nestif linter**: Adding inline `if err != nil` checks inside already-nested blocks triggers the `nestif` complexity linter. Solution: extract helper methods (like `loadTaskDetail`) to flatten nesting.
- **Decision framework for error levels**: Error = operator needs to act now (data loss, broken invariant, returned as 500). Warn = degraded but system continues (tracing loss, empty dashboard, cleanup path). All span/narrative/event failures are correctly Warn because they're observability infrastructure — losing a span doesn't break the workflow.
- **SSE patches are correctly `_ =`**: Client disconnects are the normal lifecycle of SSE connections. These should never be logged.
- **Logger availability in main.go**: The room seeding code runs before `logger` is initialized, so it uses stdlib `log.Printf` instead of `slog`.

## Artifacts

- `harness/internal/server/swarm_api.go` — Error handling for API endpoints
- `harness/internal/server/swarm_dashboard.go` — Error handling for dashboard + `loadTaskDetail` helper (line 28)
- `harness/internal/swarmorch/workflows.go` — Error handling for Temporal workflow infra calls
- `harness/internal/swarmorch/agent.go` — Logger parameter added to span helpers (lines 618-825)
- `harness/main.go` — Startup/shutdown error handling

## Action Items & Next Steps

1. **Start a research workflow end-to-end** and verify:
   - `POST /api/swarm/tasks/research` with `X-Hook-Secret` header (or use dashboard at `/swarm`)
   - Spans are created in DB and visible on the Spans tab
   - Narrative messages appear in the Chat tab
   - Research output doc appears in `thoughts/swarm/research/`
   - Artifacts tab shows the research_doc entry
   - Check logs: `sudo journalctl -u creative-mode --since "5 min ago" --no-pager | grep -iE 'swarm|warn|error'` — should see no unexpected warnings

2. **Start a code_change_plan workflow end-to-end** and verify:
   - Same checks as research, plus planning stages (classification, specialist planners, synthesis)
   - Output doc appears in `thoughts/swarm/project-plans/`
   - Child research workflow spans nest under the parent code_change_plan workflow span

3. **Test error paths** (optional):
   - Kill Temporal mid-workflow and verify cleanup logs appear (deferredCleanup warnings)
   - Check dashboard renders gracefully when DB reads fail (e.g., query a non-existent taskID)

4. **Dashboard SSE verification**:
   - Open `/swarm` in browser, start a task, verify sidebar and detail pane update in real-time
   - Check browser console for errors
   - Verify heartbeat updates (wait 30s with dashboard open)

## Other Notes

- **How to start workflows from CLI**: See MEMORY.md for curl commands. Quick reference:
  ```
  curl -X POST http://localhost:8080/api/swarm/tasks/research \
    -H "X-Hook-Secret: $CM_HOOK_SECRET" \
    -H "Content-Type: application/json" \
    -d '{"requestText":"How does the swarm temporal workflow work?"}'
  ```
- **Temporal dashboard**: http://localhost:8233 (namespace: `swarm`)
- **Monitor tmux sessions**: `tmux ls | grep cm-swarm`
- **Swarm logs**: `data/logs/swarm/` (JSONL, 7-day retention)
- **Dev login for dashboard testing**: `POST /dev/auth/login` with `username=test&role=admin`, then navigate to `/swarm`
