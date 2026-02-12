---
date: 2026-02-12T15:17:55-08:00
researcher: CoreyCole
git_commit: 6c524e651ac12c4d058d7f9bf400df3d721e1795
branch: main
repository: creative-mode
topic: "Fix WASM Client Controls & Movement Jankiness"
tags: [bugfix, controls, movement, bevy, lightyear, wasm, client]
status: complete
last_updated: 2026-02-12
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Fix WASM Client Controls & Movement Issues

## Task(s)

### Completed: JPEG texture + WebGL canvas fix
- Added `jpeg` feature to Bevy dependency so `bonecrusher_warrior.glb` JPEG-encoded textures load correctly.
- Pre-created `<canvas id="bevy-canvas">` in `index.html` and configured Bevy's `Window` to target it (`canvas: Some("#bevy-canvas".into()), fit_canvas_to_parent: true`). This prevents browser extensions (Dark Reader, ad blockers) from poisoning the canvas context before wgpu can claim it.
- Workspace compiles clean (`cargo check --workspace`).

### Work In Progress: Intermittent control bugs & janky movement
The following bugs remain and need thorough investigation:

1. **Controls stop working entirely** — Intermittently, all keyboard input stops being processed. Suspected causes:
   - Cursor lock / focus issues: `cursor_lock_system` (`client/src/main.rs:292-319`) toggles `CursorGrabMode::Locked` on click and unlocks on Escape. If the browser loses pointer lock (e.g., alt-tab, iframe focus change) the "browser-initiated unlock" detection at line 315 may not fire reliably, or cursor state might get stuck.
   - The `buffer_input` system (`client/src/main.rs:417-458`) runs in `FixedPreUpdate` in `InputSystems::WriteClientInputs`. If the `ActionState<PlayerInput>` query returns `Err` (no entity with `InputMarker`), all input is silently dropped. The `InputMarker` is only added in `handle_predicted_spawn` (line 491) which triggers on `Add<PlayerId>` for `Predicted` entities — if the predicted entity is despawned and re-created (e.g., reconnection, rollback edge case), the marker might not be re-attached.
   - Bevy's `prevent_default_event_handling: true` on the Window may interact with iframe focus in unexpected ways.

2. **D key specifically not working** — While W/A/S work, D sometimes doesn't register. This is very suspicious because D is handled identically to A/W/S in `buffer_input` (line 441). Possible causes:
   - Browser shortcut interference — some browsers use Ctrl+D for bookmarking, and in certain focus states D alone might get intercepted.
   - Bevy keyboard event handling in WASM — `prevent_default_event_handling` might not prevent all browser key interceptions within an iframe.
   - Could be a Bevy `ButtonInput<KeyCode>` bug specific to certain keys in WASM mode — needs research.

3. **Janky/non-smooth movement** — The movement feels stuttery rather than smooth. Key areas to investigate:
   - `shared_movement` (`shared/src/protocol.rs:126-134`) uses a fixed `dt = 1.0 / 64.0` but runs in `FixedUpdate`. The visual position is updated in `sync_player_meshes` (`client/src/main.rs:515-552`) which runs in `Update` and directly snaps `Transform` to `PlayerPosition` with no interpolation between fixed steps.
   - There is NO visual interpolation between physics ticks. The transform just teleports to the latest `PlayerPosition` every render frame. This is the classic "fixed timestep without render interpolation" jank. The fix is to either: (a) interpolate the transform in the `Update` schedule between the previous and current `PlayerPosition`, or (b) use Bevy's built-in `FixedUpdate` interpolation if available in 0.18.
   - Lightyear rollback/reconciliation may also cause visible stuttering — when the server corrects the predicted position, the client snaps to the new position. Lightyear's prediction should handle this smoothly but may need tuning (e.g., `CorrectionFn`, smoothing config).
   - The camera lerp in `game_camera` (line 401) uses `t = (15.0 * time.delta_secs()).min(1.0)` which is framerate-dependent exponential smoothing — this may mask or amplify movement jank.
   - `REPLICATION_INTERVAL` is 100ms (`shared/src/protocol.rs:17`) which means server updates are relatively infrequent (10Hz). Combined with 64Hz tick rate and no visual interpolation, this creates visible stepping.

## Critical References
- `template/CLAUDE.md` — Full architecture doc covering client-server prediction, Lightyear setup, and debugging
- `template/shared/src/protocol.rs` — Shared movement logic, constants, and protocol definitions
- `template/client/src/main.rs` — All client systems (input, camera, prediction, mesh sync)

## Recent changes
- `template/client/Cargo.toml:7` — Added `"jpeg"` feature to bevy dependency
- `template/client/index.html:12` — Added `<canvas id="bevy-canvas">` element
- `template/client/src/main.rs:45-46` — Added `canvas` and `fit_canvas_to_parent` to Window config

## Learnings
- Bevy re-exports image format features at the top level — use `features = ["jpeg"]` not `"bevy_image/jpeg"` (slashes not allowed in Cargo dependency features).
- The Bevy WASM best practice for canvas is to pre-create it in HTML with a known ID and point Bevy at it via `Window { canvas: Some("#id".into()), fit_canvas_to_parent: true }`. This avoids race conditions with browser extensions that call `getContext("2d")` on dynamically created canvases.
- Lightyear's `ActionState<PlayerInput>` + `InputMarker` pattern means input only works when the predicted entity exists AND has `InputMarker` attached. This is a fragile point — if the observer that attaches `InputMarker` doesn't fire (or fires on the wrong entity), controls silently break.
- The `sync_player_meshes` system does a hard snap of Transform to PlayerPosition with zero visual interpolation between fixed timesteps — this is a guaranteed source of visual jank at any non-trivial frame rate.

## Artifacts
- `template/client/Cargo.toml` — Updated with jpeg feature
- `template/client/index.html` — Updated with pre-created canvas
- `template/client/src/main.rs` — Updated Window config

## Action Items & Next Steps

1. **Research Bevy 0.18 + Lightyear 0.26 best practices** for:
   - Fixed timestep visual interpolation (Transform smoothing between FixedUpdate ticks)
   - Lightyear prediction smoothing and correction configuration
   - WASM keyboard input reliability (especially `prevent_default_event_handling` behavior in iframes)

2. **Fix visual movement interpolation** — The highest-impact fix. `sync_player_meshes` needs to interpolate Transform between the previous and current `PlayerPosition` based on the fixed timestep accumulator. Bevy 0.18 may have built-in support for this (check `FixedTime` / `Time<Fixed>` accumulator access).

3. **Debug the D-key and controls-stopping-entirely issues** — Use the `bevy-debug` skill and `playwright-cli` to:
   - Monitor `CameraState` resource (cursor_locked state) when controls stop working
   - Check if `InputMarker` entity exists when controls fail
   - Test D-key behavior with `prevent_default_event_handling` toggled
   - Test outside of iframe (direct `localhost:8081`) vs inside iframe (`localhost:8080`)

4. **Consider adjusting constants** — `REPLICATION_INTERVAL` at 100ms (10Hz) is quite low; 50ms (20Hz) or 33ms (30Hz) would reduce perceived lag. `MOVE_SPEED` at 10.0 with 3x sprint (30.0) may need tuning after interpolation is fixed.

5. **Write CHANGES.txt** after fixes are complete per template/CLAUDE.md requirements.

## Other Notes
- The game server is headless (`MinimalPlugins` + `ScheduleRunnerPlugin`) at `template/server/src/main.rs`. It has BRP (Bevy Remote Protocol) enabled for debugging server-side ECS state.
- Client debug queries work via a JS bridge (`template/client/src/debug.rs`) using `window.__debugRequest`/`__debugResponse` — this is accessible through playwright-cli.
- The camera system has two modes (first-person / third-person, toggled with V). Both modes derive movement direction from `camera_state.yaw`, so the movement direction calculation in `buffer_input` is shared between modes.
- The `handle_predicted_spawn` observer at `client/src/main.rs:478` triggers on `On<Add, PlayerId>` with a `With<Predicted>` query filter — this is how `InputMarker` gets attached. If this observer doesn't match (e.g., entity doesn't have `Predicted` when `PlayerId` is added), input will never work for that entity.
- Lightyear version is 0.26 with features: client, server, replication, prediction, interpolation, input_native, netcode, websocket, websocket_self_signed (see `template/Cargo.toml:7-17`).
