//! Headless Bevy game server with Lightyear networking.
//!
//! Listens for WebSocket connections from WASM clients, spawns player entities,
//! and replicates positions to all connected clients.

use bevy::app::ScheduleRunnerPlugin;
use bevy::log::LogPlugin;
use bevy::prelude::*;
use bevy::remote::{http::RemoteHttpPlugin, RemotePlugin};
use core::net::{Ipv4Addr, SocketAddr};
use core::time::Duration;
use lightyear::connection::client::Connected;
use lightyear::netcode::NetcodeServer;
use lightyear::prelude::input::native::*;
use lightyear::prelude::server::NetcodeConfig;
use lightyear::prelude::server::*;
use lightyear::prelude::*;
use shared::protocol::*;

fn main() {
    let port = std::env::var("GAME_PORT")
        .ok()
        .and_then(|p| p.parse::<u16>().ok())
        .unwrap_or(9001);

    let brp_port = std::env::var("BRP_PORT")
        .ok()
        .and_then(|p| p.parse::<u16>().ok())
        .unwrap_or(port + 1000);

    info!(
        "Starting game server on port {}, BRP on port {}",
        port, brp_port
    );

    let tick_duration = Duration::from_secs_f64(1.0 / FIXED_TIMESTEP_HZ);

    let mut app = App::new();

    // Headless server: minimal plugins + schedule runner
    app.add_plugins(
        MinimalPlugins.set(ScheduleRunnerPlugin::run_loop(Duration::from_secs_f64(
            1.0 / FIXED_TIMESTEP_HZ,
        ))),
    );
    app.add_plugins(LogPlugin::default());

    // Lightyear server plugin
    app.add_plugins(lightyear::prelude::server::ServerPlugins { tick_duration });

    // Shared protocol
    app.add_plugins(ProtocolPlugin);

    // Spawn server connection entity
    let server_addr = SocketAddr::new(Ipv4Addr::UNSPECIFIED.into(), port);

    let config = lightyear::websocket::server::ServerConfig::builder()
        .with_bind_address(server_addr)
        .with_no_encryption();

    app.world_mut().spawn((
        Name::from("Server"),
        NetcodeServer::new(NetcodeConfig {
            protocol_id: PROTOCOL_ID,
            private_key: PRIVATE_KEY,
            ..Default::default()
        }),
        LocalAddr(server_addr),
        WebSocketServerIo { config },
    ));

    // BRP debug server (JSON-RPC 2.0 over HTTP)
    app.add_plugins(RemotePlugin::default());
    app.add_plugins(
        RemoteHttpPlugin::default()
            .with_port(brp_port)
            .with_header("Access-Control-Allow-Origin", "*"),
    );

    // Register server systems
    app.add_systems(Startup, start_server);
    app.add_systems(FixedUpdate, movement);
    app.add_observer(handle_new_client);
    app.add_observer(handle_connected);

    app.run();
}

/// Trigger the server to start listening.
fn start_server(mut commands: Commands, server: Single<Entity, With<Server>>) {
    commands.trigger(Start {
        entity: server.into_inner(),
    });
    info!("Server started and listening for connections");
}

/// When a new client link is created, add a ReplicationSender so we can replicate entities to it.
fn handle_new_client(trigger: On<Add, LinkOf>, mut commands: Commands) {
    commands.entity(trigger.entity).insert((
        ReplicationSender::new(REPLICATION_INTERVAL, SendUpdatesMode::SinceLastAck, false),
        Name::from("Client"),
    ));
}

/// When a client is confirmed as connected, spawn a player entity for them.
fn handle_connected(
    trigger: On<Add, Connected>,
    query: Query<&RemoteId, With<ClientOf>>,
    mut commands: Commands,
) {
    let Ok(client_id) = query.get(trigger.entity) else {
        return;
    };
    let client_id = client_id.0;

    let entity = commands
        .spawn((
            PlayerBundle::new(client_id, Vec3::ZERO),
            // Replicate to all connected clients
            Replicate::to_clients(NetworkTarget::All),
            // The owning client gets prediction
            PredictionTarget::to_clients(NetworkTarget::Single(client_id)),
            // Other clients get interpolation
            InterpolationTarget::to_clients(NetworkTarget::AllExceptSingle(client_id)),
            // Mark which client controls this entity
            ControlledBy {
                owner: trigger.entity,
                lifetime: Default::default(),
            },
        ))
        .id();

    info!(
        "Spawned player entity {:?} for client {:?}",
        entity, client_id
    );
}

/// Apply client inputs to player positions on the server.
fn movement(
    mut position_query: Query<(&mut PlayerPosition, &ActionState<PlayerInput>), Without<Predicted>>,
) {
    for (position, input) in position_query.iter_mut() {
        shared_movement(position, input);
    }
}
