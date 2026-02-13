# Creative Mode - 2D Room World

A data-driven 2D room world built with Bevy 0.18 WASM. Rooms are defined as JSON files in `rooms/` — no game server needed. Multiplayer is handled by the harness chat system.

## Structure

| File | Purpose |
|------|---------|
| `src/main.rs` | Entry point |
| `src/lib.rs` | App setup, plugin registration |
| `src/room.rs` | Room loading, JSON schema types, spawning |
| `src/interaction.rs` | Hover/click detection on hotspots |
| `src/bridge.rs` | postMessage to harness parent frame |
| `src/debug.rs` | Debug query system (room/dialog/hotspot inspection) |
| `rooms/*.json` | Room definitions (data-driven content) |

## Room JSON Schema

```json
{
    "id": "room-id",
    "name": "Display Name",
    "background_color": "#1a1a2e",
    "hotspots": [
        {
            "id": "hotspot-id",
            "label": "Click Me",
            "x": 400.0,
            "y": 200.0,
            "width": 200.0,
            "height": 80.0,
            "action": { "type": "dialog", "text": "Hello!" }
        }
    ]
}
```

### Coordinates

- `x`, `y` are **top-left screen coordinates** (0,0 = top-left of 1280x720 window)
- Conversion to Bevy's centered coordinate system is handled automatically in `room.rs`

### Action Types

| Type | Fields | Effect |
|------|--------|--------|
| `dialog` | `text` | Shows text at bottom of screen |
| `navigate_room` | `room` | Loads another room JSON |
| `navigate_world` | `world_id` | Navigates to another world (via harness) |
| `navigate_checkpoint` | `world_id`, `checkpoint_id` | Navigates to specific checkpoint |
| `open_embed` | `url` | Opens embedded content (future) |

## Adding a New Room

1. Create `rooms/my-room.json` following the schema above
2. Register it in `src/room.rs` in the `ROOM_DATA` array:
   ```rust
   const ROOM_DATA: &[(&str, &str)] = &[
       ("lobby", include_str!("../rooms/lobby.json")),
       ("garden", include_str!("../rooms/garden.json")),
       ("my-room", include_str!("../rooms/my-room.json")),
   ];
   ```
3. Add a hotspot in another room with `"action": {"type": "navigate_room", "room": "my-room"}`

## Adding New Hotspot Visuals

Currently hotspots are translucent white rectangles with text labels. To add sprites:
1. Place PNG files in `assets/`
2. Load them in `room.rs` `spawn_room()` using `asset_server.load("assets/my-sprite.png")`
3. Attach `Sprite` components to hotspot entities

## Building

- Client only: `trunk build --release` (from project root)
- Check: `cargo clippy --target wasm32-unknown-unknown -- -D warnings`

### wasm-bindgen Pin

`wasm-bindgen` is pinned to exactly `0.2.108` in both `Cargo.toml` and `Trunk.toml`. These MUST match.

## Debugging

### Debug Query System

The 2D template has a debug query system (`src/debug.rs`) that lets you inspect ECS state at runtime. It uses the same JS bridge pattern as the 3D template: the harness sends a `postMessage` to the iframe, JS writes `window.__debugRequest`, the WASM system reads it and writes `window.__debugResponse`, and JS relays it back.

**Query types:**

| Query | Response |
|-------|----------|
| `{"type": "room"}` | `{room_id, hotspot_count, hotspots: [{id, label, bounds_min, bounds_max, action}]}` |
| `{"type": "dialog"}` | `{visible, text}` |
| `{"type": "hotspots"}` | Alias for `room` query |
| `{"type": "click", "hotspot_id": "..."}` | `{ok, hotspot_id, action_type}` — triggers the hotspot action directly |

**Via harness proxy** (requires auth cookie):
```bash
COOKIE=$(playwright-cli cookie-get session)

# Get current room and all hotspots
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "room"}' | python3 -m json.tool

# Check if a dialog is visible
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "dialog"}'
```

**Via playwright-cli direct JS bridge:**
```bash
playwright-cli run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const result = await frame.locator('#bevy-canvas').evaluate(el => {
    const w = el.ownerDocument.defaultView;
    w.__debugRequest = JSON.stringify({type: 'room'});
    return new Promise(resolve => {
      const poll = setInterval(() => {
        if (w.__debugResponse) {
          const r = JSON.parse(w.__debugResponse);
          w.__debugResponse = null;
          clearInterval(poll);
          resolve(r);
        }
      }, 16);
    });
  });
  console.log(JSON.stringify(result, null, 2));
}"
```

## Testing & Verification

### Clicking Hotspots by ID (Recommended)

The `click` debug query triggers a hotspot's action directly by ID — no coordinate math, no overlay management, no canvas clicking. This is the easiest way to test interactions.

```bash
COOKIE=$(playwright-cli cookie-get session)

# 1. See what's in the current room
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "room"}'
# → {"room_id": "lobby", "hotspots": [{"id": "welcome-sign", ...}, {"id": "portal", ...}]}

# 2. Click a dialog hotspot
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "click", "hotspot_id": "welcome-sign"}'
# → {"ok": true, "hotspot_id": "welcome-sign", "action_type": "dialog"}

# 3. Verify the dialog appeared
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "dialog"}'
# → {"visible": true, "text": "Welcome to Creative Mode! Click hotspots to interact."}

# 4. Navigate to another room
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "click", "hotspot_id": "portal"}'
# → {"ok": true, "hotspot_id": "portal", "action_type": "navigate_room"}

# 5. Verify room changed
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "room"}'
# → {"room_id": "garden", ...}
```

### Clicking Canvas Hotspots via Playwright (Visual Testing)

When you need to test the actual visual click path (hover highlight, cursor detection), use canvas-relative coordinates. The overlay must be minimized first.

**Coordinate math**: JSON `(x, y, w, h)` uses top-left screen coords at 1280x720. Click center = `(x + w/2, y + h/2)`, then scale to canvas: `click_x = center_x * box.width / 1280`.

```bash
playwright-cli run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const canvas = frame.locator('#bevy-canvas');
  const box = await canvas.boundingBox();
  await canvas.click({ position: { x: 500 * box.width/1280, y: 240 * box.height/720 } });
}"
```

**Reference positions** (at native 1280x720):

| Room | Hotspot | Click center `(x, y)` | Action |
|------|---------|----------------------|--------|
| lobby | welcome-sign | (500, 240) | dialog |
| lobby | portal | (175, 475) | navigate_room → garden |
| garden | tree | (360, 390) | dialog |
| garden | back-door | (125, 530) | navigate_room → lobby |

### E2E Verification Workflow

1. Open world page, wait for WASM load
2. `playwright-cli screenshot` + `playwright-cli console error` — check for load issues
3. Query room state: `curl ... -d '{"type": "room"}'`
4. Click a dialog hotspot: `curl ... -d '{"type": "click", "hotspot_id": "welcome-sign"}'`
5. Verify dialog: `curl ... -d '{"type": "dialog"}'`
6. Navigate rooms: `curl ... -d '{"type": "click", "hotspot_id": "portal"}'`
7. Verify room changed: `curl ... -d '{"type": "room"}'` → `room_id: "garden"`
8. Final `playwright-cli console error` check

## Key Differences from 3D Template

- **No server binary** — client-only WASM, no Lightyear/networking
- **No game server** — the harness doesn't start a game server for 2D worlds
- **Single crate** — not a workspace, Trunk runs from project root
- **Data-driven** — rooms are JSON, not Rust code. Add content by editing JSON files.
- **Multiplayer via chat** — uses the harness SSE chat system, not Lightyear replication

## CHANGES.txt (Required)

Before you finish, ALWAYS write a brief summary of what you changed to `CHANGES.txt`.

## MEMORY.md

Read MEMORY.md for this world's design decisions and history.
