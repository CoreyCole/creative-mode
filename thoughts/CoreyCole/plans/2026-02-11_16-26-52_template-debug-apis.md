# Template Debug Query System

## Overview

Three-phase debug system for probing Bevy ECS state from autonomous agents:

1. **Phase 1 — Client JS bridge**: Custom `window.__debugRequest`/`__debugResponse` interface in WASM, queryable via playwright
2. **Phase 2 — Server BRP**: `bevy_remote` built-in Bevy Remote Protocol — JSON-RPC 2.0 over HTTP, queryable via harness proxy
3. **Phase 3 — Harness → Client bridge**: SSE `ExecuteScript` + `postMessage` round-trip, making client state queryable from the harness (and Claude) without playwright

Any type deriving `Reflect` + `Serialize` and registered via `app.register_type::<T>()` is automatically queryable.

## Current State

### Server (`template/server/src/main.rs`)

- Headless Bevy 0.18 app running at 64Hz via `ScheduleRunnerPlugin`
- Only network interface: Lightyear WebSocket on `GAME_PORT` (env var, default 9001)
- No HTTP server, no debug endpoints, no way to query ECS state from outside
- Tracks: player entities (`PlayerId`, `PlayerPosition`, `PlayerColor`), connection events
- Uses `MinimalPlugins` — no rendering, no window

### Client (`template/client/src/main.rs`)

- Bevy WASM app rendering to a fullscreen `<canvas>` inside an iframe
- Already imports: `wasm-bindgen` 0.2.108, `web_sys` (Window, Location, UrlSearchParams, Crypto)
- No `#[wasm_bindgen]` exports, no `js-sys` usage
- Canvas is opaque to playwright — can't inspect scene graph via DOM
- State available: camera position/rotation (`FlyCameraState`, `FlyCamera` transform), predicted/interpolated player entities

### Shared Protocol (`template/shared/src/protocol.rs`)

- `PlayerPosition` — derives `Reflect`, `Serialize`, `Deserialize`
- `PlayerId` — derives `Serialize`, `Deserialize` (NOT `Reflect`) — wraps Lightyear `PeerId` (foreign type, can't derive Reflect)
- `PlayerColor` — derives `Serialize`, `Deserialize` (NOT `Reflect`)
- `PlayerInput` — derives `Reflect`, `Serialize`, `Deserialize`
- `bevy` workspace dep has `default-features = false`

### Harness (`harness/internal/world/game_server.go`)

- `GameServerManager` tracks running servers: `map[string]*GameServer{Cmd, Port, WorldID, CPID}`
- Port allocator: 9001-9999 range
- Only env var passed to game server: `GAME_PORT`
- No existing proxy/forwarding routes to game servers

## What We're NOT Doing

- Upgrading to Bevy 0.19-dev (would break Lightyear 0.26 compatibility)
- Mutation endpoints (teleport, spawn) — read-only for now
- Authentication on debug endpoints (harness proxy handles auth)
- Entity-by-ID queries (can add later)
- Direct WASM ↔ harness WebSocket (postMessage + SSE round-trip is sufficient)

## Queryability from the Harness

Both server and client state are queryable from the harness:

- **Server state** via `bevy_remote` (Phase 2): Direct HTTP to BRP server. Covers all authoritative game state — player positions, colors, entity counts, etc.
- **Client state** via SSE round-trip (Phase 3): Harness sends `ExecuteScript` via SSE to the browser, which queries the WASM iframe via `postMessage`, then POSTs the result back. Covers client-only state — camera position, predicted/interpolated entities, local visual state.

Claude agents can query either endpoint to understand the world state without needing playwright.

## Desired End State

```bash
# Server state: query player positions via harness proxy (BRP)
curl -X POST http://localhost:8080/world/<worldID>/debug \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition","shared::protocol::PlayerColor"]}}}'
# {"jsonrpc":"2.0","id":1,"result":{"entities":[...]}}

# Client state: query camera from harness (SSE round-trip to browser)
curl -X POST http://localhost:8080/world/<worldID>/client-debug \
  -H 'Content-Type: application/json' \
  -d '{"type": "resource", "name": "FlyCameraState"}'
# {"name": "FlyCameraState", "value": {"yaw": 0.1, "pitch": -0.3}}

# Client state: same query via playwright (direct, no harness round-trip)
playwright-cli run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const canvas = frame.locator('canvas');
  await canvas.evaluate(() => {
    window.__debugRequest = JSON.stringify({type: 'resource', name: 'FlyCameraState'});
  });
  await page.waitForTimeout(50);
  return await canvas.evaluate(() => {
    const r = window.__debugResponse;
    window.__debugResponse = null;
    return JSON.parse(r);
  });
}"
# {"name": "FlyCameraState", "value": {"yaw": 0.1, "pitch": -0.3}}
```

______________________________________________________________________

## Phase 1: Client JS Bridge (Playwright Debug Queries)

### Overview

A reflection-based query engine in `client/src/debug.rs` uses Bevy's `AppTypeRegistry`, `ReflectFromPtr`, and `QueryBuilder` to dynamically access any registered type. Agents write a JSON request to `window.__debugRequest`, an exclusive Bevy system processes it next frame, and writes the result to `window.__debugResponse`.

### Changes

#### 1. Add dependencies

**File**: `template/Cargo.toml` (workspace)

```toml
[workspace.dependencies]
serde_json = "1"
```

**File**: `template/shared/Cargo.toml`

```toml
[dependencies]
serde_json = { workspace = true }
```

**File**: `template/client/Cargo.toml`

```toml
[dependencies]
serde_json = { workspace = true }
js-sys = "0.3"
```

#### 2. Derive Reflect on protocol types that support it

**File**: `template/shared/src/protocol.rs`

Add `Reflect` and `#[reflect(Serialize, Deserialize)]` to types whose inner types also implement `Reflect`. **Skip `PlayerId`** — it wraps Lightyear's `PeerId`, a foreign type that doesn't implement `Reflect`.

```rust
// PlayerId — SKIP Reflect (PeerId is a foreign type)
#[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct PlayerId(pub PeerId);

// PlayerPosition — already has Reflect, add reflect attributes
#[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq, Reflect, Deref, DerefMut)]
#[reflect(Serialize, Deserialize)]
pub struct PlayerPosition(pub Vec3);

// PlayerColor — add Reflect (Bevy's Color implements Reflect)
#[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq, Reflect)]
#[reflect(Serialize, Deserialize)]
pub struct PlayerColor(pub Color);

// PlayerInput — already has Reflect, add reflect attributes
#[derive(Serialize, Deserialize, Debug, Default, PartialEq, Clone, Reflect)]
#[reflect(Serialize, Deserialize)]
pub struct PlayerInput {
    pub movement: Vec3,
    pub sprint: bool,
}
```

Register reflectable types in `ProtocolPlugin::build()`:

```rust
app.register_type::<PlayerPosition>();
app.register_type::<PlayerColor>();
app.register_type::<PlayerInput>();
```

#### 3. Client debug module

**File**: `template/client/src/debug.rs` (new)

Contains both the query engine and JS bridge. The query engine uses `QueryBuilder::<FilteredEntityRef>` with `.ref_id()` for dynamic component access.

```rust
use bevy::prelude::*;
use bevy::ecs::query::{QueryBuilder, FilteredEntityRef};
use bevy::reflect::{ReflectFromPtr, ReflectSerialize, TypeRegistration, TypeRegistry};
use bevy::reflect::serde::ReflectSerializer;
use serde::Deserialize;
use serde_json::{json, Value};
use wasm_bindgen::prelude::*;

// --- Query Protocol ---

#[derive(Deserialize)]
#[serde(tag = "type")]
pub enum DebugQuery {
    #[serde(rename = "list")]
    List,
    #[serde(rename = "resource")]
    Resource { name: String },
    #[serde(rename = "query")]
    Query { components: Vec<String> },
}

// --- JS Bridge ---

/// Exclusive system — checks for a pending JS debug request each frame.
/// No-op when no request is pending (one JS global read).
pub fn process_debug_queries(world: &mut World) {
    let Some(window) = web_sys::window() else { return };

    // Read request from window.__debugRequest
    let Ok(request_val) = js_sys::Reflect::get(
        &window,
        &JsValue::from_str("__debugRequest"),
    ) else {
        return;
    };

    if !request_val.is_string() {
        return;
    }
    let request_str = request_val.as_string().unwrap();

    // Clear request immediately
    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugRequest"),
        &JsValue::NULL,
    );

    // Parse and execute
    let result = match serde_json::from_str::<DebugQuery>(&request_str) {
        Ok(query) => execute_query(world, &query),
        Err(e) => json!({"error": format!("invalid query: {e}")}),
    };

    // Write response
    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugResponse"),
        &JsValue::from_str(&result.to_string()),
    );
}

// --- Query Engine ---

fn execute_query(world: &mut World, query: &DebugQuery) -> Value {
    let registry = world.resource::<AppTypeRegistry>().clone();
    let registry = registry.read();

    match query {
        DebugQuery::List => {
            // Only list types that have ReflectSerialize (filters out Bevy internals)
            let types: Vec<&str> = registry
                .iter()
                .filter(|reg| reg.data::<ReflectSerialize>().is_some())
                .map(|reg| reg.type_info().type_path_table().short_path())
                .collect();
            json!({ "types": types })
        }

        DebugQuery::Resource { name } => {
            let Some(registration) = find_type(&registry, name) else {
                return json!({"error": format!("type '{name}' not found")});
            };
            let type_id = registration.type_id();
            let Some(component_id) = world.components().get_resource_id(type_id) else {
                return json!({"error": format!("'{name}' is not a resource")});
            };
            let Some(ptr) = world.get_resource_by_id(component_id) else {
                return json!({"error": format!("resource '{name}' not present")});
            };
            let Some(reflect_from_ptr) = registration.data::<ReflectFromPtr>() else {
                return json!({"error": format!("'{name}' missing ReflectFromPtr")});
            };
            // SAFETY: ptr type matches — component_id derived from same type_id
            let reflected = unsafe { reflect_from_ptr.as_reflect(ptr) };
            match serialize_reflect(reflected, registration, &registry) {
                Ok(val) => json!({"name": name, "value": val}),
                Err(e) => json!({"error": format!("serialization failed: {e}")}),
            }
        }

        DebugQuery::Query { components } => {
            let mut resolved = Vec::new();
            for name in components {
                let Some(reg) = find_type(&registry, name) else {
                    return json!({"error": format!("type '{name}' not found")});
                };
                let Some(id) = world.components().get_id(reg.type_id()) else {
                    return json!({"error": format!("'{name}' is not a component")});
                };
                resolved.push((id, name.clone(), reg));
            }

            let mut builder = QueryBuilder::<FilteredEntityRef>::new(world);
            for (id, _, _) in &resolved {
                builder.ref_id(*id);
            }
            let mut query_state = builder.build();

            let mut entities = Vec::new();
            for entity_ref in query_state.iter(world) {
                let mut comps = serde_json::Map::new();
                for (id, name, reg) in &resolved {
                    if let Some(ptr) = entity_ref.get_by_id(*id) {
                        if let Some(rfp) = reg.data::<ReflectFromPtr>() {
                            let reflected = unsafe { rfp.as_reflect(ptr) };
                            if let Ok(val) = serialize_reflect(reflected, reg, &registry) {
                                comps.insert(name.clone(), val);
                            }
                        }
                    }
                }
                entities.push(json!({
                    "entity": entity_ref.id().index(),
                    "components": comps,
                }));
            }

            json!({"entities": entities, "count": entities.len()})
        }
    }
}

/// Prefers native Serialize (clean output) via ReflectSerialize,
/// falls back to ReflectSerializer (type-tagged output).
fn serialize_reflect(
    reflected: &dyn Reflect,
    registration: &TypeRegistration,
    registry: &TypeRegistry,
) -> Result<Value, String> {
    if let Some(reflect_serialize) = registration.data::<ReflectSerialize>() {
        let serializable = reflect_serialize.get_serializable(reflected);
        return serde_json::to_value(serializable.borrow()).map_err(|e| e.to_string());
    }
    let serializer = ReflectSerializer::new(reflected, registry);
    serde_json::to_value(&serializer).map_err(|e| e.to_string())
}

fn find_type<'a>(registry: &'a TypeRegistry, name: &str) -> Option<&'a TypeRegistration> {
    registry
        .iter()
        .find(|reg| reg.type_info().type_path_table().short_path() == name)
}
```

#### 4. Register in client main

**File**: `template/client/src/main.rs`

Top-level cfg'd module declaration (NOT inside a block):

```rust
#[cfg(target_family = "wasm")]
mod debug;
```

Add `Reflect` + `Serialize` to `FlyCameraState`:

```rust
use serde::Serialize;

#[derive(Resource, Default, Reflect, Serialize)]
#[reflect(Serialize)]
struct FlyCameraState {
    yaw: f32,
    pitch: f32,
}
```

Register type and system in `main()`:

```rust
app.register_type::<FlyCameraState>();

#[cfg(target_family = "wasm")]
app.add_systems(Update, debug::process_debug_queries);
```

### Verification

- [ ] Client compiles for WASM: `cargo build -p client --target wasm32-unknown-unknown`
- [ ] `{"type": "list"}` returns only debug-visible types (not hundreds of Bevy internals)
- [ ] `{"type": "resource", "name": "FlyCameraState"}` returns `{"yaw": ..., "pitch": ...}`
- [ ] `{"type": "query", "components": ["PlayerPosition"]}` returns player entities
- [ ] Camera values change after WASD input
- [ ] No-op when `__debugRequest` is null (no performance impact)

______________________________________________________________________

## Phase 2: Server Debug via `bevy_remote` + Harness Proxy

### Overview

Bevy 0.18 ships `bevy_remote` — a built-in JSON-RPC 2.0 server for ECS inspection. Instead of building a custom axum debug server, we enable the `bevy_remote` feature flag and add two plugin lines. The harness proxies requests to the BRP port.

**What `bevy_remote` gives us for free (18 built-in methods)**:

| Method | Purpose |
|--------|---------|
| `world.query` | Query entities by component filters |
| `world.get_components` | Read components from an entity |
| `world.list_components` | List all components on an entity |
| `world.get_resources` | Read resources by type path |
| `world.list_resources` | List all resources |
| `world.spawn_entity` | Spawn a new entity |
| `world.insert_components` | Add components to an entity |
| `registry.schema` | Get component/resource schema |
| `rpc.discover` | OpenRPC discovery |

**Why this works alongside Lightyear**:
- `bevy_remote` uses **smol/async-io** (Bevy's `IoTaskPool`), NOT tokio
- Lightyear's WebSocket uses **tokio** — completely independent runtimes
- Different ports, different protocols, no shared state

### Changes

#### 1. Add `bevy_remote` feature to server

**File**: `template/server/Cargo.toml`

```toml
[dependencies]
bevy = { workspace = true, default-features = false, features = [
    "bevy_state",
    "bevy_log",
    "serialize",
    "bevy_asset",
    "bevy_color",
    "multi_threaded",
    "sysinfo_plugin",
    "bevy_remote",
] }
```

#### 2. Add BRP plugins to server main

**File**: `template/server/src/main.rs`

```rust
use bevy::remote::{RemotePlugin, http::RemoteHttpPlugin};

fn main() {
    let port = std::env::var("GAME_PORT")
        .ok()
        .and_then(|p| p.parse::<u16>().ok())
        .unwrap_or(9001);

    let brp_port = std::env::var("BRP_PORT")
        .ok()
        .and_then(|p| p.parse::<u16>().ok())
        .unwrap_or(port + 1000);

    // ... existing setup ...

    // BRP debug server (JSON-RPC 2.0 over HTTP)
    app.add_plugins(RemotePlugin::default());
    app.add_plugins(
        RemoteHttpPlugin::default()
            .with_port(brp_port)
            .with_header("Access-Control-Allow-Origin", "*"),
    );

    // ... rest of existing setup ...
    app.run();
}
```

That's it on the Rust side. No custom query engine, no axum, no tokio, no channels.

#### 3. Pass BRP_PORT from harness

**File**: `harness/internal/world/game_server.go`

Add `BRP_PORT` env var in `startServer()`:

```go
cmd.Env = append(os.Environ(),
    fmt.Sprintf("GAME_PORT=%d", port),
    fmt.Sprintf("BRP_PORT=%d", port+1000),
)
```

Store the BRP port on the `GameServer` struct:

```go
type GameServer struct {
    Cmd     *exec.Cmd
    Port    int    // GAME_PORT
    BRPPort int    // BRP_PORT (GAME_PORT + 1000)
    WorldID string
    CPID    string
}
```

#### 4. Add harness proxy route

**File**: `harness/internal/server/server.go`

Proxy `POST /world/:worldID/debug` to the BRP server. The body is forwarded as-is (JSON-RPC 2.0 format).

```go
func (s *Server) handleDebugProxy(c echo.Context) error {
    worldID := c.Param("worldID")

    user, err := requireUser(c)
    if err != nil {
        return err
    }

    cpID, err := s.WorldManager.GetUserPosition(c.Request().Context(), user.ID, worldID)
    if err != nil || cpID == "" {
        return echo.NewHTTPError(http.StatusNotFound, "no active checkpoint")
    }

    gs := s.WorldManager.GameServers.GetServer(worldID, cpID)
    if gs == nil {
        return echo.NewHTTPError(http.StatusServiceUnavailable, "game server not running")
    }

    targetURL := fmt.Sprintf("http://localhost:%d", gs.BRPPort)

    client := &http.Client{Timeout: 5 * time.Second}
    resp, err := client.Post(targetURL, "application/json", c.Request().Body)
    if err != nil {
        return echo.NewHTTPError(http.StatusBadGateway, "debug server unreachable")
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    return c.JSONBlob(resp.StatusCode, body)
}
```

Register in `registerWorldRoutes()`:

```go
w.POST("/:worldID/debug", s.handleDebugProxy)
```

#### 5. Add `GetServer` helper to GameServerManager

**File**: `harness/internal/world/game_server.go`

```go
func (m *GameServerManager) GetServer(worldID, cpID string) *GameServer {
    key := worldID + "/" + cpID
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.servers[key]
}
```

### Verification

- [ ] Server compiles: `cargo build -p server`
- [ ] No clippy warnings: `cargo clippy -p server`
- [ ] Harness builds: `cd harness && go build ./...`
- [ ] `curl -X POST localhost:<BRP_PORT> -d '{"jsonrpc":"2.0","method":"world.list_resources","id":1}'` returns resources
- [ ] `curl -X POST localhost:8080/world/<id>/debug -d '{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition"]}}}'` returns entities
- [ ] Returns 503 when game server not running
- [ ] Multiple game servers run on different BRP ports without collision

______________________________________________________________________

## BRP Query Examples

### List all resources

```json
{"jsonrpc": "2.0", "method": "world.list_resources", "id": 1}
```

### Get a specific resource

```json
{
  "jsonrpc": "2.0",
  "method": "world.get_resources",
  "id": 1,
  "params": {
    "resources": ["shared::protocol::SomeResource"]
  }
}
```

### Query entities by components

```json
{
  "jsonrpc": "2.0",
  "method": "world.query",
  "id": 1,
  "params": {
    "data": {
      "components": [
        "shared::protocol::PlayerPosition",
        "shared::protocol::PlayerColor"
      ]
    }
  }
}
```

### Get components from a specific entity

```json
{
  "jsonrpc": "2.0",
  "method": "world.get_components",
  "id": 1,
  "params": {
    "entity": 42,
    "components": ["shared::protocol::PlayerPosition"]
  }
}
```

______________________________________________________________________

## Phase 3: Harness → Client Debug Queries (via SSE round-trip)

### Overview

Make client WASM state queryable from the harness (and therefore from Claude). The harness already has `sse.ExecuteScript()` to run arbitrary JS on connected browsers. We use this to execute a debug query in the iframe, then POST the result back.

**Flow**:
```
Claude/API                    Harness                  Browser                    WASM iframe
    |                           |                        |                           |
    |-- POST /client-debug ---->|                        |                           |
    |                           |-- SSE ExecuteScript -->|                           |
    |                           |                        |-- postMessage(query) ---->|
    |                           |                        |                           |-- __debugRequest
    |                           |                        |                           |-- (next frame)
    |                           |                        |<-- postMessage(result) ---|-- __debugResponse
    |                           |<-- POST /client-debug-response --|                |
    |<-- JSON response --------|                        |                           |
```

### Changes

#### 1. Add postMessage bridge to iframe page

**File**: `template/client/index.html` (or injected via harness template)

Small script in the iframe's HTML that bridges `postMessage` ↔ `__debugRequest`/`__debugResponse`:

```html
<script>
window.addEventListener('message', async (event) => {
  if (event.data?.type !== 'debug-query') return;

  // Write query to WASM bridge
  window.__debugRequest = JSON.stringify(event.data.query);

  // Poll for response (WASM processes next frame, typically <16ms)
  let response = null;
  for (let i = 0; i < 20; i++) {
    await new Promise(r => setTimeout(r, 16));
    if (window.__debugResponse) {
      response = JSON.parse(window.__debugResponse);
      window.__debugResponse = null;
      break;
    }
  }

  // Post result back to parent
  window.parent.postMessage({
    type: 'debug-response',
    id: event.data.id,
    response: response ?? { error: 'timeout' },
  }, '*');
});
</script>
```

#### 2. Pending query map in harness

**File**: `harness/internal/server/debug.go` (new)

```go
package server

import (
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    "time"

    "github.com/google/uuid"
    "github.com/labstack/echo/v4"
    "github.com/starfederation/datastar-go/datastar"
)

// pendingClientQuery holds a channel waiting for a client debug response.
type pendingClientQuery struct {
    ch chan json.RawMessage
}

var (
    pendingQueries   = make(map[string]*pendingClientQuery)
    pendingQueriesMu sync.Mutex
)

// handleClientDebug sends a debug query to a connected browser via SSE,
// waits for the browser to execute it in the WASM iframe and POST back.
func (s *Server) handleClientDebug(c echo.Context) error {
    worldID := c.Param("worldID")

    var query json.RawMessage
    if err := json.NewDecoder(c.Request().Body).Decode(&query); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
    }

    // Generate query ID and register pending response channel
    queryID := uuid.New().String()[:8]
    pending := &pendingClientQuery{ch: make(chan json.RawMessage, 1)}

    pendingQueriesMu.Lock()
    pendingQueries[queryID] = pending
    pendingQueriesMu.Unlock()

    defer func() {
        pendingQueriesMu.Lock()
        delete(pendingQueries, queryID)
        pendingQueriesMu.Unlock()
    }()

    // Build JS that the browser will execute:
    // 1. postMessage the query to the game iframe
    // 2. Listen for the response
    // 3. POST it back to the harness
    js := fmt.Sprintf(`
(function() {
  const frame = document.getElementById('game-frame');
  if (!frame) return;
  const id = %q;

  function onMessage(event) {
    if (event.data?.type !== 'debug-response' || event.data.id !== id) return;
    window.removeEventListener('message', onMessage);
    fetch('/world/%s/client-debug-response?id=' + id, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(event.data.response),
    });
  }
  window.addEventListener('message', onMessage);

  frame.contentWindow.postMessage({
    type: 'debug-query',
    id: id,
    query: %s,
  }, '*');
})();
`, queryID, worldID, string(query))

    // Send ExecuteScript to all SSE connections for this world
    if s.EventBus != nil {
        s.EventBus.Publish(worldID, map[string]any{
            "event":  "execute_script",
            "script": js,
        })
    }

    // Wait for response with timeout
    select {
    case result := <-pending.ch:
        return c.JSONBlob(http.StatusOK, result)
    case <-time.After(5 * time.Second):
        return echo.NewHTTPError(http.StatusGatewayTimeout, "client debug query timed out")
    case <-c.Request().Context().Done():
        return nil
    }
}

// handleClientDebugResponse receives the debug query result POSTed back
// by the browser JS after querying the WASM iframe.
func (s *Server) handleClientDebugResponse(c echo.Context) error {
    queryID := c.QueryParam("id")
    if queryID == "" {
        return echo.NewHTTPError(http.StatusBadRequest, "missing query id")
    }

    var result json.RawMessage
    if err := json.NewDecoder(c.Request().Body).Decode(&result); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
    }

    pendingQueriesMu.Lock()
    pending, ok := pendingQueries[queryID]
    pendingQueriesMu.Unlock()

    if !ok {
        return c.NoContent(http.StatusGone) // query already timed out
    }

    // Non-blocking send (buffer of 1)
    select {
    case pending.ch <- result:
    default:
    }

    return c.NoContent(http.StatusOK)
}
```

#### 3. Register routes

**File**: `harness/internal/server/server.go`

In `registerWorldRoutes()`:

```go
w.POST("/:worldID/client-debug", s.handleClientDebug)
w.POST("/:worldID/client-debug-response", s.handleClientDebugResponse)
```

#### 4. Handle `execute_script` events in world SSE handler

In the world SSE handler, when receiving an `execute_script` event from the EventBus, call `sse.ExecuteScript(script)` to forward it to connected browsers.

### Usage

```bash
# Query client camera state from harness (no playwright needed)
curl -X POST -b "session=<cookie>" \
  http://localhost:8080/world/<worldID>/client-debug \
  -H 'Content-Type: application/json' \
  -d '{"type": "resource", "name": "FlyCameraState"}'
# {"name": "FlyCameraState", "value": {"yaw": 0.1, "pitch": -0.3}}

# List queryable client types
curl -X POST -b "session=<cookie>" \
  http://localhost:8080/world/<worldID>/client-debug \
  -H 'Content-Type: application/json' \
  -d '{"type": "list"}'
# {"types": ["FlyCameraState", "PlayerPosition", "PlayerColor", ...]}

# Query player entities on client (predicted + interpolated)
curl -X POST -b "session=<cookie>" \
  http://localhost:8080/world/<worldID>/client-debug \
  -H 'Content-Type: application/json' \
  -d '{"type": "query", "components": ["PlayerPosition"]}'
```

### Caveats

- **Requires a connected browser**: If no browser is viewing the world, the query times out (5s). The error message tells the caller.
- **First connected browser wins**: If multiple browsers are connected, all receive the SSE event, but the first response is used. Could be scoped to a specific user/session if needed.
- **Latency**: ~50-100ms round-trip (SSE delivery + 1 frame + postMessage + POST back). Fine for debug queries.

### Verification

- [ ] Harness builds: `cd harness && go build ./...`
- [ ] `curl POST /world/<id>/client-debug` with `{"type": "list"}` returns types when browser connected
- [ ] Returns 504 timeout when no browser is connected
- [ ] Camera state values change after WASD movement
- [ ] Multiple concurrent queries don't interfere (distinct query IDs)

______________________________________________________________________

## E2E Testing

```bash
# Client: discover queryable types via playwright
playwright-cli run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const canvas = frame.locator('canvas');
  const debugQuery = async (query) => {
    await canvas.evaluate((q) => {
      window.__debugRequest = JSON.stringify(q);
    }, query);
    await page.waitForTimeout(50);
    return JSON.parse(await canvas.evaluate(() => {
      const r = window.__debugResponse;
      window.__debugResponse = null;
      return r;
    }));
  };
  return await debugQuery({type: 'list'});
}"

# Client: get camera state
playwright-cli run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const canvas = frame.locator('canvas');
  const debugQuery = async (query) => {
    await canvas.evaluate((q) => { window.__debugRequest = JSON.stringify(q); }, query);
    await page.waitForTimeout(50);
    return JSON.parse(await canvas.evaluate(() => {
      const r = window.__debugResponse; window.__debugResponse = null; return r;
    }));
  };
  return await debugQuery({type: 'resource', name: 'FlyCameraState'});
}"

# Server: list resources via harness proxy (BRP)
curl -X POST -b "session=<cookie>" \
  http://localhost:8080/world/<worldID>/debug \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.list_resources","id":1}'

# Server: query players via harness proxy (BRP)
curl -X POST -b "session=<cookie>" \
  http://localhost:8080/world/<worldID>/debug \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition","shared::protocol::PlayerColor"]}}}'
```

## Performance Notes

- **Client no-op when idle**: System checks one JS global per frame, returns immediately if null
- **Server (bevy_remote)**: Runs in Bevy's `RemoteLast` schedule, uses `IoTaskPool` — zero impact on game loop when idle
- **On-demand serialization**: Both sides only serialize when a query arrives
- **BRP overhead**: Negligible — smol runtime on IoTaskPool, single HTTP request per query

## Future Extensions

- Mutation via BRP: `world.insert_components`, `world.mutate_resources` (already built-in, just needs auth consideration)
- Watch queries via BRP: `world.get_components+watch` for streaming entity state changes
- Custom BRP methods: `RemotePlugin::with_method()` for game-specific queries
- Scope client-debug to specific user/session (currently broadcasts to all connected browsers)

## Key Design Decisions

**`bevy_remote` over custom axum server (Phase 2)**: Eliminates ~150 lines of custom Rust, removes axum/tokio dependencies, provides 18 methods for free vs our 3, and is maintained upstream. BRP uses smol (Bevy's IoTaskPool), not tokio, so no runtime conflict with Lightyear.

**Custom query engine for client (Phase 1)**: `bevy_remote` can't compile to WASM (HTTP transport is `cfg(not(wasm))`). The JS bridge approach is the only way to query WASM ECS state from playwright.

**`list` query filters by `ReflectSerialize`**: Without filtering, Bevy registers hundreds of internal types. Filtering to types with `ReflectSerialize` returns only explicitly-opted-in types.

**`PlayerId` skipped from Reflect**: Wraps Lightyear's `PeerId` which is a foreign type without `Reflect`. Not a problem — `PlayerPosition` and `PlayerColor` are the useful queryable components.

**Two different query protocols**: Client uses simple `{type, name/components}` JSON. Server uses BRP JSON-RPC 2.0. Different protocols for different transports is fine — they serve different use cases and the BRP format is standard and well-documented.

**SSE round-trip for client debug (Phase 3)**: Reuses existing infrastructure — `sse.ExecuteScript()`, `postMessage`, and the Phase 1 JS bridge. No new WebSocket connections or long-lived state. The tradeoff is latency (~50-100ms) and requiring a connected browser, which is acceptable for debug queries.

## References

- Template server: `template/server/src/main.rs`
- Template client: `template/client/src/main.rs`
- Shared protocol: `template/shared/src/protocol.rs`
- Game server manager: `harness/internal/world/game_server.go`
- Harness routes: `harness/internal/server/server.go`
- Bevy Remote Protocol docs: `docs.rs/bevy/0.18/bevy/remote/`
- QueryBuilder docs: `docs.rs/bevy/0.18/bevy/ecs/prelude/struct.QueryBuilder.html`
