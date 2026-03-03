# Swarm: Durable Result Files + Remove watchSession Goroutine

## Overview

Two linked fixes for swarm session reliability:
1. Move result files from `/tmp` to `thoughts/swarm/results/` as markdown — write on session start, append during work, update final status at end
2. Remove the `watchSession` goroutine — hooks call `handleSessionComplete` directly, heartbeat is the safety net

## Current State Analysis

**Result files are ephemeral and written last.** `ResultFilePath()` returns `/tmp/swarm-result-<id>.txt`. The prompt template tells Claude to write this "as the very last step." If Claude crashes, runs out of context, or the file gets cleaned up, `ParseResultFile` returns `infra_failure` — which is exactly what happened to all 4 CRE-8 children.

**`watchSession` is a goroutine that dies on restart.** `spawnSession()` at `manager.go:495` launches `go m.watchSession(...)` which blocks on hook signals and tmux health checks. When `air` hot-reloads, the goroutine dies via `m.ctx.Done()` without calling `handleSessionComplete`. The `RecoverWorkflows()` method at `manager.go:304` tries to re-attach watchers after restart, but there's a race window and it's fundamentally a band-aid.

**The hooks already do the real work.** `handleSwarmHookSessionComplete` (`swarm_hooks.go:201`) and `handleSwarmHookSessionEnded` (`swarm_hooks.go:236`) fire when Claude Code stops. They read the result file and signal the `CompletionRegistry` channel. The goroutine receives from that channel and calls `handleSessionComplete`. This indirection is unnecessary — hooks can call `handleSessionComplete` directly.

**The heartbeat already catches orphans.** `LeadFDEWorkflow` runs every 2 minutes and calls `SpawnPendingSessions` → `ReadTicketQueue`, which detects dead tmux sessions and calls `handleSessionComplete` at `activities.go:163`. This is the durable safety net.

### Key Discoveries:
- `ResultFilePath()` at `manager.go:2027` → `/tmp/swarm-result-<id>.txt`
- `ParseResultFile()` at `result.go:28` → returns `infra_failure` on missing file
- `base.md.tmpl:117` → "As the **very last step**, write the RESULT block to `{{ .ResultPath }}`"
- `watchSession` at `manager.go:502` — goroutine with `m.ctx.Done()` exit path
- `handleSessionComplete` at `manager.go:590` — has double-fire guard (`session.CompletedAt.Valid`)
- `CompletionRegistry` at `registry.go:19` — only exists to bridge hooks → goroutine
- `RunClaudeSession` activity at `activities.go:22` — existing Temporal activity that polls DB (not currently wired into main flow)

## Desired End State

1. Result files live at `thoughts/swarm/results/<sessionID>.md` as structured markdown
2. Created at session start with `RESULT: in_progress`, updated throughout, finalized at end
3. If Claude crashes mid-session, the file exists with partial progress and `in_progress` status
4. `ParseResultFile` treats `in_progress` as `infra_failure` with context
5. No `watchSession` goroutine — hooks call `handleSessionComplete` directly
6. `CompletionRegistry` removed, `StartRegistry` kept for logging
7. `RecoverWorkflows` removed — heartbeat handles recovery

### Verification:
- `just check` passes
- Kill a running swarm session mid-work → result file exists with `in_progress` and partial content
- Restart harness during active session → heartbeat detects and completes within 2 minutes
- Hook-driven completion still works for normal session endings

## What We're NOT Doing

- NOT wiring `advanceWorkflow` through Temporal `SessionWorkflow` (hooks + heartbeat is sufficient)
- NOT moving other `/tmp/swarm-*` files (tokens, learning context, context pressure sentinel) — those are truly ephemeral
- NOT changing the RESULT format — just the file location and lifecycle
- NOT modifying the SKILL.md files in `.claude/skills/swarm-*/` — those are supplementary docs; the rendered prompt template is the canonical instruction

## Implementation Approach

The changes split into two independent phases. Phase 1 (result files) can ship alone and immediately improves reliability. Phase 2 (remove goroutine) depends on Phase 1 working but is a clean simplification.

---

## Phase 1: Durable Result Files

### Overview
Move result files from `/tmp/*.txt` to `thoughts/swarm/results/*.md`. Write on session start with `in_progress`, append progress during work, finalize at end.

### Changes Required:

#### 1. Result file path and directory
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Update `ResultFilePath()` to use `thoughts/swarm/results/` and `.md` extension

```go
// ResultFilePath returns the durable path for a session's RESULT output.
func ResultFilePath(sessionID string) string {
    return filepath.Join("thoughts", "swarm", "results", sessionID+".md")
}
```

Remove `resultFilePrefix` constant (no longer needed).

Ensure the directory exists — add `os.MkdirAll` call in `NewManager()` or `spawnSession()`.

#### 2. Stop deleting result files after reading
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Remove `os.Remove(resultPath)` at line 758. Result files are now a durable audit trail.

#### 3. Update prompt template — write-first pattern
**File**: `harness/internal/swarm/prompt/templates/base.md.tmpl`
**Changes**: Replace the "Result File Output" section at the bottom AND add a new "Session Initialization" section near the top of the Process block. The key change: write the file FIRST with `in_progress`, then update it at the end.

Update the base template's Result File Output section (lines 115-118):
```markdown
## Session Initialization

As your **very first step**, before any research or analysis, write the result file to `{{ .ResultPath }}`:

```
RESULT: in_progress
Phase: {current_phase}
Ticket: {{ .TicketID }}
Session: {{ .SessionID }}
Started: {ISO 8601 timestamp}

## Progress
- Starting {phase} phase...
```

Update this file as you make progress — append key milestones to the Progress section.

## Result File Output

As your **final step**, update `{{ .ResultPath }}` with the final status:

```
RESULT: success
Phase: {current_phase}
Handoff: thoughts/swarm/handoffs-{type}/{filename}

Summary: {one-line summary}
```

Replace the entire file content — do not append. This is how the orchestrator detects completion.
```

#### 4. Update PromptContext with result path
**File**: `harness/internal/swarm/prompt/context.go`
**Changes**: No change needed — `ResultPath` field already exists and is populated by `buildEnv()` and `RenderPrompt()`.

#### 5. Add `in_progress` as a recognized but non-success result
**File**: `harness/internal/swarm/result.go`
**Changes**: Treat `in_progress` in `ParseResultFile` as a crash indicator with context.

In `ParseResultFile`, after the `if !foundResult` check, add handling for `in_progress`:
```go
// Treat in_progress as a crash — the session started but never completed.
if data.Result == "in_progress" {
    return &SessionResultData{
        Result:  ResultInfraFailure,
        Summary: fmt.Sprintf("session crashed mid-execution (progress: %s)", data.Summary),
    }, nil
}
```

Also add `in_progress` to the `Valid()` method check so it doesn't get caught by the "invalid result value" branch. Or: define `ResultInProgress SessionResult = "in_progress"` and check for it explicitly before the `Valid()` check.

#### 6. Ensure directory exists
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: In `spawnSession()`, before writing to `ResultFilePath`, ensure `thoughts/swarm/results/` exists:

```go
_ = os.MkdirAll(filepath.Dir(ResultFilePath(sessionID)), 0o755)
```

Add this once in `NewManager()` since it's a stable path.

#### 7. Update tests
**File**: `harness/internal/swarmorch/manager_test.go`
**Changes**: Update `TestHandleSessionCompleteDoubleFireGuard` and any other tests that reference `/tmp/swarm-result-*` to use the new path.

**File**: `harness/internal/swarm/prompt/render_test.go`
**Changes**: Update `ResultPath` in test contexts from `/tmp/swarm-result-*.txt` to `thoughts/swarm/results/*.md`.

**File**: `harness/internal/swarm/env_test.go`
**Changes**: Update `ResultPath` test values.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `cd harness && go test ./internal/swarm/... ./internal/swarmorch/...` — all tests pass
- [ ] `thoughts/swarm/results/` directory exists after harness start
- [ ] Result file created at session spawn time (verify with `ls thoughts/swarm/results/`)

#### Manual Verification:
- [ ] Start a swarm workflow → result file appears immediately with `in_progress`
- [ ] After session completes → result file updated to `success`/`logic_failure`
- [ ] Kill Claude mid-session → result file exists with `in_progress` + partial progress

---

## Phase 2: Remove watchSession Goroutine

### Overview
Make hook handlers call `handleSessionComplete` directly instead of signaling through `CompletionRegistry` → goroutine. Remove `watchSession`, `CompletionRegistry`, and `RecoverWorkflows`.

### Changes Required:

#### 1. Export `handleSessionComplete` for hook handlers
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Rename `handleSessionComplete` → `HandleSessionComplete` (exported). The double-fire guard at line 605 (`session.CompletedAt.Valid`) makes it safe to call from multiple sources.

#### 2. Hook handlers call HandleSessionComplete directly
**File**: `harness/internal/server/swarm_hooks.go`
**Changes**:

In `handleSwarmHookSessionComplete` (line 201): replace `SignalCompletion` call with direct `HandleSessionComplete`:
```go
// Previously: s.SwarmManager.SignalCompletion(sessionID, ...)
s.SwarmManager.HandleSessionComplete(c.Request().Context(), sessionID)
```

In `handleSwarmHookSessionEnded` (line 236): same change:
```go
s.SwarmManager.HandleSessionComplete(c.Request().Context(), sessionID)
```

Note: The hooks no longer need to read/parse the result file themselves — `HandleSessionComplete` already does that. Remove the `ParseResultFile` calls from the hook handlers (lines 215-216 and 250-251). Keep the JSONL event writing.

#### 3. Remove watchSession goroutine
**File**: `harness/internal/swarmorch/manager.go`
**Changes**:
- Remove `go m.watchSession(...)` from `spawnSession()` (line 495)
- Remove `go m.watchSession(...)` from `RecoverWorkflows()` (line 333)
- Remove the entire `watchSession` function (lines 500-586)
- Remove `startCh`/`completionCh` registration from `spawnSession()` (lines 430-431)

#### 4. Remove CompletionRegistry
**File**: `harness/internal/swarmorch/registry.go`
**Changes**: Remove `CompletionRegistry` struct and all its methods (lines 16-68). Keep `StartRegistry` — it's still used by `handleSwarmHookSessionStarted` for logging.

Also remove `SessionResult` struct (lines 9-14) — no longer needed since hooks don't pass results through channels.

**File**: `harness/internal/swarmorch/manager.go`
**Changes**:
- Remove `completionReg` field from `Manager` struct (line 65)
- Remove `NewCompletionRegistry()` from `NewManager()` (line 114)
- Remove `SignalCompletion` method (line 1578-1580)
- Remove `m.completionReg.Unregister(sessionID)` from `watchSession` defer (will be gone)
- Remove `completionCh` references from `spawnSession()`

#### 5. Remove RecoverWorkflows
**File**: `harness/internal/swarmorch/manager.go`
**Changes**: Remove `RecoverWorkflows()` function (lines 304-345). The heartbeat's `SpawnPendingSessions` → `ReadTicketQueue` handles recovery durably — it detects dead tmux sessions and calls `HandleSessionComplete` on the next heartbeat tick (within 2 minutes).

**File**: `harness/main.go` (or wherever `RecoverWorkflows` is called)
**Changes**: Remove the call to `RecoverWorkflows()` on startup.

#### 6. Simplify Manager lifecycle
**File**: `harness/internal/swarmorch/manager.go`
**Changes**:
- Remove lifecycle context (`ctx`, `cancel` fields) — no longer needed for goroutine management
- Remove `Shutdown()` method (or simplify to just close JSONL writers)
- Remove `tmuxFallbackInterval` and `startTimeout` constants (lines 33-34) — only used by `watchSession`

#### 7. Update ReadTicketQueue to use HandleSessionComplete
**File**: `harness/internal/swarmorch/activities.go`
**Changes**: At line 163, `a.mgr.handleSessionComplete` → `a.mgr.HandleSessionComplete` (now exported).

#### 8. Update tests
**File**: `harness/internal/swarmorch/manager_test.go`
**Changes**:
- Update `TestHandleSessionCompleteDoubleFireGuard` to call `HandleSessionComplete` (exported)
- Remove any tests that depend on `CompletionRegistry` or `watchSession`
- Add test: hook handler calls `HandleSessionComplete` directly

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] `cd harness && go test ./internal/swarmorch/...` — all tests pass
- [ ] `grep -r "watchSession\|CompletionRegistry" harness/internal/swarmorch/` — no matches (except comments/docs)

#### Manual Verification:
- [ ] Start a swarm workflow → session spawns without goroutine
- [ ] Session completes normally → hook fires → `HandleSessionComplete` called → workflow advances
- [ ] Kill `air` during active session → restart → heartbeat catches dead session within 2 min
- [ ] No "watchSession shutting down" messages in logs after restart

---

## Phase 3: Update Documentation

### Changes Required:

#### 1. Update CLAUDE.md
**File**: `CLAUDE.md`
**Changes**: In the Swarm Orchestrator section, add a note about Temporal-first architecture:

> **Execution model**: Session monitoring is hook-driven with Temporal heartbeat as safety net. No long-lived goroutines for session watching — process restarts are transparent. Goroutines are only used for fire-and-forget side-effects (Linear API calls, Discord alerts).

#### 2. Update harness/CLAUDE.md
**File**: `harness/CLAUDE.md`
**Changes**: Update the swarm architecture section:
- Remove references to `watchSession` goroutine
- Note that `CompletionRegistry` is removed
- Document that hooks → `HandleSessionComplete` is the primary completion path
- Document that `SpawnPendingSessions` heartbeat activity is the recovery path
- Update result file path in env var docs: `thoughts/swarm/results/<sessionID>.md`

---

## Testing Strategy

### Unit Tests:
- `ParseResultFile` with `in_progress` → returns `infra_failure` with context
- `ResultFilePath` returns `thoughts/swarm/results/<id>.md`
- `HandleSessionComplete` double-fire guard still works
- Prompt template renders with correct result path

### Manual Testing Steps:
1. Start a swarm workflow (`POST /api/swarm/start`)
2. Verify result file appears at `thoughts/swarm/results/<sessionID>.md` with `in_progress`
3. Let session complete → verify result file updated to final status
4. Start another workflow, kill tmux session manually → verify heartbeat recovers within 2 min
5. Start workflow, restart harness (`sudo systemctl restart creative-mode`) → verify recovery

## Performance Considerations

- Result files in `thoughts/swarm/results/` are small markdown (~500 bytes). No disk pressure concern.
- Removing goroutines slightly reduces memory/scheduling overhead per session.
- Hook-driven completion is synchronous in the HTTP handler — `HandleSessionComplete` does DB writes, event emission, and `advanceWorkflow`. This is acceptable since it's a single request per session completion (not high-throughput).

## Migration Notes

- Existing `/tmp/swarm-result-*` files are already cleaned up by `handleSessionComplete`. No migration needed.
- `thoughts/swarm/results/` directory must be created. Add to `NewManager()` init or gitkeep.
- Old result files won't exist after restart (they were in `/tmp`). No backward compatibility concern.

## References

- Failed CRE-8 children: all 4 died at `code_plan` with "result file missing: /tmp/swarm-result-*.txt"
- Working pattern: `~/.claude/commands/research_codebase.md` — writes output to `thoughts/` as core deliverable
- Working pattern: `~/.claude/commands/create_handoff.md` — same approach
- `manager.go:2027` — current `ResultFilePath()`
- `result.go:28` — current `ParseResultFile()`
- `base.md.tmpl:117` — current "very last step" instruction
- `registry.go:19` — `CompletionRegistry` to be removed
- `activities.go:163` — heartbeat safety net for dead sessions
