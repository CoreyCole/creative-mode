---
date: 2026-02-12T22:08:41-08:00
researcher: CoreyCole
git_commit: 613ac2b6792fb0059daad7ed523de4b611f59bdb
branch: main
repository: creative-mode
topic: "Multi-template support and 2D world template architecture"
tags: [research, codebase, templates, 2d-world, multi-template, architecture]
status: complete
last_updated: 2026-02-12
last_updated_by: CoreyCole
---

# Research: Multi-Template Support & 2D World Template Architecture

**Date**: 2026-02-12T22:08:41-08:00
**Researcher**: CoreyCole
**Git Commit**: 613ac2b6792fb0059daad7ed523de4b611f59bdb
**Branch**: main
**Repository**: creative-mode

## Research Question

How should we refactor the codebase to support multiple templates (currently only a single 3D Bevy template exists)? Specifically, how do we create a 2D world template where navigation is room-based (clicking interactive elements navigates between "rooms"/worlds/checkpoints), and multiplayer is chat-only?

## Summary

The current system is hardcoded to a single 3D Bevy/Lightyear template. Adding multi-template support requires changes at three layers: (1) the database needs a `template_type` discriminator on worlds, (2) the harness world manager needs to select the correct template directory when creating worlds, and (3) the build pipeline needs to handle different build strategies per template type. The 2D template is dramatically simpler than the 3D one — it's a **client-only WASM app** (no game server needed) where "rooms" are Bevy scenes with clickable sprite hotspots that navigate between worlds/checkpoints via `postMessage` to the harness parent frame.

## Detailed Findings

### Current Architecture: Single Template, Hardcoded

**Template selection is hardcoded in `main.go:41`:**
```go
templateDir, err := filepath.Abs(filepath.Join("..", "template"))
```

This single path is passed to `world.NewManager()` and used for every `CreateWorld()` call. The `worlds` DB table has no `template_type` column — just `id`, `name`, `description`, `created_by`, `created_at`.

**World creation always copies from the same template** (`manager.go:83`):
```go
if err := copyDir(m.templateDir, cpDir, []string{"target"}); err != nil {
```

**The build pipeline assumes Cargo workspace with server+client crates** (`builder.go:86-122`):
1. `cargo build --release -p server` — builds native game server
2. `trunk build --release --dist {wasmDir} ` — builds WASM client from `client/` subdirectory

**Game server startup assumes a native binary** (`game_server.go:192-201`):
- Production: `./target/release/server`
- Dev: `cargo watch -w shared -w server -x 'run -p server'`

### The retro_2d Example (context/retro_2d/)

An older Bevy 0.15.3 project demonstrating interactive 2D sprites. Key patterns:

- **Single-crate structure** — no server crate, no shared crate, just one binary + lib (`Cargo.toml` defines `[[bin]]` and `[lib]`)
- **Bevy 0.15.3** — we need 0.18; the code won't compile directly but the patterns are reusable
- **Interaction system** — custom `Interactable` component with bounding box hit testing, `InteractionState` resource tracking hovered entities per group
- **Drag system** — `Draggable` component with drop strategies (Reset/Leave), Y-axis locking for clothesline effect
- **Item state management** — `ItemState` component tracks normal/glow/selected sprite variants, swaps `Sprite.image` on hover/click
- **Background scaling** — `setup_background()` scales a background image to cover the window while maintaining aspect ratio
- **Embedded assets** — uses `bevy_embedded_assets` (not HTTP-served assets like our current template)

### Proposed 2D Template Architecture

#### Why This Is Simpler

The 2D template eliminates the most complex parts of the current stack:
- **No game server** — no Lightyear, no netcode, no server-authoritative movement, no entity replication
- **No `shared/` crate** — no protocol definitions needed
- **No `server/` crate** — nothing to `cargo build -p server`
- **Chat-only multiplayer** — handled entirely by the existing harness SSE/Datastar system (already works)
- **Room navigation = URL navigation** — clicking a hotspot in the Bevy scene posts a message to the parent frame, which navigates to a different world/checkpoint URL

#### Template File Structure

```
template_2d/
  Cargo.toml           # Single crate (not a workspace)
  Cargo.lock
  client/
    Cargo.toml         # The WASM binary crate
    Trunk.toml         # Trunk build config
    index.html         # Canvas + postMessage bridge
    src/
      main.rs          # App entry, scene setup, camera
      interact.rs      # Click detection on 2D sprites
      rooms.rs         # Room definition, hotspot data, navigation
      assets.rs        # Asset loading state machine
  CLAUDE.md            # Instructions for inner Claude
  CHANGES.txt          # Work summary (same pattern as 3D)
  MEMORY.md            # World-specific design history
```

The single-crate-in-client-dir structure keeps compatibility with the existing build pipeline's assumption that WASM builds happen from a `client/` subdirectory.

#### Core Concepts

**Rooms**: A room is a Bevy scene — a background image with positioned interactive sprites (hotspots). Each room is defined as data (not hardcoded systems).

```rust
#[derive(Resource, Serialize, Deserialize)]
pub struct RoomDefinition {
    pub background: String,           // asset path
    pub hotspots: Vec<Hotspot>,
}

#[derive(Serialize, Deserialize, Clone)]
pub struct Hotspot {
    pub id: String,
    pub sprite: String,              // asset path
    pub sprite_hover: Option<String>, // glow/hover variant
    pub position: Vec2,
    pub size: Vec2,                  // bounding box
    pub action: HotspotAction,
}

#[derive(Serialize, Deserialize, Clone)]
pub enum HotspotAction {
    NavigateRoom(String),            // switch to another room in same world
    NavigateWorld(String),           // navigate to another world ID
    NavigateCheckpoint(String),      // navigate to a specific checkpoint
    OpenEmbed(String),               // YouTube URL, iframe embed
    ShowDialog(String),              // display text overlay
}
```

**Navigation via postMessage**: When a hotspot is clicked, the WASM client sends a message to the parent harness frame:

```rust
// In the Bevy click handler
fn handle_hotspot_click(action: &HotspotAction) {
    match action {
        HotspotAction::NavigateWorld(world_id) => {
            post_message_to_parent(&format!("navigate:/world/{}", world_id));
        }
        HotspotAction::NavigateCheckpoint(cp_id) => {
            post_message_to_parent(&format!("navigate:/world/{}/checkpoint/{}", current_world, cp_id));
        }
        HotspotAction::NavigateRoom(room_id) => {
            // Internal room switch — load a different RoomDefinition
            // No parent navigation needed
        }
        HotspotAction::OpenEmbed(url) => {
            post_message_to_parent(&format!("embed:{}", url));
        }
        HotspotAction::ShowDialog(text) => {
            // Spawn a text overlay in Bevy
        }
    }
}
```

The existing `game-loader.js` already handles `postMessage` from the iframe. We'd add a new message type for navigation:

```javascript
// In game-loader.js, add:
case 'navigate':
    window.location.href = event.data.url;
    break;
case 'embed':
    // Open YouTube/iframe overlay
    break;
```

**Room data as JSON assets**: Rooms can be defined as JSON files loaded via Bevy's asset system:

```json
{
  "background": "room_main.png",
  "hotspots": [
    {
      "id": "tv",
      "sprite": "tv.png",
      "sprite_hover": "tv_glow.png",
      "position": [300, 200],
      "size": [120, 90],
      "action": { "NavigateRoom": "tv_closeup" }
    },
    {
      "id": "door",
      "sprite": "door.png",
      "position": [-200, 0],
      "size": [80, 160],
      "action": { "NavigateWorld": "abc12345" }
    }
  ]
}
```

This is what inner Claude would generate when the user says "add a TV to the room that you can click on."

### Harness Changes Required for Multi-Template

#### 1. Database: Add `template_type` to `worlds`

New migration (`003_template_type.sql`):
```sql
ALTER TABLE worlds ADD COLUMN template_type TEXT NOT NULL DEFAULT '3d';
```

Values: `"3d"` (current Bevy/Lightyear template), `"2d"` (new room-based template).

Update `CreateWorld` query to accept template_type. Update `models.go` (auto-generated by sqlc).

#### 2. Manager: Template Directory Selection

Change `Manager` to hold multiple template directories:

```go
type Manager struct {
    db          *db.DB
    logger      *slog.Logger
    dataDir     string
    templates   map[string]string  // template_type -> directory path
    // ...
}
```

In `CreateWorld()`, accept a `templateType` parameter and copy from `m.templates[templateType]`.

#### 3. Build Pipeline: Template-Specific Build Strategy

The key insight: **2D worlds don't need a game server build step**. The build pipeline currently always runs:
1. `cargo build --release -p server`
2. `trunk build --release --dist ...`

For 2D worlds, it should only run:
1. `trunk build --release --dist ...` (from `client/` subdir)

Options:
- **Option A**: Check if `server/` directory exists in the checkpoint. If not, skip `cargo build -p server`.
- **Option B**: Add a `build_config.json` or similar to each template that specifies build steps.
- **Option C**: Switch on `template_type` in the builder.

Option A is the most pragmatic — it's a simple filesystem check and doesn't require new config formats.

#### 4. Game Server Management: 2D Worlds Don't Need Servers

In `BuildCheckpoint()`, after build succeeds:
- For 3D: start game server via `GameServers.Connect()` (current behavior)
- For 2D: skip game server startup, just serve the WASM

The world page (`world.templ`) already handles the case where `serverPort == 0` — it renders the iframe without a `?server_port=` parameter. The 2D client would simply not read `server_port` since it doesn't connect to a game server.

#### 5. Lobby UI: Template Selection

Add a template selector to the world creation form:

```html
<select name="template_type">
    <option value="3d">3D World (multiplayer)</option>
    <option value="2d">2D Room World (click to explore)</option>
</select>
```

#### 6. CLAUDE.md Per Template

Each template needs its own `CLAUDE.md` with instructions specific to that world type:

- `template/CLAUDE.md` — existing 3D Bevy/Lightyear instructions
- `template_2d/CLAUDE.md` — 2D room-based instructions, room JSON format, hotspot patterns, asset requirements

### The Clothesline Example (Starter Content)

The retro_2d example's clothesline with a draggable hoodie translates to our room system as:

**Room: "backyard"**
- Background: clothesline scene (cows_and_basket.png equivalent)
- Hotspot: hoodie shirt on the clothesline
  - Hover: glow effect (sprite swap)
  - Click: `NavigateRoom("shirt_closeup")` — zoom into the shirt

**Room: "shirt_closeup"**
- Background: zoomed-in view of the shirt
- Hotspot: tag on the shirt
  - Click: `ShowDialog("Made in Creative Mode")` or `NavigateWorld("shop_world_id")`

This gives inner Claude a clear pattern to extend: "Add a TV to the room" → add a hotspot to the room JSON with a `NavigateRoom("tv_closeup")` action → create a new room JSON for the close-up → add a VCR hotspot with `OpenEmbed("https://youtube.com/...")`.

### Connection to Existing Checkpoint/World System

The 2D room navigation maps beautifully onto the existing checkpoint tree:

- **Different rooms in the same world** = different checkpoints of the same world. Each checkpoint's WASM build shows a different scene. Clicking a hotspot with `NavigateCheckpoint(cpID)` loads a different checkpoint's WASM.
- **Different worlds** = entirely separate creative spaces. Clicking a hotspot with `NavigateWorld(worldID)` navigates to a different world.
- **Branching via prompts** = the existing fork/prompt system. A user says "add a secret door behind the bookshelf" → Claude edits the room JSON to add a new hotspot → new checkpoint with the door.

The checkpoint tree visualization already exists in the UI, so users can navigate between "rooms" (checkpoints) both via in-game clicks and via the overlay's checkpoint tree.

## Code References

- `harness/main.go:41` — hardcoded template directory path
- `harness/internal/world/manager.go:62-207` — `CreateWorld()` with single template copy
- `harness/internal/world/manager.go:83` — `copyDir(m.templateDir, ...)`
- `harness/internal/build/builder.go:86-122` — build pipeline: `cargo build -p server` + `trunk build`
- `harness/internal/world/game_server.go:112-231` — game server tmux session startup
- `harness/internal/db/migrations/001_initial.sql:18-24` — worlds table (no template_type)
- `harness/views/lobby/lobby.templ:50-62` — world creation form (no template selector)
- `harness/views/world/world.templ:10-37` — iframe rendering with server_port check
- `harness/static/game-loader.js:1-42` — postMessage bridge (needs navigate handler)
- `template/CLAUDE.md` — current 3D template instructions
- `context/retro_2d/lib/interact/interact.rs` — 2D interaction/bounding-box system
- `context/retro_2d/lib/world/clothes.rs` — clickable sprite with hover/selected states
- `context/retro_2d/lib/assets.rs` — asset loading state machine

## Architecture Insights

### Key Design Decisions

1. **Template as a directory convention, not a plugin system**: Templates are just directories of source code that get copied. The harness doesn't need to understand the template's internal structure deeply — it just needs to know what build commands to run.

2. **2D template = client-only WASM**: The biggest simplification. No server binary, no port allocation, no tmux game server sessions. The "multiplayer" is the existing harness chat system. The "navigation" is URL-based.

3. **Room data as JSON, not Rust code**: This is critical for inner Claude. JSON room definitions are easier for Claude to edit than Rust ECS code. The Bevy systems for rendering rooms and handling clicks are generic — Claude edits the data, not the rendering logic.

4. **postMessage as the navigation bridge**: The existing `game-loader.js` postMessage pattern naturally extends to navigation. The WASM client tells the parent frame where to go, and the harness handles the URL change. This keeps the WASM client isolated in its iframe.

5. **Checkpoints as rooms**: The existing checkpoint system provides "branching rooms" for free. Each checkpoint is a different version of the room — the fork/edit/build cycle creates new rooms naturally.

### Risks and Considerations

- **Bevy for 2D is heavy for simple scenes**: A 2D point-and-click game could be pure HTML/CSS/JS. Using Bevy/WASM adds ~3-5MB of WASM download. The tradeoff is: Claude already knows how to edit Bevy code from the 3D template, and we get animations, particle effects, shaders for free.
- **Asset pipeline**: 2D worlds need images (backgrounds, sprites). These need to be served from somewhere. The existing harness `/assets/` endpoint could work, or images could be embedded in the WASM via `bevy_embedded_assets`. HTTP-served is better for iterability (Claude can add new images without rebuilding WASM).
- **Room JSON schema evolution**: As features are added (animations, sound, parallax), the room JSON format will grow. A Bevy custom asset loader would be the right approach.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/component-4-bevy-game-template.md` — original plan for the 3D Bevy template
- `thoughts/CoreyCole/plans/component-3-world-management-build.md` — world management and build pipeline design
- `thoughts/CoreyCole/research/2026-02-11_22-28-57_rebuild-hot-reload-wasm-assets.md` — WASM asset serving research

## Open Questions

1. **Should 2D worlds skip the server build entirely, or should we keep a minimal server for future expansion (e.g., real-time collaborative room editing)?**
   - Recommendation: Skip for now. The harness chat system provides multiplayer. Add a server later if needed.

2. **Should room navigation happen within a single WASM instance (internal state change) or via full page/iframe reload (navigate to different checkpoint)?**
   - Recommendation: Both. `NavigateRoom` switches scenes internally (fast). `NavigateWorld`/`NavigateCheckpoint` navigates the browser (loads a different WASM build).

3. **How should Claude be instructed to create room content? Free-form Rust code, or structured JSON that gets loaded?**
   - Recommendation: JSON room definitions loaded by generic Rust rendering code. Claude edits JSON for content, Rust for behavior. This is more reliable than having Claude write Bevy ECS code for every scene.

4. **Should we create `template_2d/` as a new top-level directory alongside `template/`, or restructure to `templates/3d/` and `templates/2d/`?**
   - Recommendation: `templates/3d/` and `templates/2d/` — cleaner organization as we add more template types.
