---
date: 2026-02-12T15:56:32-08:00
researcher: CoreyCole
git_commit: 4bb2d811c27f5ffec51a77194d2cd0605c33f3c2
branch: main
repository: creative-mode
topic: "Tab Overlay Toggle via postMessage"
tags: [input, mouse-capture, harness-ui, postmessage, datastar, docker, bevy, wasm]
status: complete
last_updated: 2026-02-12
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Tab Overlay Toggle via postMessage

## Task(s)

### Completed: Implementation of Tab-to-toggle overlay + mouse capture coordination
Code is written and compiles cleanly on both Rust and Go sides. The three-part implementation:

1. **Bevy client (WASM)** — Added `post_message_to_parent()` helper and modified `cursor_lock_system` to send `cursor-locked` / `cursor-unlocked` messages to the parent frame on click-to-lock / Tab-to-unlock. **Status: complete, compiles clean.**

2. **Harness JS bridge** (`harness/static/game-loader.js`) — Added `window.addEventListener('message', ...)` that listens for iframe postMessages and clicks hidden trigger buttons. **Status: complete.**

3. **Harness template** (`harness/views/world/world.templ`) — Added hidden `<button>` elements inside `#harness-overlay` div with `data-on:click` expressions that set `$overlay_expanded` Datastar signal. **Status: complete, templ generated, Go builds clean.**

### Blocked: Changes not reflected in Docker container
The harness runs inside Docker (OrbStack on macOS, port 8080). The compiled Go binary and templates inside the container are stale — the live page does not have the new trigger buttons (`document.getElementById('game-cursor-unlock-trigger')` returns null). The static JS file (`game-loader.js`) is also served from the container and may be stale.

**The container needs to be rebuilt/restarted for the changes to take effect.** This is the critical blocker — the code is correct but the user can't verify it until the Docker container picks up the changes.

### Partially working: Tab releases mouse
The user confirmed that pressing Tab does release the mouse from the game (the Bevy-side `cursor_lock_system` change is working since the WASM client is served by trunk dev server at port 8081, not the harness Go binary). But the overlay doesn't appear because the harness-side template/JS changes haven't been deployed to the container.

## Critical References
- `template/client/src/main.rs:292-340` — `post_message_to_parent()` and updated `cursor_lock_system`
- `harness/views/world/world.templ:25-34` — Hidden trigger buttons and overlay div
- `harness/static/game-loader.js:1-14` — postMessage bridge JS

## Recent changes
- `template/client/src/main.rs:26` — Added `use wasm_bindgen::prelude::*;` (cfg-gated for wasm)
- `template/client/src/main.rs:295-316` — Added `post_message_to_parent()` function with wasm/non-wasm variants
- `template/client/src/main.rs:318-340` — Updated `cursor_lock_system`: Tab unlocks + sends `cursor-unlocked`, click-to-lock sends `cursor-locked`
- `harness/static/game-loader.js:1-14` — Added postMessage listener that clicks hidden trigger buttons
- `harness/views/world/world.templ:31-32` — Added hidden `<button id="game-cursor-lock-trigger">` and `<button id="game-cursor-unlock-trigger">` with Datastar `data-on:click` expressions
- `template/CHANGES.txt` — Updated with overlay toggle description

## Learnings

### Datastar custom events don't work with hyphenated names
We first tried `data-on:game-cursor-locked` on the overlay div with `CustomEvent` dispatch. This **does not work** because the browser's HTML `dataset` API converts hyphenated `data-*` attribute names to camelCase — so Datastar sees `gameCursorLocked` as the event name but we dispatched `game-cursor-locked`. The mismatch means the handler never fires.

**Solution**: Use hidden `<button>` elements with `data-on:click` and programmatically call `.click()` from JS. This is reliable because `data-on:click` is well-tested in Datastar, and `.click()` fires on hidden elements.

### postMessage flow for iframe ↔ parent communication
- Bevy WASM calls `web_sys::window().parent().post_message()` to send to the parent frame
- Parent's `window.addEventListener('message', ...)` receives it
- The `js_sys::Object` + `Reflect::set` pattern (from `template/client/src/debug.rs`) works for building JS objects from Rust without extra dependencies

### Docker container serves stale Go templates and static files
The harness binary and its templates are compiled into the Docker image. Changes to `.templ` files, `_templ.go` generated files, and static JS files require rebuilding and restarting the container. The trunk dev server (port 8081) serves the WASM client independently, which is why Bevy-side changes work immediately but harness-side changes don't.

### Bevy 0.18 mouse capture research (from web research)
- `CursorGrabMode::Locked` maps to browser Pointer Lock API on WASM
- `KeyCode::Tab` is the correct variant; `prevent_default_event_handling: true` prevents browser Tab focus cycling
- Known bug (Bevy #8949): browser Escape doesn't report back to Bevy — existing workaround at `template/client/src/main.rs:336-340` handles this
- Community plugin `bevy_fix_cursor_unlock_web` v0.3 supports Bevy 0.18 if the workaround proves insufficient
- `CursorOptions` was split from `Window` in Bevy 0.18 (PR #19668) — current code already uses the correct pattern

## Artifacts
- `template/client/src/main.rs` — postMessage helper + updated cursor_lock_system
- `harness/views/world/world.templ` — Hidden trigger buttons
- `harness/static/game-loader.js` — postMessage bridge
- `template/CHANGES.txt` — Updated

## Action Items & Next Steps

1. **Rebuild/restart the Docker harness container** so it picks up the template and static JS changes. Check how the Docker dev workflow handles harness rebuilds — see `harness/docker-compose.yml`, `harness/Dockerfile`, `harness/scripts/dev-entrypoint.sh`. The previous handoff at `thoughts/CoreyCole/handoffs/general/2026-02-11_01-56-00_docker-dev-hot-reload-plan.md` may have context on the Docker hot-reload setup.

2. **Verify the full flow end-to-end** after container restart:
   - Page loads → overlay expanded, cursor free
   - Click into game → cursor locks, overlay minimizes
   - Press Tab → cursor unlocks, overlay expands
   - Click back into game → cursor locks, overlay minimizes

3. **Consider whether Escape should also show the overlay** — currently Escape unlocks the cursor but does NOT send a postMessage (so overlay stays in its current state). The user may want Escape to behave like Tab (show overlay) or differently (just free the cursor without changing overlay).

4. **Test iframe focus edge cases** — particularly around whether clicking the game iframe from the overlay reliably re-captures the mouse. The `cursor_lock_system` click-to-lock should handle this, but pointer lock requires a user gesture and the click event might be consumed by the overlay's `pointer-events-auto` children before reaching the iframe.

5. **D-key and controls-stopping bugs** from `thoughts/CoreyCole/handoffs/general/2026-02-12_15-17-55_fix-controls-and-movement.md` are still unresolved.

## Other Notes
- The harness is served by OrbStack/Docker on port 8080. The trunk dev server serves the WASM client on port 8081 (referenced in the iframe `src`).
- `templ generate` showed `updates=0` even after editing `world.templ` — this was misleading; the generated `world_templ.go` DID have the changes (confirmed via grep). The confusing output may be a templ version mismatch issue (generator v0.3.943 vs go.mod v0.3.977).
- All Rust code compiles clean: `cargo check --workspace` and `cargo clippy --workspace` pass.
- All Go code compiles clean: `just generate && go build ./...` pass.
