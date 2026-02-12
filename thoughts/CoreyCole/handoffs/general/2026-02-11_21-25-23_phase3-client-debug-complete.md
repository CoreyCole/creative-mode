---
date: 2026-02-11T21:25:23-08:00
researcher: CoreyCole
git_commit: 201f5081dee64d82cff5dd16bb4498e2c5ae0c93
branch: main
repository: creative-mode
topic: "Phase 3 Client Debug SSE Round-Trip — Complete & E2E Verified"
tags: [implementation, bevy, debug, sse, postmessage, wasm, ecs, camera-bug]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Phase 3 Client Debug + Camera Bug Confirmed

## Task(s)

- **Phase 3 — Harness → Client debug bridge**: **Complete and E2E verified.** SSE round-trip allows querying WASM ECS state from curl via `POST /world/:worldID/client-debug`, no playwright needed. Implementation follows the plan at `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md:613`.
- **Camera movement bug investigation**: **Confirmed root cause via Phase 3 debug data.** `shared_movement()` in `template/shared/src/protocol.rs:126-133` has no delta-time scaling — runs at 64Hz fixed timestep, so player moves at 640 units/sec while the fly camera moves at 10 units/sec (64x mismatch). Fix identified but **not yet applied**.
- **bevy-debug skill created**: **Complete.** Consolidated all debugging tips into `.claude/skills/bevy-debug/SKILL.md`.
- **Commit all changes**: **Not done.** Everything remains uncommitted on `main`.

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` (Phase 1-3 design)
- **Previous handoff**: `thoughts/CoreyCole/handoffs/general/2026-02-11_20-48-14_debug-apis-e2e-verified.md` (Phase 1-2 complete)

## Recent changes

All changes are uncommitted on `main`. Changes since the previous handoff:

### Phase 3 implementation (harness)
- `harness/internal/server/debug.go` (new) — `handleClientDebug` (SSE round-trip initiator with pending query map) and `handleClientDebugResponse` (browser callback receiver)
- `harness/internal/events/types.go:13` — Added `EventExecuteScript = "execute_script"` constant
- `harness/internal/server/events.go:288-293` — Added `execute_script` case to `handleWorldEvent` that calls `sse.ExecuteScript(script)`
- `harness/internal/server/server.go:174-175` — Registered `POST /:worldID/client-debug` and `POST /:worldID/client-debug-response` routes

### Phase 3 implementation (client)
- `template/client/index.html:11-31` — Added postMessage bridge script that relays `debug-query` messages from parent harness overlay to WASM `__debugRequest`/`__debugResponse` globals, polls for response, and `postMessage`s result back

### Skill
- `.claude/skills/bevy-debug/SKILL.md` (new) — Debug query reference guide with three query methods, workflows, setup requirements, and gotchas

## Learnings

1. **Phase 3 SSE round-trip flow**: `POST /client-debug` → EventBus publishes `execute_script` → world SSE handler calls `sse.ExecuteScript(js)` → browser JS postMessages to `#game-frame` iframe → iframe bridge writes `__debugRequest`, polls `__debugResponse` → result postMessaged back to parent → parent JS `fetch`es `POST /client-debug-response?id=<queryID>` → pending channel unblocks → original request returns result. Latency ~50-100ms.

2. **Camera bug root cause confirmed with data**: After holding W for 2 seconds, `PlayerPosition` moved from `[0,0,0]` to `[0,0,-1280]` (640 units/sec). The fly camera moves at 10 units/sec. The fix is `position.0 += input.movement * speed * (1.0 / FIXED_TIMESTEP_HZ as f32)` in `shared_movement()` at `template/shared/src/protocol.rs:132`. This is deterministic since both sides use the same constant.

3. **Trunk `--public-url ./` is required** when the WASM client is served from a subdirectory (e.g., `/wasm/<worldID>/<cpID>/`). Without it, Trunk generates absolute paths (`/client-xxx.js`) that 404 in the iframe context.

4. **Harness overlay blocks iframe clicks**: The overlay div has `pointer-events-none` on the parent but child elements get `pointer-events-auto`. A `flex-1` spacer div intercepts clicks on the game area. Workarounds: minimize overlay first ("—" button), or use `{force: true}` in playwright clicks.

5. **`FlyCameraState` only stores yaw/pitch (rotation)**, not position. To get camera position, query `Transform` on the `FlyCamera` entity. The `FlyCameraState` staying at `{yaw:0, pitch:0}` after WASD is expected — WASD changes `transform.translation`, not yaw/pitch.

6. **Docker dev rebuild needed after new Go files**: The harness runs in Docker via `docker-compose.yml`. New Go files (like `debug.go`) are volume-mounted but the running binary doesn't include them until `POST /dev/rebuild` is triggered or the file watcher picks them up.

## Artifacts

- `harness/internal/server/debug.go` — Phase 3 client debug handlers (pending query map, SSE round-trip, browser callback)
- `harness/internal/events/types.go:13` — `EventExecuteScript` constant
- `harness/internal/server/events.go:288-293` — `execute_script` event handler in world SSE loop
- `harness/internal/server/server.go:174-175` — Route registration for client-debug endpoints
- `template/client/index.html:11-31` — postMessage bridge script in WASM iframe HTML
- `.claude/skills/bevy-debug/SKILL.md` — Consolidated debug query skill (3 query methods, workflows, gotchas)
- `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` — Full 3-phase plan (all phases now implemented)

## Action Items & Next Steps

1. **Fix the camera movement bug** — Apply delta-time scaling in `shared_movement()` at `template/shared/src/protocol.rs:132`. Change `position.0 += input.movement * speed` to `position.0 += input.movement * speed * (1.0 / FIXED_TIMESTEP_HZ as f32)`. This makes player movement ~10 units/sec (matching fly camera), ~30 units/sec with sprint. Verify with Phase 3 debug queries after the fix.

2. **Commit all changes** — Everything from the previous handoff (Phase 1-2) plus this session (Phase 3 + skill) is still uncommitted. Consider logical commit grouping:
   - Phase 1-2 debug system (reflect fixes, BRP, JS bridge, harness proxy)
   - Phase 3 client-debug SSE round-trip
   - bevy-debug skill
   - Camera bug fix (after implementing)

3. **E2E verify camera fix** — After applying the fix, rebuild WASM (`trunk build --release --public-url ./`), copy to `data/wasm-builds/`, restart game server, and use Phase 3 queries to confirm PlayerPosition moves at ~10 units/sec instead of 640.

## Other Notes

- **Standalone testing setup** (no harness build pipeline): Build server `cargo build --release -p server`, build WASM `trunk build --release --public-url ./`, copy dist to `data/wasm-builds/<worldID>/<cpID>/`, set `server_port` in SQLite checkpoint row, start game server with `GAME_PORT=9001`, accept WSS cert in browser.
- **BRP port**: Always `GAME_PORT + 1000` (e.g., game on 9001 → BRP on 10001).
- **Phase 3 query protocols differ from BRP**: Client uses `{type, name/components}` JSON. Server BRP uses JSON-RPC 2.0 with full type paths (`shared::protocol::PlayerPosition` vs `PlayerPosition`).
- The `list` query on the client returns hundreds of Bevy internal types. Filter for custom types by checking for absence of `::` in the type name.
- `PlayerId` intentionally skips `Reflect` — wraps Lightyear's `PeerId` (foreign type).
- The harness `data/` directory is at project root (volume-mounted to `/app/data/` in Docker), not inside `harness/`.
