//! Bevy WASM client with Lightyear networking.
//!
//! Connects to the game server via WebSocket, provides a fly camera (WASD + mouse),
//! renders other players as pill meshes (capsules), and shows a ground plane.

use bevy::input::mouse::AccumulatedMouseMotion;
use bevy::prelude::*;
use core::net::{Ipv4Addr, SocketAddr};
use core::time::Duration;
use lightyear::netcode::NetcodeClient;
use lightyear::prelude::client::input::*;
use lightyear::prelude::client::*;
use lightyear::prelude::input::native::*;
use lightyear::prelude::*;
use lightyear::websocket::client::WebSocketTarget;
use shared::protocol::*;

// --- Entry Point ---

fn main() {
    let server_port = get_server_port();
    let client_id = get_client_id();

    let tick_duration = Duration::from_secs_f64(1.0 / FIXED_TIMESTEP_HZ);
    let server_addr = SocketAddr::new(Ipv4Addr::LOCALHOST.into(), server_port);

    let mut app = App::new();

    // Full Bevy with rendering for the client
    app.add_plugins(DefaultPlugins.set(WindowPlugin {
        primary_window: Some(Window {
            title: "Creative Mode".to_string(),
            resolution: (1280, 720).into(),
            prevent_default_event_handling: true,
            ..default()
        }),
        ..default()
    }));

    // Lightyear client plugin
    app.add_plugins(lightyear::prelude::client::ClientPlugins { tick_duration });

    // Shared protocol
    app.add_plugins(ProtocolPlugin);

    // Spawn client connection entity with WebSocket transport
    let ws_config = {
        #[cfg(target_family = "wasm")]
        {
            lightyear::prelude::client::ClientConfig::default()
        }
        #[cfg(not(target_family = "wasm"))]
        {
            lightyear::prelude::client::ClientConfig::builder().with_no_cert_validation()
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
            target: WebSocketTarget::Addr(Default::default()),
        },
    ));

    // Client systems
    app.add_systems(Startup, (connect_to_server, setup_scene));
    app.add_systems(
        FixedPreUpdate,
        buffer_input.in_set(InputSystems::WriteClientInputs),
    );
    app.add_systems(FixedUpdate, client_movement);
    app.add_systems(Update, fly_camera);
    app.add_systems(Update, sync_player_meshes);
    app.add_observer(handle_predicted_spawn);
    app.add_observer(handle_interpolated_spawn);

    // Camera state resource
    app.init_resource::<FlyCameraState>();

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

/// Marker for the fly camera.
#[derive(Component)]
struct FlyCamera;

/// State for fly camera rotation.
#[derive(Resource, Default)]
struct FlyCameraState {
    yaw: f32,
    pitch: f32,
}

/// Set up the 3D scene: ground plane, lighting, and camera.
fn setup_scene(
    mut commands: Commands,
    mut meshes: ResMut<Assets<Mesh>>,
    mut materials: ResMut<Assets<StandardMaterial>>,
) {
    // Ground plane
    commands.spawn((
        Mesh3d(meshes.add(Plane3d::default().mesh().size(200.0, 200.0))),
        MeshMaterial3d(materials.add(StandardMaterial {
            base_color: Color::srgb(0.3, 0.5, 0.3),
            ..default()
        })),
        Transform::from_translation(Vec3::ZERO),
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

    // Fly camera
    commands.spawn((
        Camera3d::default(),
        Transform::from_xyz(0.0, 5.0, 10.0).looking_at(Vec3::ZERO, Vec3::Y),
        FlyCamera,
    ));
}

// --- Fly Camera ---

/// WASD + mouse fly camera system.
fn fly_camera(
    time: Res<Time>,
    keys: Res<ButtonInput<KeyCode>>,
    accumulated_mouse: Res<AccumulatedMouseMotion>,
    mut camera_state: ResMut<FlyCameraState>,
    mut camera_query: Query<&mut Transform, With<FlyCamera>>,
    mouse_buttons: Res<ButtonInput<MouseButton>>,
) {
    let Ok(mut transform) = camera_query.single_mut() else {
        return;
    };

    // Mouse look (only when right mouse button is held)
    if mouse_buttons.pressed(MouseButton::Right) {
        let sensitivity = 0.003;
        camera_state.yaw -= accumulated_mouse.delta.x * sensitivity;
        camera_state.pitch -= accumulated_mouse.delta.y * sensitivity;
        camera_state.pitch = camera_state.pitch.clamp(-1.5, 1.5);
    }

    transform.rotation =
        Quat::from_rotation_y(camera_state.yaw) * Quat::from_rotation_x(camera_state.pitch);

    // Movement
    let speed = if keys.pressed(KeyCode::ShiftLeft) || keys.pressed(KeyCode::ShiftRight) {
        30.0
    } else {
        10.0
    };

    let forward = transform.forward().as_vec3();
    let right = transform.right().as_vec3();
    let up = Vec3::Y;

    let mut velocity = Vec3::ZERO;
    if keys.pressed(KeyCode::KeyW) {
        velocity += forward;
    }
    if keys.pressed(KeyCode::KeyS) {
        velocity -= forward;
    }
    if keys.pressed(KeyCode::KeyA) {
        velocity -= right;
    }
    if keys.pressed(KeyCode::KeyD) {
        velocity += right;
    }
    if keys.pressed(KeyCode::Space) {
        velocity += up;
    }
    if keys.pressed(KeyCode::ControlLeft) || keys.pressed(KeyCode::ControlRight) {
        velocity -= up;
    }

    if velocity.length_squared() > 0.0 {
        velocity = velocity.normalize() * speed * time.delta_secs();
        transform.translation += velocity;
    }
}

// --- Input Buffering ---

/// Read keyboard input and buffer it for Lightyear to send to the server.
fn buffer_input(
    mut query: Query<&mut ActionState<PlayerInput>, With<InputMarker<PlayerInput>>>,
    keys: Res<ButtonInput<KeyCode>>,
    camera_query: Query<&Transform, With<FlyCamera>>,
) {
    let Ok(mut action_state) = query.single_mut() else {
        return;
    };

    let Ok(cam_transform) = camera_query.single() else {
        return;
    };

    let forward = cam_transform.forward().as_vec3();
    let right = cam_transform.right().as_vec3();

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

// --- Entity Spawn Handling ---

/// Marker component for entities that have had their mesh spawned.
#[derive(Component)]
struct PlayerMeshSpawned;

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
fn sync_player_meshes(
    mut commands: Commands,
    mut meshes: ResMut<Assets<Mesh>>,
    mut materials: ResMut<Assets<StandardMaterial>>,
    new_players: Query<
        (Entity, &PlayerPosition, &PlayerColor),
        (
            Or<(With<Predicted>, With<Interpolated>)>,
            Without<PlayerMeshSpawned>,
        ),
    >,
    mut existing_players: Query<
        (&PlayerPosition, &mut Transform),
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
            PlayerMeshSpawned,
        ));
    }

    // Update transforms for existing player meshes
    for (pos, mut transform) in existing_players.iter_mut() {
        transform.translation = pos.0 + Vec3::new(0.0, 0.9, 0.0);
    }
}
