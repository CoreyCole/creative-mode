---
date: 2026-02-12T16:29:39-08:00
researcher: CoreyCole
git_commit: b36cf1a16c6190ade8828be8e7b6586487d08ba7
branch: main
repository: creative-mode
topic: "Air Hot-Reload + Tab Overlay Toggle + Multiplayer Position Bugs"
tags: [air, docker, hot-reload, overlay, tab-key, multiplayer, position-sync]
status: complete
last_updated: 2026-02-12
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Air Hot-Reload, Tab Overlay Toggle, and Multiplayer Position Mismatch

## Task(s)

### 1. Air Hot-Reload in Docker (COMPLETED)
Replaced the manual build-run-restart loop in the Docker dev container with `air` for automatic file-watching and hot-reload. The container is now fully self-contained — `docker compose up` watches `.go` and `.templ` files and rebuilds automatically.

**Committed as `b36cf1a`.**

### 2. Tab Key Overlay Toggle Bug (WORK IN PROGRESS)
The Tab key is supposed to toggle the overlay (show/hide). Current behavior: pressing Tab unlocks the cursor and sends `cursor-unlocked` postMessage, which expands the overlay. But the **reverse** (pressing Tab when overlay is open to hide it and re-lock the cursor) is not implemented. Additionally, there's a bug where **releasing the mouse button on the Tab trigger area doesn't properly toggle the overlay**.

### 3. Multiplayer Position Mismatch (BUG - NOT STARTED)
When two clients view the same world, their rendered positions for entities don't match. The screenshot shows the same scene from two browsers with capsule positions visibly offset. This likely involves the Lightyear prediction/interpolation pipeline.

## Critical References
- `template/shared/src/protocol.rs` — shared movement logic and replication config
- `template/client/src/main.rs` — cursor lock system, input buffering, visual interpolation
- `harness/static/game-loader.js` — postMessage bridge between game iframe and harness overlay

## Recent changes

- `harness/Dockerfile:21-23` — Added `air@v1.62.0` and `templ@v0.3.977` install
- `harness/.air.toml` — New file: Air config with poll-based watching for Docker/VirtioFS
- `harness/scripts/dev-entrypoint.sh` — Replaced manual build loop with `exec air`

## Learnings

- **Air v1.62.0 is the latest Go 1.24-compatible version**. v1.63.0+ requires Go 1.25. The Docker image uses `golang:1.24-bookworm`, so we pin `air@v1.62.0`.
- **`poll = true` is required for Docker on macOS** — inotify/VirtioFS file events are unreliable. Poll interval of 1000ms works well.
- **`exclude_regex = ["_templ\\.go$"]` prevents rebuild loops** — `templ generate` writes `_templ.go` files, which would trigger another watch event without this exclusion.
- **Tab overlay toggle data flow**: Bevy `cursor_lock_system` (client/main.rs:348-352) → `post_message_to_parent("cursor-unlocked")` → game-loader.js receives postMessage → clicks hidden `#game-cursor-unlock-trigger` button → Datastar updates `$overlay_expanded` signal → overlay shows/hides via `data-show`.
- **Multiplayer uses Lightyear**: prediction for the owning client, interpolation for others. The shared movement function is at `protocol.rs:126-134`. Server replicates at 100ms intervals (`REPLICATION_INTERVAL`), fixed timestep is 64Hz.

## Artifacts

- `harness/Dockerfile:21-23` — air + templ install layer
- `harness/.air.toml` — air configuration (new file)
- `harness/scripts/dev-entrypoint.sh` — simplified entrypoint

## Action Items & Next Steps

### Tab Overlay Toggle
1. **Implement Tab-to-hide**: When overlay is expanded and user presses Tab, it should hide the overlay and re-lock the cursor. Currently the Bevy client only sends `cursor-unlocked` on Tab — it needs to also handle the case where Tab should send `cursor-locked` (or a new `toggle-overlay` message type).
2. **Debug mouse release bug**: The hidden trigger buttons (`#game-cursor-lock-trigger` / `#game-cursor-unlock-trigger` in `harness/views/world/world.templ`) are clicked programmatically by game-loader.js. Investigate whether the click is being swallowed or the Datastar signal isn't updating on mouse release.
3. Key files to examine:
   - `template/client/src/main.rs:322-360` — `cursor_lock_system()`, Tab key handling at line 348
   - `harness/static/game-loader.js` — postMessage listener
   - `harness/views/world/world.templ` — hidden trigger buttons and Datastar signal wiring
   - `harness/views/world/overlay.templ` — `data-show="$overlay_expanded"` visibility

### Multiplayer Position Mismatch
1. **Investigate interpolation vs prediction divergence**: The two clients show different positions for the same entities. Check if `InterpolationTarget` config is correct at `template/server/src/main.rs:124`.
2. **Check `shared_movement()` determinism**: Both client prediction (`client/main.rs:510-516`) and server authority (`server/main.rs:140-146`) call `shared_movement()` from `protocol.rs:126-134`. Verify both use the same timestep.
3. **Check `update_camera_and_mesh()` lerp**: Visual interpolation between fixed timesteps at `client/main.rs:582-633` — the alpha calculation or previous position tracking could cause visual divergence.
4. **Verify `REPLICATION_INTERVAL` and `FIXED_TIMESTEP_HZ`**: 100ms replication interval with 64Hz fixed timestep at `protocol.rs:13,17`.

## Other Notes

- The Docker container with Air is currently running (background task `b7f4889`). It was verified working end-to-end: editing a `.templ` file triggers rebuild within ~2-3 seconds.
- The `/dev/rebuild` endpoint still exists for manual triggers — Air doesn't remove that capability.
- `just watch` / `just live` host-side commands still work but are no longer required.
- The multiplayer screenshot shows two browser windows viewing the same world (`localhost:8080/world/28b69cd8`). Both show a skeleton warrior character and two capsule entities, but their positions are offset relative to each other, suggesting the issue is in how position state is replicated/rendered rather than which entities exist.
