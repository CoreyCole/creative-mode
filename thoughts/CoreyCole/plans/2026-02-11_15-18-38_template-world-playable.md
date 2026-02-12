# Template World — Visual Scene + End-to-End Playability

## Overview

Make the game template visually interesting and fully playable end-to-end. Incorporate a bloom_3d-inspired scene into the template, fix the camera-player relationship so the camera IS the player, ensure game servers start after initial builds, and auto-create + auto-redirect users to a template world.

## Current State Analysis

### What works:
- Multiplayer architecture is in place: Lightyear netcode, server-authoritative movement, client-side prediction, interpolation for remote players
- Server headless binary runs at 64Hz, receives client input, applies `shared_movement()`, replicates positions
- Client compiles to WASM via Trunk, connects over WebSocket
- Players are represented as colored capsule meshes

### What's broken/missing:
1. **Visually boring** — flat green plane, no interesting geometry or lighting
2. **Camera/player disconnect** — fly camera and player capsule both respond to WASD but move independently at different rates; they drift apart
3. **No game server after initial build** — `CreateWorld` runs `Builder.Build()` in a background goroutine but does NOT start a game server afterward. Only the Claude orchestrator pipeline (`BuildCheckpoint`) starts game servers. Result: freshly created worlds have empty iframes.
4. **No default world** — users always land on the lobby, no world exists until someone creates one
5. **No auto-redirect** — approved users see the lobby, not a world

### Key Discoveries:
- `template/client/src/main.rs:215-273` — `fly_camera()` moves camera independently from player
- `template/client/src/main.rs:278-321` — `buffer_input()` reads WASD + camera transform, writes PlayerInput
- `template/client/src/main.rs:326-332` — `client_movement()` applies shared_movement to Predicted PlayerPosition
- `template/client/src/main.rs:378-415` — `sync_player_meshes()` renders capsules at PlayerPosition
- `harness/internal/world/manager.go:146-173` — initial build goroutine: builds but doesn't start game server
- `harness/internal/server/server.go:52-80` — `handleRoot` always renders lobby for approved users
- `harness/internal/server/server.go:262` — explicit comment: "Do NOT call GameServers.Connect here"

## Desired End State

After this plan:
1. User logs in → auto-redirected to the template world (lobby accessible via nav)
2. Template world has a visually striking bloom scene with glowing emissive spheres
3. Camera IS the player — WASD moves the player, camera position syncs to player position, mouse look rotates camera
4. Other players appear as colored capsules in the world
5. Game server starts automatically after the initial build completes

### Verification:
- Open two browser sessions, log in as different users
- Both land in the template world
- Each sees the other as a colored capsule
- WASD moves the player (camera moves with it)
- Right-click + mouse rotates the camera view
- Bloom spheres are visible and bouncing

## What We're NOT Doing

- Adding multiple starter worlds (Phase 2)
- Game server restart on harness restart (follow-up)
- Third-person camera mode
- Player collision or physics
- Any changes to the harness overlay UI (views/ directory — parallel tailwind migration)
- Chat integration with game state

## Implementation Approach

Four sequential steps. Template code changes first (client scene + camera), then harness changes (game server + auto-create).

---

## Phase 1: Bloom Scene in Template Client

### Overview
Replace the boring green plane scene with a bloom_3d-inspired environment: glowing emissive spheres bouncing in a dark space with HDR bloom post-processing. Keep a ground plane for orientation.

### Changes Required:

#### 1. Client scene setup
**File**: `template/client/src/main.rs`
**Changes**: Replace `setup_scene()` with bloom-based scene. Add bloom imports + bouncing system.

Add to imports:
```rust
use bevy::core_pipeline::tonemapping::Tonemapping;
use bevy::post_process::bloom::Bloom;
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
```

Replace `setup_scene()`:
```rust
fn setup_scene(
    mut commands: Commands,
    mut meshes: ResMut<Assets<Mesh>>,
    mut materials: ResMut<Assets<StandardMaterial>>,
) {
    // Ground plane (dark, reflective)
    commands.spawn((
        Mesh3d(meshes.add(Plane3d::default().mesh().size(200.0, 200.0))),
        MeshMaterial3d(materials.add(StandardMaterial {
            base_color: Color::srgb(0.05, 0.05, 0.08),
            perceptual_roughness: 0.3,
            metallic: 0.8,
            ..default()
        })),
    ));

    // Emissive sphere grid (bloom_3d-inspired)
    let mesh = meshes.add(Sphere::new(0.4).mesh().ico(5).unwrap());
    let mat_blue = materials.add(StandardMaterial {
        emissive: LinearRgba::rgb(0.0, 0.0, 150.0),
        ..default()
    });
    let mat_white = materials.add(StandardMaterial {
        emissive: LinearRgba::rgb(1000.0, 1000.0, 1000.0),
        ..default()
    });
    let mat_red = materials.add(StandardMaterial {
        emissive: LinearRgba::rgb(50.0, 0.0, 0.0),
        ..default()
    });
    let mat_dark = materials.add(StandardMaterial {
        base_color: Color::BLACK,
        ..default()
    });

    for x in -5..5 {
        for z in -5..5 {
            let mut hasher = DefaultHasher::new();
            (x, z).hash(&mut hasher);
            let rand = (hasher.finish() + 3) % 6;
            let (material, scale) = match rand {
                0 => (mat_blue.clone(), 0.5),
                1 => (mat_white.clone(), 0.1),
                2 => (mat_red.clone(), 1.0),
                3..=5 => (mat_dark.clone(), 1.5),
                _ => unreachable!(),
            };
            commands.spawn((
                Mesh3d(mesh.clone()),
                MeshMaterial3d(material),
                Transform::from_xyz(x as f32 * 2.0, 0.0, z as f32 * 2.0)
                    .with_scale(Vec3::splat(scale)),
                Bouncing,
            ));
        }
    }

    // Subtle directional light for ground reflections
    commands.spawn((
        DirectionalLight {
            illuminance: 500.0,
            ..default()
        },
        Transform::from_rotation(Quat::from_euler(EulerRot::XYZ, -0.5, 0.5, 0.0)),
    ));
}

#[derive(Component)]
struct Bouncing;

fn bounce_spheres(time: Res<Time>, mut query: Query<&mut Transform, With<Bouncing>>) {
    for mut transform in query.iter_mut() {
        transform.translation.y =
            (transform.translation.x + transform.translation.z + time.elapsed_secs()).sin();
    }
}
```

Register `bounce_spheres` in `Update` systems.

#### 2. Camera setup
**Changes**: Modify camera spawn to use bloom. The camera entity will be set up with `Bloom::NATURAL` and `Tonemapping::TonyMcMapface`, black clear color.

The camera spawn currently happens in `setup_scene`. Move it to spawn with bloom settings:
```rust
commands.spawn((
    Camera3d::default(),
    Camera {
        clear_color: ClearColorConfig::Custom(Color::BLACK),
        ..default()
    },
    Tonemapping::TonyMcMapface,
    Bloom::NATURAL,
    Transform::from_xyz(0.0, 5.0, 10.0).looking_at(Vec3::ZERO, Vec3::Y),
));
```

### Success Criteria:

#### Automated Verification:
- [ ] Template compiles: `cd template && cargo build --release -p client --target wasm32-unknown-unknown`
- [ ] Server compiles: `cd template && cargo build --release -p server`
- [ ] No warnings: `cd template && cargo clippy --workspace`

#### Manual Verification:
- [ ] Scene renders with glowing spheres and dark background when loaded in browser
- [ ] Spheres bounce with sine wave animation
- [ ] Bloom glow is visible around emissive spheres

---

## Phase 2: Camera = Player

### Overview
Make the camera position track the predicted player's `PlayerPosition`. Mouse look still rotates the camera independently. Remove the duplicate WASD position movement from the fly camera. Other players still render as capsules.

### Changes Required:

#### 1. Refactor fly_camera → mouse_look
**File**: `template/client/src/main.rs`
**Changes**: Strip all position movement from `fly_camera`. Keep only mouse look (yaw/pitch on right-click). Rename to `mouse_look`.

```rust
fn mouse_look(
    mut state: ResMut<FlyCameraState>,
    mouse_motion: Res<AccumulatedMouseMotion>,
    mouse_button: Res<ButtonInput<MouseButton>>,
    mut camera: Query<&mut Transform, With<Camera3d>>,
) {
    if !mouse_button.pressed(MouseButton::Right) {
        return;
    }
    let Ok(mut transform) = camera.single_mut() else { return };
    let sensitivity = 0.003;
    state.yaw -= mouse_motion.delta.x * sensitivity;
    state.pitch -= mouse_motion.delta.y * sensitivity;
    state.pitch = state.pitch.clamp(-1.5, 1.5);
    transform.rotation = Quat::from_euler(EulerRot::YXZ, state.yaw, state.pitch, 0.0);
}
```

#### 2. Add camera-follows-player system
**File**: `template/client/src/main.rs`
**Changes**: New system that syncs camera translation to the predicted player's position.

```rust
fn sync_camera_to_player(
    player: Query<&PlayerPosition, With<Predicted>>,
    mut camera: Query<&mut Transform, With<Camera3d>>,
) {
    let Ok(pos) = player.single() else { return };
    let Ok(mut cam) = camera.single_mut() else { return };
    cam.translation = pos.0 + Vec3::new(0.0, 1.6, 0.0); // eye height offset
}
```

Register in `Update` after `sync_player_meshes`.

#### 3. Don't render capsule for local player
**File**: `template/client/src/main.rs`
**Changes**: In `sync_player_meshes`, skip spawning a mesh for entities with `Predicted` marker. Remote players (with `Interpolated`) still get capsules.

Change the new-player query filter from `(With<Predicted>, With<Interpolated>).or()` to just `With<Interpolated>`. The predicted local player becomes invisible (first-person perspective).

#### 4. Update system registration
**File**: `template/client/src/main.rs`
**Changes**: Replace `fly_camera` with `mouse_look` and add `sync_camera_to_player`.

```rust
.add_systems(Update, (mouse_look, sync_player_meshes, sync_camera_to_player).chain())
```

### Success Criteria:

#### Automated Verification:
- [ ] Template compiles: `cd template && cargo build --release -p client --target wasm32-unknown-unknown`
- [ ] Server compiles: `cd template && cargo build --release -p server`

#### Manual Verification:
- [ ] Camera position follows player movement (WASD moves camera)
- [ ] Right-click + mouse rotates camera view
- [ ] No visible capsule for local player (first-person)
- [ ] Open second browser: other player appears as capsule in the world
- [ ] Other player's capsule moves as they move

---

## Phase 3: Game Server Auto-Start After Initial Build

### Overview
Currently `CreateWorld` runs the initial build in a background goroutine but does not start a game server afterward. Fix this so the game server starts and the port is persisted to the DB, making the world playable as soon as the build finishes.

### Changes Required:

#### 1. Start game server after initial build
**File**: `harness/internal/world/manager.go`
**Changes**: In the `CreateWorld` background goroutine (lines 146-173), after a successful build:
1. Call `GameServers.Connect(worldID, cpID, cpDir)` to start the game server
2. Update the checkpoint's `server_port` in the DB
3. Publish a build completion event so SSE clients get notified

```go
// After successful build...
_, _ = m.db.UpdateCheckpointStatus(bgCtx, sqlc.UpdateCheckpointStatusParams{
    Status: "ready",
    ID:     cpID,
})
m.Builder.PostBuild(cp)

// Start game server and persist port.
srv, srvErr := m.GameServers.Connect(worldID, cpID, cpDir)
if srvErr != nil {
    m.logger.Error("failed to start game server", "error", srvErr)
} else {
    _, _ = m.db.UpdateCheckpointServerPort(bgCtx, sqlc.UpdateCheckpointServerPortParams{
        ServerPort: sql.NullInt64{Int64: int64(srv.Port), Valid: true},
        ID:         cpID,
    })
}
```

Note: Need to verify `UpdateCheckpointServerPort` exists in sqlc queries. If not, add it.

#### 2. Check for existing sqlc query
**File**: `harness/internal/db/sqlc/` — search for `UpdateCheckpointServerPort` or similar. If it doesn't exist, add a migration or query.

### Success Criteria:

#### Automated Verification:
- [ ] Harness builds: `cd harness && go build ./...`
- [ ] Lint passes: `cd harness && just lint`

#### Manual Verification:
- [ ] Create a world from the lobby
- [ ] Wait for build to complete (watch server logs)
- [ ] Refresh the world page — iframe loads with game running
- [ ] Game server process is running on an allocated port

---

## Phase 4: Auto-Create Template World + Auto-Redirect

### Overview
On first startup, auto-create a "Template World" so users have something to play with immediately. Redirect approved users directly to the template world instead of the lobby.

### Changes Required:

#### 1. Auto-create template world on startup
**File**: `harness/main.go`
**Changes**: After creating the WorldManager and before starting the HTTP server, check if any worlds exist. If not, create a "Template World".

```go
// After WorldManager creation, before server.Start()...
worlds, _ := database.ListWorlds(ctx)
if len(worlds) == 0 {
    logger.Info("no worlds found, creating template world")
    _, err := worldManager.CreateWorld(ctx, "Template World", "The default creative sandbox", "system")
    if err != nil {
        logger.Error("failed to create template world", "error", err)
    }
}
```

The "system" userID is a sentinel — the template world isn't owned by any real user. Need to ensure this doesn't break FK constraints (the `created_by` field is nullable in the schema — use empty string or handle in CreateWorld).

#### 2. Auto-redirect approved users to template world
**File**: `harness/internal/server/server.go`
**Changes**: In `handleRoot`, for approved users, find the first world and redirect to it.

```go
// In handleRoot, after confirming user is approved:
worlds, err := s.DB.ListWorlds(ctx)
if err != nil {
    return err
}
if len(worlds) > 0 {
    return c.Redirect(http.StatusTemporaryRedirect, "/world/"+worlds[0].ID)
}
// Fall through to lobby if somehow no worlds exist
return render(c, lobby.Page(&user, worlds))
```

#### 3. Lobby remains accessible
The world view overlay already has a "Lobby" link in the top bar. Users can navigate to `GET /` — but since `handleRoot` now redirects, we may need a dedicated `/lobby` route. Or, better: only auto-redirect if the user has no explicit intent (first visit). Simplest approach: add a query param `?lobby=1` that skips the redirect.

```go
if len(worlds) > 0 && c.QueryParam("lobby") == "" {
    return c.Redirect(http.StatusTemporaryRedirect, "/world/"+worlds[0].ID)
}
```

Update the "Lobby" link in the overlay to use `/?lobby=1`.

### Success Criteria:

#### Automated Verification:
- [ ] Harness builds: `cd harness && go build ./...`
- [ ] Lint passes: `cd harness && just lint`

#### Manual Verification:
- [ ] Start harness fresh (empty DB) — "Template World" auto-created in logs
- [ ] Log in as new user → approved → redirected to template world
- [ ] Click "Lobby" in overlay → see lobby with Template World listed
- [ ] World page shows build status, then game loads when build completes

---

---

## Phase 5: Playwright E2E Verification

### Overview
Verify the template world loads, the game canvas renders, and player input works — all via `playwright-cli`. This phase also updates `harness/E2E_PLAYBOOK.md` with a new test section.

### How Playwright Interacts with the Game

The Bevy WASM game runs inside an `<iframe id="game-frame">` as a WebGL/WebGPU `<canvas>`. Playwright can:

| Action | How | Works? |
|--------|-----|--------|
| Verify iframe loaded | `snapshot` — check `#game-frame` has `src="/wasm/..."` | Yes |
| Verify canvas exists | `run-code` — `page.frameLocator('#game-frame').locator('canvas')` | Yes |
| Take screenshot | `screenshot` — captures full page including rendered WebGL | Yes |
| Send WASD keys | Click iframe to focus, then `keyboard.press` / `keyboard.down`+`keyboard.up` | Yes |
| Send mouse look | `run-code` — dispatch synthetic `mousedown`(right)+`mousemove` events to canvas | Yes (complex) |
| Read camera position | Not possible without game-side debug API | No |

**Verification strategy**: Screenshot comparison. Take screenshot before input, send keys, take screenshot after — if they differ, the player moved and the scene is rendering.

### Test Steps

```bash
# T9: Template World Auto-Load
# Prerequisites: harness running fresh, no existing worlds

# T9.1: Login and verify redirect
playwright-cli -s=admin open http://localhost:8080 --headed --persistent
playwright-cli -s=admin snapshot
playwright-cli -s=admin fill <username-ref> "test-admin"
playwright-cli -s=admin selectOption <role-ref> "admin"
playwright-cli -s=admin click <login-btn-ref>
# Expected: redirected to /world/<templateWorldID>

# T9.2: Verify world page structure
playwright-cli -s=admin snapshot
# Expected: #game-frame iframe present, overlay visible

# T9.3: Wait for build + game server
# Poll until iframe has src attribute (build complete, game server started)
playwright-cli -s=admin run-code "async page => {
  for (let i = 0; i < 60; i++) {
    const src = await page.locator('#game-frame').getAttribute('src');
    if (src && src.includes('/wasm/')) return 'Game loaded: ' + src;
    await page.waitForTimeout(5000);
  }
  return 'Timeout: game did not load';
}"
# Expected: "Game loaded: /wasm/<worldID>/<cpID>/index.html?server_port=<port>"
# Note: initial build can take several minutes (cargo + trunk)

# T9.4: Verify canvas renders (not blank)
playwright-cli -s=admin screenshot
# Expected: bloom spheres visible — glowing orbs on dark background
# Read the screenshot file to visually verify

# T9.5: Verify player movement via keyboard
playwright-cli -s=admin screenshot  # "before" shot
# Focus the iframe and send movement keys
playwright-cli -s=admin run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const canvas = frame.locator('canvas');
  await canvas.click();
  await page.keyboard.down('KeyW');
  await page.waitForTimeout(1000);
  await page.keyboard.up('KeyW');
  return 'Moved forward for 1 second';
}"
playwright-cli -s=admin screenshot  # "after" shot
# Expected: scene perspective has changed (camera moved forward)

# T9.6: Verify multiplayer — second player joins
playwright-cli -s=user2 open http://localhost:8080 --headed --persistent
playwright-cli -s=user2 snapshot
playwright-cli -s=user2 fill <username-ref> "test-user2"
playwright-cli -s=user2 selectOption <role-ref> "user"
playwright-cli -s=user2 click <login-btn-ref>
# Expected: redirected to template world (same world as admin)

# T9.7: Verify second player sees capsule (admin's avatar)
# Wait for user2's game to connect and render
playwright-cli -s=user2 run-code "async page => {
  const frame = page.frameLocator('#game-frame');
  const canvas = frame.locator('canvas');
  await canvas.click();
  await page.waitForTimeout(3000);
  return 'Connected';
}"
playwright-cli -s=user2 screenshot
# Expected: admin's colored capsule visible in user2's view
# (may need to move user2's camera to look toward origin where admin started)

# T9.8: Console error check
playwright-cli -s=admin console error
playwright-cli -s=user2 console error
# Expected: no JS errors (WASM panics would show here)

# Cleanup
playwright-cli -s=admin close
playwright-cli -s=user2 close
```

### Limitations & Notes

- **Build wait**: The initial `cargo build --release + trunk build --release` can take 5-10 minutes on first run (no cached `target/`). The polling loop in T9.3 accounts for this with a 5-minute timeout (60 iterations * 5s).
- **Visual verification**: Screenshots are the primary verification method. Claude Code is multimodal and can visually inspect the PNG to confirm bloom spheres are rendering.
- **Canvas focus**: The iframe must be clicked before keyboard events reach the Bevy canvas. The `run-code` approach in T9.5 handles this by clicking the canvas inside the frame locator.
- **Mouse look**: Possible via synthetic `MouseEvent` dispatch but complex. Skipped for Phase 1 — WASD movement is sufficient to prove the pipeline works.
- **No position readback**: We cannot read the exact camera/player position from WASM. We rely on screenshot-before/after comparison to verify movement occurred.

### Playbook Update

Add T9 to `harness/E2E_PLAYBOOK.md` following the existing format, with all steps above formatted as the standard table layout.

### Success Criteria:

#### Automated Verification:
- [ ] All playwright-cli commands execute without errors

#### Manual Verification:
- [ ] T9.4 screenshot shows glowing bloom spheres (not black/blank)
- [ ] T9.5 before/after screenshots show camera has moved (scene perspective changed)
- [ ] T9.7 screenshot shows a colored capsule (the other player)
- [ ] T9.8 no JS errors or WASM panics in console

---

## Testing Strategy

### End-to-End Test (via Playwright — Phase 5):
See Phase 5 above. The playwright verification IS the primary testing strategy.

### Smoke Test (manual, faster):
1. Start harness fresh (`just live` with clean DB)
2. Open browser to `http://localhost:8080`
3. Dev login as admin
4. Verify redirect to template world
5. Wait for build, verify bloom scene loads
6. WASD to move around
7. Open second browser tab, login as different user
8. Verify both players see each other

### Unit Tests:
- `sanitizeName` edge cases (already exists, can add test)
- `copyDir` with exclude (already exists, can add test)

## Performance Considerations

- Bloom post-processing adds GPU cost — the `Bloom::NATURAL` preset is lightweight
- 100 spheres (10x10 grid) is minimal geometry
- Game server binary compiled with `--release` for production performance
- Build cache cloning (APFS/hardlinks) keeps initial build fast
- Initial build takes 5-10 minutes cold; subsequent builds use cached `target/`

## References

- Bloom example: `context/bevy/examples/3d/bloom_3d.rs`
- Template client: `template/client/src/main.rs`
- Template protocol: `template/shared/src/protocol.rs`
- World manager: `harness/internal/world/manager.go`
- Server routes: `harness/internal/server/server.go`
- Game server lifecycle: `harness/internal/world/game_server.go`
- E2E playbook: `harness/E2E_PLAYBOOK.md`
