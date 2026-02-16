# Creative Mode - 2D Room World

A data-driven 2D room world built with Bevy 0.18 WASM. Rooms are defined as JSON files in `rooms/` — no game server needed. Multiplayer is handled by the harness chat system.

## Architecture

### How It Runs

The 2D template is a **client-only WASM app** loaded inside an iframe by the harness. There is no game server — all game logic runs in the browser.

```
Harness (Go server, port 8080)
  └─ World page → <iframe src="http://localhost:{trunkPort}/">
                    └─ Trunk-built HTML + WASM
                        └─ Bevy app (Camera2d, sprites, text, input)
                            ├─ room.rs     — room loading & entity spawning
                            ├─ interaction.rs — hover/click detection
                            ├─ bridge.rs   — postMessage to parent frame
                            └─ debug.rs    — runtime ECS inspection
```

### Crate Layout

This is a **single crate** (not a workspace) with both a library and a binary target:

```toml
[lib]
crate-type = ["cdylib", "rlib"]  # cdylib for wasm-bindgen, rlib for binary
```

- **Binary** (`src/main.rs`): calls `room_world::run()` — this is what Trunk builds
- **Library** (`src/lib.rs`): defines `run()` which sets up the Bevy App with all plugins

### Build Pipeline (Trunk)

Trunk compiles the Rust binary to WASM, runs wasm-bindgen, and serves the result:

```
src/main.rs → cargo build --target wasm32-unknown-unknown --bin room-world
            → wasm-bindgen (generates .js + .wasm)
            → index.html (injects <script> to load .js which calls main())
            → trunk serve (dev server on allocated port)
```

**CRITICAL — `index.html` must target the binary, not the library:**

```html
<!-- CORRECT: targets the binary, which calls main() → Bevy starts -->
<link data-trunk rel="rust" data-bin="room-world" />

<!-- WRONG: targets the cdylib, loads WASM but never calls main() → black screen -->
<link data-trunk rel="rust" data-target-name="room_world" />
```

This crate has both a `[lib]` (artifact name: `room_world`) and an implicit binary (artifact name: `room-world`). Trunk can't auto-detect which to use, so `index.html` must specify. The binary calls `main()` which starts Bevy; the cdylib only exports wasm-bindgen bindings without an entry point.

### Bevy Features

```toml
bevy = { version = "0.18", default-features = false, features = ["2d", "default_font"] }
```

The `2d` feature includes everything needed for WASM rendering:
- `default_platform` → `bevy_winit` (window/event loop) + `webgl2` (WASM renderer)
- `2d_bevy_render` → `bevy_render` + `bevy_core_pipeline` + `bevy_sprite_render`
- `default_app` → `bevy_asset` + `bevy_window` + `bevy_log`

**Do NOT add `webgl2` explicitly** — it's already included via `2d` → `default_platform`.

### Harness Integration

The harness loads this world in an iframe with the trunk serve URL. Communication between WASM and harness uses `window.postMessage`:

| Direction | Mechanism | Purpose |
|-----------|-----------|---------|
| WASM → Harness | `bridge.rs` → `postMessage` to parent | World/room navigation, open embeds |
| Harness → WASM | `postMessage` → `index.html` JS → `window.__debugRequest` | Debug queries |
| WASM → Harness | `window.__debugResponse` → `index.html` JS → `postMessage` to parent | Debug responses |

The harness `game-loader.js` listens for these messages and dispatches navigation.

## Structure

| File | Purpose |
|------|---------|
| `src/main.rs` | Entry point — calls `room_world::run()` |
| `src/lib.rs` | App setup: `DefaultPlugins` + `WindowPlugin` + game plugins |
| `src/room.rs` | Room loading, JSON schema types, entity spawning, camera setup |
| `src/interaction.rs` | Hover highlighting + click detection on hotspots |
| `src/bridge.rs` | `postMessage` bridge to harness parent frame for navigation |
| `src/debug.rs` | Debug query system (room/dialog/hotspot inspection via JS bridge) |
| `rooms/*.room.json` | Room definitions (data-driven content, seeded to shared-assets on startup) |
| `index.html` | Trunk entry point — canvas element, JS bridge, keyboard handler |
| `Trunk.toml` | Trunk build config — wasm-bindgen version pin, `public_url = "./"` for static serving |
| `Cargo.toml` | Rust dependencies — Bevy features, wasm-bindgen pin |

## Room JSON Schema

Rooms are loaded at runtime via HTTP (`/assets/rooms/{id}.room.json`), not compiled into WASM. The harness seeds `data/shared-assets/rooms/` from `templates/2d/rooms/` on startup (won't overwrite existing files).

```json
{
    "id": "room-id",
    "name": "Display Name",
    "background_color": "#1a1a2e",
    "background_image": "rooms/bg.png",
    "hotspots": [
        {
            "id": "hotspot-id",
            "label": "Click Me",
            "x": 400.0,
            "y": 200.0,
            "width": 200.0,
            "height": 80.0,
            "image": "rooms/hotspot.png",
            "action": { "type": "dialog", "text": "Hello!" }
        }
    ]
}
```

- `background_image` (optional): path relative to `/assets/`, rendered on top of background color at z=0.5
- `image` on hotspots (optional): replaces the translucent color sprite with an image. Hover brightens instead of changing opacity.

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

1. Create `rooms/my-room.room.json` following the schema above
2. Place it in `data/shared-assets/rooms/` (or in `templates/2d/rooms/` for it to be seeded on startup)
3. Add a hotspot in another room with `"action": {"type": "navigate_room", "room": "my-room"}`
4. No WASM rebuild needed — click "Reload" in the overlay or refresh the page

## Adding Images

1. Upload images via `POST /api/assets/upload` (multipart, `file` + optional `folder` field)
2. Or place them directly in `data/shared-assets/`
3. Reference them in room JSON: `"background_image": "rooms/bg.png"` or `"image": "rooms/sprite.png"` on hotspots
4. Click "Reload" in the overlay to see changes without rebuilding

## Building a New 2D Game from This Template

This template is designed to be modified. Here's how to evolve it beyond rooms.

### Replacing the Room System

The room/hotspot system is just one way to use this template. To build a different kind of 2D game:

1. **Keep**: `main.rs`, `lib.rs` (app setup), `bridge.rs` (harness communication), `index.html`
2. **Replace**: `room.rs` and `interaction.rs` with your own game logic
3. **Keep or extend**: `debug.rs` (add your own debug queries)

Example: to make a platformer, replace `RoomPlugin` with your own plugin:

```rust
// src/lib.rs
pub fn run() {
    let mut app = App::new();
    app.add_plugins(DefaultPlugins.set(WindowPlugin { /* same config */ }));
    app.add_plugins(my_game::GamePlugin);  // your game logic
    app.add_plugins(bridge::BridgePlugin); // keep for harness navigation
    app.run();
}
```

### Adding New Bevy Plugins

Add Bevy features to `Cargo.toml` as needed:

```toml
# Example: add audio support
bevy = { version = "0.18", default-features = false, features = ["2d", "default_font", "bevy_audio"] }
```

Available features that work with WASM: `bevy_audio`, `bevy_ui`, `bevy_text`, `bevy_animation`, `bevy_state`. Check Bevy docs for the full list.

### Adding External Crates

WASM-compatible crates work fine. Add to `[dependencies]` as usual. For crates that need `getrandom`, the `[target.'cfg(target_family = "wasm")'.dependencies]` section already handles the v0.2 JS feature.

### Canvas and Window Setup

The window is configured in `lib.rs`:
- Resolution: 1280x720 (Bevy's logical size)
- Canvas: `#bevy-canvas` element in `index.html`
- `fit_canvas_to_parent: true` — scales to iframe size
- `prevent_default_event_handling: true` — Bevy captures keyboard/mouse

Camera is a simple `Camera2d` spawned in `room.rs:setup_camera`. Bevy's 2D coordinate system has (0,0) at center, +Y up, +X right.

### Communicating with the Harness

Use `bridge.rs` to send navigation events to the parent frame:

```rust
// Set PendingBridgeAction resource to trigger navigation
pending_bridge.0 = Some(BridgeAction::NavigateWorld("some-world-id".into()));
```

The harness `game-loader.js` handles: `navigate-world`, `navigate-checkpoint`, `open-embed`.

## Building

- Client only: `trunk build --release` (from project root)
- Check: `cargo clippy --target wasm32-unknown-unknown -- -D warnings`

### wasm-bindgen Pin

`wasm-bindgen` is pinned to exactly `0.2.108` in both `Cargo.toml` and `Trunk.toml`. These MUST match.

### public_url

`Trunk.toml` sets `public_url = "./"` so that `trunk build` generates relative asset paths (`./room-world-*.js`) instead of root-absolute (`/room-world-*.js`). This is required for static WASM serving — template worlds are served at `/wasm/{worldID}/{cpID}/`, and root-absolute paths would 404.

### Common Build Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| Black screen, no errors | `index.html` targets cdylib instead of binary | Use `data-bin="room-world"`, not `data-target-name="room_world"` |
| "found more than one target artifact" | Trunk can't choose between lib and bin | Add `data-bin="room-world"` to the `<link>` tag in `index.html` |
| Canvas stays 300x150 | Bevy's window plugin never ran | Same as black screen — `main()` isn't being called |
| Trunk doesn't rebuild after edit | Docker bind mount doesn't propagate inotify on macOS | Restart the container: `just down && just up` |
| Linker error about missing `.rcgu.o` | Stale incremental build artifacts | Clean target: `rm -rf target/wasm32-unknown-unknown/debug/incremental` |

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

**Via debug CLI** (recommended):
```bash
just debug $WORLD room          # current room + hotspots
just debug $WORLD dialog        # dialog visibility + text
just debug $WORLD click portal  # trigger hotspot by ID
```

**Via curl** (fallback — requires manual cookie):
```bash
COOKIE=$(playwright-cli cookie-get session)
curl -s -X POST -b "session=$COOKIE" \
  http://localhost:8080/world/$WORLD/client-debug \
  -d '{"type": "room"}' | python3 -m json.tool
```

## Testing & Verification

### Clicking Hotspots by ID (Recommended)

The `click` debug query triggers a hotspot's action directly by ID — no coordinate math, no overlay management, no canvas clicking. This is the easiest way to test interactions.

```bash
# 1. See what's in the current room
just debug $WORLD room
# → {"room_id": "lobby", "hotspots": [{"id": "welcome-sign", ...}, {"id": "portal", ...}]}

# 2. Click a dialog hotspot
just debug $WORLD click welcome-sign
# → {"ok": true, "hotspot_id": "welcome-sign", "action_type": "dialog"}

# 3. Verify the dialog appeared
just debug $WORLD dialog
# → {"visible": true, "text": "Welcome to Creative Mode! Click hotspots to interact."}

# 4. Navigate to another room
just debug $WORLD click portal
# → {"ok": true, "hotspot_id": "portal", "action_type": "navigate_room"}

# 5. Verify room changed
just debug $WORLD room
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
3. Query room state: `just debug $WORLD room`
4. Click a dialog hotspot: `just debug $WORLD click welcome-sign`
5. Verify dialog: `just debug $WORLD dialog`
6. Navigate rooms: `just debug $WORLD click portal`
7. Verify room changed: `just debug $WORLD room` → `room_id: "garden"`
8. Final `playwright-cli console error` check

## Key Patterns

### Entity Spawning

All room entities are tagged with `RoomEntity` so they can be bulk-despawned on navigation:

```rust
commands.spawn((
    Sprite { color, custom_size: Some(Vec2::new(w, h)), ..default() },
    Transform::from_xyz(x, y, z_layer),
    RoomEntity,  // despawned when room changes
));
```

Z-layers: 0 = background, 1 = hotspots, 2 = labels, 10 = dialog overlay.

### Input Handling

`interaction.rs` uses Bevy's camera projection to convert screen cursor position to world coordinates:

```rust
let cursor_pos = window.cursor_position()
    .and_then(|pos| camera.viewport_to_world_2d(camera_transform, pos).ok());
```

Then checks `hotspot.bounds.contains(cursor_pos)` for hit detection.

### Resource-Based Communication

Systems communicate via resources, not events:
- `PendingNavigation(Option<String>)` — room navigation queue
- `PendingBridgeAction(Option<BridgeAction>)` — harness navigation queue
- `CurrentRoom(String)` — current room ID

Set the resource in one system, consume it in another. This keeps systems decoupled.

### WASM-Conditional Code

Use `#[cfg(target_family = "wasm")]` for code that uses `web_sys`/`js_sys`:

```rust
#[cfg(target_family = "wasm")]
mod debug;

#[cfg(target_family = "wasm")]
app.add_systems(Update, debug::process_debug_queries);
```

This allows `cargo clippy` to run on the host without WASM dependencies.

## Key Differences from 3D Template

- **No server binary** — client-only WASM, no Lightyear/networking
- **No game server** — the harness doesn't start a game server for 2D worlds
- **Single crate** — not a workspace, Trunk runs from project root
- **Data-driven** — rooms are JSON, not Rust code. Add content by editing JSON files.
- **Multiplayer via chat** — uses the harness SSE chat system, not Lightyear replication

## Mayor Context

2D worlds are the default template for mayor-managed worlds created through the onboarding flow. Mayor-triggered builds (via `POST /api/mayor/build`) use the same fork → Claude Code → build pipeline as browser prompts — no special handling needed.

## CHANGES.txt (Required)

Before you finish, ALWAYS write a brief summary of what you changed to `CHANGES.txt`.

## MEMORY.md

Read MEMORY.md for this world's design decisions and history.
