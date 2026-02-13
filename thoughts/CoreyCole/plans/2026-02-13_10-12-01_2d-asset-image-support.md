# 2D Template Asset & Image Support — Implementation Plan

## Overview

Add image/asset support to the 2D room world template. Room definitions and images are loaded at runtime via HTTP from the harness's `data/shared-assets/` directory — no WASM rebuild needed to swap content. A simple upload endpoint lets users push images to the server. Assets are stored at the harness level (shared between world checkpoints) and served via the existing `/assets/*` route.

## Current State Analysis

### What exists:
- **3D template** has a working asset pipeline: `AssetPlugin { file_path: "/assets" }` + Trunk proxy `[[proxy]] backend = "http://127.0.0.1:8080/assets/"` + harness serves from `data/shared-assets/` at `GET /assets/*`
- **Harness** serves shared assets at `/assets/*` (`server.go:514-546`) with path traversal protection, custom MIME types, and cache headers. Directory auto-created at startup (`main.go:37-39`)
- **2D template** has no runtime asset loading — rooms are `include_str!` JSON with solid-color backgrounds and text-only hotspots. `AssetPlugin` uses default `file_path` ("assets") and has no Trunk proxy
- **postMessage bridge** already handles harness→WASM communication for debug queries (`index.html:30-51`, `debug.rs`)

### What's missing:
- No Trunk proxy in 2D template's `Trunk.toml`
- No HTTP asset path configured in 2D `AssetPlugin`
- Room JSON is compiled into WASM via `include_str!` — can't be changed without rebuild
- No image fields in the room JSON schema (`RoomDef`, `HotspotDef`)
- No asset upload handlers in the harness
- No reload mechanism to hot-swap content

### Key Discoveries:
- Bevy 0.18 `Sprite` has an `image: Handle<Image>` field — just load an image handle and set it (`room.rs:145-153`)
- `AssetMetaCheck::Never` is already configured in 2D template (`lib.rs:27`) — critical for HTTP-served assets
- The `2d` Bevy feature already includes PNG/JPEG image loading support via `bevy_render` → `bevy_image`
- Bevy supports custom `AssetLoader` implementations — we can register a JSON loader for room definitions
- `spawn_room()` at `room.rs:141` only takes `Commands` and `&RoomDef` — needs `AssetServer` added
- `interaction.rs:40-46` sets `sprite.color` directly for hover highlight — this tints image sprites and needs adjustment
- The existing `index.html` message listener pattern (debug queries) can be extended for reload commands

## Desired End State

After implementation:

1. **Room JSON loaded at runtime** via HTTP (not compiled into WASM). Editing room files on disk and triggering a reload updates the game without rebuilding.
2. **Room JSON schema** supports optional `background_image` on rooms and optional `image` on hotspots, with paths relative to `/assets/`
3. **2D Bevy client** loads room definitions and images from the harness via HTTP (same pipeline as the 3D template)
4. **Simple upload endpoint** lets users push images to `data/shared-assets/` via multipart POST
5. **Reload trigger** via postMessage lets the harness tell the WASM client to re-fetch room data

### Verification:
- Edit `rooms/lobby.json` in `data/shared-assets/`, trigger reload → room updates without WASM rebuild
- Add `"background_image": "rooms/lobby-bg.png"` to lobby.json → room renders with that image as background
- Add `"image": "sprites/tree.png"` to a hotspot → hotspot renders as a sprite instead of a translucent rectangle
- Upload an image via `curl -F "file=@test.png" http://localhost:8080/api/assets/upload` → file lands in `data/shared-assets/`
- Assets persist across checkpoint forks (shared at harness level)
- Trunk dev mode proxies asset requests to the harness correctly

## What We're NOT Doing

- Per-world or per-checkpoint asset isolation (all assets are shared via `data/shared-assets/`)
- Database tables for asset metadata (filesystem is the source of truth)
- Asset tags, search, or browse UI (can be added later when there's a workflow that needs it)
- Image processing/thumbnailing on upload (serve originals only)
- Drag-and-drop reordering or canvas-based asset placement
- Asset versioning, history, or deletion management
- Audio/video asset support (images only for now)

## Implementation Approach

Three phases, each independently testable:

1. **Wire up 2D Bevy client** to load assets from harness (Trunk proxy + AssetPlugin config)
2. **Runtime room loading + image fields** (custom RoomAsset loader, async load state, remove `include_str!`, image schema, hover fix)
3. **Simple upload endpoint + reload trigger** (multipart upload to filesystem, postMessage reload bridge)

---

## Phase 1: Wire Up 2D Asset Pipeline

### Overview
Configure the 2D template to load assets from the harness via HTTP, matching the 3D template's pattern.

### Changes Required:

#### 1. Add Trunk proxy rule
**File**: `templates/2d/Trunk.toml`
**Changes**: Add proxy rule to forward `/assets/` requests to the harness

```toml
[build]
target = "index.html"
filehash = true
minify = "on_release"

[tools]
wasm_bindgen = "0.2.108"

[[proxy]]
backend = "http://127.0.0.1:8080/assets/"
```

#### 2. Configure AssetPlugin for HTTP loading
**File**: `templates/2d/src/lib.rs`
**Changes**: Set `file_path` to `"/assets"` so Bevy's WASM HTTP asset reader fetches from the proxied path

```rust
.set(AssetPlugin {
    file_path: "/assets".to_string(),
    meta_check: AssetMetaCheck::Never,
    ..default()
})
```

### Success Criteria:

#### Automated Verification:
- [ ] `cargo clippy --target wasm32-unknown-unknown -- -D warnings` passes (from `templates/2d/`)
- [ ] `trunk build` succeeds (from `templates/2d/`)

#### Manual Verification:
- [ ] Place a test image at `data/shared-assets/test.png`
- [ ] In the 2D template, temporarily add `asset_server.load::<Image>("test.png")` and verify no 404 in browser console
- [ ] Existing room rendering (solid colors, text) still works

---

## Phase 2: Runtime Room Loading + Image Fields

### Overview
Replace compile-time `include_str!` room loading with HTTP-based runtime loading via a custom Bevy `AssetLoader`. Add optional image fields to the room schema. Room definitions become runtime data that can be swapped without rebuilding WASM.

### Design: Runtime Room Loading

Room JSON files move from `templates/2d/rooms/` (compiled into WASM) to `data/shared-assets/rooms/` (served by harness at runtime). A custom `RoomAssetLoader` deserializes JSON into a `RoomAsset` type. Room navigation becomes async: request a room load → wait for HTTP fetch to complete → spawn entities.

```
Before: include_str!("rooms/lobby.json") → parse → spawn (synchronous)
After:  asset_server.load("rooms/lobby.json") → HTTP fetch → asset ready → spawn (async)
```

The room is blank for ~1 frame while JSON loads over HTTP — same latency pattern as image loading, which is acceptable.

### Design: Room JSON Schema Extensions

Optional image fields coexist with the existing color-based system:

**Room background** — `background_image` is optional. When present, rendered as a full-screen sprite on top of the background color (which acts as a fallback/loading color):

```json
{
    "id": "lobby",
    "name": "Lobby",
    "background_color": "#1a1a2e",
    "background_image": "rooms/lobby-bg.png",
    "hotspots": [...]
}
```

**Hotspot images** — `image` is optional. When present, the hotspot renders as a sprite instead of a translucent rectangle. The `width`/`height` still define the click bounds:

```json
{
    "id": "tree",
    "label": "Old Tree",
    "x": 300.0,
    "y": 300.0,
    "width": 120.0,
    "height": 180.0,
    "image": "sprites/tree.png",
    "action": { "type": "dialog", "text": "An ancient tree..." }
}
```

All image paths are relative to `/assets/` (which maps to `data/shared-assets/` on the harness).

### Changes Required:

#### 1. Create RoomAsset type and JSON loader
**File**: `templates/2d/src/room.rs`
**Changes**: Define `RoomAsset` as a Bevy `Asset` with a custom JSON loader

```rust
use bevy::asset::{AssetLoader, LoadContext, io::Reader};

/// Room definition loaded as a Bevy asset from JSON.
#[derive(Asset, TypePath, Deserialize, Clone, Debug)]
pub struct RoomAsset {
    pub id: String,
    pub name: String,
    pub background_color: String,
    #[serde(default)]
    pub background_image: Option<String>,
    pub hotspots: Vec<HotspotDef>,
}

#[derive(Default)]
pub struct RoomAssetLoader;

impl AssetLoader for RoomAssetLoader {
    type Asset = RoomAsset;
    type Settings = ();
    type Error = std::io::Error;

    async fn load(
        &self,
        reader: &mut dyn Reader,
        _settings: &Self::Settings,
        _load_context: &mut LoadContext<'_>,
    ) -> Result<Self::Asset, Self::Error> {
        let mut bytes = Vec::new();
        reader.read_to_end(&mut bytes).await?;
        serde_json::from_slice(&bytes)
            .map_err(|e| std::io::Error::new(std::io::ErrorKind::InvalidData, e))
    }

    fn extensions(&self) -> &[&str] {
        &["room.json"]
    }
}
```

Note: The exact `AssetLoader` trait signature may differ in Bevy 0.18 — verify against docs. The `extensions()` method uses `.room.json` so it doesn't conflict with other JSON files. Room files should be named `lobby.room.json`, `garden.room.json`, etc. Alternatively, use `.json` extension and register the loader only for the `RoomAsset` type path.

**Important design decision — file extension**: Using `.room.json` avoids the loader intercepting all `.json` files. But it means renaming room files. Alternative: use plain `.json` and rely on Bevy's typed loading (`asset_server.load::<RoomAsset>("rooms/lobby.json")`) to dispatch to the correct loader. Bevy 0.18 supports this — the type parameter selects the loader. **Prefer plain `.json` with typed loading** to keep filenames simple.

#### 2. Add async room loading state machine
**File**: `templates/2d/src/room.rs`
**Changes**: Replace synchronous room loading with a handle-based async pattern

```rust
/// Tracks the current room load state.
#[derive(Resource)]
pub enum RoomLoadState {
    /// No room load in progress.
    Idle,
    /// Waiting for room asset to load from HTTP.
    Loading {
        handle: Handle<RoomAsset>,
        room_id: String,
    },
    /// Room is loaded and spawned.
    Ready,
}
```

Remove the `ROOM_DATA` const, `include_str!` calls, and `find_room()` function entirely.

**New system: `start_room_load`** — replaces `handle_room_navigation`'s synchronous spawn:
```rust
fn start_room_load(
    mut commands: Commands,
    mut pending: ResMut<PendingNavigation>,
    mut load_state: ResMut<RoomLoadState>,
    asset_server: Res<AssetServer>,
    room_entities: Query<Entity, With<RoomEntity>>,
) {
    let Some(room_id) = pending.0.take() else { return; };

    // Despawn current room entities.
    for entity in room_entities.iter() {
        commands.entity(entity).despawn();
    }

    // Start async load.
    let handle = asset_server.load::<RoomAsset>(format!("rooms/{room_id}.json"));
    *load_state = RoomLoadState::Loading { handle, room_id };
}
```

**New system: `finish_room_load`** — watches for the asset to become ready and spawns:
```rust
fn finish_room_load(
    mut commands: Commands,
    mut load_state: ResMut<RoomLoadState>,
    mut current_room: ResMut<CurrentRoom>,
    room_assets: Res<Assets<RoomAsset>>,
    asset_server: Res<AssetServer>,
) {
    let RoomLoadState::Loading { handle, room_id } = &*load_state else { return; };

    let Some(room) = room_assets.get(handle) else { return; }; // Not loaded yet.

    spawn_room(&mut commands, room, &asset_server);
    current_room.0 = room_id.clone();
    *load_state = RoomLoadState::Ready;
}
```

**Update `load_initial_room`** — triggers the first room load instead of spawning directly:
```rust
fn load_initial_room(mut pending: ResMut<PendingNavigation>) {
    pending.0 = Some("lobby".to_string());
}
```

**Register the loader and systems in `RoomPlugin::build`**:
```rust
impl Plugin for RoomPlugin {
    fn build(&self, app: &mut App) {
        app.init_asset::<RoomAsset>();
        app.init_asset_loader::<RoomAssetLoader>();
        app.insert_resource(PendingNavigation(None));
        app.insert_resource(CurrentRoom("lobby".to_string()));
        app.insert_resource(RoomLoadState::Idle);
        app.add_systems(Startup, setup_camera);
        app.add_systems(Startup, load_initial_room.after(setup_camera));
        app.add_systems(Update, (start_room_load, finish_room_load).chain());
    }
}
```

#### 3. Extend HotspotDef with image field
**File**: `templates/2d/src/room.rs`
**Changes**: Add optional image field to `HotspotDef`

```rust
#[derive(Deserialize, Clone, Debug)]
pub struct HotspotDef {
    pub id: String,
    pub label: String,
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
    #[serde(default)]
    pub image: Option<String>,
    pub action: ActionDef,
}
```

Note: `RoomAsset` already includes `background_image: Option<String>` from step 1. `RoomDef` is replaced by `RoomAsset` — the old struct is deleted.

#### 4. Update spawn_room() to accept AssetServer and load images
**File**: `templates/2d/src/room.rs`
**Changes**: Update signature and spawning logic

```rust
pub fn spawn_room(commands: &mut Commands, room: &RoomAsset, asset_server: &AssetServer) {
    let bg_color = parse_hex_color(&room.background_color);

    // Background color (always present, acts as loading fallback)
    commands.spawn((
        Sprite {
            color: bg_color,
            custom_size: Some(Vec2::new(1280.0, 720.0)),
            ..default()
        },
        Transform::from_xyz(0.0, 0.0, 0.0),
        RoomEntity,
    ));

    // Background image (optional, rendered on top of color)
    if let Some(ref path) = room.background_image {
        commands.spawn((
            Sprite {
                image: asset_server.load::<Image>(path),
                custom_size: Some(Vec2::new(1280.0, 720.0)),
                ..default()
            },
            Transform::from_xyz(0.0, 0.0, 0.5),
            RoomEntity,
        ));
    }

    // Room title
    // ... (unchanged)

    for hotspot in &room.hotspots {
        let center_x = hotspot.x + hotspot.width / 2.0 - 640.0;
        let center_y = -(hotspot.y + hotspot.height / 2.0 - 360.0);
        let bounds = Rect::from_center_size(
            Vec2::new(center_x, center_y),
            Vec2::new(hotspot.width, hotspot.height),
        );

        if let Some(ref path) = hotspot.image {
            // Image hotspot — sprite with loaded image
            commands.spawn((
                Sprite {
                    image: asset_server.load::<Image>(path),
                    custom_size: Some(Vec2::new(hotspot.width, hotspot.height)),
                    ..default()
                },
                Transform::from_xyz(center_x, center_y, 1.0),
                Hotspot { id: hotspot.id.clone(), label: hotspot.label.clone(), bounds, action: hotspot.action.clone() },
                HasImage, // marker for hover system
                RoomEntity,
            ));
        } else {
            // Color hotspot — translucent rectangle (existing behavior)
            commands.spawn((
                Sprite {
                    color: Color::srgba(1.0, 1.0, 1.0, 0.1),
                    custom_size: Some(Vec2::new(hotspot.width, hotspot.height)),
                    ..default()
                },
                Transform::from_xyz(center_x, center_y, 1.0),
                Hotspot { id: hotspot.id.clone(), label: hotspot.label.clone(), bounds, action: hotspot.action.clone() },
                RoomEntity,
            ));
        }

        // Hotspot label (unchanged)
        // ...
    }
}
```

#### 5. Fix hover highlight for image hotspots
**File**: `templates/2d/src/interaction.rs`
**Changes**: Image hotspots need a different hover effect than color hotspots

Currently `hotspot_hover` sets `sprite.color` directly. For color-only hotspots this changes opacity. For image hotspots, setting color to `Color::srgba(1.0, 1.0, 1.0, 0.3)` makes the image 30% opaque — wrong.

Add a `HasImage` marker component to distinguish:

```rust
/// Marker: this hotspot has an image sprite (not a color sprite).
#[derive(Component)]
pub struct HasImage;
```

Update `hotspot_hover` to handle both cases:

```rust
fn hotspot_hover(
    windows: Query<&Window, With<PrimaryWindow>>,
    camera_q: Query<(&Camera, &GlobalTransform)>,
    mut color_hotspots: Query<(&Hotspot, &mut Sprite), Without<HasImage>>,
    mut image_hotspots: Query<(&Hotspot, &mut Sprite), With<HasImage>>,
) {
    // ... cursor_pos calculation unchanged ...

    // Color hotspots: change opacity (existing behavior)
    for (hotspot, mut sprite) in color_hotspots.iter_mut() {
        if hotspot.bounds.contains(cursor_pos) {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.3);
        } else {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.1);
        }
    }

    // Image hotspots: brighten on hover (tint toward white)
    for (hotspot, mut sprite) in image_hotspots.iter_mut() {
        if hotspot.bounds.contains(cursor_pos) {
            sprite.color = Color::srgba(1.2, 1.2, 1.2, 1.0); // slight brighten
        } else {
            sprite.color = Color::WHITE; // normal
        }
    }
}
```

#### 6. Copy default room JSON to shared-assets during template init
**File**: `harness/internal/world/manager.go` (or wherever 2D worlds are initialized)
**Changes**: When creating a 2D world, copy `templates/2d/rooms/*.json` to `data/shared-assets/rooms/`

This seeds the runtime room data from the template's defaults. The files are then editable on disk without touching the WASM binary.

Note: If rooms are global (shared across all 2D worlds), this copy only needs to happen once. If per-world rooms are desired later, copy to a world-scoped path. For now, global is fine — matches how `data/shared-assets/` already works.

#### 7. Delete compile-time room data
**File**: `templates/2d/src/room.rs`
**Changes**: Remove `ROOM_DATA`, `include_str!` calls, and `find_room()` entirely

The `templates/2d/rooms/` directory still exists as the source-of-truth for default room content, but it's no longer compiled into the binary. It's copied to `data/shared-assets/rooms/` at world init time (step 6).

### Success Criteria:

#### Automated Verification:
- [ ] `cargo clippy --target wasm32-unknown-unknown -- -D warnings` passes
- [ ] Existing room JSON (without image fields) still deserializes correctly as `RoomAsset`
- [ ] `trunk build` succeeds

#### Manual Verification:
- [ ] Rooms load from `data/shared-assets/rooms/` via HTTP (not compiled in)
- [ ] Edit `lobby.json` on disk → trigger reload → room updates without WASM rebuild
- [ ] Place test images in `data/shared-assets/rooms/` and `data/shared-assets/sprites/`
- [ ] Add `"background_image": "rooms/test-bg.png"` to `lobby.json` → background image renders
- [ ] Add `"image": "sprites/test.png"` to a hotspot → sprite renders at the hotspot position
- [ ] Remove the image fields → rooms fall back to solid colors and translucent rectangles
- [ ] Hover highlight: color hotspots change opacity, image hotspots brighten
- [ ] Click detection still works (bounds-based, not sprite-based)
- [ ] Room navigation (despawn/respawn) properly cleans up all entities
- [ ] Brief blank frame during room load is acceptable (not a white flash)

---

## Phase 3: Simple Upload Endpoint + Reload Trigger

### Overview
Add a multipart file upload endpoint that writes images to `data/shared-assets/` (no database — filesystem is the source of truth). Add a reload mechanism so the harness can tell the WASM client to re-fetch room data after content changes.

### Design: Upload

A single `POST /api/assets/upload` endpoint. No database tables, no tags, no browse UI. The filesystem *is* the asset index — `os.ReadDir` provides listing when needed later.

### Design: Reload Bridge

Extend the existing postMessage bridge (`index.html` message listener) with a `reload-room` command. The harness overlay gets a "Reload" button that sends this message to the iframe. A Bevy system detects the flag and re-navigates to the current room, forcing a fresh HTTP fetch.

```
Edit rooms/lobby.json on disk (Claude, user, or upload)
  → Click "Reload" in overlay (or trigger via debug command)
    → Harness sends postMessage { type: "reload-room" } to iframe
      → JS sets window.__reloadRoom = true
        → Bevy system: despawn room → re-load asset → re-spawn
```

### Changes Required:

#### 1. Add upload route
**File**: `harness/internal/server/server.go`
**Changes**: Add upload route under `approved` group in `RegisterRoutes()` (NOT in `registerWorldRoutes` — assets are global). Use `/api/assets/` prefix to avoid conflict with the existing `GET /assets/*` wildcard.

```go
// Asset upload (approved users) — uses /api/assets/ to avoid conflict with GET /assets/* file serving
approved.POST("/api/assets/upload", s.handleAssetUpload)
```

#### 2. Implement upload handler
**File**: `harness/internal/server/assets.go` (new file in `server` package)
**Changes**: Multipart file upload handler

```go
func (s *Server) handleAssetUpload(c echo.Context) error {
    // 1. Parse multipart form
    file, err := c.FormFile("file")
    if err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "missing file")
    }

    // 2. Read folder from form (default: root)
    folder := c.FormValue("folder")

    // 3. Validate MIME type (image/png, image/jpeg, image/webp, image/gif)
    // 4. Sanitize filename: strip path components, reject "..", normalize
    // 5. Sanitize folder: reject "..", leading "/", normalize
    // 6. Create target directory: data/shared-assets/{folder}/
    // 7. Write file to data/shared-assets/{folder}/{filename}
    //    - If file already exists, return 409 Conflict (don't silently overwrite)
    // 8. Return JSON with the asset path: {"path": "folder/filename.png"}
}
```

Upload size limit: 10MB via Echo's `BodyLimit` middleware.

Allowed MIME types: `image/png`, `image/jpeg`, `image/webp`, `image/gif`.

**Filename conflict handling**: Return 409 if the file already exists. The caller must choose a different name or explicitly delete first. This avoids silent overwrites.

#### 3. Add reload-room command to postMessage bridge
**File**: `templates/2d/index.html`
**Changes**: Extend the message listener to handle `reload-room`

```js
window.addEventListener('message', async (event) => {
  // Existing debug-query handler...
  if (event.data?.type === 'debug-query') { /* ... unchanged ... */ }

  // New: reload trigger
  if (event.data?.type === 'reload-room') {
    window.__reloadRoom = true;
  }
});
```

#### 4. Add reload detection system in Bevy
**File**: `templates/2d/src/room.rs` (or `bridge.rs`)
**Changes**: A system that checks `window.__reloadRoom` and triggers re-navigation

```rust
#[cfg(target_family = "wasm")]
fn check_reload_request(
    current_room: Res<CurrentRoom>,
    mut pending: ResMut<PendingNavigation>,
) {
    use wasm_bindgen::prelude::*;

    let Some(window) = web_sys::window() else { return; };
    let flag = js_sys::Reflect::get(&window, &JsValue::from_str("__reloadRoom"))
        .unwrap_or(JsValue::FALSE);

    if flag.as_bool().unwrap_or(false) {
        // Clear the flag
        let _ = js_sys::Reflect::set(&window, &JsValue::from_str("__reloadRoom"), &JsValue::FALSE);
        // Re-navigate to current room (triggers despawn + fresh HTTP fetch)
        pending.0 = Some(current_room.0.clone());
    }
}
```

**Cache busting**: When re-loading the same room, Bevy's `AssetServer` may return the cached handle. To force a fresh HTTP fetch, the reload system should call `asset_server.reload()` on the current room handle before re-navigating, or the `start_room_load` system should detect same-room navigation and explicitly reload.

#### 5. Add "Reload" button to overlay top bar
**File**: `harness/views/world/overlay.templ`
**Changes**: Add a reload button next to the existing "Tree" toggle

```go
@button.Button(button.ButtonArgs{Size: "sm", Variant: "outline",
    Attributes: templ.Attributes{
        "data-on:click": "document.getElementById('game-frame').contentWindow.postMessage({type:'reload-room'},'*')",
    },
}) {
    Reload
}
```

This is a client-side-only interaction — no server round-trip needed. The button sends a postMessage directly to the game iframe.

#### 6. Register reload system
**File**: `templates/2d/src/lib.rs` or `room.rs`
**Changes**: Add `check_reload_request` to the Update schedule (WASM-only)

```rust
#[cfg(target_family = "wasm")]
app.add_systems(Update, check_reload_request.before(start_room_load));
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint` passes
- [ ] `cargo clippy --target wasm32-unknown-unknown -- -D warnings` passes
- [ ] `trunk build` succeeds

#### Manual Verification:
- [ ] `curl -F "file=@test.png" -F "folder=rooms" -b "session=$COOKIE" http://localhost:8080/api/assets/upload` succeeds
- [ ] File appears at `data/shared-assets/rooms/test.png`
- [ ] File is accessible at `http://localhost:8080/assets/rooms/test.png` (no auth, via existing wildcard route)
- [ ] Uploading duplicate filename returns 409
- [ ] Path traversal attempts (`../`, absolute paths) are rejected
- [ ] Edit `rooms/lobby.json` on disk → click "Reload" in overlay → room updates in game
- [ ] Reload preserves camera position (it's a static Camera2d, so this is automatic)
- [ ] Multiple rapid reloads don't cause entity leaks or panics

---

## Testing Strategy

### Unit Tests:
- JSON deserialization: `RoomAsset` with and without optional image fields (backward compat)
- Filename sanitization logic (strip paths, reject `..`)
- Folder path validation (reject `..`, leading `/`)

### Integration Tests:
- Upload → file on disk → accessible via `/assets/*` route
- Upload duplicate → 409

### Manual Testing Steps:
1. Start harness with `just live`
2. Open 2D world, verify rooms load from HTTP (check network tab — requests to `/assets/rooms/lobby.json`)
3. Verify existing rooms render correctly (solid colors, text, hotspots)
4. Upload a PNG via curl to `data/shared-assets/rooms/`
5. Edit `lobby.json` to add `"background_image": "rooms/uploaded-image.png"`
6. Click "Reload" in overlay → background image appears (no WASM rebuild)
7. Add `"image": "sprites/some-sprite.png"` to a hotspot
8. Click "Reload" → sprite renders, click detection works, hover highlight works correctly for both image and color hotspots

## Performance Considerations

- **Room loading latency**: Room JSON is small (~1KB), so HTTP fetch adds negligible delay (~1 frame). The room may be blank for a single frame during load. Background color renders immediately as a fallback.
- **Image size**: No resizing on upload — Bevy handles GPU textures. Large images (>4096px) may cause issues on some WebGL2 implementations. Consider logging a warning for oversized uploads.
- **Asset caching**: Bevy caches loaded assets in memory. Room reloads need explicit cache invalidation (via `asset_server.reload()`). Images cached normally — replacing an image file on disk requires reload to pick up the change.
- **Upload size**: 10MB limit is generous for 2D sprites. Most game sprites are well under 1MB.

## Migration Notes

- **Room file location change**: Room JSON moves from `templates/2d/rooms/` (compiled in) to `data/shared-assets/rooms/` (runtime). The template directory still holds the default files; they're copied to `shared-assets` on world creation.
- **No data migration needed**: No database changes in this plan.
- **Existing room JSON**: Works as-is with `RoomAsset` (all new fields are `Option` with `#[serde(default)]`).
- **WASM binary no longer contains room data**: The binary is smaller and more reusable. Room content is fully decoupled from the game engine.

## Future Enhancements (Not In Scope)

These are natural follow-ups but not part of this plan:

- **Asset browse UI**: Overlay panel showing `os.ReadDir` results with thumbnails, click-to-copy path
- **Per-world rooms**: Load from `data/worlds/{worldID}/rooms/` instead of shared path
- **Auto-reload**: File watcher on `data/shared-assets/` triggers reload automatically (no manual button click)
- **Room editor**: Visual editor in the overlay for positioning hotspots (drag-and-drop)
- **Asset database**: If tagging/search becomes needed, add DB tables then

## References

- Handoff: `thoughts/CoreyCole/handoffs/general/2026-02-13_09-58-54_2d-template-rendering-fix-and-image-planning.md`
- 3D template asset pattern: `templates/3d/client/Trunk.toml:9-10` (proxy), `templates/3d/client/src/main.rs:54-58` (AssetPlugin)
- Harness shared assets handler: `harness/internal/server/server.go:514-546`
- 2D room spawning: `templates/2d/src/room.rs:141-207`
- 2D AssetPlugin config: `templates/2d/src/lib.rs:26-29`
- 2D postMessage bridge: `templates/2d/index.html:30-51`, `templates/2d/src/bridge.rs`
- 2D hover interaction: `templates/2d/src/interaction.rs:40-46`
- Overlay top bar: `harness/views/world/overlay.templ:36-55`
