---
date: 2026-02-13T09:58:54-08:00
researcher: CoreyCole
git_commit: 697d8978522dc0bbc1f320d4ef31418eeafb539c
branch: main
repository: creative-mode
topic: "2D Template Rendering Fix + Image Asset Planning"
tags: [implementation, 2d-template, bevy, wasm, trunk, asset-management, planning]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Fix 2D template WASM rendering + plan image asset support

## Task(s)

### 1. Fix 2D template Bevy WASM rendering (COMPLETED)
Resumed from previous handoff (`2026-02-13_08-30-35_multi-world-trunk-ports-and-2d-fix.md`). The 2D template loaded in the iframe but rendered a black screen. Diagnosed and fixed the root cause.

**Root cause**: `index.html` used `<link data-trunk rel="rust" data-target-name="room_world" />` which targets the **cdylib library** artifact. Trunk loads the WASM module via `init()` but never calls `main()`, so Bevy's app/window/renderer never started. The canvas stayed at its default 300x150 HTML size.

**Fix**: Changed to `<link data-trunk rel="rust" data-bin="room-world" />` to target the **binary**, which runs `main()` → `room_world::run()` → Bevy starts.

**What was NOT the issue**: Missing `webgl2` feature. The `2d` feature already includes `default_platform` which transitively enables `bevy_winit`, `webgl2`, and all WASM necessities. Verified by reading Bevy 0.18's feature definitions inside the Docker container at `/usr/local/cargo/registry/src/index.crates.io-*/bevy-0.18.0/Cargo.toml`.

### 2. Multi-world trunk port allocation (COMPLETED — from prior session)
All harness changes from the previous handoff were committed together with the rendering fix.

### 3. Comprehensive 2D template documentation (COMPLETED)
Added thorough architecture docs to `templates/2d/CLAUDE.md` covering build pipeline, critical gotchas, Bevy feature breakdown, harness integration, and patterns for building new 2D games.

### 4. Plan image/asset support for 2D worlds (NEXT — not started)
The user wants to plan how to include images in the 2D world. Key requirements:
- Harness server serves the images (not embedded in WASM)
- Users can upload assets
- Assets organized in folders
- Bevy loads images via HTTP from the harness

## Critical References
- `templates/2d/CLAUDE.md` — Comprehensive 2D template architecture, build pipeline, key patterns
- `harness/CLAUDE.md` — Harness server architecture, game server management
- `templates/3d/CLAUDE.md` — 3D template's asset loading pattern (uses `asset_server.load("http://{harness_host}/assets/...")`)

## Recent changes

All committed on `main` in two commits:

**Commit `c56db7e`**: Multi-world trunk port allocation + 2D rendering fix
- `templates/2d/index.html:6` — Changed `data-target-name="room_world"` to `data-bin="room-world"` (the critical rendering fix)
- `harness/internal/world/ports.go:11-12` — Added `trunkMinPort`/`trunkMaxPort` constants (8081-8180)
- `harness/internal/world/ports.go:24-25` — `NewPortAllocator` now accepts `minPort, maxPort` params
- `harness/internal/world/game_server.go:70-78` — Added `trunkPorts *PortAllocator` field, dual allocator construction
- `harness/internal/world/game_server.go:241-268` — Trunk-only liveness checks for Port:0 entries
- `harness/internal/world/game_server.go:560-700` — `StartTrunkServe()` allocates from trunk port pool, auto-creates Port:0 entries, detects `client/` vs root dir
- `harness/internal/world/manager.go:348-550` — `EnsureTemplateWorlds()` loops over all template types
- `harness/main.go:136-141` — Recovery uses `strings.HasSuffix(w.Name, "Template World")`
- `harness/docker-compose.yml:8` — Port range `8081-8180:8081-8180`
- `templates/2d/src/debug.rs` — New debug query system for 2D template
- `templates/2d/src/lib.rs` — Registers debug system, adds `debug` module
- `templates/2d/src/room.rs:42` — Added `Serialize` derive to `ActionDef` for debug output

**Commit `697d897`**: Comprehensive architecture docs for 2D template CLAUDE.md

## Learnings

1. **cdylib vs binary target in Trunk**: When a crate has both `[lib] crate-type = ["cdylib", "rlib"]` and a `main.rs`, trunk sees two artifacts. Using `data-target-name` to select the cdylib loads the WASM but never calls `main()` — resulting in a black screen with no errors. Always use `data-bin="crate-name"` for Bevy WASM apps.

2. **Bevy 0.18 `2d` feature tree**: `2d` → `default_platform` → `bevy_winit` + `webgl2`. No need to add `webgl2` explicitly. The `2d` and `3d` meta-features are symmetric — both include `default_app` + `default_platform` + their respective renderers.

3. **Docker macOS bind mount file watching**: Trunk's file watcher inside Docker doesn't detect host filesystem events on macOS. Must restart the container (`just down && just up`) after changing `Cargo.toml` or other files.

4. **3D template asset loading pattern**: The 3D template uses `asset_server.load("http://{harness_host}/assets/...")` to load assets served by the harness. The 2D template could use the same pattern. Currently the 3D client's `Trunk.toml` has a proxy rule: `[[proxy]] backend = "http://127.0.0.1:8080/assets/"`.

## Artifacts
- `templates/2d/CLAUDE.md` — Comprehensive architecture docs (207 new lines)
- `templates/2d/index.html` — Fixed trunk target + debug bridge JS
- `templates/2d/src/debug.rs` — New debug query system
- `harness/internal/world/game_server.go` — Trunk port allocation
- `harness/internal/world/manager.go` — Multi-template provisioning
- `harness/internal/world/ports.go` — Parameterized port allocator

## Action Items & Next Steps

1. **Plan image/asset support for 2D worlds** — The immediate next task. Key design decisions:
   - **Asset storage**: Where on disk? Per-world? Per-checkpoint? Shared library?
   - **Upload mechanism**: How do users upload images? UI in the harness overlay? Drag-and-drop? File picker?
   - **Folder organization**: How are assets organized? Flat list? User-created folders? Tags?
   - **Serving**: The harness already serves static files at `/static/`. Need a new route like `/world/{worldID}/assets/...` or `/assets/{worldID}/...`
   - **Bevy loading**: Use `asset_server.load("http://harness-host/world/{worldID}/assets/image.png")`. The 3D template already does this pattern.
   - **Template integration**: How does `room.rs` reference images? Add an `image` field to hotspots? Sprite-based backgrounds?

2. **Consider the 3D template's approach**: Check `templates/3d/client/Trunk.toml` for the proxy rule and `templates/3d/client/src/main.rs` for how assets are loaded. The 2D template could use a similar proxy or direct HTTP URLs.

3. **Database schema**: May need new tables for asset metadata (filename, world_id, folder, upload date, mime type).

## Other Notes

- Both template worlds are running correctly in Docker: 3D on gamePort=9001/trunkPort=8081, 2D on gamePort=0/trunkPort=8082
- The 2D world renders the lobby room with "Lobby" title, "Welcome" dialog hotspot, and "Garden" navigation hotspot — verified via playwright screenshot
- The 2D template's `Cargo.toml` kept `default-features = false` with just `["2d", "default_font"]` — this is minimal and correct
- Bevy's `AssetPlugin` is configured with `meta_check: AssetMetaCheck::Never` in `lib.rs:24-27` which is important for HTTP-served assets (no `.meta` files)
- The current room system uses `include_str!` to embed JSON at compile time. Images would need to be loaded at runtime via HTTP, which is a different pattern.
