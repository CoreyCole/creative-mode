---
date: 2026-02-11T20:48:14-08:00
researcher: CoreyCole
git_commit: eddc76d8ad6c90ddc8de171ff19891034ef77ce0
branch: main
repository: creative-mode
topic: "Debug Query System - E2E Verified, Camera Bug Found"
tags: [implementation, bevy, debug, bevy_remote, wasm, ecs, camera]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Debug Query System E2E Verified + Camera Movement Bug

## Task(s)

- **E2E testing of Phase 1 & 2 debug queries**: **Complete.** Both phases verified with a live connected client. Player position, color, and camera state are all queryable with real non-default data.
- **Bug fix — `#[reflect(Component)]` missing**: **Complete.** Server BRP queries returned empty components because `PlayerPosition` and `PlayerColor` had `#[reflect(Serialize, Deserialize)]` but were missing `Component`. Fixed to `#[reflect(Component, Serialize, Deserialize)]`.
- **Template CLAUDE.md updated**: **Complete.** Added `Reflect` as standard derive pattern for all new components/resources so Claude always makes types debuggable.
- **Phase 3 — Harness → Client bridge**: **Not started.** Plan is in `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` starting at line 613.
- **Camera bug investigation**: **Not started.** WASD moves the capsule mesh but the camera does not follow. This is the primary next task.

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md`
- **Previous handoff**: `thoughts/CoreyCole/handoffs/general/2026-02-11_20-23-25_debug-apis-phase1-2-complete.md`

## Recent changes

All changes are uncommitted on `main`. Changes since the previous handoff:

- `template/shared/src/protocol.rs:52` — Changed `#[reflect(Serialize, Deserialize)]` to `#[reflect(Component, Serialize, Deserialize)]` on `PlayerPosition`
- `template/shared/src/protocol.rs:65` — Changed `#[reflect(Serialize, Deserialize)]` to `#[reflect(Component, Serialize, Deserialize)]` on `PlayerColor`
- `template/CLAUDE.md:95-125` — Updated "Adding New Replicated Components" to always include `Reflect` + `#[reflect(Component, Serialize, Deserialize)]` + `register_type`
- `template/CLAUDE.md:108-115` — Added new "Adding New Resources" section with `Reflect` + `#[reflect(Serialize)]` pattern
- `template/CLAUDE.md:117-147` — Expanded "Debugging" section to document the debug query system (server BRP + client JS bridge)

## Learnings

1. **`#[reflect(Component)]` is required for BRP component queries**: When you have `#[derive(Component, Reflect)]` with `#[reflect(Serialize, Deserialize)]`, the explicit `#[reflect(...)]` attribute overrides automatic `ReflectComponent` registration. You must include `Component` in the reflect attribute: `#[reflect(Component, Serialize, Deserialize)]`. Without this, BRP returns `"Component isn't reflectable"` errors.

2. **Self-signed WSS cert acceptance in Chrome**: Lightyear generates a new self-signed cert on each server start. To connect via browser: navigate to `https://127.0.0.1:<GAME_PORT>`, hit the Chrome cert interstitial, type `thisisunsafe` (hidden Chrome bypass). Must repeat after each server restart. The persistent Chrome profile (`--persistent` flag) remembers the acceptance for a given cert.

3. **E2E test setup for standalone testing** (no harness build pipeline):
   - Build server: `cd template && cargo build --release -p server`
   - Build WASM client: `cd template/client && trunk build --release`
   - Start server: `GAME_PORT=9001 BRP_PORT=10001 template/target/release/server`
   - Serve WASM: `cd template/client/dist && python3 -m http.server 8081 --bind 127.0.0.1`
   - Open in playwright: `playwright-cli open http://localhost:8081/index.html?server_port=9001 --headed --persistent`
   - Accept cert: navigate to `https://127.0.0.1:9001`, type `thisisunsafe`, go back to game URL

4. **Phase 1 `list` query returns many Bevy internal types**: The `ReflectSerialize` filter was meant to show only user types, but Bevy registers `ReflectSerialize` on hundreds of its own types (Vec3, Color, etc.). Minor cosmetic issue — doesn't affect functionality.

5. **Camera bug observed**: WASD moves the player capsule (confirmed via `PlayerPosition` debug query changing values) but the camera stays fixed. The `FlyCameraState` resource stays at `{yaw: 0, pitch: 0}` after WASD input but does change with right-click mouse drag. This suggests the camera follows mouse rotation but doesn't follow the player's movement — likely needs a camera follow system or the fly camera system needs to update its position based on the player entity.

## Artifacts

- `template/shared/src/protocol.rs:52,65` — Fixed `#[reflect(Component, ...)]` attributes
- `template/CLAUDE.md:95-147` — Updated component/resource/debugging documentation
- `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` — Full plan (Phase 3 still relevant)
- `template/client/src/debug.rs` — WASM debug query engine (unchanged from previous handoff)

## Action Items & Next Steps

1. **Debug camera movement bug** — Primary task. WASD moves the capsule but camera stays fixed. Investigate `template/client/src/main.rs` to understand:
   - How `FlyCameraState` (yaw/pitch) relates to camera transform
   - Whether camera position tracks the player entity or is independent
   - The `FlyCamera` system — does it update position or only rotation?
   - Use the debug query system to inspect state: `{type: "resource", name: "FlyCameraState"}` for camera state, `{type: "query", components: ["PlayerPosition"]}` for player position

2. **Implement Phase 3** — SSE round-trip for client debug queries without playwright. Plan at `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md:613`.

3. **Commit all changes** — Everything is still uncommitted on `main`.

## Other Notes

- The debug query system is fully functional for both server (BRP) and client (JS bridge). Use it to investigate the camera bug.
- Server BRP query for player data: `curl -X POST http://localhost:10001 -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition","shared::protocol::PlayerColor"]}}}'`
- Client debug query via playwright: set `window.__debugRequest = JSON.stringify({type: "query", components: ["PlayerPosition"]})`, wait 100ms, read `window.__debugResponse`
- BRP port is `GAME_PORT + 1000` (e.g., game on 9001 → BRP on 10001)
- `PlayerId` intentionally skips `Reflect` — wraps Lightyear's `PeerId` (foreign type)
- `cargo clippy --workspace` passes clean with all changes
