---
date: 2026-02-11T23:00:16-08:00
researcher: CoreyCole
git_commit: 7081ecaa867759b6267e132b6c9776ec8095f518
branch: main
repository: creative-mode
topic: "Docker Ports + Auto-Reload Game on Build Completion"
tags: [implementation, docker, game-server, sse, auto-reload]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Docker Ports + Auto-Reload + Old Server Cleanup

## Task(s)

### Completed: Code Implementation (all 5 files)
All code changes are implemented and pass `just generate && go build ./... && just lint`:

1. **Docker port exposure** - `harness/docker-compose.yml` and `harness/Dockerfile` now expose ports 9001-9100
2. **Server port in build.completed event** - `harness/internal/claude/claude.go` includes `serverPort` in the event payload
3. **Auto-reload iframe via ExecuteScript** - `harness/internal/server/events.go` injects JS to set `#game-frame` src on build completion
4. **Stop old game servers** - `harness/internal/world/game_server.go` has new `StopByWorldExcept` method, called before `Connect` in claude.go

### Work In Progress: E2E Testing
Testing was attempted but blocked by the Docker development environment not having Rust/Cargo toolchain. The existing regression test world has stale checkpoint data (DB references `root0001` dir that doesn't exist on disk).

## Critical References
- Plan transcript: `/Users/coreycole/.claude/projects/-Users-coreycole-cdev-creative-mode/262b2f67-135c-48e9-8081-832aaea2c661.jsonl`
- Harness CLAUDE.md with Datastar/SSE patterns: `harness/CLAUDE.md`

## Recent changes

- `harness/docker-compose.yml:8` — added `"9001-9100:9001-9100"` port mapping
- `harness/Dockerfile:16` — added `EXPOSE 9001-9100`
- `harness/internal/world/game_server.go:212-228` — new `StopByWorldExcept(worldID, keepCPID string)` method that iterates servers map, kills all matching worldID except keepCPID
- `harness/internal/claude/claude.go:145-146` — calls `StopByWorldExcept` before `GameServers.Connect()`
- `harness/internal/claude/claude.go:165-170` — extracts `srv.Port` into `serverPort` variable, includes it in `build.completed` event payload
- `harness/internal/server/events.go:2` — added `"fmt"` import
- `harness/internal/server/events.go:257-294` — updated `EventBuildCompleted` case: patches `current_checkpoint_id` signal, sends chat notification, then if `serverPort > 0` calls `sse.ExecuteScript()` to set iframe src

## Learnings

### Docker Environment Limitations
- The Docker container (`harness/Dockerfile`) only has Go + gcc — **no Rust/Cargo/Trunk**. Game server binaries and WASM builds cannot be produced inside Docker.
- The template server binary at `/app/template/target/release/server` is a macOS Mach-O binary (compiled on host) and **cannot run inside the Linux Docker container** ("Exec format error").
- This means the full build pipeline (cargo build server + trunk build WASM client) only works when the harness runs natively on the host, not inside Docker.

### Game Server Lifecycle
- Game servers are **only** started by `Orchestrator.BuildCheckpoint()` (`harness/internal/claude/claude.go:97`). There is no API to start a game server on demand.
- The world page handler (`harness/internal/server/server.go:267-272`) explicitly does NOT call `GameServers.Connect()` to avoid leaking refcounts — it only reads the stored port from SQLite.
- After a Docker container restart, all game server processes are lost, but SQLite still has stale port numbers. The WASM client loads and tries to WebSocket-connect to a dead port.
- `loadCheckpoint()` in `harness/static/game-loader.js` just does `window.location.href` navigation — it doesn't start servers.

### WASM Client WebSocket URL
- The iframe src pattern is `/wasm/{worldID}/{cpID}/index.html?server_port={port}` (see `harness/views/world/world.templ:14`)
- The WASM client reads `server_port` query param and connects via WebSocket to `wss://127.0.0.1:{port}/` (see `template/client/src/main.rs:137-155`)
- **Bug caught during testing**: The original plan specified `/world/%s/checkpoint/%s/game?port=%d` for the ExecuteScript URL, which has no matching route handler. Fixed to use `/wasm/%s/%s/index.html?server_port=%d` to match the existing pattern.

### Stale Test Data
- The regression test world (`d39012bc`) has checkpoint `root0001` in SQLite with `dir_path=/app/data/worlds/d39012bc/root0001` and `server_port=9001`, `status=ready`
- But the actual directory on disk is `/app/data/worlds/d39012bc/c68618b0` (empty), and `root0001` doesn't exist
- WASM artifacts do exist at `/app/data/wasm-builds/d39012bc/root0001/`
- Database location: `/app/data/creative-mode.db` (mapped from host `/Users/coreycole/cdev/creative-mode/data/creative-mode.db`)

### Hot Reload
- `POST /dev/rebuild` triggers the harness to recompile and restart inside Docker (see `harness/scripts/dev-entrypoint.sh`)
- After rebuild, the harness picks up code changes from the volume mount

## Artifacts
- `harness/docker-compose.yml` — port range added
- `harness/Dockerfile` — EXPOSE added
- `harness/internal/world/game_server.go:212-228` — StopByWorldExcept method
- `harness/internal/claude/claude.go:145-170` — old server cleanup + serverPort in event
- `harness/internal/server/events.go:257-294` — auto-reload iframe logic

## Action Items & Next Steps

### Testing the Implementation
To properly E2E test, the next agent should:

1. **Option A: Run harness natively on host** instead of Docker, so cargo/trunk are available and game server binaries can run. Check if `just harness` or `just dev` runs the harness outside Docker.

2. **Option B: Fix stale test data** — either delete the DB and recreate the regression test world, or manually fix the checkpoint's `dir_path` in SQLite to point to a valid directory with a compiled server binary.

3. **Option C: Install Rust in Docker** — add Rust toolchain to the Dockerfile so builds can happen inside the container. This is a larger change.

4. **The actual test**: Once a game server is running, open the world in two browser tabs/sessions (one via playwright-cli default session, one via `-s=second` named session). Both clients should connect to the same game server port via WebSocket. Verify that player entities from one client are visible in the other.

### Improvement: Auto-start game servers on page load
Consider adding logic to start a game server when a user navigates to a world with a "ready" checkpoint but no running server process. The current design intentionally avoids this to prevent refcount leaks, but after a container restart all servers are dead. A "start if not running" check with a timeout-based cleanup could solve this.

## Other Notes

### Key File Locations
- World page handler: `harness/internal/server/server.go:230-277`
- WASM artifact serving: `harness/internal/server/server.go:446-462`
- SSE event handlers: `harness/internal/server/events.go`
- Build pipeline: `harness/internal/build/builder.go`
- Game server manager: `harness/internal/world/game_server.go`
- Port allocator: `harness/internal/world/ports.go` (range 9001-9999)
- World template: `harness/views/world/world.templ` (iframe src logic at line 12-19)
- Client WebSocket connection: `template/client/src/main.rs:137-155`

### Docker Container Details
- Container name: `harness-harness-1`
- Volume mount: `..:/app:cached` (project root at /app)
- Data dir: `/app/data/` (on host: `creative-mode/data/`)
- sqlite3 was manually installed in the container for debugging; it won't persist across container recreations
