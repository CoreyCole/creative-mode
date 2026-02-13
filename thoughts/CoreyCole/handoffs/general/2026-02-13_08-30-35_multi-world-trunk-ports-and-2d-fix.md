---
date: 2026-02-13T08:30:35-08:00
researcher: CoreyCole
git_commit: b7b7f492ae58b6cae3cf5e73b581da6822ee8608
branch: main
repository: creative-mode
topic: "Multi-world trunk port allocation + 2D template world fix"
tags: [implementation, harness, game-server, trunk-serve, 2d-template, port-allocation]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Multi-world trunk port allocation + fix 2D template world rendering

## Task(s)

### 1. Multi-world trunk port allocation (COMPLETED)
Implemented dynamic trunk port allocation so each template world gets its own trunk serve instance. Previously only a single hardcoded port (8081) was used via `TEMPLATE_TRUNK_PORT` env var.

### 2. Auto-provision all template types (COMPLETED)
Generalized `EnsureTemplateWorld` (singular, 3D-only) to `EnsureTemplateWorlds` (plural, all types). Both 3D and 2D template worlds are now auto-provisioned on startup. 2D worlds run trunk-only (no game server).

### 3. Fix 2D template WASM rendering (WORK IN PROGRESS)
The 2D template world loads in the iframe (trunk serve is working, WASM is compiled and loaded, canvas element exists) but renders a completely black screen. The Bevy app's `spawn_room()` function creates white text and semi-transparent hotspots, but nothing is visually rendering. This needs investigation — it's a Bevy WASM rendering issue, not a harness/port issue.

## Critical References
- `templates/2d/CLAUDE.md` — 2D template architecture, room JSON schema, build instructions
- `harness/CLAUDE.md` — Harness server architecture, game server management
- `harness/internal/world/game_server.go` — Core game server + trunk serve management

## Recent changes

All changes are uncommitted on `main`. Files modified:

- `harness/internal/world/ports.go:22-25` — `NewPortAllocator` now accepts `minPort, maxPort` params; added `trunkMinPort`/`trunkMaxPort` constants (8081-8180)
- `harness/internal/world/game_server.go:70-91` — Added `trunkPorts *PortAllocator` field to `GameServerManager`; constructor creates both allocators
- `harness/internal/world/game_server.go:244-270` — `GetServer()` now handles trunk-only entries (Port: 0) by checking trunk session liveness instead of game server session
- `harness/internal/world/game_server.go:270-290` — `Disconnect()` releases trunk ports; guards `ports.Release` with `Port > 0`
- `harness/internal/world/game_server.go:300-320` — `StopByWorldExcept()` also kills trunk sessions and releases trunk ports
- `harness/internal/world/game_server.go:330-345` — `Shutdown()` releases trunk ports
- `harness/internal/world/game_server.go:395-435` — `Recover()` creates Port:0 entries for trunk-only sessions instead of treating them as orphans; marks trunk ports in use
- `harness/internal/world/game_server.go:560-670` — `StartTrunkServe()` allocates from trunk port pool, auto-creates Port:0 server entry for trunk-only worlds, detects `client/` vs root dir for trunk working dir
- `harness/internal/world/game_server.go:675-695` — `StopTrunkServe()` releases trunk port back to pool
- `harness/internal/world/manager.go:348-410` — New `EnsureTemplateWorlds()` loops over all template types; `ensureSingleTemplateWorld()` finds or creates per-type
- `harness/internal/world/manager.go:415-465` — `createTemplateWorldDev()` accepts `templateType`/`templateDir` params
- `harness/internal/world/manager.go:470-505` — `ensureTemplateDevReady()` accepts template params
- `harness/internal/world/manager.go:510-550` — `startTemplateDevServers()` branches: 3D gets cargo watch + trunk; 2D gets trunk-only
- `harness/main.go:139-141` — Recovery uses `strings.HasSuffix(w.Name, "Template World")` instead of exact match
- `harness/main.go:174-176` — Calls `EnsureTemplateWorlds` (plural)
- `harness/docker-compose.yml:8` — Port range `8081-8180:8081-8180` (was `8081:8081`)
- `harness/docker-compose.yml:20` — Removed `TEMPLATE_TRUNK_PORT=8081` env var
- `templates/2d/index.html:5` — Added `<link data-trunk rel="rust" data-target-name="room_world" />` to fix trunk build ambiguity between binary and cdylib targets

## Learnings

1. **Trunk-only entries need special liveness checks**: `GetServer()` was cleaning up Port:0 entries because `IsAlive()` checks the `cm-server-*` tmux session which doesn't exist for 2D worlds. Fixed by checking trunk session liveness for Port:0 entries instead.

2. **Trunk build target ambiguity**: The 2D template's `Cargo.toml` has both a `[lib]` (cdylib → `room_world`) and implicit binary target (`room-world` from `main.rs`). Trunk couldn't decide which artifact to use. Fixed by adding `<link data-trunk rel="rust" data-target-name="room_world" />` to `index.html`.

3. **`clientDir` detection**: The 3D template has WASM client in a `client/` subdirectory, but the 2D template has `index.html` at root. `StartTrunkServe` now checks for `client/index.html` and falls back to the root dir.

4. **Port release safety**: All paths that delete server entries must also release trunk ports. Added `Port > 0` guards to avoid releasing port 0 from the game server allocator.

5. **2D Bevy WASM renders black**: The WASM loads successfully (canvas exists, no JS errors), trunk serve builds and serves correctly, but nothing renders visually. The `spawn_room()` function in `templates/2d/src/room.rs:141-207` creates sprites and text entities but they don't appear. This likely requires Bevy-specific WASM debugging (e.g. checking if the camera, window setup, or WebGL context is working).

## Artifacts
- `harness/internal/world/ports.go` — Updated PortAllocator constructor
- `harness/internal/world/game_server.go` — Trunk port allocator, auto-create entries, recovery fix
- `harness/internal/world/manager.go` — Multi-template provisioning
- `harness/main.go` — Recovery + startup changes
- `harness/docker-compose.yml` — Port range expansion
- `templates/2d/index.html` — Trunk target fix

## Action Items & Next Steps

1. **Fix 2D template Bevy WASM rendering** — The immediate issue. The room spawn code (`templates/2d/src/room.rs:141-207`) creates entities but nothing renders. Debug approaches:
   - Use the `/bevy-debug` skill to inspect ECS state via BRP (though 2D has no server, so BRP may not be available in WASM)
   - Check if `Camera2d` is spawning correctly in WASM (`room.rs:106`)
   - Check if Bevy's `2d` feature includes WebGL2 rendering — the `Cargo.toml` uses `default-features = false, features = ["2d", "default_font"]` which may be missing a required rendering feature for WASM
   - Try adding `bevy/webgl2` or `bevy/webgpu` feature to `Cargo.toml` — this is likely the root cause since Bevy's `2d` feature alone may not include a WASM-compatible renderer
   - Compare with 3D template's Cargo.toml to see which Bevy features it enables for WASM

2. **Commit the port allocation changes** — All harness changes build and lint cleanly. The port allocation is working correctly (verified via Docker logs showing both worlds with separate trunk ports).

3. **Test world hopping** — After fixing 2D rendering, verify switching between 3D and 2D template worlds from the lobby works with both trunk serve instances running simultaneously.

## Other Notes

- The 2D template world is provisioned as worldID `ef3a75b9`, checkpointID `b13fd1c6` (these are generated UUIDs, will differ on fresh DB)
- The 3D template world is provisioned as worldID `b90e523c`, checkpointID `24edfb38`
- Docker logs confirm both worlds start correctly: 3D on gamePort=9001/trunkPort=8081, 2D on gamePort=0/trunkPort=8082
- The `just live` file watcher can trigger double rebuilds — the second rebuild killed the server during testing. Running `docker compose up -d` recovers it.
- The most likely fix for 2D rendering is adding WASM rendering features to `templates/2d/Cargo.toml`. Check `templates/3d/client/Cargo.toml` for reference on which Bevy features are needed.
