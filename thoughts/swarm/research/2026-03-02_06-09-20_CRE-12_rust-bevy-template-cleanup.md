---
ticket: CRE-12
workflow: 996b4552
session: a5610e73
timestamp: 2026-03-02T06:09:20Z
---

# Research: Rust/Bevy Template Cleanup Audit

## Questions
1. Exhaustive catalog of all magic numbers in templates/2d/, templates/3d/, and templates/boardgame/ with proposed constant names
2. Analyze 3D client main.rs structure — list all systems, resources, and plugins, map their dependencies, and propose module split
3. Review `#[allow(dead_code)]` annotations in boardgame template to determine if code is actually dead or just conditionally used
4. Propose naming conventions for constants

## Findings

### 1. Magic Number Inventory

#### 2D Template (`templates/2d/src/`) — ~60 occurrences across 6 files

**camera.rs** (11 magic numbers):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 68 | `2.0` | Narrow screen scale threshold | `NARROW_SCREEN_SCALE_THRESHOLD` |
| 156 | `1.0` | Minimum pinch distance | `MIN_PINCH_DISTANCE` |
| 158, 264 | `0.5` | Max zoom-in scale (pinch + scroll) | `MAX_ZOOM_IN_SCALE` |
| 201 | `10.0` | Drag start threshold in pixels | `DRAG_THRESHOLD_PX` |
| 240 | `10.0` | Tap max distance in pixels | `TAP_MAX_DISTANCE_PX` |
| 240 | `0.3` | Tap max duration in seconds | `TAP_MAX_DURATION_SECS` |
| 263 | `0.9` | Scroll zoom-in factor | `SCROLL_ZOOM_IN_FACTOR` |
| 263 | `1.1` | Scroll zoom-out factor | `SCROLL_ZOOM_OUT_FACTOR` |

**lib.rs** (5 magic values):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 18 | `"Creative Mode - 2D World"` | Window title | `WINDOW_TITLE` |
| 19 | `1280, 720` | Window resolution | `WINDOW_WIDTH`, `WINDOW_HEIGHT` |
| 21 | `"#bevy-canvas"` | HTML canvas selector | `CANVAS_SELECTOR` |
| 28 | `"/assets"` | Asset base path | `ASSET_BASE_PATH` |

**room.rs** (21 magic numbers):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 20, 153 | `"lobby"` | Default room ID (duplicated) | `INITIAL_ROOM_ID` |
| 233 | `4000.0` | Background fill size (oversized) | `BG_FILL_SIZE` |
| 245 | `1280.0, 720.0` | Background image dimensions (duplicates camera.rs) | Should reference `ROOM_WIDTH`/`ROOM_HEIGHT` |
| 257 | `32.0` | Room title font size | `ROOM_TITLE_FONT_SIZE` |
| 261 | `320.0` | Room title Y position | `ROOM_TITLE_Y` |
| 268, 269 | `640.0, 360.0` | Half room dimensions for coord conversion | `ROOM_HALF_WIDTH`, `ROOM_HALF_HEIGHT` |
| 297 | `Color::srgba(1.0, 1.0, 1.0, 0.1)` | Default hotspot color (3x duplication) | `HOTSPOT_DEFAULT_COLOR` |
| 318 | `18.0` | Hotspot label font size | `HOTSPOT_LABEL_FONT_SIZE` |
| 332-336 | `0.1, 0.1, 0.1` / `26, 26, 46` | Fallback background color | `FALLBACK_BG_COLOR` |

**interaction.rs** (13 magic numbers):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 37, 50 | `Color::srgba(1.0, 1.0, 1.0, 0.1)` | Default hotspot color (shared) | `HOTSPOT_DEFAULT_COLOR` |
| 48 | `Color::srgba(1.0, 1.0, 1.0, 0.3)` | Hovered hotspot color | `HOTSPOT_HOVER_COLOR` |
| 57 | `Color::srgba(1.2, 1.2, 1.2, 1.0)` | Image hotspot hover tint | `IMAGE_HOTSPOT_HOVER_TINT` |
| 116 | `Color::srgba(0.0, 0.0, 0.0, 0.75)` | Dialog backdrop | `DIALOG_BACKDROP_COLOR` |
| 117 | `900.0, 60.0` | Dialog dimensions | `DIALOG_WIDTH`, `DIALOG_HEIGHT` |
| 120, 131 | `-300.0` | Dialog Y position | `DIALOG_Y_POSITION` |
| 127 | `20.0` | Dialog font size | `DIALOG_FONT_SIZE` |
| 130 | `Color::srgba(1.0, 1.0, 1.0, 0.9)` | Dialog text color | `DIALOG_TEXT_COLOR` |

**debug.rs** (9 magic numbers — all duplicated from interaction.rs):
Lines 154-169 are a copy-paste of the dialog rendering block from interaction.rs with identical values.

**Key duplication patterns:**
1. Dialog rendering block: 9 values copy-pasted between `interaction.rs:114-134` and `debug.rs:152-172`
2. Hotspot default color `(1.0, 1.0, 1.0, 0.1)`: 3 occurrences across room.rs and interaction.rs
3. Room dimensions `1280.0 × 720.0`: defined as constants in camera.rs but re-hardcoded in room.rs and lib.rs

**Z-layer system (implicit):**

| Z Value | Usage | Files |
|---------|-------|-------|
| `0.0` | Background color fill | room.rs |
| `0.5` | Background image | room.rs |
| `1.0` | Hotspots + room title | room.rs |
| `2.0` | Hotspot labels | room.rs |
| `9.5` | Dialog backdrop | interaction.rs, debug.rs |
| `10.0` | Dialog text | interaction.rs, debug.rs |

#### 3D Template (`templates/3d/`) — 38 occurrences across 3 files

**shared/src/protocol.rs** (6 inline, 5 already named constants):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 32 | `30` | Hue multiplier for player color | `PLAYER_HUE_MULTIPLIER` |
| 32 | `360` / `360.0` | Hue degree range | `HUE_DEGREE_RANGE` |
| 33 | `0.8` | Player color saturation | `PLAYER_COLOR_SATURATION` |
| 34 | `0.5` | Player color lightness | `PLAYER_COLOR_LIGHTNESS` |
| 128 | `3.0` | Sprint speed multiplier | `SPRINT_MULTIPLIER` |

**client/src/main.rs** (30 magic numbers):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 46 | `1280, 720` | Window resolution | `DEFAULT_WINDOW_WIDTH/HEIGHT` |
| 87 | `5` | Client timeout seconds | `CLIENT_TIMEOUT_SECS` |
| 151, 158 | `9001` | Default server port (2x) | `DEFAULT_SERVER_PORT` |
| 224 | `0.0, -0.3` | Default yaw/pitch | `DEFAULT_CAMERA_YAW/PITCH` |
| 225 | `8.0` | Default camera distance | `DEFAULT_CAMERA_DISTANCE` |
| 226 | `1.5` | Camera height offset | `DEFAULT_CAMERA_HEIGHT_OFFSET` |
| 251 | `200.0` | Ground plane size | `GROUND_PLANE_SIZE` |
| 254 | `25.0` | Ground texture tile scale | `GROUND_TEXTURE_TILE_SCALE` |
| 272 | `10000.0` | Sun illuminance | `SUN_ILLUMINANCE` |
| 276 | `-0.7, 0.3, 0.0` | Sun rotation euler | `SUN_ROTATION_X/Y/Z` |
| 281 | `(0.6, 0.7, 0.9)` | Ambient light color | `AMBIENT_LIGHT_COLOR` |
| 282 | `300.0` | Ambient light brightness | `AMBIENT_LIGHT_BRIGHTNESS` |
| 289 | `(0.0, 5.0, 10.0)` | Initial camera position | `INITIAL_CAMERA_POSITION` |
| 406 | `0.003` | Mouse look sensitivity | `MOUSE_SENSITIVITY` |
| 413 | `1.55` | First-person pitch limit | `PITCH_LIMIT_FIRST_PERSON` |
| 414 | `1.0` | Third-person pitch limit | `PITCH_LIMIT_THIRD_PERSON` |
| 434 | `1.7` | First-person eye height | `FIRST_PERSON_EYE_HEIGHT` |
| 450 | `15.0` | Camera smoothing speed | `CAMERA_SMOOTHING_SPEED` |
| 550 | `0.4` | Predicted player desaturation | `PREDICTED_PLAYER_SATURATION` |
| 568 | `0.1` | Interpolated player desaturation | `INTERPOLATED_PLAYER_SATURATION` |
| 603 | `0.3, 1.2` | Player capsule radius/height | `PLAYER_CAPSULE_RADIUS/HEIGHT` |
| 610, 620 | `0.9` | Player mesh Y offset (2x) | `PLAYER_MESH_Y_OFFSET` |

**server/src/main.rs** (2 magic numbers):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 24 | `9001` | Default game port | `DEFAULT_SERVER_PORT` |
| 29 | `1000` | BRP port offset | `BRP_PORT_OFFSET` |

#### Boardgame Template (`templates/boardgame/src/`) — ~35 occurrences across 6 files

**board.rs** (most impactful):

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 8 | `8.0` | Board dimension (float) | `BOARD_DIMENSION_F32` |
| 74-75 | `3.5` | Board center offset (2x) | `BOARD_CENTER_OFFSET` |
| 81-82 | `4.0` | Inverse center offset (2x) | `BOARD_HALF_SQUARES` |
| 92-93, 134-135 | `0..8u8` | Board iteration range (4x) | `BOARD_DIMENSION` |
| 147 | `0.8` | Piece sprite scale | `PIECE_SCALE` |
| 160 | `0.3` | Crown sprite scale | `CROWN_SCALE` |

**interaction.rs:**

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 127 | `0.5` | Selection highlight Z-layer | `Z_HIGHLIGHT` |
| 139 | `0.5` | Move highlight scale | `MOVE_HIGHLIGHT_SCALE` |

**ui.rs:**

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 28 | `30.0` | Turn indicator Y offset | `TURN_INDICATOR_Y_OFFSET` |
| 32 | `24.0` | Turn indicator font size | `TURN_INDICATOR_FONT_SIZE` |
| 63 | `1.0, 0.84, 0.0` | Win text color (gold) | `WIN_TEXT_COLOR` |
| 70 | `18.0` | Win message font size | `WIN_MESSAGE_FONT_SIZE` |
| 73 | `1.0, 1.0, 1.0, 0.7` | Win message subtitle color | `WIN_MESSAGE_COLOR` |
| 74 | `60.0` | Win message Y offset | `WIN_MESSAGE_Y_OFFSET` |

**rules.rs** — 14 occurrences of `8` (board dimension) as loop ranges and array sizes:

| Line | Value | Context | Proposed Constant |
|------|-------|---------|-------------------|
| 57 | `[[None; 8]; 8]` | Board array | `BOARD_DIMENSION` |
| 60 | `0..3u8` | Red piece starting rows | `RED_HOME_ROWS` |
| 69 | `5..8u8` | Black piece starting rows | `BLACK_HOME_ROWS` |
| 192 | `7` | Red promotion row | `RED_PROMOTION_ROW` |
| 193 | `0` | Black promotion row | `BLACK_PROMOTION_ROW` |

**Most pervasive magic number**: `8` (board dimension) appears in **14 distinct locations** across rules.rs, board.rs, and debug.rs.

**Z-layer system (implicit):**

| Z Value | Usage | Files |
|---------|-------|-------|
| `0.0` | Board squares | board.rs |
| `0.5` | Highlights | interaction.rs |
| `1.0` | Pieces | board.rs |
| `1.0 + 0.1` | Crown overlay | board.rs |
| `10.0` | UI text | ui.rs |

### 2. 3D Client main.rs Structure Analysis

**File**: `templates/3d/client/src/main.rs` — 623 lines mixing 5 concerns.

#### All Systems (12 total)

| # | System | Schedule | Purpose |
|---|--------|----------|---------|
| 1 | `connect_to_server` | Startup | Triggers Lightyear Connect |
| 2 | `setup_scene` | Startup | Ground, model, lights, camera |
| 3 | `save_previous_positions` | FixedFirst | Copy positions for interpolation |
| 4 | `buffer_input` | FixedPreUpdate | Keyboard → PlayerInput |
| 5 | `client_movement` | FixedUpdate | Client-side prediction |
| 6 | `cursor_lock_system` | Update (chained) | Pointer lock management |
| 7 | `toggle_camera_mode` | Update (chained) | V key first/third person toggle |
| 8 | `game_camera` | Update (chained) | Camera positioning + mouse look |
| 9 | `sync_player_meshes` | Update (chained) | Spawn/interpolate player capsules |
| 10 | `handle_predicted_spawn` | Observer (Add PlayerId) | Desaturate predicted player |
| 11 | `handle_interpolated_spawn` | Observer (Add PlayerColor) | Desaturate interpolated player |
| 12 | `process_debug_queries` | Update (WASM only) | Runtime ECS debug |

Systems 6-9 are chained: cursor lock → toggle camera → camera update → mesh sync.

#### Custom Types

- **Resources**: `CameraState` (mode, yaw, pitch, distance, height_offset, cursor_locked)
- **Components**: `GameCamera` (marker), `PlayerMeshSpawned` (marker), `PreviousPlayerPosition` (interpolation)
- **Enums**: `CameraMode` (FirstPerson/ThirdPerson)
- **Plugins**: `DefaultPlugins`, `ClientPlugins` (Lightyear), `ProtocolPlugin` (shared)

#### Proposed Module Split

```
client/src/
  main.rs           ~60 lines  (App::new, plugins, system scheduling)
  camera.rs         ~130 lines (CameraState, CameraMode, GameCamera, toggle_camera_mode, game_camera)
  input.rs          ~80 lines  (cursor_lock_system, buffer_input, post_message_to_parent)
  scene.rs          ~60 lines  (setup_scene — ground, model, lights, camera spawn)
  player.rs         ~100 lines (PlayerMeshSpawned, PreviousPlayerPosition, save_previous_positions,
                                client_movement, sync_player_meshes, handle_predicted_spawn,
                                handle_interpolated_spawn)
  connection.rs     ~70 lines  (get_server_port, get_client_id, connect_to_server, netcode config)
  debug.rs          unchanged  (process_debug_queries — exclusive system)
```

**Cross-module dependencies:**
- `input.rs` → `camera.rs` (reads `CameraState`)
- `scene.rs` → `camera.rs` (spawns `GameCamera` component)
- `camera.rs` → `player.rs` (reads `PlayerPosition`, `PreviousPlayerPosition`, `PlayerMeshSpawned`)
- `player.rs` → shared (reads `PlayerPosition`, `PlayerColor`, calls `shared_movement`)
- `connection.rs` → shared (reads `PRIVATE_KEY`, `PROTOCOL_ID`, `FIXED_TIMESTEP_HZ`)

No circular dependencies. The `main.rs` reduction is clean — only `mod` declarations and `App::new()` builder.

### 3. Dead Code Audit (Boardgame Template)

Three `#[allow(dead_code)]` annotations found:

| File:Line | Item | Verdict |
|-----------|------|---------|
| `board.rs:22` | `struct Square { row, col }` | **Truly dead fields** — struct spawned as component but `row`/`col` never read anywhere |
| `board.rs:30` | `struct Piece { row, col }` | **Truly dead fields** — queried but destructured with `_`, fields never read |
| `bridge.rs:16` | `enum BridgeAction { NavigateWorld, NavigateCheckpoint, OpenEmbed }` | **Truly dead** — all 3 variants never constructed; bridge pipeline is scaffolding copied from 2D template |

**Detail:**
- `Square` and `Piece` are used as ECS component markers (spawned, queried for entity identification), but their `row`/`col` fields are redundant — the game state is tracked entirely through the `GameState` resource in `rules.rs`, not through component data. The fields could be removed, converting them to unit structs.
- `BridgeAction` is the entire inter-frame communication system copied from the 2D template. The `send_bridge_actions` system runs every frame and would dispatch `NavigateWorld`/`NavigateCheckpoint`/`OpenEmbed` via `postMessage`, but nothing in the boardgame code ever populates `PendingBridgeAction`. It's dead scaffolding.

### 4. Proposed Naming Conventions

#### Constant Naming Pattern
```rust
// Module prefix + descriptive name
const CAMERA_MIN_ZOOM_SCALE: f32 = 0.5;
const CAMERA_DRAG_THRESHOLD_PX: f32 = 10.0;
const DIALOG_BACKDROP_COLOR: Color = Color::srgba(0.0, 0.0, 0.0, 0.75);
```

**Rules:**
1. **SCREAMING_SNAKE_CASE** for all constants
2. **Domain prefix**: `CAMERA_`, `DIALOG_`, `HOTSPOT_`, `PLAYER_`, `BOARD_`, `UI_`
3. **Unit suffix** where ambiguous: `_PX` (pixels), `_SECS` (seconds), `_HZ` (hertz)
4. **Z-layers as a numbered set**: `Z_BACKGROUND`, `Z_HOTSPOT`, `Z_LABEL`, `Z_DIALOG`, `Z_UI`
5. **Color constants**: `{DOMAIN}_{ELEMENT}_COLOR` (e.g., `DIALOG_TEXT_COLOR`, `HOTSPOT_HOVER_COLOR`)

#### File Organization
Each template should have a `constants.rs` module exporting all named constants for that template:

```rust
// templates/2d/src/constants.rs
// Room geometry
pub const ROOM_WIDTH: f32 = 1280.0;
pub const ROOM_HEIGHT: f32 = 720.0;
pub const ROOM_HALF_WIDTH: f32 = ROOM_WIDTH / 2.0;
pub const ROOM_HALF_HEIGHT: f32 = ROOM_HEIGHT / 2.0;

// Z-layers (rendering order)
pub const Z_BACKGROUND: f32 = 0.0;
pub const Z_BACKGROUND_IMAGE: f32 = 0.5;
pub const Z_HOTSPOT: f32 = 1.0;
pub const Z_LABEL: f32 = 2.0;
pub const Z_DIALOG_BACKDROP: f32 = 9.5;
pub const Z_DIALOG_TEXT: f32 = 10.0;

// Camera
pub const CAMERA_MAX_ZOOM_IN_SCALE: f32 = 0.5;
pub const CAMERA_DRAG_THRESHOLD_PX: f32 = 10.0;
// ... etc
```

For the boardgame template, define `BOARD_SIZE: u8 = 8` once and derive all other values from it.

## Architecture Notes

- All three templates share the same `BridgePlugin` pattern for JS interop (postMessage to parent iframe), making a shared crate feasible
- The 3D template already has good constant naming in `shared/src/protocol.rs` — client/main.rs is where the debt accumulated
- The 2D template has the worst duplication: dialog rendering is literally copy-pasted between interaction.rs and debug.rs
- Templates are independent crates (no shared Rust code between them currently), so constants.rs per template is the right granularity

## Risks and Considerations

1. **Bevy 0.18 const Color limitations**: `Color::srgba()` may not be usable in `const` contexts depending on the Bevy version — verify before extracting color constants. May need `const fn` wrappers or lazy_static.
2. **Constants file could grow large**: For the 2D template with 60+ magic numbers, the constants file would be substantial. Group by domain section with clear headers.
3. **3D module split touches system scheduling**: The chained systems (cursor_lock → toggle → camera → mesh_sync) must stay in the same `add_systems` call or use explicit ordering constraints. Moving systems to modules doesn't break this, but the scheduling in main.rs must reference all cross-module systems.
4. **Dead code removal in boardgame**: Removing `Square`/`Piece` fields changes the public API of those structs. If mayors or Claude Code sessions are generating boardgame code that references `.row`/`.col`, this would break them. Check CLAUDE.md templates.

## Recommendations

### Priority 1 — Quick Wins
1. **Extract board dimension constant** in boardgame template — single change eliminates 14 magic numbers
2. **Extract Z-layer constants** in all templates — makes rendering order explicit and maintainable
3. **De-duplicate dialog rendering** in 2D template — extract helper function used by both interaction.rs and debug.rs

### Priority 2 — Systematic Cleanup
4. **Create `constants.rs`** per template with all named constants
5. **Split 3D client main.rs** into 5 modules (camera, input, scene, player, connection)
6. **Remove dead fields** from boardgame `Square`/`Piece` (convert to unit structs)

### Priority 3 — Cross-Template
7. **Extract shared BridgePlugin** crate for 2D and boardgame templates
8. **Remove dead BridgeAction scaffolding** from boardgame template (or wire it up to actual interactions)
9. **Standardize window dimensions** — both 2D and 3D use 1280×720, boardgame uses 800×800; consider making these configurable or at least named constants
