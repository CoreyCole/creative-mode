---
date: 2026-02-11T20:04:20-08:00
researcher: CoreyCole
git_commit: 292e8b1bff85c74ece58f951ce0e95b07579fa26
branch: main
repository: creative-mode
topic: "Template Debug Query System - ECS Debug APIs for Autonomous Agents"
tags: [implementation, bevy, debug, bevy_remote, wasm, ecs, playwright]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Template Debug Query System

## Task(s)

**Status: Plan complete, implementation not started.**

Designed a three-phase debug query system that lets Claude agents probe Bevy ECS state from both the game server (native) and game client (WASM). The plan went through two rounds of review — the original plan used a custom axum HTTP server for Phase 2, which was replaced with Bevy's built-in `bevy_remote` (BRP) after research confirmed it ships in Bevy 0.18 and doesn't conflict with Lightyear.

- **Phase 1 — Client JS Bridge**: Custom reflection-based query engine in `client/src/debug.rs` with `window.__debugRequest`/`__debugResponse` JS globals, polled each frame by an exclusive Bevy system. Queryable via playwright. *Status: planned*
- **Phase 2 — Server BRP**: Enable `bevy_remote` feature flag on server, add `RemotePlugin` + `RemoteHttpPlugin`. Harness proxies `POST /world/:worldID/debug` to BRP port. *Status: planned*
- **Phase 3 — Harness → Client Bridge**: SSE `ExecuteScript` round-trip — harness sends JS to browser via SSE, browser forwards query to iframe via `postMessage`, iframe queries WASM ECS, posts result back. Endpoint: `POST /world/:worldID/client-debug`. *Status: planned*

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` — full plan with code snippets for all three phases
- **Template CLAUDE.md**: `template/CLAUDE.md` — Bevy/Lightyear architecture, build instructions, required `CHANGES.txt`

## Recent changes

No code changes — this session was plan review and refinement only. The plan document was rewritten to incorporate `bevy_remote` and the Phase 3 SSE round-trip.

## Learnings

1. **`bevy_remote` ships in Bevy 0.18** behind the `bevy_remote` feature flag. It provides 18 JSON-RPC 2.0 methods (query, get/set resources, spawn, schema, etc.) using smol/async-io (NOT tokio), so it coexists with Lightyear's tokio-based WebSocket transport without conflict. Works with `MinimalPlugins`. Does NOT compile to WASM.

2. **QueryBuilder API confirmed for Bevy 0.18**: `QueryBuilder::<FilteredEntityRef>::new(&mut world)` with `.ref_id(ComponentId)` exists. Important: do NOT wrap in tuple `(FilteredEntityRef,)` — known bug causes empty access. Use `FilteredEntityRef` directly.

3. **`PlayerId` cannot derive `Reflect`** — it wraps Lightyear's `PeerId`, a foreign type without `Reflect`. Skip it from reflection. `PlayerPosition` and `PlayerColor` are the useful queryable components.

4. **`mod` declarations can't go inside `#[cfg]` blocks** — `#[cfg(target_family = "wasm")] { mod debug; }` is invalid. Must be top-level: `#[cfg(target_family = "wasm")] mod debug;`.

5. **`list` query must filter by `ReflectSerialize`** — without filtering, Bevy registers hundreds of internal types. Filtering to types with `ReflectSerialize` returns only explicitly opted-in types.

6. **BRP uses full type paths** like `shared::protocol::PlayerPosition`, not short names. The client JS bridge uses short names (`PlayerPosition`). Two different protocols for two different transports.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` — full implementation plan (updated)

## Action Items & Next Steps

1. **Implement Phase 1** — Client JS bridge:
   - Add `serde_json` to workspace and client deps, `js-sys` to client deps
   - Add `#[reflect(Serialize, Deserialize)]` to `PlayerPosition`, `PlayerColor`, `PlayerInput` in `template/shared/src/protocol.rs`
   - Register types in `ProtocolPlugin::build()`
   - Add `Reflect` + `Serialize` to `FlyCameraState` in `template/client/src/main.rs`
   - Create `template/client/src/debug.rs` with query engine + JS bridge
   - Add `#[cfg(target_family = "wasm")] mod debug;` and system registration
   - Verify with playwright

2. **Implement Phase 2** — Server BRP:
   - Add `"bevy_remote"` to server's bevy features in `template/server/Cargo.toml`
   - Add `RemotePlugin` + `RemoteHttpPlugin` to server main with configurable BRP_PORT
   - Pass `BRP_PORT` env var from harness in `game_server.go`
   - Add `BRPPort` field to `GameServer` struct and `GetServer()` helper
   - Add proxy route `POST /world/:worldID/debug` in harness server
   - Verify with curl

3. **Implement Phase 3** — Harness → Client bridge:
   - Add `postMessage` listener script to iframe HTML
   - Create `harness/internal/server/debug.go` with pending query map, `handleClientDebug`, `handleClientDebugResponse`
   - Register routes in `registerWorldRoutes()`
   - Handle `execute_script` events in world SSE handler
   - Verify with curl (requires connected browser)

## Other Notes

- The plan document has full code snippets for each change — follow them closely, especially the unsafe blocks in the query engine which have specific safety invariants documented.
- `bevy_remote` defaults to port 15702, but since multiple game servers can run, we pass `BRP_PORT = GAME_PORT + 1000` to avoid collisions.
- The harness port allocator uses range 9001-9999 for GAME_PORT, so BRP ports will be 10001-10999.
- `PlayerColor(pub Color)` with `#[reflect(Serialize)]` will produce Bevy's verbose `Color` enum serialization (e.g., `{"Hsla": {"hue": 210, ...}}`), not simple `{"r", "g", "b"}`.
- The Phase 3 client-debug endpoint broadcasts the SSE event to ALL connected browsers for a world — first response wins. Future work could scope to a specific user/session.
