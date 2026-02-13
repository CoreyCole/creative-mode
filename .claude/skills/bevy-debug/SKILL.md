---
name: bevy-debug
description: Debug Bevy/WASM game client and Go harness server ECS state. Use when investigating gameplay bugs, inspecting entity/component/resource state, verifying client-server sync, or diagnosing movement/camera/input issues.
allowed-tools: Bash(curl:*), Bash(playwright-cli:*)
---

# Bevy Debug Queries

Three ways to inspect ECS state at runtime, listed from easiest to most manual.

## 1. Client State via Harness (Phase 3 — preferred)

Queries WASM ECS state through an SSE round-trip. No playwright needed — just a session cookie and a connected browser viewing the world.

```bash
# Camera rotation
curl -s -X POST -b "session=<COOKIE>" \
  http://localhost:8080/world/<WORLD_ID>/client-debug \
  -H 'Content-Type: application/json' \
  -d '{"type": "resource", "name": "FlyCameraState"}'
# {"name":"FlyCameraState","value":{"yaw":0.1,"pitch":-0.3}}

# Player positions (all entities with PlayerPosition)
curl -s -X POST -b "session=<COOKIE>" \
  http://localhost:8080/world/<WORLD_ID>/client-debug \
  -H 'Content-Type: application/json' \
  -d '{"type": "query", "components": ["PlayerPosition"]}'
# {"entities":[{"entity":111,"components":{"PlayerPosition":[0,0,0]}}],"count":1}

# List all queryable types
curl -s -X POST -b "session=<COOKIE>" \
  http://localhost:8080/world/<WORLD_ID>/client-debug \
  -d '{"type": "list"}'
```

**Requires**: A browser with the world page open (SSE connection active + game iframe loaded).

**Getting the session cookie**: `playwright-cli cookie-get session` or browser DevTools > Application > Cookies.

**Timeout**: Returns 504 after 5s if no browser is connected to the world.

## 2. Server State via BRP (Bevy Remote Protocol)

Queries the authoritative server ECS via JSON-RPC 2.0. Goes through the harness proxy.

```bash
# Query player positions + colors
curl -s -X POST -b "session=<COOKIE>" \
  http://localhost:8080/world/<WORLD_ID>/debug \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition","shared::protocol::PlayerColor"]}}}'

# List all resources
curl -s -X POST -b "session=<COOKIE>" \
  http://localhost:8080/world/<WORLD_ID>/debug \
  -d '{"jsonrpc":"2.0","method":"world.list_resources","id":1}'

# List components on a specific entity
curl -s -X POST -b "session=<COOKIE>" \
  http://localhost:8080/world/<WORLD_ID>/debug \
  -d '{"jsonrpc":"2.0","method":"world.list_components","id":1,"params":{"entity":42}}'
```

**Note**: BRP uses full type paths (`shared::protocol::PlayerPosition`), not short names.

**Requires**: A running game server for the checkpoint. Returns 503 if no server is running.

## 3. Client State via Playwright (Phase 1 — fallback)

Direct JS bridge when Phase 3 isn't working (no SSE connection, debugging the bridge itself).

```bash
playwright-cli run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const canvas = frame.locator('canvas');
  const debugQuery = async (query) => {
    await canvas.evaluate((q) => {
      window.__debugRequest = JSON.stringify(q);
    }, query);
    await page.waitForTimeout(100);
    return JSON.parse(await canvas.evaluate(() => {
      const r = window.__debugResponse;
      window.__debugResponse = null;
      return r;
    }));
  };
  return await debugQuery({type: 'resource', name: 'FlyCameraState'});
}"
```

## Debugging Workflows

### Compare Client vs Server State

Useful for diagnosing prediction desync or replication issues.

```bash
COOKIE="<session cookie>"
WORLD="<world id>"

echo "=== Server ===" && \
curl -s -X POST -b "session=$COOKIE" "http://localhost:8080/world/$WORLD/debug" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"world.query","id":1,"params":{"data":{"components":["shared::protocol::PlayerPosition"]}}}' | python3 -m json.tool

echo "=== Client ===" && \
curl -s -X POST -b "session=$COOKIE" "http://localhost:8080/world/$WORLD/client-debug" \
  -H 'Content-Type: application/json' \
  -d '{"type": "query", "components": ["PlayerPosition"]}' | python3 -m json.tool
```

### Track Movement Over Time

Sample position repeatedly to detect speed bugs or stuck entities.

```bash
for i in {1..5}; do
  curl -s -X POST -b "session=$COOKIE" "http://localhost:8080/world/$WORLD/client-debug" \
    -d '{"type": "query", "components": ["PlayerPosition"]}'
  echo ""
  sleep 0.5
done
```

- **Smooth increments**: correct delta-time
- **Huge jumps per sample**: missing delta-time scaling (fixed-timestep bug)
- **No change**: movement frozen or input not reaching entity

### Investigate Input Issues

1. Minimize overlay first (it blocks iframe clicks): click the "—" button
2. Force-click iframe: `playwright-cli run-code "async page => { await page.locator('#game-frame').click({force: true}); }"`
3. Hold a key: `playwright-cli run-code "async page => { await page.keyboard.down('KeyW'); await page.waitForTimeout(2000); await page.keyboard.up('KeyW'); }"`
4. Query position after input to verify movement

## Setup Requirements

1. **Harness running**: `curl http://localhost:8080/health`
2. **Game server running**: Check checkpoint has `server_port` set
3. **WASM client built**: `cd template/client && trunk build --release --public-url ./`
4. **WSS cert accepted**: Navigate to `https://127.0.0.1:<GAME_PORT>` in browser, type `thisisunsafe` on Chrome interstitial. With `--persistent` playwright profile, this persists for 7 days.
5. **Browser on world page**: The Phase 3 SSE round-trip requires an active browser connection

## Key Gotchas

- **`FlyCameraState` is rotation only** (yaw/pitch). For camera position, query `Transform` on the `FlyCamera` entity.
- **BRP uses full type paths** (`shared::protocol::PlayerPosition`), client bridge uses **short names** (`PlayerPosition`).
- **`list` returns hundreds of Bevy internal types**. Filter output: `| python3 -c "import sys,json; [print(t) for t in json.load(sys.stdin)['types'] if '::' not in t]"`
- **Overlay blocks iframe interaction**. Minimize it or use `{force: true}` with playwright clicks.
- **BRP port = GAME_PORT + 1000** (e.g., game on 9001 means BRP on 10001).
- **Self-signed certs regenerate on each server restart**. Re-accept if you see `ERR_CERT_AUTHORITY_INVALID`.
- **Any type with `Reflect` + `Serialize` + `register_type` is automatically queryable**. Add these derives to new components/resources to make them debuggable.
