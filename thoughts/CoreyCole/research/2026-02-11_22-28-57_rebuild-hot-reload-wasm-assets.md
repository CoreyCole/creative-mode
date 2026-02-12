---
date: 2026-02-11T22:28:57-08:00
researcher: CoreyCole
git_commit: 7081ecaa867759b6267e132b6c9776ec8095f518
branch: main
repository: creative-mode
topic: "How rebuilds work, WASM asset organization, hot reloading, and wss://127.0.0.1:9001 connection failure"
tags: [research, codebase, build-pipeline, hot-reload, wasm, websocket, game-server, docker]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
---

# Research: Rebuild System, WASM Assets, Hot Reloading, and WebSocket Connection Failure

**Date**: 2026-02-11T22:28:57-08:00
**Researcher**: CoreyCole
**Git Commit**: 7081ecaa867759b6267e132b6c9776ec8095f518
**Branch**: main
**Repository**: creative-mode

## Research Question

How do rebuilds work when changes are made? The user sees "Firefox can't establish a connection to the server at wss://127.0.0.1:9001/" in the console. The goal is to have each game world hot-reload as Claude makes changes to different worlds in parallel. If a world is cloned to go down a different path, it should also hot reload. How are the WASM assets organized? How does the rebuild happen? We need to build for local dev (`just live`) and on the server later.

## Summary

There are **two separate hot-reload systems**: one for the Go harness server itself (dev mode, file watcher → Docker rebuild → Datastar SSE morph), and one for game world checkpoints (Claude edits code → hook fires → build pipeline → new WASM + game server). The `wss://127.0.0.1:9001/` error is caused by **Docker not exposing game server ports** (only 8080 is mapped) and potentially **Firefox rejecting the self-signed TLS certificate** on the WebSocket connection. Game world "hot reload" currently requires a full page navigation to a new checkpoint — there is no automatic iframe refresh when a build completes.

## Detailed Findings

### 1. WASM Asset Organization

WASM build artifacts are stored per-checkpoint at:
```
data/wasm-builds/{worldID}/{cpID}/
├── index.html              ← Trunk-processed HTML with injected loader
├── client-{hash}_bg.wasm   ← Compiled WASM binary
└── client-{hash}.js        ← wasm-bindgen JS glue
```

- **Trunk config** (`template/client/Trunk.toml`): `filehash = true` produces content-hashed filenames for cache busting. `minify = "on_release"`. `wasm_bindgen = "0.2.108"` pinned.
- **Per-checkpoint isolation**: Each checkpoint gets its own WASM build output directory. Multiple versions of the game can coexist.
- **Served by harness** at `GET /wasm/:worldID/:cpID/*` → `handleWASMArtifacts()` (`server.go:446-462`). Requires auth (approved users only).
- **Shared game assets** (textures, models) live in `data/shared-assets/` served at `GET /assets/*` without auth. The Bevy client's `AssetPlugin` uses `file_path: "/assets"` to load them via HTTP.

### 2. The Two Build Pipelines

#### A. Harness Dev Hot-Reload (for Go server changes)

Triggered by `just live` which runs three parallel processes:

1. **`just up`** — Docker container via `docker compose up --build`
2. **`just tailwind-watch`** — CSS watcher (no hash, dev mode)
3. **`just watch`** — fswatch with native FSEvents (300ms latency)

**File change cycle:**
```
Host: .templ changed → templ generate → POST /dev/rebuild
Host: .go changed   → POST /dev/rebuild
Host: .css changed  → POST /dev/reload-static

Container: /dev/rebuild handler (dev.go:152-194)
  → go build -o /tmp/harness-next
  → os.Rename to /tmp/harness
  → SIGTERM self

Container: dev-entrypoint.sh restart loop
  → detects exit, restarts /tmp/harness

Browser: SSE connection to /dev/sse breaks
  → Datastar auto-reconnects
  → handleDevSSE → devMorphPage (dev.go:94-147)
  → internal HTTP GET to self (with cookies)
  → extract #page-content innerHTML
  → PatchElements morph (no full page reload)
```

Key implementation files:
- `harness/justfile:68-93` — fswatch watcher
- `harness/scripts/dev-entrypoint.sh` — restart loop
- `harness/internal/server/dev.go:152-194` — rebuild handler
- `harness/internal/server/dev.go:94-147` — page morphing
- `harness/views/layout/layout.templ:30-31` — dev SSE connection

#### B. Game World Build Pipeline (for Rust/Bevy game changes)

Triggered when Claude Code finishes editing a checkpoint:

```
1. User submits prompt → handlePrompt() (server.go:305)
   → Orchestrator.HandlePrompt() (claude.go:62)
   → ForkCheckpoint() copies source dir + clones target/ cache
   → tmux session launches Claude Code

2. Claude Code edits game files

3. Claude Code stops → on-stop.sh hook → POST /api/claude-event
   → Orchestrator.BuildCheckpoint() (claude.go:97)

4. Build pipeline (builder.go:49-141):
   Step 1: cargo build --release -p server    (native binary)
   Step 2: trunk build --release --dist {wasmDir}  (WASM client)
   Timeouts: 5min incremental, 15min initial

5. Game server started:
   → GameServers.Connect() (game_server.go:52)
   → PortAllocator.Allocate() → 9001-9999
   → exec {cpDir}/target/release/server with GAME_PORT={port}
   → Port stored in DB (checkpoints.server_port)

6. EventBus publishes build.completed → SSE → browser
   → build_status signal updates to "ready"
   → BuildReadyNotification chat message with link

7. User clicks link → full page navigation to new checkpoint
   → iframe loads /wasm/{worldID}/{cpID}/index.html?server_port={port}
```

### 3. Why `wss://127.0.0.1:9001/` Fails

The WASM client connects **directly** to the game server — there is no WebSocket proxy in the harness.

**Root cause 1: Docker port not exposed (when running via Docker)**

`docker-compose.yml` only exposes port 8080:
```yaml
ports:
  - "8080:8080"
```

Game server ports 9001-9999 are **not mapped** to the host. The browser on the host tries `wss://127.0.0.1:9001` but the game server is listening inside the Docker container.

**Root cause 2: Self-signed TLS certificate rejected by Firefox**

The game server uses `Identity::self_signed()` (`server/src/main.rs:62-67`) with SANs for localhost/127.0.0.1/::1. Firefox does **not** trust self-signed certs for WebSocket connections and provides no UI to accept them (unlike HTTPS page warnings). The connection simply fails silently.

**Root cause 3: Game server may not be running**

If `server_port` was persisted in the DB from a previous session but the process has since died (harness restart, crash), the iframe URL points to a dead port. The page render at `server.go:268` trusts the DB value without verifying the process is alive.

**How the URL is constructed:**
1. Client reads `server_port` from iframe query string (`client/src/main.rs:137-155`)
2. Falls back to `9001` if absent
3. `SocketAddr::new(Ipv4Addr::LOCALHOST, port)` → `127.0.0.1:{port}`
4. Lightyear uses `WebSocketScheme::Secure` (default) → `wss://127.0.0.1:{port}`

### 4. Hot Reload for Game Worlds — Current State vs Goal

**Current state**: No automatic iframe refresh. When a build completes:
- The browser receives a `build.completed` SSE event
- A `BuildReadyNotification` appears in chat with a link
- User must **manually click** to navigate to the new checkpoint
- This triggers a full page load, loading the new iframe

**Goal**: Each game world should hot-reload automatically when Claude makes changes. Cloned worlds should also hot-reload independently.

**What's needed for true hot reload:**
1. After `build.completed` event, the SSE handler should automatically update the iframe `src` to point to the new checkpoint's WASM build
2. The game server for the old checkpoint needs to be stopped (or the client needs to reconnect to the new port)
3. Each world's iframe is independent, so parallel editing naturally isolates

**Possible approach:**
- On `build.completed`, send an SSE `ExecuteScript` that sets `document.getElementById('game-frame').src = '/wasm/{worldID}/{newCpID}/index.html?server_port={newPort}'`
- Or use Datastar signal patching to update the iframe src reactively
- The old game server will auto-stop after the 2-minute grace period once the refcount drops to 0

### 5. Build Cache for Fast Incremental Builds

Checkpoint forking preserves the Rust `target/` directory using platform-specific efficient copies:
- **macOS (APFS)**: `cp -cR` — copy-on-write clones, nearly instant (`manager.go:421-429`)
- **Linux**: `cp -al` — hardlinks (`manager.go:432-440`)

This means incremental `cargo build` after a fork only recompiles changed crates, not the entire dependency tree.

### 6. Docker Networking Fix Required

To make game servers accessible from the browser when running via Docker, the port range needs to be exposed:

```yaml
# docker-compose.yml
ports:
  - "8080:8080"
  - "9001-9100:9001-9100"  # game server ports
```

Or use Docker host networking:
```yaml
network_mode: host
```

The Dockerfile also needs `EXPOSE 9001-9999` for documentation (though `EXPOSE` alone doesn't publish ports).

### 7. Dev vs Production Considerations

**Local dev (`just live`)**:
- Harness runs in Docker, game servers spawn as child processes inside the container
- Browser connects directly to `wss://127.0.0.1:{port}` — needs Docker port mapping
- Self-signed TLS — needs browser acceptance or switch to `ws://` for local dev

**Production (server)**:
- Game servers need to be accessible from the internet
- Requires a proper TLS reverse proxy (nginx/Caddy) that terminates TLS and forwards to game server ports
- Or use a WebSocket proxy in the harness itself
- The `127.0.0.1` hardcoding in the client needs to become configurable (use the harness host instead)

## Code References

- `harness/docker-compose.yml:7` — Only port 8080 exposed
- `harness/Dockerfile:15` — Only `EXPOSE 8080`
- `harness/justfile:50-93` — `just live`, `just watch`, dev hot-reload
- `harness/scripts/dev-entrypoint.sh` — Docker container restart loop
- `harness/internal/server/dev.go:152-194` — `/dev/rebuild` handler
- `harness/internal/server/dev.go:94-147` — `devMorphPage` page morphing
- `harness/internal/build/builder.go:49-141` — Two-step build (server + WASM)
- `harness/internal/world/game_server.go:52-189` — Game server lifecycle
- `harness/internal/world/ports.go:8-11` — Port range 9001-9999
- `harness/internal/world/manager.go:62-269` — World creation and checkpoint forking
- `harness/internal/world/manager.go:410-441` — Build cache cloning (APFS/hardlinks)
- `harness/internal/claude/claude.go:62-171` — Orchestrator: prompt → build → game server
- `harness/internal/server/events.go:234-301` — SSE event routing to browser
- `harness/internal/server/server.go:446-462` — WASM artifact serving
- `harness/internal/server/server.go:465-497` — Shared asset serving
- `harness/views/world/world.templ:10-28` — Iframe with server_port query param
- `template/client/Trunk.toml:1-7` — Trunk build config
- `template/client/src/main.rs:34,88-101,137-155` — Client WebSocket connection setup
- `template/server/src/main.rs:21-24,55-78` — Server port + self-signed TLS
- `template/shared/src/protocol.rs` — Shared protocol constants
- `template/.claude/hooks/on-stop.sh` — Hook that triggers build pipeline

## Architecture Insights

1. **No WebSocket Proxy**: The harness does NOT proxy WebSocket game traffic. The browser connects directly to game servers on their allocated ports. This is a lightweight design but creates problems with Docker networking and TLS.

2. **Two Independent Hot-Reload Systems**: The harness dev hot-reload (Datastar morph) and the game world build pipeline are completely separate systems. The harness hot-reload is mature (automatic page morph). The game world reload requires manual navigation.

3. **Reference-Counted Game Servers**: `GameServerManager` shares one process across multiple users on the same checkpoint, with a 2-minute grace period before killing idle servers.

4. **APFS Clone Trick**: On macOS, `cp -cR` creates copy-on-write clones of the Rust `target/` directory, making checkpoint forks nearly instant while preserving incremental build capability.

5. **Event-Driven Pipeline**: Claude hooks → EventBus → SSE → Datastar signals. All browser state updates flow through this chain. The `build_status` signal drives the bottom bar UI.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/component-3-world-management-build.md` — Original design for the world management and build pipeline
- `thoughts/CoreyCole/plans/2026-02-11-docker-dev-hot-reload.md` — Docker dev hot-reload implementation plan
- `thoughts/CoreyCole/research/2026-02-11-local-dev-setup-hot-reload.md` — Prior research on local dev setup and hot-reload
- `thoughts/CoreyCole/plans/component-4-bevy-game-template.md` — Bevy game template design (WASM build, WebSocket)
- `thoughts/CoreyCole/plans/component-7-integration-docker.md` — Docker integration design
- `thoughts/CoreyCole/handoffs/general/2026-02-11_02-28-40_docker-dev-hot-reload-complete-playwright-next.md` — Docker hot-reload completion handoff

## Open Questions

1. **Should we add a WebSocket proxy to the harness?** This would solve both the Docker port mapping issue and the TLS issue (harness terminates TLS, proxies plain WS to game server). But adds latency and complexity for real-time game traffic.

2. **Should we switch to `ws://` (plain WebSocket) for local dev?** The self-signed TLS cert causes Firefox failures. For local dev, plain WS to localhost is sufficient. The `websocket_self_signed` Cargo feature could be made conditional.

3. **How should game world hot-reload work?** Options:
   - (a) SSE `ExecuteScript` to update iframe `src` automatically on `build.completed`
   - (b) Datastar signal patch that reactively updates iframe src
   - (c) Keep manual navigation but make it more prominent (auto-scroll to notification)

4. **How will production differ?** The `127.0.0.1` hardcoding in the client must become configurable. The server address should come from the harness (e.g., via query param or environment variable injected into the WASM HTML).

5. **Should Docker use host networking?** `network_mode: host` would expose all ports automatically but sacrifices container isolation. Alternatively, expose a port range like `9001-9100:9001-9100`.
