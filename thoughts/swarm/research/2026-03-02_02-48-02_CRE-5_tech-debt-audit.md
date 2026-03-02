---
ticket: CRE-5
workflow: 124c06f6
session: a100526a
timestamp: 2026-03-02T02:48:02Z
---

# Research: Tech Debt Audit — Identify and Consolidate Duplicated Patterns, Improve Observability

## Questions

1. What duplicated code patterns exist across harness, templates, and shared packages?
2. What inconsistent conventions exist (naming, error handling, logging)?
3. Where is observability missing (logging gaps, metrics, health checks)?
4. What maintainability improvements are needed (unclear abstractions, over-complexity)?

## Findings

### 1. Duplicated Code Patterns

#### 1.1 Swarm Manager Nil Guard (18 occurrences)

The same 4-line guard block is copy-pasted into every swarm API handler, dashboard handler, and hook handler:

```go
if s.SwarmManager == nil {
    return echo.NewHTTPError(http.StatusServiceUnavailable, "swarm manager not configured")
}
```

**Files**: `swarm_api.go` (9x), `swarm_dashboard.go` (3x), `swarm_hooks.go` (6x)

**Fix**: Extract to Echo middleware on the swarm route groups.

#### 1.2 Template Cross-Duplication (Rust — 2D & Boardgame)

Near-identical files across `templates/2d/` and `templates/boardgame/`:

| File | Similarity | Key Differences |
|------|-----------|----------------|
| `src/bridge.rs` | ~98% identical | One `#[allow(dead_code)]` annotation |
| `src/debug.rs` | ~80% identical | Only domain-specific query variants differ |
| `src/camera.rs` | ~60% identical | 2D has touch/zoom/pan; boardgame is simpler |
| `src/lib.rs` | ~70% identical | Same Bevy app bootstrap, different plugins |
| `index.html` | ~90% identical | Different title/binary name, 2D has `reload-room` handler |

**Fix**: Extract a shared Bevy utility crate (`templates/shared/`) with `bridge`, `debug`, and `camera` modules. Templates import and extend.

#### 1.3 Path Sanitization Pattern (4 occurrences)

```go
fullPath := filepath.Clean(filepath.Join(baseDir, ...))
if !strings.HasPrefix(fullPath, filepath.Clean(baseDir)+string(os.PathSeparator)) { ... }
```

**Files**: `server.go:657,674`, `mayor_dashboard.go:135,177`

**Fix**: Extract `func safePath(baseDir, relPath string) (string, error)` utility.

#### 1.4 OpenClaw CLI Patterns (Mayor + President)

Duplicated between `mayor/openclaw.go` and `president/president.go`:
- `BindAgentToDiscord` / `bindToChannel` — identical config get/set/append flow
- Agent creation via CLI — same `exec.CommandContext` pattern
- Workspace file writing loop — same `map[string]string` + `os.WriteFile` pattern
- `openclawCLITimeout = 30 * time.Second` — defined independently in both

**Fix**: Extract `pkg/openclaw/` with `Client` struct wrapping the CLI. Both mayor and president import it.

#### 1.5 Duplicated `mimeToExt` Functions (3 implementations)

| Location | Name | Handles GIF? |
|----------|------|-------------|
| `mayor/mayor.go:217` | `mimeToExt` | No |
| `pkg/mayorchat/cover.go:43` | `MimeToExt` | Yes |
| `server/imagegen.go:311` | `extensionForMIME` | No |

**Fix**: Consolidate to `MimeToExt` in `pkg/mayorchat` (or a new `pkg/media` package) and remove the others.

#### 1.6 OPENCLAW_HOME Resolution (3x in one file)

```go
openclawHome := os.Getenv("OPENCLAW_HOME")
if openclawHome == "" { openclawHome = filepath.Join(s.DataDir, "openclaw") }
```

Repeated at `mayor_dashboard.go:45,129,171`.

**Fix**: Resolve once in `Server` struct or at route registration.

#### 1.7 Short UUID Generation (`uuid.New().String()[:8]`)

17+ occurrences across 10+ files.

**Fix**: Extract `func shortID() string` in a `pkg/id` or utility package.

#### 1.8 Cover Art Save-to-Disk Pattern

Duplicated between `mayor/mayor.go:107-131` and `server/create.go:775-796` — same `MkdirAll` + `WriteFile` + `UpdateWorldCoverImage` sequence.

**Fix**: Extract to a shared `saveCoverImage(db, dataDir, worldID, data, mime)` function.

#### 1.9 President Tmux Spawn Pattern (3x)

Three president API handlers (`handlePresidentRepoBuild`, `handlePresidentTemplateUpdate`, `handlePresidentDeploy`) repeat the same repo root resolution + tmux session creation + JSON response pattern.

**Fix**: Extract `func spawnPresidentSession(name, command string) (string, error)`.

#### 1.10 Game Server Status Map Building (3 occurrences)

Same `GameServers.GetServer` → `map[string]any{"running": true, "port": ...}` in `mayor_api.go:210`, `server.go:887`, `president_api.go:78`.

**Fix**: Add `GameServer.StatusMap() map[string]any` method.

---

### 2. Inconsistent Conventions

#### 2.1 Logging

| Pattern | Location | Issue |
|---------|----------|-------|
| `slog.Warn(msg, "err", err)` | `events.go:31` | Uses global `slog` + wrong key `"err"` |
| `slog.Error(...)` | `events.go:160` | Uses global `slog` instead of `s.Logger` |
| `log.Fatalf(...)` | `main.go:305,314` | After structured logger is available |

**Convention**: All code should use the injected `*slog.Logger` with key `"error"` (not `"err"`).

#### 2.2 Error Handling in HTTP Handlers

Three patterns coexist:

| Pattern | Example | Issue |
|---------|---------|-------|
| Opaque `echo.NewHTTPError` | `server.go:377` | Correct — hides internals |
| `echo.NewHTTPError(..., err.Error())` | `swarm_api.go:54,130` | Leaks internal error details |
| Bare `fmt.Errorf` returned | `auth.go:63,138,283,311,455` | Echo auto-converts to 500, leaks details |

**Convention**: Always use `echo.NewHTTPError` with user-safe messages. Log `err` separately.

#### 2.3 HTTP Response Status Codes

`handleSwarmStart` (async workflow creation) returns `200 OK`, while `handleMayorBuild` (async build) returns `202 Accepted` for semantically identical operations.

**Convention**: Async operations → `202 Accepted`. Synchronous → `200 OK`.

#### 2.4 Response Body Shape

No standard envelope. Responses vary between:
- `map[string]string{"status": "ok"}`
- `map[string]string{"workflow_id": ..., "status": "running"}`
- Raw sqlc structs
- `map[string]any{...}` with many fields

**Convention**: Define a standard `APIResponse` struct or at minimum always include `"status"` key.

#### 2.5 Environment Variable Loading

Four patterns coexist:
1. Centralized in `main.go` (most vars) — **correct**
2. At closure/middleware creation time — **acceptable**
3. Per-request via `os.Getenv` — `mayor_dashboard.go` (3x), `dev.go:108` — **problematic**
4. Internal packages calling `os.Getenv` directly — `swarmorch`, `tmux` — **reduces testability**

**Convention**: Pass env vars via constructors. Per-request `os.Getenv` is a code smell.

#### 2.6 Naming Inconsistencies

- `mimeToExt` vs `extensionForMIME` vs `MimeToExt` — three names for the same function
- `sanitizeName` vs `slugify` — similar functions with different limits (48 vs 40 chars)
- `Server.DB` (exported) vs `manager.db` (unexported) — only `Server` exports the field
- `toNullString()` helper exists only in `swarmorch`, inline `sql.NullString{}` everywhere else

#### 2.7 Timestamp Formats

| Format | Usage |
|--------|-------|
| `time.RFC3339` | Events, build logs, API responses |
| `"15:04"` | Chat message EventBus `"ts"` |
| `"Jan 2, 3:04 PM"` | Asset file timestamps |
| `"2006-01-02 15:04:05"` | SQLite datetime parsing (hardcoded string, no constant) |
| `"2006-01-02"` | Digest dates |

---

### 3. Missing Observability

#### 3.1 Critical Gaps

| Gap | Location | Impact |
|-----|----------|--------|
| **Health endpoint checks nothing** | `server.go:321-323` | Returns `{"status":"ok"}` unconditionally — no DB, disk, tmux, or gateway checks |
| **No request IDs** | Entire codebase | Cannot correlate logs under concurrent load |
| **EventBus drops events silently** | `bus.go:56-62` | Slow SSE clients miss updates with no record |
| **Hook decode failures silent** | `swarm_hooks.go:66,161,199,234` | Malformed hook data → zero-value fields, no error log |
| **No disk space monitoring** | N/A | WASM builds (~5GB each) can fill VPS disk undetected |

#### 3.2 Request Logging

The Echo request logger (`server.go:111-122`) only captures URI and status. Missing: HTTP method, latency, remote IP, response size, errors. All supported by Echo's `RequestLoggerConfig`.

#### 3.3 No Metrics System

Zero Prometheus/OpenTelemetry instrumentation anywhere. The swarm has SQL-query-based snapshot metrics (60s cache) but no time-series data, no percentiles, no rate metrics.

Missing metrics across all subsystems:
- HTTP: request rate, error rate, latency percentiles
- Builds: success/failure rate, duration histograms, queue depth, WASM artifact sizes
- Game servers: active count, port utilization, orphan reaps
- EventBus: subscriber count, dropped events, throughput
- Auth: login rate, active sessions, OAuth failures
- Swarm: token usage aggregation, budget tracking, registry size

#### 3.4 Error Swallowing

Pervasive pattern of discarding errors from secondary operations:

| Location | What's lost |
|----------|-------------|
| `builder.go:179-193` | PostBuild DB summary updates |
| `builder.go:277-279` | JSONL log write failures |
| `claude.go:174,201-209` | Checkpoint status DB updates |
| `manager.go:894-920` | All learning capture results |
| `manager.go:1276` | Session JSONL writes |
| `auth/middleware.go:49` | `UpdateLastSeen` failures |
| `swarm_hooks.go` (4x) | JSON decode errors |
| `mayor_dashboard.go:31-41` | Five consecutive `_, _ :=` DB query results |

**Fix**: At minimum, log errors from all discarded operations. Group "best-effort" calls through a helper that logs on failure.

#### 3.5 Subsystem Health Not Aggregated

- `/health` — trivial (always OK)
- `/api/swarm/health` — sophisticated but swarm-only
- Game server health is tmux-only (`has-session`), not process-level
- Discord gateway health not monitored
- No composite health endpoint

#### 3.6 No Alerting for Non-Swarm Subsystems

The swarm has Discord alerts for failures, stalls, and retries (`swarmorch/alerts.go`). No other subsystem has alerting for build failures, game server crashes, auth failures, or DB errors.

#### 3.7 Swarm-Specific Gaps

- Token usage not in metrics or alerts — no budget monitoring
- Phase duration tracks session time, not wall-clock time including retry backoffs
- Completion/Start registries have no leak detection for abandoned channels
- Stall detection is passive (45min threshold, 1hr alert dedup)
- Context pressure has no aggregate visibility

---

### 4. Maintainability Issues

#### 4.1 Oversized Types

| Type | File | Lines | Methods | Issue |
|------|------|-------|---------|-------|
| `server.Server` | `server.go` | 14 fields | 40+ handlers | God struct — convergence point for all subsystems |
| `swarmorch.Manager` | `manager.go` | 1822 lines | 30+ methods | Monolith — workflow, session, state machine, Linear, tmux, JSONL all in one |
| `advanceWorkflow` | `manager.go:645-882` | 237 lines | — | 7 outcomes, 6 repeated side-effect triples |

#### 4.2 Dual Execution Path (Temporal vs Goroutine)

`if m.temporalRuntime != nil` appears 6 times in `swarmorch`:
- `StartWorkflow` (188), `advanceWorkflow` (857), `advanceFromGate` (1623), `RejectGate` (1545), `StartMaintenance` (1685), `RecoverWorkflows` (249)

No abstraction over the scheduling mechanism — each call site duplicates the branch.

#### 4.3 Untyped Checkpoint Statuses

Checkpoint statuses are bare strings (`"building"`, `"ready"`, `"failed"`) scattered across `world/`, `claude/`, `server/`. Compare with swarm's typed enums (`swarm.Phase`, `swarm.WorkflowStatus`). No compile-time typo protection.

#### 4.4 Template Type Strings Untyped

`"3d"`, `"2d"`, `"boardgame"` appear in 5+ files with no constants or validation type.

#### 4.5 Dead Code

| Item | Location | Notes |
|------|----------|-------|
| `mayor.Manager.DeleteAgent()` | `openclaw.go:130-148` | Never called |
| `mayor.Manager.BindAgentToDiscord()` | `openclaw.go:75-127` | Never called in production |
| `swarmorch.SessionName()` | `manager.go:1800-1803` | Only used in tests; production uses inline `Sprintf` |
| `HeartbeatWorkflow` | `workflows.go:53-119` | Superseded by `LeadFDEWorkflow`, registered but never scheduled |
| `SpawnRequest` vs `SessionParams` | `workflows.go:29-49` | Identical fields; line 94 does direct type conversion |

#### 4.6 Test Coverage

**Tested**: `swarm/`, `swarmorch/`, `linear/`, `graphite/` — solid coverage

**Zero tests**: `server/`, `auth/`, `claude/`, `world/`, `mayor/`, `president/`, `discord/`, `builder/`, `events/`, `db/` — all critical packages

Notable untested code: `db.GetCheckpointAncestry` (cycle detection logic, 48 lines), all HTTP handlers, auth middleware, EventBus concurrency.

**Test schema drift**: `swarmorch/manager_test.go` maintains a 123-line hand-written schema copy that must be manually synced with migrations.

#### 4.7 `main.go` Wiring Complexity

533-line `main()` with 15+ component wiring, 20+ env vars, setter injection (`SetAlertManager`, `SetLinearClient`, etc.), and conditional initialization. No builder pattern or DI container.

#### 4.8 Raw SQL Bypassing sqlc

`swarmorch/metrics.go:184-224` and `swarmorch/health.go:144-226` bypass sqlc with raw `*sql.DB` queries. Schema changes won't be caught at compile time. Uses `fmt.Sprintf` for query construction (has `//nolint:gosec`).

---

## Architecture Notes

- **Clear domain/orchestration split**: `internal/swarm/` (pure domain types, enums, state machine) vs `internal/swarmorch/` (manager, hooks, metrics) is a good pattern. The rest of the codebase (world, claude, mayor) doesn't have this separation.
- **EventBus is the primary real-time channel**: All SSE data flows through `events.EventBus`. Its reliability is critical but unmonitored.
- **SQLite is the single data store**: No external caching, no Redis, no message queue. This simplifies operations but means the DB is a single point of failure for all subsystems.
- **Templates are intentionally independent**: Each template is a standalone Bevy project. Cross-template code sharing was explicitly avoided for flexibility, but the 2D/boardgame convergence suggests a shared crate would reduce maintenance.

## Risks and Considerations

1. **Health endpoint blindness**: A load balancer checking `/health` would never take the service out of rotation, even with a locked database, full disk, or dead Discord connection.
2. **Error swallowing masks systematic failures**: If the SQLite connection degrades, dozens of "best-effort" operations silently fail with no aggregate signal.
3. **No request tracing makes production debugging expensive**: Under concurrent load, interleaved log lines cannot be correlated to specific requests.
4. **Test coverage gap in auth/server is a security risk**: Auth middleware, session validation, and role checks have zero automated testing.
5. **`manager.go` monolith risk**: At 1822 lines with 30+ methods, this file is the highest merge conflict risk and hardest to reason about for new contributors.
6. **Template drift**: As 2D and boardgame diverge, maintaining near-identical copies of bridge/debug/camera code becomes increasingly error-prone.

## Recommendations

### High Priority (consolidation + safety)

1. **Extract swarm nil guard to middleware** — 18 occurrences → 1 middleware. Immediate win.
2. **Standardize error handling in HTTP handlers** — Audit and fix `err.Error()` leaks and bare `fmt.Errorf` returns in auth handlers.
3. **Enrich health endpoint** — Check DB, disk space, tmux availability, game server count. Compose with existing swarm health.
4. **Add request IDs** — Echo middleware to generate and log `X-Request-Id` on every request.
5. **Enrich request logger** — Enable `LogMethod`, `LogLatency`, `LogRemoteIP`, `LogResponseSize` in Echo config.

### Medium Priority (observability + maintainability)

6. **Extract `pkg/openclaw/` client** — Consolidate mayor/president CLI duplication.
7. **Extract shared Bevy crate** — `templates/shared/` for bridge, debug, camera modules.
8. **Type checkpoint statuses** — Add `type CheckpointStatus string` constants.
9. **Type template types** — Add `type TemplateType string` constants.
10. **Log all discarded errors** — Replace `_ = operation()` with `if err := operation(); err != nil { logger.Warn(...) }` for all best-effort calls.
11. **Extract `shortID()` utility** — Replace 17+ `uuid.New().String()[:8]` calls.
12. **Consolidate `mimeToExt`** — Single function in shared package.

### Lower Priority (structural improvements)

13. **Split `swarmorch.Manager`** — Extract session lifecycle, Linear integration, and JSONL logging into separate types.
14. **Abstract Temporal vs goroutine scheduling** — Single interface to eliminate 6 branch points.
15. **Add HTTP handler tests** — Start with auth middleware and swarm API handlers.
16. **Remove dead code** — `DeleteAgent`, unused `BindAgentToDiscord`, `HeartbeatWorkflow`, duplicate `SpawnRequest`/`SessionParams`.
17. **Consolidate path sanitization** — Single `safePath()` utility.
18. **Resolve OPENCLAW_HOME once** — Store on Server struct, not per-request.
