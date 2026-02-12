---
date: 2026-02-12T15:29:20-08:00
researcher: CoreyCole
git_commit: d5d5fda4ccc02a5605419ee65bdafecc3569ade2
branch: main
repository: creative-mode
topic: "Mouse Capture & Tab Overlay Toggle"
tags: [input, mouse-capture, harness-ui, controls, bevy, wasm]
status: complete
last_updated: 2026-02-12
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Mouse Capture & Tab Overlay Toggle

## Task(s)

### Completed: Fixed-timestep visual interpolation
- Added `PreviousPlayerPosition` component and `save_previous_positions` system to `template/client/src/main.rs`
- `sync_player_meshes` and `game_camera` now interpolate between physics ticks using `Time<Fixed>::overstep_fraction()`, eliminating the stuttery teleport-per-tick movement jank
- Workspace compiles clean (`cargo check --workspace`, `cargo clippy --workspace`)
- User confirmed movement feels much smoother

### Next: Mouse capture + Tab to toggle harness UI overlay
The game currently uses click-to-lock / Escape-to-unlock cursor grab (`cursor_lock_system` at `template/client/src/main.rs:294-321`). The desired behavior:

1. **Mouse capture in game**: When the user clicks into the game iframe, the mouse should be captured by the game (pointer lock). This already works via `cursor_lock_system`.
2. **Tab key releases mouse and shows harness UI**: Pressing Tab should release the mouse from the game and bring up the harness overlay UI (the surrounding harness chrome — chat, world controls, etc.). This requires coordination between the Bevy WASM client (inside iframe) and the harness page (parent frame).
3. **Clicking back into game re-captures mouse**: When the user clicks back into the game iframe after using the harness UI, the mouse should be re-captured.

Key considerations:
- The game client runs inside an iframe served by trunk. The harness UI is the parent page.
- Communication between iframe and parent is via `postMessage` (already used for debug queries — see `template/client/index.html:17-38`).
- The harness overlay is defined in `harness/views/world/overlay.templ` — this is the UI that should appear/disappear on Tab.
- Currently Escape unlocks the cursor; Tab should also unlock AND signal the parent frame to show the overlay.
- `prevent_default_event_handling: true` is set on the Bevy Window (`template/client/src/main.rs:45`) — this should prevent Tab from doing browser tab-focus cycling inside the game.

## Critical References
- `template/client/src/main.rs` — Client input, cursor lock, camera systems
- `harness/views/world/world.templ` — World page with iframe embedding the game client
- `harness/views/world/overlay.templ` — Harness overlay UI that should toggle on Tab

## Recent changes
- `template/client/src/main.rs:477-478` — Added `PreviousPlayerPosition` component
- `template/client/src/main.rs:482-493` — Added `save_previous_positions` system in `FixedFirst`
- `template/client/src/main.rs:107` — Registered `save_previous_positions` in `FixedFirst` schedule
- `template/client/src/main.rs:534-574` — Modified `sync_player_meshes` to interpolate using `Time<Fixed>::overstep_fraction()` and spawn `PreviousPlayerPosition` with new meshes
- `template/client/src/main.rs:353-415` — Modified `game_camera` to use interpolated player position via `PreviousPlayerPosition` + `Time<Fixed>`
- `template/CHANGES.txt` — Updated with interpolation change description

## Learnings
- `Time<Fixed>::overstep_fraction()` returns a value between 0.0 and 1.0 representing how far between the last and next fixed update the current render frame is. Available as `Res<Time<Fixed>>` in any schedule in Bevy 0.18.
- The standard pattern: save position in `FixedFirst` (before movement), movement updates in `FixedUpdate`, then lerp between saved and current in `Update`. This renders one tick behind (~15.6ms at 64Hz) — imperceptible latency for perfectly smooth interpolation.
- The iframe ↔ parent communication pattern is already established in `template/client/index.html` via `window.addEventListener('message', ...)` and `window.parent.postMessage(...)`. This same pattern can be used for Tab overlay signaling.
- `cursor_lock_system` (`template/client/src/main.rs:294-321`) manages `CursorGrabMode::Locked` on click and `CursorGrabMode::None` on Escape. Adding Tab as another unlock trigger is straightforward on the client side. The parent frame side needs JS to listen for a `postMessage` event to toggle the overlay.
- The harness overlay visibility is likely controlled via Datastar signals or CSS in the world view templates.

## Artifacts
- `template/client/src/main.rs` — All interpolation changes
- `template/CHANGES.txt` — Updated change description

## Action Items & Next Steps

1. **Add Tab key handling in `cursor_lock_system`** (`template/client/src/main.rs:294-321`): When Tab is pressed and cursor is locked, unlock cursor and send a `postMessage` to the parent frame (e.g., `{ type: 'toggle-overlay' }`).

2. **Add postMessage listener in harness world page** (`harness/views/world/world.templ` or overlay JS): Listen for `toggle-overlay` messages from the iframe and show/hide the harness overlay UI accordingly.

3. **Implement overlay show/hide** in the harness: The overlay (`harness/views/world/overlay.templ`) needs a mechanism to toggle visibility — either a Datastar signal, a CSS class toggle, or similar. Check current overlay implementation for existing patterns.

4. **Handle click-to-re-capture**: When the overlay is visible and the user clicks back into the game iframe, the overlay should hide and the game should re-capture the mouse. This may happen naturally via the existing click-to-lock behavior in `cursor_lock_system`, but the parent frame needs to know to hide the overlay.

5. **Test iframe focus edge cases**: Tab key behavior can be tricky in iframes. Test with `prevent_default_event_handling: true` to ensure Tab doesn't also trigger browser tab-focus cycling. May need to explicitly `preventDefault()` on Tab in the iframe's JS.

6. **Consider the D-key and controls-stopping bugs** from the previous handoff (`thoughts/CoreyCole/handoffs/general/2026-02-12_15-17-55_fix-controls-and-movement.md`) — these are still unresolved and may be related to focus/capture issues.

## Other Notes
- The previous handoff at `thoughts/CoreyCole/handoffs/general/2026-02-12_15-17-55_fix-controls-and-movement.md` has detailed analysis of controls-stopping-entirely and D-key bugs that are still outstanding.
- The game server is headless (`MinimalPlugins` + `ScheduleRunnerPlugin`) at `template/server/src/main.rs`. Server-side has BRP enabled for debugging.
- Client debug queries work via JS bridge (`template/client/src/debug.rs`) using `window.__debugRequest`/`__debugResponse`.
- Lightyear version 0.26, Bevy version 0.18. wasm-bindgen pinned at 0.2.108.
- `REPLICATION_INTERVAL` is 100ms (10Hz) — still potentially worth increasing to 20-30Hz after more testing, as noted in previous handoff.
