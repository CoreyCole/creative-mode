---
date: 2026-02-11T00:15:53-08:00
researcher: CoreyCole
git_commit: 7ae1f2a071191710e964489e0c945c9f0bad9e43
branch: main
repository: creative-mode
topic: "Harness codebase cleanup and bugfix audit"
tags: [research, codebase, harness, security, cleanup, bugfixes]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
---

# Research: Harness Codebase Cleanup & Bugfix Audit

**Date**: 2026-02-11T00:15:53-08:00
**Researcher**: CoreyCole
**Git Commit**: 7ae1f2a071191710e964489e0c945c9f0bad9e43
**Branch**: main
**Repository**: creative-mode

## Research Question

After completing the dsutil/template cleanup (Phase 1-3), what additional cleanup opportunities and bugfixes remain in the harness codebase?

## Summary

The audit found **~40 issues** across the harness codebase, ranging from critical security concerns to minor code style inconsistencies. The most impactful findings are: path traversal vulnerabilities in two file-serving handlers, orphaned game server processes after crashes, a missing `worldName` field in build notifications, and several operations that should be transactional but aren't.

---

## Critical / High Severity

### 1. Path Traversal in `handleLogStream` and `handleWASMArtifacts`

**Files**: `internal/server/server.go:393-418`

URL parameters are joined directly into filesystem paths without containment checks. A crafted `worldID`, `cpID`, or `logType` with `..` could escape the intended directory. The wildcard `*` parameter in `handleWASMArtifacts` is especially risky since it accepts slashes.

**Fix**: Add `filepath.Clean` + `strings.HasPrefix(cleanedPath, expectedBase)` after `filepath.Join`.

### 2. Orphaned Game Server Processes After Crash

**File**: `internal/world/game_server.go:139-153`

The crash monitor goroutine logs the error and closes the log file but **never** removes the server from `m.servers`, releases the port, or resets the refcount. Subsequent `Connect()` calls return a stale entry pointing at a dead process. Over time, enough crashes exhaust the 999-port range.

**Fix**: The crash monitor goroutine should acquire `m.mu`, remove from `m.servers`/`m.refCount`, and call `m.ports.Release(srv.Port)`.

### 3. Unprotected `/api/claude-event` Endpoint

**File**: `internal/server/server.go:101-102, 422-445`

No authentication. Any network peer can POST arbitrary events to the EventBus and trigger `BuildCheckpoint`. The comment says "internal same-machine communication" but the server binds to `0.0.0.0:8080`.

**Fix**: Add a shared secret header check, or bind the endpoint to localhost only, or add it to an auth-protected group.

### 4. Missing `worldName` in `build.completed` Event (BUG)

**Files**: `internal/claude/claude.go:162` vs `internal/server/events.go:260`

The `build.completed` event published by the orchestrator does not include a `worldName` field. The consumer at `events.go:260` extracts `e["worldName"]` which type-asserts to `""`. The `BuildReadyNotification` renders with an empty world name.

**Fix**: Add `"worldName": worldName` to the event map at `claude.go:162` (the value is already available from `o.worldName()` called earlier at line 104).

### 5. `CreateWorld` and `ForkCheckpoint` Not in Transactions

**Files**: `internal/world/manager.go:86-117, 196-219`

`CreateWorld` performs three dependent DB inserts (world, checkpoint, user_position) without a transaction. If checkpoint insert fails, an orphaned world row exists. Same pattern in `ForkCheckpoint`. The sqlc-generated `WithTx` method exists but is never used anywhere.

**Fix**: Wrap the multi-insert sequences in `db.BeginTx` / `tx.Commit`.

### 6. Filesystem Leak on DB Insert Failure

**File**: `internal/world/manager.go:64-109`

Directories and files are created on disk (rsync, build cache clone) before DB inserts. If the insert fails, the filesystem artifacts are never cleaned up. Same in `ForkCheckpoint`.

**Fix**: Add cleanup in error paths (`os.RemoveAll(cpDir)` on failure).

### 7. No `ON DELETE CASCADE` on Any Foreign Key

**File**: `internal/db/migrations/001_initial.sql`

Every FK uses default `ON DELETE NO ACTION`. With `_foreign_keys=on`, `DeleteUser` fails for any user who has sessions, worlds, checkpoints, positions, messages, or prompt history. The `HandleRejectUser` flow only deletes sessions before deleting the user, missing all other tables.

**Fix**: Add `ON DELETE CASCADE` to appropriate FKs (sessions, user_positions, prompt_history) and/or add a proper cleanup sequence.

### 8. RateLimiter.Check Swallows DB Errors

**File**: `internal/world/rate_limit.go:54-62`

If `CountActiveBuilds` returns an error, the error is silently ignored and the function returns nil (not rate-limited). A database failure effectively disables the active-build rate limit.

**Fix**: Return the error or treat DB failure as rate-limited (fail-closed).

---

## Medium Severity

### 9. `handleSaveCheckpoint` Missing Authorization Check

**File**: `internal/server/server.go:362-390`

Does not call `requireUser(c)`. Any approved user can rename any checkpoint in any world. Other write handlers extract and use user identity.

### 10. No Tmux Session Watchdog

**Files**: `internal/claude/claude.go:62-90`, `internal/tmux/session.go`

If Claude Code hangs or crashes without triggering hooks, the checkpoint stays in `"building"` status forever and the tmux session persists indefinitely. No reaper or timeout mechanism exists.

### 11. HandlePrompt Partial Failure Leaves Orphaned Resources

**File**: `internal/claude/claude.go:62-90`

If `session.Create` or `session.SendPrompt` fails after `ForkCheckpoint` succeeds, the checkpoint is left in `"building"` status permanently. The tmux session (if created) is not killed in the error path.

### 12. SSE Write Errors Don't Terminate Connection

**File**: `internal/server/events.go:94-96`

Event handler methods have void return signatures. SSE write errors are logged but the connection stays alive. The heartbeat (30s interval) eventually detects the broken connection, but until then every event dispatch fails and logs errors.

### 13. Silent Message Dropping in EventBus

**File**: `internal/events/bus.go:57-60`

When a subscriber's channel buffer (100) is full, messages are silently dropped with no logging, metrics, or recovery mechanism.

### 14. UUID Truncation Collision Risk

**Files**: `internal/world/manager.go:60-61, 175`, `internal/claude/claude.go:176`, `internal/server/server.go:467`

UUIDs truncated to 8 hex chars (32 bits). Birthday paradox: ~1% collision probability at ~9,300 IDs, ~50% at ~77,000. Message IDs in server.go:467 also use this pattern.

### 15. Unused `user` Parameter in `Overlay` Template

**File**: `views/world/overlay.templ:10`

`templ Overlay(w, cp, user, checkpoints)` -- the `user` parameter is never referenced in the template body after the `OverlayTopBar` signature was cleaned up. Should be removed from `Overlay` too, and the call site in `world.templ` updated.

### 16. Unused dsutil Exports

**Files**: `views/dsutil/data_class.go`, `views/dsutil/expressions.go`, `views/dsutil/signals.go`

Several methods/types were ported from the reference but never called: `NewDataClass`, `DataClass.Add`, `BuildConditional`, `DatastarExpression.Conditional`, `DatastarExpression.SetSignal`, `SignalManager.NotEquals`, `SignalManager.Conditional`, `SignalManager.ConditionalAction`.

### 17. Dead Code: `rateLimitMaxCPPerWorld`

**File**: `internal/world/rate_limit.go:14, 24`

Constant set to 50, stored in the struct, but never checked anywhere. Incomplete feature or dead code.

### 18. `lastSubmit` Map Grows Unbounded

**File**: `internal/world/rate_limit.go:22, 64`

The `lastSubmit` map is only ever written to, never pruned. Every unique userID adds a permanent entry.

### 19. Hardcoded Event Type Strings

**Files**: `internal/server/events.go:201-287`, `internal/claude/claude.go:130-170`

Event types like `"chat.message"`, `"build.completed"`, `"claude.session_stopped"` are raw string literals with no compile-time contract between publishers and consumers.

### 20. No Periodic Session Cleanup

**File**: `main.go:51-54`

Expired sessions cleaned only at startup. Long-running server accumulates expired rows indefinitely.

### 21. First-User Admin Race Condition

**File**: `internal/auth/auth.go:152-177`

`CountUsers` + `UpsertUser` not atomic. Two simultaneous signups on empty DB could both get admin role.

---

## Low Severity

### 22. Silently Ignored DB Errors in Handlers

- `server.go:72` -- `ListWorlds` error discarded (empty lobby on failure)
- `server.go:226-227` -- `GetUserPosition` and `GetCheckpointTree` errors discarded
- `server.go:267` -- `SetUserPosition` error discarded
- `events.go:152` -- `GetRecentMessagesWithUser` error discarded (no chat history, no logging)

### 23. Missing Accessibility in Templates

- 4 form inputs without `<label>` or `aria-label`
- 2 iframes without `title` attribute
- Minimize button uses em-dash as only accessible name
- `pending.templ:12` uses unhelpful `alt="avatar"`

### 24. Missing Database Indexes

- `sessions.user_id` -- no index (full scan on `DeleteSessionsByUserID`)
- `sessions.expires_at` -- no index (full scan on `DeleteExpiredSessions`)
- `checkpoints.world_id` -- no index (full scan on `GetCheckpointTree`)
- `checkpoints(created_by, status)` -- no composite index (full scan on rate-limit check)
- `messages(world_id, created_at)` -- no composite index

### 25. `GetCheckpointAncestry` Potential Infinite Loop

**File**: `internal/db/db.go:96-112`

Walks parent chain with no cycle detection or max-depth guard. A data cycle causes infinite loop + OOM.

### 26. No `SetMaxOpenConns` for SQLite

**File**: `internal/db/db.go:25-58`

Default unlimited connections competing for SQLite's single-writer lock.

### 27. `sseLogErr` Skips Server-Side Logging

**File**: `internal/server/events.go:22-30`

Only logs server-side when `ConsoleError` fails (connection broken). Successful browser-forwarded errors have no server-side trace.

### 28. `updateMemory` Silently Ignores All Errors

**File**: `internal/claude/memory.go:12-18`

Both `ReadFile` and `WriteFile` errors discarded. Claude Code may run without prompt context.

### 29. Remaining Raw Signal Expressions in Templates

Some `data-show` bindings still use raw `$signal` strings instead of dsutil helpers:
- `overlay.templ:12,22,25,59`
- `checkpoint_tree.templ:9`
- `chat.templ:17,18`

These are simple reads (not assignments), so the inconsistency is minor.

### 30. `prompt_history` Table is Write-Only

**File**: `internal/db/queries/prompt_history.sql`

Only has `CreatePromptHistory`. No read queries exist. Data stored but never retrieved.

---

## Recommended Priority Order for Fixes

### Tier 1 (Security / Data Bugs)
1. Path traversal containment check (#1)
2. Fix missing `worldName` in build event (#4)
3. Orphaned game server cleanup (#2)
4. Protect `/api/claude-event` endpoint (#3)

### Tier 2 (Data Integrity)
5. Wrap CreateWorld/ForkCheckpoint in transactions (#5)
6. Filesystem cleanup on DB failure (#6)
7. Rate limiter fail-closed on DB error (#8)
8. Add ON DELETE CASCADE / proper cleanup (#7)

### Tier 3 (Operational Robustness)
9. Add tmux session watchdog/reaper (#10)
10. HandlePrompt error-path cleanup (#11)
11. Terminate SSE on write error (#12)
12. Add authorization to handleSaveCheckpoint (#9)
13. Add database indexes (#24)

### Tier 4 (Code Quality)
14. Remove unused dsutil exports (#16)
15. Remove unused `user` param from Overlay (#15)
16. Remove dead `rateLimitMaxCPPerWorld` (#17)
17. Add event type constants (#19)
18. Prune `lastSubmit` map (#18)

---

## Open Questions

- Is the `/api/claude-event` endpoint intentionally exposed to the network, or should it be localhost-only?
- Should we extend the UUID from 8 to 12+ hex characters, or switch to full UUIDs?
- Is `prompt_history` planned for future use, or should it be removed?
- Should we add a migration versioning system before adding indexes/cascades?
