# Harness Codebase Cleanup & Bugfix Implementation Plan

## Overview

Fix all ~30 issues identified in the [harness codebase audit](../../CoreyCole/research/2026-02-11-harness-cleanup-bugfixes-audit.md). Issues span security vulnerabilities, data integrity gaps, operational robustness, and code quality. Organized into 6 phases, ordered by severity and dependency.

## Current State Analysis

The harness is a working Go/Echo server with GitHub OAuth, SQLite persistence, Datastar-powered SSE UI, and a Claude Code orchestration pipeline. The codebase was recently refactored (Wave 4: templ + Datastar + dsutil), but the audit revealed ~30 issues ranging from path traversal vulnerabilities to unbounded maps.

### Key Discoveries
- Path params flow directly into `filepath.Join` with no containment check (`server.go:398,412`)
- `/api/claude-event` endpoint binds to `0.0.0.0:8080` with zero auth (`server.go:102`)
- Game server crash monitor goroutine does no cleanup (`game_server.go:139-153`)
- `CreateWorld` and `ForkCheckpoint` perform 3+ DB inserts without transactions (`manager.go:86-109, 196-219`)
- `build.completed` event omits `worldName` field (`claude.go:162-166`)
- DB has zero `ON DELETE CASCADE` clauses and only 1 index (`001_initial.sql`)
- DB wrapper exposes `BeginTx` pattern via sqlc's `WithTx` but it's never used

## What We're NOT Doing

- Switching from truncated UUIDs to full UUIDs (#14) — requires migration of all existing data
- Adding a migration versioning system — out of scope, single migration file works for now
- Building read queries for `prompt_history` (#30) — write-only table, future feature
- Adding `<label>`/`aria-label` accessibility fixes (#23) — separate UI pass
- Adding event bus logging/metrics for dropped messages (#13) — low severity, observability pass

---

## Phase 1: Security Hardening

### Overview
Fix path traversal, protect the claude-event endpoint, add auth to handleSaveCheckpoint.

### Changes Required:

#### 1a. Path traversal containment in `handleLogStream` and `handleWASMArtifacts`
**File**: `internal/server/server.go:393-418`

Add `strings` to imports. After computing `logPath`/`fullPath` via `filepath.Join`, validate the resolved path stays within the expected base directory:

```go
// handleLogStream — after line 398
func (s *Server) handleLogStream(c echo.Context) error {
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")
	logType := c.Param("logType")

	baseDir := filepath.Join(s.DataDir, "logs", "worlds")
	logPath := filepath.Join(baseDir, worldID, cpID, logType+".jsonl")
	if !strings.HasPrefix(filepath.Clean(logPath), filepath.Clean(baseDir)+string(os.PathSeparator)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid path")
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "log not found")
	}
	return c.File(logPath)
}

// handleWASMArtifacts — same pattern
func (s *Server) handleWASMArtifacts(c echo.Context) error {
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")
	filePath := c.Param("*")

	baseDir := filepath.Join(s.DataDir, "wasm-builds")
	fullPath := filepath.Join(baseDir, worldID, cpID, filePath)
	if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(baseDir)+string(os.PathSeparator)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid path")
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "artifact not found")
	}
	return c.File(fullPath)
}
```

#### 1b. Protect `/api/claude-event` endpoint
**File**: `internal/server/server.go:101-102, 422-445`

Add a shared secret check. The harness and Claude hook scripts share the same machine, so use an env var `CM_HOOK_SECRET`:

```go
// In RegisterRoutes, change line 102:
e.POST("/api/claude-event", s.handleClaudeEvent)

// To:
e.POST("/api/claude-event", s.handleClaudeEvent, hookSecretMiddleware())
```

Add a new middleware:

```go
// hookSecretMiddleware validates the X-Hook-Secret header against CM_HOOK_SECRET.
func hookSecretMiddleware() echo.MiddlewareFunc {
	secret := os.Getenv("CM_HOOK_SECRET")
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if secret != "" && c.Request().Header.Get("X-Hook-Secret") != secret {
				return echo.NewHTTPError(http.StatusForbidden, "invalid hook secret")
			}
			return next(c)
		}
	}
}
```

The hook scripts that POST to this endpoint also need updating to include the header. Check `scripts/` or `hooks/` for the curl/fetch calls and add `-H "X-Hook-Secret: $CM_HOOK_SECRET"`.

#### 1c. Add `requireUser` to `handleSaveCheckpoint`
**File**: `internal/server/server.go:362-390`

Add user extraction at the top of the handler (matches pattern in `handlePrompt`, `handleWorldView`, etc.):

```go
func (s *Server) handleSaveCheckpoint(c echo.Context) error {
	ctx := c.Request().Context()
	worldID := c.Param("worldID")

	_, err := requireUser(c)
	if err != nil {
		return err
	}
	// ... rest unchanged
```

### Success Criteria:

#### Automated Verification:
- [ ] Build succeeds: `cd harness && just generate && go build ./...`
- [ ] Lint passes: `cd harness && just lint`

#### Manual Verification:
- [ ] `/world/abc/checkpoint/def/logs/../../etc/passwd` returns 400 not file contents
- [ ] `curl -X POST localhost:8080/api/claude-event -d '{}'` returns 403 when `CM_HOOK_SECRET` is set
- [ ] Unauthenticated POST to `/world/x/checkpoint` returns 401

---

## Phase 2: Data Bug Fixes

### Overview
Fix the missing `worldName` in build events, orphaned game server cleanup, and rate limiter error handling.

### Changes Required:

#### 2a. Add `worldName` to `build.completed` event
**File**: `internal/claude/claude.go:162-166`

The `worldName` is already resolved at line 104 via `o.worldName(ctx, worldID)`. Add it to the event map:

```go
o.eventBus.Publish(worldID, map[string]any{
	"event":     "build.completed",
	"worldID":   worldID,
	"cpID":      cpID,
	"worldName": worldName, // worldName already available from line 104
})
```

#### 2b. Orphaned game server cleanup on crash
**File**: `internal/world/game_server.go:138-153`

The crash monitor goroutine must clean up the server entry, release the port, and reset the refcount:

```go
// Monitor for crashes.
go func() {
	if waitErr := cmd.Wait(); waitErr != nil {
		m.logger.Error(
			"game server crashed",
			"worldID", worldID,
			"cpID", cpID,
			"error", waitErr,
		)
	}

	_ = logFile.Close()

	// Clean up crashed server.
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.servers[key]; ok && existing == srv {
		m.ports.Release(srv.Port)
		delete(m.servers, key)
		delete(m.refCount, key)
		m.logger.Info("cleaned up crashed game server", "key", key, "port", srv.Port)
	}
}()
```

Note: The `key` variable needs to be captured in the goroutine closure. Currently `worldID` and `cpID` are already captured. Compute `key` before the goroutine: `key := worldID + "/" + cpID` (add it before the `go func()` at ~line 138, or capture via the existing variables).

Actually, `key` is not currently in scope at line 138. We need to pass it. Best approach: compute `key` inside `startServer` from the already-captured `worldID`/`cpID`:

```go
go func() {
	// ... existing Wait + logFile.Close ...

	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()
	// Only clean up if this is still the registered server (not replaced).
	if existing, ok := m.servers[key]; ok && existing == srv {
		m.ports.Release(srv.Port)
		delete(m.servers, key)
		delete(m.refCount, key)
		m.logger.Info("cleaned up crashed game server", "key", key, "port", srv.Port)
	}
}()
```

#### 2c. Rate limiter: fail-closed on DB error
**File**: `internal/world/rate_limit.go:53-62`

Change from fail-open to fail-closed:

```go
// Check for active builds.
activeBuilds, err := r.db.CountActiveBuilds(
	ctx,
	sql.NullString{String: userID, Valid: userID != ""},
)
if err != nil {
	return &RateLimitError{
		Message: "Unable to verify build status, please try again",
	}
}
if activeBuilds > 0 {
	return &RateLimitError{
		Message: "You already have a build in progress",
	}
}
```

### Success Criteria:

#### Automated Verification:
- [ ] Build succeeds: `cd harness && just generate && go build ./...`
- [ ] Lint passes: `cd harness && just lint`

#### Manual Verification:
- [ ] After build completion, `BuildReadyNotification` shows the world name instead of empty string
- [ ] After a game server crash (kill the process), subsequent `Connect()` calls start a fresh server

---

## Phase 3: Data Integrity

### Overview
Wrap multi-insert flows in transactions, add filesystem cleanup on DB failure, add ON DELETE CASCADE, and add missing indexes.

### Changes Required:

#### 3a. Expose `BeginTx` on the DB wrapper
**File**: `internal/db/db.go`

Add a `BeginTx` method and a way to get `Queries` scoped to a transaction:

```go
// BeginTx starts a new transaction.
func (d *DB) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, nil)
}

// WithTx returns a Queries instance scoped to the given transaction.
func (d *DB) WithTx(tx *sql.Tx) *sqlc.Queries {
	return d.Queries.WithTx(tx)
}
```

#### 3b. Wrap `CreateWorld` in a transaction + filesystem cleanup
**File**: `internal/world/manager.go:56-155`

After filesystem operations succeed, wrap DB inserts in a transaction. On DB failure, clean up the filesystem:

```go
func (m *Manager) CreateWorld(
	ctx context.Context,
	name, description, userID string,
) (*sqlc.World, error) {
	worldID := uuid.New().String()[:8]
	cpID := uuid.New().String()[:8]
	cpDir := filepath.Join(m.dataDir, "worlds", worldID, cpID)

	// Filesystem setup (same as before).
	if err := os.MkdirAll(cpDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating checkpoint directory: %w", err)
	}

	// rsync + cloneBuildCache (unchanged) ...

	// DB inserts in a transaction.
	tx, err := m.db.BeginTx(ctx)
	if err != nil {
		_ = os.RemoveAll(filepath.Join(m.dataDir, "worlds", worldID))
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			m.logger.Error("rollback failed", "error", err)
		}
	}()

	qtx := m.db.WithTx(tx)

	if err := qtx.CreateWorld(ctx, sqlc.CreateWorldParams{...}); err != nil {
		_ = os.RemoveAll(filepath.Join(m.dataDir, "worlds", worldID))
		return nil, fmt.Errorf("inserting world: %w", err)
	}

	if err := qtx.CreateCheckpoint(ctx, sqlc.CreateCheckpointParams{...}); err != nil {
		_ = os.RemoveAll(filepath.Join(m.dataDir, "worlds", worldID))
		return nil, fmt.Errorf("inserting root checkpoint: %w", err)
	}

	if err := qtx.SetUserPosition(ctx, sqlc.SetUserPositionParams{...}); err != nil {
		m.logger.Error("failed to set initial user position", "error", err)
	}

	if err := tx.Commit(); err != nil {
		_ = os.RemoveAll(filepath.Join(m.dataDir, "worlds", worldID))
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	// Background build (unchanged) ...
}
```

#### 3c. Wrap `ForkCheckpoint` in a transaction + filesystem cleanup
**File**: `internal/world/manager.go:159-227`

Same pattern: wrap `CreateCheckpoint`, `CreatePromptHistory`, `SetUserPosition` in a transaction. On failure, `os.RemoveAll(newDir)`.

#### 3d. Add ON DELETE CASCADE to migration
**File**: `internal/db/migrations/001_initial.sql`

Since we use `CREATE TABLE IF NOT EXISTS` and SQLite doesn't support `ALTER TABLE ... ADD CONSTRAINT`, we need a new migration file `002_add_cascades_and_indexes.sql`. This migration drops and recreates the tables that need CASCADE — but only if data can be rebuilt. For a safer approach, we can add a cleanup sequence to `HandleRejectUser` instead.

**Pragmatic approach**: Add a new migration `002_cascades_indexes.sql` that:
1. Adds missing indexes (safe, additive)
2. For CASCADE, enhance the `HandleRejectUser` handler to delete dependent records in order instead of relying on CASCADE (avoids risky table rebuilds)

```sql
-- 002_cascades_indexes.sql
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_checkpoints_world_id ON checkpoints(world_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_created_by_status ON checkpoints(created_by, status);
CREATE INDEX IF NOT EXISTS idx_messages_world_id_created_at ON messages(world_id, created_at);
CREATE INDEX IF NOT EXISTS idx_user_positions_world_id ON user_positions(world_id);
CREATE INDEX IF NOT EXISTS idx_prompt_history_checkpoint_id ON prompt_history(checkpoint_id);
```

Update `db.go:runMigrations` to also execute this file.

#### 3e. Fix `HandleRejectUser` cleanup sequence
**File**: `internal/auth/auth.go:258-272`

Delete dependent records before deleting the user:

```go
func (h *Handler) HandleRejectUser(c echo.Context) error {
	ctx := c.Request().Context()
	userID := c.Param("userID")

	// Delete all dependent records before the user.
	_ = h.db.DeleteSessionsByUserID(ctx, userID)
	_ = h.db.DeleteUserPositionsByUserID(ctx, userID)
	_ = h.db.DeletePromptHistoryByUserID(ctx, userID)
	_ = h.db.DeleteMessagesByUserID(ctx, userID)
	// Note: don't delete worlds/checkpoints — they may be shared.

	if err := h.db.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	h.logger.Info("user rejected", "userID", userID)
	return c.JSON(http.StatusOK, map[string]string{"status": "rejected"})
}
```

This requires adding new sqlc queries: `DeleteUserPositionsByUserID`, `DeletePromptHistoryByUserID`, `DeleteMessagesByUserID`.

#### 3f. Add `GetCheckpointAncestry` cycle detection
**File**: `internal/db/db.go:95-112`

Add a visited set:

```go
visited := make(map[string]bool, len(cpMap))
var ancestry []sqlc.Checkpoint
currentID := cpID
for currentID != "" {
	if visited[currentID] {
		return nil, fmt.Errorf("cycle detected at checkpoint %s in world %s", currentID, worldID)
	}
	visited[currentID] = true
	cp, ok := cpMap[currentID]
	if !ok {
		return nil, fmt.Errorf("checkpoint %s not found in world %s", currentID, worldID)
	}
	ancestry = append(ancestry, cp)
	if cp.ParentCheckpointID.Valid {
		currentID = cp.ParentCheckpointID.String
	} else {
		currentID = ""
	}
}
```

### Success Criteria:

#### Automated Verification:
- [ ] Build succeeds: `cd harness && just generate && go build ./...`
- [ ] Lint passes: `cd harness && just lint`
- [ ] New migration applies cleanly (server startup)

#### Manual Verification:
- [ ] Create a world, verify all 3 DB records exist atomically
- [ ] Kill the DB mid-insert (simulate), verify no orphaned directories

---

## Phase 4: Operational Robustness

### Overview
Add HandlePrompt error-path cleanup, improve SSE error handling, add periodic session cleanup, set SQLite max connections.

### Changes Required:

#### 4a. HandlePrompt error-path cleanup
**File**: `internal/claude/claude.go:62-90`

If `session.Create` or `session.SendPrompt` fails after `ForkCheckpoint`, mark the checkpoint as failed and kill the tmux session:

```go
func (o *Orchestrator) HandlePrompt(
	ctx context.Context,
	worldID, sourceCPID, prompt, userID string,
) (*sqlc.Checkpoint, error) {
	cp, err := o.worldManager.ForkCheckpoint(ctx, worldID, sourceCPID, prompt, userID)
	if err != nil {
		return nil, err
	}

	updateMemory(cp.DirPath, prompt)

	session := tmux.NewSession(worldID, cp.ID, cp.DirPath)
	if err := session.Create(ctx, worldID, cp.ID, o.logsDir, o.harnessURL); err != nil {
		o.markCheckpointFailed(ctx, cp.ID, "tmux session creation failed")
		return nil, fmt.Errorf("creating tmux session: %w", err)
	}

	if err := session.SendPrompt(ctx, prompt); err != nil {
		session.Kill() // Clean up the tmux session
		o.markCheckpointFailed(ctx, cp.ID, "sending prompt failed")
		return nil, fmt.Errorf("sending prompt: %w", err)
	}

	// ... rest unchanged
}

// markCheckpointFailed updates a checkpoint to "failed" status.
func (o *Orchestrator) markCheckpointFailed(ctx context.Context, cpID, reason string) {
	_, _ = o.db.UpdateCheckpointStatus(ctx, sqlc.UpdateCheckpointStatusParams{
		Status:   "failed",
		BuildLog: sql.NullString{String: reason, Valid: true},
		ID:       cpID,
	})
}
```

This requires a `Kill()` method on the tmux session. Check if it exists; if not, add it.

#### 4b. Periodic session cleanup
**File**: `main.go:51-54`

Add a background ticker:

```go
// Clean up expired sessions on startup.
if cleanErr := database.DeleteExpiredSessions(context.Background()); cleanErr != nil {
	logger.Error("failed to clean expired sessions", "error", cleanErr)
}

// Periodically clean expired sessions.
go func() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := database.DeleteExpiredSessions(context.Background()); err != nil {
				logger.Error("periodic session cleanup failed", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}()
```

Note: The `ctx` from `signal.NotifyContext` (line 108) needs to be created before this goroutine, so move the signal context setup above this block.

#### 4c. Set SQLite MaxOpenConns
**File**: `internal/db/db.go:25-58`

After opening the connection, configure the pool for SQLite:

```go
sqlDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
if err != nil {
	return nil, fmt.Errorf("opening database: %w", err)
}

// SQLite performs best with a single writer connection.
sqlDB.SetMaxOpenConns(1)
```

#### 4d. Log silent DB errors in handlers
**Files**: `internal/server/server.go:72,226-227,267` and `internal/server/events.go:152`

Change `_ =` assignments to log on error:

```go
// server.go:72
worlds, err := s.DB.ListWorlds(ctx)
if err != nil {
	s.Logger.Error("failed to list worlds", "error", err)
}

// server.go:226-227
cpID, err := s.WorldManager.GetUserPosition(ctx, user.ID, worldID)
if err != nil {
	s.Logger.Warn("failed to get user position", "error", err)
}
checkpoints, err := s.WorldManager.GetCheckpointTree(ctx, worldID)
if err != nil {
	s.Logger.Warn("failed to get checkpoint tree", "error", err)
}

// server.go:267
if err := s.WorldManager.SetUserPosition(ctx, user.ID, worldID, cpID); err != nil {
	s.Logger.Warn("failed to set user position", "error", err)
}

// events.go:152
recentMsgs, err := s.DB.GetRecentMessagesWithUser(ctx, recentMessageLimit)
if err != nil {
	slog.Error("failed to load chat history", "error", err)
}
```

#### 4e. `updateMemory` error logging
**File**: `internal/claude/memory.go:12-18`

Add error logging (not return — keep the function void):

```go
func updateMemory(checkpointDir, prompt string) {
	memoryPath := filepath.Join(checkpointDir, "MEMORY.md")
	content, err := os.ReadFile(memoryPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to read MEMORY.md", "path", memoryPath, "error", err)
	}

	addition := fmt.Sprintf("\n\n## Latest Prompt\n%s\n", prompt)
	if err := os.WriteFile(memoryPath, append(content, []byte(addition)...), 0o600); err != nil {
		slog.Error("failed to write MEMORY.md", "path", memoryPath, "error", err)
	}
}
```

Add `"log/slog"` to imports.

### Success Criteria:

#### Automated Verification:
- [ ] Build succeeds: `cd harness && just generate && go build ./...`
- [ ] Lint passes: `cd harness && just lint`

#### Manual Verification:
- [ ] On `SendPrompt` failure, checkpoint shows "failed" status (not stuck "building")
- [ ] After 1+ hours, expired sessions are cleaned from the DB

---

## Phase 5: Hardening (SSE, EventBus, Auth)

### Overview
SSE write error propagation, event bus logging, first-user admin race condition.

### Changes Required:

#### 5a. SSE: return errors from event handlers
**File**: `internal/server/events.go`

Change `ssePatchChat` and `ssePatchSignals` to return errors, and propagate in the main loop:

```go
func ssePatchChat(...) error {
	return sse.PatchElementTempl(component,
		datastar.WithSelectorID("chat-log"),
		datastar.WithModeAppend(),
	)
}

func ssePatchSignals(...) error {
	return sse.MarshalAndPatchSignals(signals)
}
```

Update `handleGlobalEvent` and `handleWorldEvent` to return `error`. In the main SSE loop:

```go
for {
	select {
	case event := <-globalCh:
		if err := s.handleGlobalEvent(sse, event); err != nil {
			return nil // Connection broken
		}
	case event := <-worldCh:
		if err := s.handleWorldEvent(sse, event); err != nil {
			return nil // Connection broken
		}
	// ... heartbeat and ctx.Done unchanged
	}
}
```

This is a moderate refactor — all the switch branches in `handleGlobalEvent`/`handleWorldEvent` need to propagate errors.

#### 5b. First-user admin race condition
**File**: `internal/auth/auth.go:152-177`

Wrap the count + insert in a transaction using `BEGIN IMMEDIATE` (SQLite write lock):

```go
// New user: determine role.
role := "pending"

tx, txErr := h.db.BeginTx(ctx)
if txErr != nil {
	return fmt.Errorf("begin tx: %w", txErr)
}
defer func() { _ = tx.Rollback() }()

qtx := h.db.WithTx(tx)
count, countErr := qtx.CountUsers(ctx)
if countErr != nil {
	return fmt.Errorf("counting users: %w", countErr)
}
if count == 0 {
	role = "admin"
}

userID = uuid.New().String()
if upsertErr := qtx.UpsertUser(ctx, sqlc.UpsertUserParams{...}); upsertErr != nil {
	return fmt.Errorf("creating user: %w", upsertErr)
}
if err := tx.Commit(); err != nil {
	return fmt.Errorf("commit: %w", err)
}
```

Note: This requires `BeginTx`/`WithTx` from Phase 3a. Phase 5 depends on Phase 3.

### Success Criteria:

#### Automated Verification:
- [ ] Build succeeds: `cd harness && just generate && go build ./...`
- [ ] Lint passes: `cd harness && just lint`

---

## Phase 6: Code Quality Cleanup

### Overview
Remove dead code, unused parameters, add event type constants, prune unbounded map.

### Changes Required:

#### 6a. Remove dead `rateLimitMaxCPPerWorld`
**File**: `internal/world/rate_limit.go:14,24,33`

- Remove the constant `rateLimitMaxCPPerWorld` (line 14)
- Remove the `maxCPPerWorld` field from `RateLimiter` struct (line 24)
- Remove `maxCPPerWorld: rateLimitMaxCPPerWorld` from `NewRateLimiter` (line 33)

#### 6b. Prune `lastSubmit` map
**File**: `internal/world/rate_limit.go`

Add cleanup of stale entries during `Check()`:

```go
func (r *RateLimiter) Check(ctx context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Prune stale entries (older than 2x cooldown).
	cutoff := time.Now().Add(-2 * r.cooldown)
	for uid, ts := range r.lastSubmit {
		if ts.Before(cutoff) {
			delete(r.lastSubmit, uid)
		}
	}

	// ... rest of cooldown check + active builds check
}
```

#### 6c. Remove unused `user` param from `Overlay` template
**Files**: `views/world/overlay.templ:10`, `views/world/world.templ` (call site)

Change the `Overlay` signature:

```go
// overlay.templ
templ Overlay(w sqlc.World, cp sqlc.Checkpoint, checkpoints []sqlc.Checkpoint) {
```

Update the call site in `world.templ` to remove the `user` argument.

Also remove the `"creative-mode/harness/internal/db/sqlc"` import from `overlay.templ` if `sqlc` is no longer used after removing the user param (check if `sqlc.World`, `sqlc.Checkpoint` are still used — yes they are, so keep the import).

#### 6d. Remove unused dsutil `DataClass` type
**File**: `views/dsutil/data_class.go`

If `NewDataClass`, `DataClass.Add`, `DataClass.Build` are truly unused (the `SignalManager.DataClass()` method in `signals.go` has its own implementation), delete the entire `data_class.go` file.

Verify by grepping for `NewDataClass` and `DataClass{` usage first.

#### 6e. Add event type constants
**File**: New file `internal/events/types.go`

```go
package events

const (
	EventChatMessage       = "chat.message"
	EventPlayerJoined      = "player.joined"
	EventPlayerLeft        = "player.left"
	EventBuildStarted      = "build.started"
	EventBuildCompleted    = "build.completed"
	EventBuildFailed       = "build.failed"
	EventClaudeToolUsePre  = "claude.tool_use.pre"
	EventClaudeSessionStop = "claude.session_stopped"
	EventClaudeRateLimited = "claude.rate_limited"
)
```

Replace string literals in `internal/server/events.go`, `internal/claude/claude.go`, and `internal/server/server.go` with these constants.

#### 6f. `sseLogErr` — add server-side logging
**File**: `internal/server/events.go:22-30`

Always log server-side, not just when ConsoleError fails:

```go
func sseLogErr(
	sse *datastar.ServerSentEventGenerator,
	err error,
	msg string,
) {
	slog.Warn(msg, "err", err)
	_ = sse.ConsoleError(err)
}
```

### Success Criteria:

#### Automated Verification:
- [ ] Build succeeds: `cd harness && just generate && go build ./...`
- [ ] Lint passes: `cd harness && just lint`
- [ ] No unused code flagged by linter

---

## Verification (End-to-End)

After all phases:

```bash
cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint
```

Manual smoke test:
1. Start the server, log in via GitHub OAuth
2. Create a world — verify lobby shows it
3. Submit a prompt — verify build pipeline works end-to-end
4. Open browser console — verify no SSE errors
5. Check server logs — verify DB errors are now logged

## References

- Audit document: `thoughts/CoreyCole/research/2026-02-11-harness-cleanup-bugfixes-audit.md`
- Key files modified:
  - `internal/server/server.go` (Phases 1, 4)
  - `internal/server/events.go` (Phases 5, 6)
  - `internal/world/game_server.go` (Phase 2)
  - `internal/world/manager.go` (Phase 3)
  - `internal/world/rate_limit.go` (Phases 2, 6)
  - `internal/claude/claude.go` (Phases 2, 4)
  - `internal/claude/memory.go` (Phase 4)
  - `internal/db/db.go` (Phases 3, 4)
  - `internal/db/migrations/` (Phase 3)
  - `internal/auth/auth.go` (Phases 3, 5)
  - `main.go` (Phase 4)
  - `views/world/overlay.templ` (Phase 6)
  - `views/dsutil/data_class.go` (Phase 6)
