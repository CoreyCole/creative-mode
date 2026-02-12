---
date: 2026-02-11T20:23:25-08:00
researcher: CoreyCole
git_commit: b75915e05835b2eae747daeb97976d870c31018e
branch: main
repository: creative-mode
topic: "Template Debug Query System - Phases 1 & 2 Implemented"
tags: [implementation, bevy, debug, bevy_remote, wasm, ecs, playwright]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Debug Query System — Phases 1 & 2 Complete, E2E Testing Next

## Task(s)

Implemented the first two phases of the three-phase debug query system from the plan at `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md`.

- **Phase 1 — Client JS Bridge**: **Complete.** Custom query engine in `template/client/src/debug.rs` with `window.__debugRequest`/`__debugResponse` JS globals. Supports `list`, `resource`, and `query` commands. Registered on WASM target only.
- **Phase 2 — Server BRP**: **Complete.** `bevy_remote` feature enabled on server with `RemotePlugin` + `RemoteHttpPlugin`. Harness proxies `POST /world/:worldID/debug` to BRP port. `GameServer` struct updated with `BRPPort` field.
- **Phase 3 — Harness → Client Bridge**: **Not started.** SSE `ExecuteScript` round-trip design is in the plan doc but no code written.

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` — full plan with code snippets for all three phases
- **Previous handoff**: `thoughts/CoreyCole/handoffs/general/2026-02-11_20-04-20_template-debug-apis.md` — original plan-only handoff with learnings

## Recent changes

All changes are uncommitted on `main`. Files modified:

**Rust (template/):**
- `template/Cargo.toml:6` — Added `serde_json = "1"` to workspace deps
- `template/shared/Cargo.toml:9` — Added `serde_json` dep
- `template/client/Cargo.toml:9-11` — Added `serde`, `serde_json`, `js-sys` deps
- `template/shared/src/protocol.rs:51-52` — Added `#[reflect(Serialize, Deserialize)]` to `PlayerPosition`
- `template/shared/src/protocol.rs:65-66` — Added `Reflect` derive and `#[reflect(Serialize, Deserialize)]` to `PlayerColor`
- `template/shared/src/protocol.rs:75-76` — Added `#[reflect(Serialize, Deserialize)]` to `PlayerInput`
- `template/shared/src/protocol.rs:97-99` — Registered `PlayerPosition`, `PlayerColor`, `PlayerInput` types in `ProtocolPlugin::build()`
- `template/server/Cargo.toml:15` — Added `"bevy_remote"` feature
- `template/server/src/main.rs:8` — Added `use bevy::remote::{http::RemoteHttpPlugin, RemotePlugin}`
- `template/server/src/main.rs:26-29` — Parse `BRP_PORT` env var
- `template/server/src/main.rs:73-78` — Added `RemotePlugin` + `RemoteHttpPlugin` plugins
- `template/client/src/debug.rs` — **New file**: query engine + JS bridge (166 lines)
- `template/client/src/main.rs:6-7` — Added `#[cfg(target_family = "wasm")] mod debug;`
- `template/client/src/main.rs:19` — Added `use serde::Serialize;`
- `template/client/src/main.rs:168-169` — Added `Reflect`, `Serialize`, `#[reflect(Serialize)]` to `FlyCameraState`
- `template/client/src/main.rs:105` — Added `app.register_type::<FlyCameraState>()`
- `template/client/src/main.rs:108-109` — Added debug system registration (WASM only)

**Go (harness/):**
- `harness/internal/world/game_server.go:17` — Added `brpPortOffset` constant
- `harness/internal/world/game_server.go:22` — Added `BRPPort int` field to `GameServer`
- `harness/internal/world/game_server.go:82-89` — Added `GetServer()` helper method
- `harness/internal/world/game_server.go:136-139` — Pass `BRP_PORT` env var in `startServer()`
- `harness/internal/server/server.go:35` — Added `debugProxyTimeout` constant
- `harness/internal/server/server.go:172` — Registered `POST /:worldID/debug` route
- `harness/internal/server/server.go:562-599` — Added `handleDebugProxy()` handler

## Learnings

1. **Bevy 0.18 API differences from plan**: Three corrections were needed vs the original plan:
   - `FilteredEntityRef` lives at `bevy::ecs::world::FilteredEntityRef`, NOT `bevy::ecs::query::FilteredEntityRef`
   - `Serializable` (from `ReflectSerialize.get_serializable()`) implements `Deref` but not `Borrow`. Use `serializable.deref()` not `.borrow()`. Requires `use core::ops::Deref;`
   - `Entity::index()` returns `EntityIndex` (not `u32`). Use `Entity::index_u32()` for JSON serialization.

2. **golangci-lint rules to watch**: The harness lint config enforces `golines` (line length), `noctx` (must use `NewRequestWithContext` not `client.Post`), `mnd` (magic number detection), and `errcheck` (must handle `resp.Body.Close()` errors with `_ =`).

3. **All builds pass clean**: `cargo clippy --workspace`, `cargo build -p client --target wasm32-unknown-unknown`, `go build ./...`, and `just lint` all pass with zero warnings.

## Artifacts

- `template/client/src/debug.rs` — New file: WASM debug query engine + JS bridge
- `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` — Full implementation plan (Phase 3 still relevant)

## Action Items & Next Steps

1. **E2E test with playwright** — The primary next step. Verify the debug system works end-to-end:
   - Start the harness dev server (`just harness` or `just dev`)
   - Navigate to a template world, ensure the game client loads in the iframe
   - Use playwright to test Phase 1 (client JS bridge):
     - `snapshot` to find the game iframe
     - Use `run-code` to set `window.__debugRequest` in the iframe and read `window.__debugResponse`
     - Test `{"type": "list"}`, `{"type": "resource", "name": "FlyCameraState"}`, `{"type": "query", "components": ["PlayerPosition"]}`
   - Use curl to test Phase 2 (server BRP via harness proxy):
     - `POST /world/<worldID>/debug` with `{"jsonrpc":"2.0","method":"world.list_resources","id":1}`
     - `POST /world/<worldID>/debug` with `{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition"]}}}`
   - Verify both server and client state are accessible

2. **Implement Phase 3** — Harness → Client Bridge (SSE round-trip). Full plan with code snippets is in `thoughts/CoreyCole/plans/2026-02-11_16-26-52_template-debug-apis.md` starting at line 613.

3. **Commit changes** — All changes are currently uncommitted.

## Other Notes

- The `bevy_remote` BRP server defaults to port 15702, but we use `GAME_PORT + 1000` to avoid collisions when multiple game servers run. Port allocator uses 9001-9999 for game ports, so BRP ports are 10001-10999.
- The Phase 1 debug system is a no-op when idle — it checks one JS global per frame and returns immediately if null.
- `PlayerColor(pub Color)` with `#[reflect(Serialize)]` produces Bevy's verbose `Color` enum serialization (e.g., `{"Hsla": {"hue": 210, ...}}`), not simple RGB.
- `PlayerId` is intentionally skipped from `Reflect` — it wraps Lightyear's `PeerId` (foreign type without `Reflect`).
- The `list` query filters by `ReflectSerialize` to return only explicitly opted-in types, not hundreds of Bevy internals.
- Playwright testing tips are in the root `CLAUDE.md` under "E2E Testing Tips" — notably, `fill`/`keyboard.type()` don't update Datastar signal bindings, and `--headed --persistent` flags are required for `playwright-cli open`.
