# Creative Mode - Game World

This is a Bevy 0.18 + Lightyear 0.26 multiplayer game. Server-authoritative architecture with client-side prediction and interpolation, communicating over WebSocket using Lightyear's netcode protocol.

## Structure

| Crate | Purpose | Runs as |
|-------|---------|---------|
| `shared/` | Protocol definitions (components, inputs, channels, movement logic) | Library — compiled into both server and client |
| `server/` | Headless game server | Native binary (`target/release/server`) |
| `client/` | Game client with 3D rendering | WASM via Trunk (loaded in browser iframe) |

## Architecture

### Client ↔ Server Connection

Client and server are **separate processes** with **separate ECS worlds**. They do NOT share memory. The `shared` crate is a compile-time dependency only — both sides import it to agree on types and deterministic logic.

```
Client (WASM in browser)              Server (native binary)
========================              =====================
Bevy ECS (own world)                  Bevy ECS (own world)
    |                                     |
Lightyear ClientPlugins               Lightyear ServerPlugins
    |                                     |
    +--- WebSocket (GAME_PORT) -----------+
         Lightyear netcode protocol
```

**Client → Server**: `PlayerInput` structs (movement direction + sprint flag), sent every tick via Lightyear's input plugin.

**Server → Client**: Component replication diffs (`PlayerPosition`, `PlayerColor`, `PlayerId`) for ALL players, sent every 100ms.

### Server-Authoritative Movement

The same `shared_movement()` function (`shared/src/protocol.rs`) runs on both sides but on different entities:

- **Server**: runs on the authoritative entity — source of truth
- **Client**: runs on a local "predicted" shadow entity — instant feedback

If the client's prediction diverges from the server, Lightyear does **rollback + replay**: rewinds the predicted entity to the last confirmed server state, then replays buffered inputs.

### Entity Replication

The server spawns the real entity. Lightyear automatically creates shadow copies on each client:

```
Server spawns:  PlayerBundle { id, position, color }
                + Replicate (to all clients)
                + PredictionTarget (to owning client only)
                + InterpolationTarget (to everyone else)

Client A gets:  Predicted shadow entity (local player)
                - runs shared_movement() locally for instant feedback
                - Lightyear reconciles with server via rollback

Client B gets:  Interpolated shadow entity (remote player)
                - smoothly lerps between server position updates
                - uses PlayerPosition::Ease impl (Vec3::lerp)
```

There are 3+ copies of each player entity across the network — one authoritative on the server, one predicted on the owning client, one interpolated on every other client.

### Constants (`shared/src/protocol.rs`)

| Constant | Value | Purpose |
|----------|-------|---------|
| `FIXED_TIMESTEP_HZ` | 64.0 | Physics/game tick rate (both sides) |
| `REPLICATION_INTERVAL` | 100ms | Server → client state update frequency |
| `MOVE_SPEED` | 10.0 | Units per tick (30.0 when sprinting) |
| `PROTOCOL_ID` | 0 | Netcode authentication (must match) |
| `PRIVATE_KEY` | `[0; 32]` | Netcode authentication (must match) |

### Key Source Files

| File | What it defines |
|------|----------------|
| `shared/src/protocol.rs` | `PlayerBundle`, `PlayerId`, `PlayerPosition`, `PlayerColor`, `PlayerInput`, `shared_movement()`, `ProtocolPlugin` |
| `server/src/main.rs` | Headless server: connection handling, spawns player entities, runs `shared_movement()` authoritatively |
| `client/src/main.rs` | WASM client: scene rendering, camera, input buffering, prediction, mesh sync |

## Building

- Server: `cargo build --release -p server`
- Client: `cd client && trunk build --release`
  - Trunk handles cargo build → wasm-bindgen → wasm-opt → index.html
  - Output goes to client/dist/ by default
- Both: `cargo build --release` (builds entire workspace)
- Check: `cargo clippy --workspace`

### wasm-bindgen Pin

`wasm-bindgen` is pinned to exactly `0.2.108` in both `Cargo.toml` and `client/Trunk.toml`. These MUST match. If you update one, update the other.

### public_url

`client/Trunk.toml` sets `public_url = "./"` so that `trunk build` generates relative asset paths (`./client-*.js`) instead of root-absolute (`/client-*.js`). This is required for static WASM serving — template worlds are served at `/wasm/{worldID}/{cpID}/`, and root-absolute paths would 404.

## Adding New Replicated Components

1. Define the component in `shared/src/protocol.rs`. **Always** include `Reflect` and `#[reflect(Component, Serialize, Deserialize)]` so it's queryable via the debug system:
   ```rust
   #[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq, Reflect)]
   #[reflect(Component, Serialize, Deserialize)]
   pub struct MyComponent(pub f32);
   ```
2. Register in `ProtocolPlugin::build()`:
   ```rust
   app.register_component::<MyComponent>(ChannelDirection::ServerToClient)
       .add_prediction()           // if client should predict it
       .add_linear_interpolation() // if remote clients should interpolate it
   app.register_type::<MyComponent>();  // required for debug queries
   ```
3. If interpolated, implement `Ease` via `Curve<Self>` (see `PlayerPosition` for example)
4. Add to the entity bundle spawned in `server/src/main.rs` `handle_connected()`
5. Handle the component on the client side in spawn observers and sync systems

## Adding New Resources

Always include `Reflect` and `#[reflect(Serialize)]` so resources are queryable via the debug system:
```rust
#[derive(Resource, Default, Reflect, Serialize)]
#[reflect(Serialize)]
struct MyResource {
    value: f32,
}
```
Register in `main()`: `app.register_type::<MyResource>();`

## Adding New Inputs

1. Add fields to `PlayerInput` in `shared/src/protocol.rs`
2. Update `buffer_input()` in `client/src/main.rs` to read the new input
3. Update `shared_movement()` (or add new shared functions) to use the new input
4. Call the shared function from both `server/src/main.rs` `movement()` and `client/src/main.rs` `client_movement()`

The shared function MUST be deterministic — same input → same output on both server and client, or prediction will constantly desync and rollback.

## Debugging

### Debug Query System

Both server and client ECS state are queryable at runtime via HTTP. Any type with `Reflect` + `Serialize` + `register_type` is automatically visible.

**Server state** (via Bevy Remote Protocol — JSON-RPC 2.0):

Via debug CLI (recommended):
```bash
just debug $WORLD query shared::protocol::PlayerPosition
just debug $WORLD resources
just debug $WORLD components 42
```

Via dev server directly (no auth needed, if `$CM_BRP_PORT` is set):
```bash
curl -s -X POST http://localhost:$CM_BRP_PORT \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition"]}}}'
```

**Client state** (via playwright JS bridge):
```bash
# In playwright, set window.__debugRequest and read window.__debugResponse
# Supported queries: {type: "list"}, {type: "resource", name: "..."}, {type: "query", components: ["..."]}
```

**Making types debuggable:** Add `Reflect`, the appropriate `#[reflect(...)]` attrs, and call `app.register_type::<T>()`. See "Adding New Replicated Components" and "Adding New Resources" above. Components need `#[reflect(Component, Serialize, Deserialize)]`. Resources need `#[reflect(Serialize)]`.

### Logs

Log files are at `$CM_LOG_DIR/` (set by the harness):
- Game server logs (raw text, tail for runtime issues):
  `tail -f $CM_LOG_DIR/game-server.log`
- Build logs (JSONL format, check for compile errors):
  `tail -f $CM_LOG_DIR/build.jsonl | jq .`
- Harness server log:
  `tail -f data/logs/harness.jsonl | jq .`

When debugging a crash or unexpected behavior, ALWAYS check game-server.log first.

## Dev Server (Live Feedback)

A dev server runs alongside your editing session via `cargo watch` in a
separate tmux session. It auto-rebuilds when you edit `shared/` or `server/` files.

### Querying Game State (no auth needed)

```bash
# Check if dev server is running
curl -sf http://localhost:$CM_BRP_PORT > /dev/null && echo "UP" || echo "DOWN"

# Query player positions
curl -s -X POST http://localhost:$CM_BRP_PORT \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.query","id":1,
       "params":{"data":{"components":["shared::protocol::PlayerPosition"]}}}' | jq .

# List all resources
curl -s -X POST http://localhost:$CM_BRP_PORT \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.list_resources","id":1}' | jq .
```

- Dev server runs in **debug mode** (fast compile, fine for ECS queries)
- Only server code auto-rebuilds. Client WASM builds after your session ends.
- Wait a few seconds after editing for cargo watch to rebuild, then query BRP.

## Key Patterns

- All replicated components go in `shared/src/protocol.rs`
- Server is authoritative — client sends inputs, server applies them
- Use Lightyear's `Replicate` bundle for entity sync
- Assets load from HTTP: `asset_server.load("http://{harness_host}/assets/...")`
- Do NOT use `copy-dir` in client/index.html for assets — they are served separately
- Camera-relative input: client computes movement in camera space and sends the world-space direction vector. Server applies it directly without needing camera orientation.

## Mayor Context

Mayor-triggered builds (via `POST /api/mayor/build`) use the same fork → Claude Code → build pipeline as browser prompts — no special handling needed.

## CHANGES.txt (Required)

Before you finish, ALWAYS write a brief summary of what you changed to `CHANGES.txt`
in the project root. This is shown to users in the UI as context for their next prompt.

Keep it concise (2-4 sentences). Describe WHAT you built/changed and WHY, not which
files you edited. Example:
```
Added Perlin noise terrain generation with green grass material and rolling hill
geometry. Hills have configurable amplitude and frequency for natural-looking terrain.
The tallest hill is tracked so future prompts can reference "the highest point."
```

## MEMORY.md

Read MEMORY.md for this world's design decisions and history.
