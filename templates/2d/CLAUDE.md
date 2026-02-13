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
