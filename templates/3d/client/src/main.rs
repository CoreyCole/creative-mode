//! Bevy WASM client with Lightyear networking.
//!
//! Connects to the game server via WebSocket, provides first/third-person camera
//! (toggled with V), renders other players as pill meshes (capsules), and shows a ground plane.

#[cfg(target_family = "wasm")]
mod debug;

use bevy::asset::{AssetMetaCheck, AssetPlugin};
use bevy::gltf::GltfAssetLabel;
use bevy::image::{ImageAddressMode, ImageLoaderSettings, ImageSampler, ImageSamplerDescriptor};
use bevy::input::mouse::AccumulatedMouseMotion;
use bevy::math::Affine2;
use bevy::prelude::*;
use bevy::window::{CursorGrabMode, CursorOptions};
use core::net::{Ipv4Addr, SocketAddr};
use core::time::Duration;
use lightyear::netcode::NetcodeClient;
use lightyear::prelude::client::input::*;
use lightyear::prelude::client::*;
use lightyear::prelude::input::native::*;
use lightyear::prelude::*;
use lightyear::websocket::client::{WebSocketScheme, WebSocketTarget};
use serde::Serialize;
use shared::protocol::*;
#[cfg(target_family = "wasm")]
use wasm_bindgen::prelude::*;

// --- Entry Point ---

fn main() {
    let server_port = get_server_port();
    let client_id = get_client_id();

    let tick_duration = Duration::from_secs_f64(1.0 / FIXED_TIMESTEP_HZ);
    let server_addr = SocketAddr::new(Ipv4Addr::LOCALHOST.into(), server_port);

    let mut app = App::new();

    // Full Bevy with rendering for the client
    app.add_plugins(
        DefaultPlugins
            .set(WindowPlugin {
                primary_window: Some(Window {
                    title: "Creative Mode".to_string(),
                    resolution: (1280, 720).into(),
                    prevent_default_event_handling: true,
                    canvas: Some("#bevy-canvas".into()),
                    fit_canvas_to_parent: true,
                    ..default()
                }),
                ..default()
            })
            .set(AssetPlugin {
                file_path: "/assets".to_string(),
                meta_check: AssetMetaCheck::Never,
                ..default()
            }),
    );

    // Lightyear client plugin
    app.add_plugins(lightyear::prelude::client::ClientPlugins { tick_duration });

    // Shared protocol
    app.add_plugins(ProtocolPlugin);

    // Spawn client connection entity with WebSocket transport
    let ws_config = {
        #[cfg(target_family = "wasm")]
        {
            lightyear::prelude::client::ClientConfig
        }
        #[cfg(not(target_family = "wasm"))]
        {
            lightyear::prelude::client::ClientConfig::builder().with_no_encryption()
        }
    };

    let auth = Authentication::Manual {
        server_addr,
        client_id,
        private_key: PRIVATE_KEY,
        protocol_id: PROTOCOL_ID,
    };

    let netcode_config = lightyear::prelude::client::NetcodeConfig {
        client_timeout_secs: 5,
        token_expire_secs: -1,
        ..default()
    };

    app.world_mut().spawn((
        Client::default(),
        Link::new(None::<lightyear::link::RecvLinkConditioner>),
        LocalAddr(SocketAddr::new(Ipv4Addr::UNSPECIFIED.into(), 0)),
        PeerAddr(server_addr),
        ReplicationReceiver::default(),
        PredictionManager::default(),
        Name::from("Client"),
        NetcodeClient::new(auth, netcode_config).expect("Failed to create netcode client"),
        WebSocketClientIo {
            config: ws_config,
            target: WebSocketTarget::Addr(WebSocketScheme::Plain),
        },
    ));

    // Client systems
    app.add_systems(Startup, (connect_to_server, setup_scene));
    app.add_systems(FixedFirst, save_previous_positions);
    app.add_systems(
        FixedPreUpdate,
        buffer_input.in_set(InputSystems::WriteClientInputs),
    );
    app.add_systems(FixedUpdate, client_movement);
    app.add_systems(
        Update,
        (
            cursor_lock_system,
            toggle_camera_mode,
            game_camera,
            sync_player_meshes,
        )
            .chain(),
    );
    app.add_observer(handle_predicted_spawn);
    app.add_observer(handle_interpolated_spawn);

    // Camera state resource
    app.init_resource::<CameraState>();
    app.register_type::<CameraState>();

    // Debug query system (WASM only)
    #[cfg(target_family = "wasm")]
    app.add_systems(Update, debug::process_debug_queries);

    app.run();
}

// --- URL Parameter Reading ---

/// Read server port from URL query parameters (WASM) or default.
fn get_server_port() -> u16 {
    #[cfg(target_family = "wasm")]
    {
        let window = web_sys::window().unwrap();
        let search = window.location().search().unwrap();
        let params = web_sys::UrlSearchParams::new_with_str(&search).unwrap();
        params
            .get("server_port")
            .and_then(|p| p.parse().ok())
            .unwrap_or(9001)
    }
    #[cfg(not(target_family = "wasm"))]
    {
        std::env::var("SERVER_PORT")
            .ok()
            .and_then(|p| p.parse().ok())
            .unwrap_or(9001)
    }
}

/// Generate a random-ish client ID.
fn get_client_id() -> u64 {
    #[cfg(target_family = "wasm")]
    {
        let window = web_sys::window().unwrap();
        let crypto = window.crypto().expect("crypto not available");
        let mut buf = [0u8; 8];
        crypto
            .get_random_values_with_u8_array(&mut buf)
            .expect("getRandomValues failed");
        u64::from_le_bytes(buf)
    }
    #[cfg(not(target_family = "wasm"))]
    {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos() as u64
    }
}

// --- Connection ---

/// Trigger the client to connect to the server.
fn connect_to_server(mut commands: Commands, client: Single<Entity, With<Client>>) {
    commands.trigger(Connect {
        entity: client.into_inner(),
    });
    info!("Connecting to server...");
}

// --- Scene Setup ---

/// Marker for the game camera entity.
#[derive(Component)]
struct GameCamera;

/// Camera perspective mode.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Reflect, Serialize)]
enum CameraMode {
    FirstPerson,
    #[default]
    ThirdPerson,
}

/// Camera state resource controlling view parameters.
#[derive(Resource, Reflect, Serialize)]
#[reflect(Serialize)]
struct CameraState {
    mode: CameraMode,
    yaw: f32,
    pitch: f32,
    distance: f32,
    height_offset: f32,
    cursor_locked: bool,
}

impl Default for CameraState {
    fn default() -> Self {
        Self {
            mode: CameraMode::ThirdPerson,
            yaw: 0.0,
            pitch: -0.3,
            distance: 8.0,
            height_offset: 1.5,
            cursor_locked: false,
        }
    }
}

/// Set up the 3D scene: ground plane, lighting, and camera.
fn setup_scene(
    mut commands: Commands,
    mut meshes: ResMut<Assets<Mesh>>,
    mut materials: ResMut<Assets<StandardMaterial>>,
    asset_server: Res<AssetServer>,
) {
    // Ground plane with tiled checkerboard texture from shared assets
    let ground_texture: Handle<Image> = asset_server.load_with_settings(
        "textures/test-checkerboard.png",
        |settings: &mut ImageLoaderSettings| {
            settings.sampler = ImageSampler::Descriptor(ImageSamplerDescriptor {
                address_mode_u: ImageAddressMode::Repeat,
                address_mode_v: ImageAddressMode::Repeat,
                ..default()
            });
        },
    );
    commands.spawn((
        Mesh3d(meshes.add(Plane3d::default().mesh().size(200.0, 200.0))),
        MeshMaterial3d(materials.add(StandardMaterial {
            base_color_texture: Some(ground_texture),
            uv_transform: Affine2::from_scale(Vec2::splat(25.0)),
            ..default()
        })),
        Transform::from_translation(Vec3::ZERO),
    ));

    // Spawn GLB model from shared assets
    commands.spawn((
        SceneRoot(
            asset_server
                .load(GltfAssetLabel::Scene(0).from_asset("models/bonecrusher_warrior.glb")),
        ),
        Transform::from_xyz(0.0, 0.0, 0.0),
    ));

    // Directional light (sun)
    commands.spawn((
        DirectionalLight {
            illuminance: 10000.0,
            shadows_enabled: true,
            ..default()
        },
        Transform::from_rotation(Quat::from_euler(EulerRot::XYZ, -0.7, 0.3, 0.0)),
    ));

    // Global ambient light
    commands.insert_resource(GlobalAmbientLight {
        color: Color::srgb(0.6, 0.7, 0.9),
        brightness: 300.0,
        ..default()
    });

    // Game camera
    commands.spawn((
        Camera3d::default(),
        Transform::from_xyz(0.0, 5.0, 10.0).looking_at(Vec3::ZERO, Vec3::Y),
        GameCamera,
    ));
}

// --- Parent Frame Communication ---

/// Send a message to the parent window (harness overlay) via postMessage.
/// Used to coordinate cursor lock state with overlay visibility.
#[cfg(target_family = "wasm")]
fn post_message_to_parent(msg_type: &str) {
    let Some(window) = web_sys::window() else {
        return;
    };
    let Ok(Some(parent)) = window.parent() else {
        return;
    };
    let obj = js_sys::Object::new();
    let _ = js_sys::Reflect::set(
        &obj,
        &JsValue::from_str("type"),
        &JsValue::from_str(msg_type),
    );
    let _ = parent.post_message(&obj, "*");
}

#[cfg(not(target_family = "wasm"))]
fn post_message_to_parent(_msg_type: &str) {}

// --- Cursor Lock ---

/// Lock cursor on click, unlock on Escape/Backquote. Detects browser-initiated unlock.
/// Signals the parent harness frame to show/hide the overlay accordingly.
fn cursor_lock_system(
    mouse_buttons: Res<ButtonInput<MouseButton>>,
    keys: Res<ButtonInput<KeyCode>>,
    mut camera_state: ResMut<CameraState>,
    mut cursor_opts: Query<&mut CursorOptions, With<Window>>,
) {
    let Ok(mut cursor) = cursor_opts.single_mut() else {
        return;
    };

    // Click to lock cursor and hide overlay
    if mouse_buttons.just_pressed(MouseButton::Left) && !camera_state.cursor_locked {
        cursor.grab_mode = CursorGrabMode::Locked;
        cursor.visible = false;
        camera_state.cursor_locked = true;
        post_message_to_parent("cursor-locked");
    }

    // Escape unlocks cursor
    if keys.just_pressed(KeyCode::Escape) && camera_state.cursor_locked {
        cursor.grab_mode = CursorGrabMode::None;
        cursor.visible = true;
        camera_state.cursor_locked = false;
    }

    // Backquote unlocks cursor and shows overlay
    if keys.just_pressed(KeyCode::Backquote) && camera_state.cursor_locked {
        cursor.grab_mode = CursorGrabMode::None;
        cursor.visible = true;
        camera_state.cursor_locked = false;
        post_message_to_parent("cursor-unlocked");
    }

    // Detect browser-initiated unlock (WASM Escape interception)
    if camera_state.cursor_locked && cursor.grab_mode == CursorGrabMode::None {
        cursor.visible = true;
        camera_state.cursor_locked = false;
    }
}

// --- Camera Mode Toggle ---

/// Toggle between first-person and third-person with V key.
fn toggle_camera_mode(
    keys: Res<ButtonInput<KeyCode>>,
    mut camera_state: ResMut<CameraState>,
    mut players: Query<&mut Visibility, (With<Predicted>, With<PlayerMeshSpawned>)>,
) {
    if !keys.just_pressed(KeyCode::KeyV) {
        return;
    }

    camera_state.mode = match camera_state.mode {
        CameraMode::FirstPerson => CameraMode::ThirdPerson,
        CameraMode::ThirdPerson => CameraMode::FirstPerson,
    };

    let vis = match camera_state.mode {
        CameraMode::FirstPerson => Visibility::Hidden,
        CameraMode::ThirdPerson => Visibility::Inherited,
    };

    for mut visibility in players.iter_mut() {
        *visibility = vis;
    }
}

// --- Game Camera ---

/// Unified camera system supporting first-person and third-person modes.
fn game_camera(
    time: Res<Time>,
    fixed_time: Res<Time<Fixed>>,
    accumulated_mouse: Res<AccumulatedMouseMotion>,
    mut camera_state: ResMut<CameraState>,
    mut camera_query: Query<&mut Transform, With<GameCamera>>,
    player_query: Query<(&PlayerPosition, Option<&PreviousPlayerPosition>), With<Predicted>>,
) {
    let Ok(mut cam_transform) = camera_query.single_mut() else {
        return;
    };

    // Mouse look when cursor is locked
    if camera_state.cursor_locked {
        let sensitivity = 0.003;
        camera_state.yaw -= accumulated_mouse.delta.x * sensitivity;
        camera_state.pitch -= accumulated_mouse.delta.y * sensitivity;
    }

    // Clamp pitch based on mode
    let pitch_limit = match camera_state.mode {
        CameraMode::FirstPerson => 1.55,
        CameraMode::ThirdPerson => 1.0,
    };
    camera_state.pitch = camera_state.pitch.clamp(-1.55, pitch_limit);

    let yaw = camera_state.yaw;
    let pitch = camera_state.pitch;

    // Get interpolated player position if available
    let alpha = fixed_time.overstep_fraction();
    let player_pos = player_query.iter().next().map(|(p, prev)| {
        if let Some(prev) = prev {
            Vec3::lerp(prev.0, p.0, alpha)
        } else {
            p.0
        }
    });

    match camera_state.mode {
        CameraMode::FirstPerson => {
            if let Some(pos) = player_pos {
                let eye_pos = pos + Vec3::Y * 1.7;
                cam_transform.translation = eye_pos;
            }
            cam_transform.rotation = Quat::from_euler(EulerRot::YXZ, yaw, pitch, 0.0);
        }
        CameraMode::ThirdPerson => {
            let rotation = Quat::from_euler(EulerRot::YXZ, yaw, pitch, 0.0);

            if let Some(pos) = player_pos {
                let pivot = pos + Vec3::Y * camera_state.height_offset;
                let offset = rotation * Vec3::new(0.0, 0.0, camera_state.distance);
                let desired_pos = pivot + offset;
                let desired_rot = Transform::from_translation(desired_pos)
                    .looking_at(pivot, Vec3::Y)
                    .rotation;

                let t = (15.0 * time.delta_secs()).min(1.0);
                cam_transform.translation = cam_transform.translation.lerp(desired_pos, t);
                cam_transform.rotation = cam_transform.rotation.slerp(desired_rot, t);
            } else {
                // Pre-connection: just apply rotation in place
                cam_transform.rotation = rotation;
            }
        }
    }
}

// --- Input Buffering ---

/// Read keyboard input and buffer it for Lightyear to send to the server.
fn buffer_input(
    mut query: Query<&mut ActionState<PlayerInput>, With<InputMarker<PlayerInput>>>,
    keys: Res<ButtonInput<KeyCode>>,
    camera_state: Res<CameraState>,
) {
    let Ok(mut action_state) = query.single_mut() else {
        return;
    };

    // Use yaw only for horizontal movement direction (both modes)
    let yaw_rotation = Quat::from_rotation_y(camera_state.yaw);
    let forward = yaw_rotation * -Vec3::Z;
    let right = yaw_rotation * Vec3::X;

    let mut movement = Vec3::ZERO;
    if keys.pressed(KeyCode::KeyW) {
        movement += forward;
    }
    if keys.pressed(KeyCode::KeyS) {
        movement -= forward;
    }
    if keys.pressed(KeyCode::KeyA) {
        movement -= right;
    }
    if keys.pressed(KeyCode::KeyD) {
        movement += right;
    }
    if keys.pressed(KeyCode::Space) {
        movement += Vec3::Y;
    }
    if keys.pressed(KeyCode::ControlLeft) || keys.pressed(KeyCode::ControlRight) {
        movement -= Vec3::Y;
    }

    if movement.length_squared() > 0.0 {
        movement = movement.normalize();
    }

    let sprint = keys.pressed(KeyCode::ShiftLeft) || keys.pressed(KeyCode::ShiftRight);

    action_state.0 = PlayerInput { movement, sprint };
}

// --- Client-Side Prediction ---

/// Apply inputs to predicted entities (client-side prediction).
fn client_movement(
    mut position_query: Query<(&mut PlayerPosition, &ActionState<PlayerInput>), With<Predicted>>,
) {
    for (position, input) in position_query.iter_mut() {
        shared_movement(position, input);
    }
}

// --- Fixed Timestep Interpolation ---

/// Save current positions before the fixed update so we can interpolate during rendering.
/// Runs in FixedFirst (before movement), so after all fixed steps complete:
/// - PreviousPlayerPosition = position before the last step
/// - PlayerPosition = position after the last step
/// - overstep_fraction lerps between them for smooth per-frame rendering.
fn save_previous_positions(mut query: Query<(&PlayerPosition, &mut PreviousPlayerPosition)>) {
    for (pos, mut prev) in query.iter_mut() {
        prev.0 = pos.0;
    }
}

// --- Entity Spawn Handling ---

/// Marker component for entities that have had their mesh spawned.
#[derive(Component)]
struct PlayerMeshSpawned;

/// Stores the player position from the previous fixed timestep for visual interpolation.
#[derive(Component)]
struct PreviousPlayerPosition(Vec3);

/// When a predicted entity spawns, adjust its color and add InputMarker.
fn handle_predicted_spawn(
    trigger: On<Add, PlayerId>,
    mut predicted: Query<&mut PlayerColor, With<Predicted>>,
    mut commands: Commands,
) {
    let entity = trigger.entity;
    if let Ok(mut color) = predicted.get_mut(entity) {
        let hsva = Hsva {
            saturation: 0.4,
            ..Hsva::from(color.0)
        };
        color.0 = Color::from(hsva);
        info!("Adding InputMarker to predicted entity: {:?}", entity);
        commands
            .entity(entity)
            .insert(InputMarker::<PlayerInput>::default());
    }
}

/// When an interpolated entity spawns, adjust its color saturation.
fn handle_interpolated_spawn(
    trigger: On<Add, PlayerColor>,
    mut interpolated: Query<&mut PlayerColor, With<Interpolated>>,
) {
    if let Ok(mut color) = interpolated.get_mut(trigger.entity) {
        let hsva = Hsva {
            saturation: 0.1,
            ..Hsva::from(color.0)
        };
        color.0 = Color::from(hsva);
    }
}

// --- Mesh Sync ---

/// Spawn/update meshes for player entities that have PlayerPosition and PlayerColor.
/// Uses fixed-timestep interpolation: lerps between previous and current position
/// using Time<Fixed>::overstep_fraction() for smooth per-frame rendering.
#[allow(clippy::type_complexity)]
fn sync_player_meshes(
    mut commands: Commands,
    mut meshes: ResMut<Assets<Mesh>>,
    mut materials: ResMut<Assets<StandardMaterial>>,
    fixed_time: Res<Time<Fixed>>,
    new_players: Query<
        (Entity, &PlayerPosition, &PlayerColor),
        (
            Or<(With<Predicted>, With<Interpolated>)>,
            Without<PlayerMeshSpawned>,
        ),
    >,
    mut existing_players: Query<
        (&PlayerPosition, &PreviousPlayerPosition, &mut Transform),
        (
            Or<(With<Predicted>, With<Interpolated>)>,
            With<PlayerMeshSpawned>,
        ),
    >,
) {
    // Spawn meshes for new player entities
    for (entity, pos, color) in new_players.iter() {
        let capsule = Capsule3d::new(0.3, 1.2);
        commands.entity(entity).insert((
            Mesh3d(meshes.add(capsule)),
            MeshMaterial3d(materials.add(StandardMaterial {
                base_color: color.0,
                ..default()
            })),
            Transform::from_translation(pos.0 + Vec3::new(0.0, 0.9, 0.0)),
            PreviousPlayerPosition(pos.0),
            PlayerMeshSpawned,
        ));
    }

    // Interpolate transforms for existing player meshes
    let alpha = fixed_time.overstep_fraction();
    for (pos, prev_pos, mut transform) in existing_players.iter_mut() {
        let interpolated = Vec3::lerp(prev_pos.0, pos.0, alpha);
        transform.translation = interpolated + Vec3::new(0.0, 0.9, 0.0);
    }
}
