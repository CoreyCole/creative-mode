//! Lightyear protocol definitions shared between server and client.
//!
//! Defines replicated components, inputs, channels, and the protocol plugin
//! that registers everything with Lightyear.

use bevy::math::Curve;
use bevy::prelude::*;
use lightyear::prelude::*;
use serde::{Deserialize, Serialize};

// --- Constants ---

pub const FIXED_TIMESTEP_HZ: f64 = 64.0;
pub const PROTOCOL_ID: u64 = 0;
pub const PRIVATE_KEY: [u8; 32] = [0; 32];
/// How often the server sends replication updates to clients.
pub const REPLICATION_INTERVAL: core::time::Duration = core::time::Duration::from_millis(100);
pub const MOVE_SPEED: f32 = 10.0;

// --- Player Bundle ---

#[derive(Bundle)]
pub struct PlayerBundle {
    pub id: PlayerId,
    pub position: PlayerPosition,
    pub color: PlayerColor,
}

impl PlayerBundle {
    pub fn new(id: PeerId, position: Vec3) -> Self {
        // Generate pseudo-random color from client id
        let h = (((id.to_bits().wrapping_mul(30)) % 360) as f32) / 360.0;
        let s = 0.8;
        let l = 0.5;
        let color = Color::hsl(h, s, l);
        Self {
            id: PlayerId(id),
            position: PlayerPosition(position),
            color: PlayerColor(color),
        }
    }
}

// --- Components ---

/// Identifies which player owns this entity.
#[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct PlayerId(pub PeerId);

/// The player's 3D position in the world. Replicated with prediction + interpolation.
#[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq, Reflect, Deref, DerefMut)]
pub struct PlayerPosition(pub Vec3);

impl Ease for PlayerPosition {
    fn interpolating_curve_unbounded(start: Self, end: Self) -> impl Curve<Self> {
        FunctionCurve::new(Interval::UNIT, move |t| {
            PlayerPosition(Vec3::lerp(start.0, end.0, t))
        })
    }
}

/// The player's color used for rendering their pill mesh.
#[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct PlayerColor(pub Color);

// --- Channels ---

/// Reliable ordered channel for important game events.
pub struct GameChannel;

// --- Inputs ---

/// Movement input sent from client to server each tick.
#[derive(Serialize, Deserialize, Debug, Default, PartialEq, Clone, Reflect)]
pub struct PlayerInput {
    /// Movement direction in local camera space (forward/back/left/right/up/down).
    pub movement: Vec3,
    /// Whether shift is held for speed boost.
    pub sprint: bool,
}

impl bevy::ecs::entity::MapEntities for PlayerInput {
    fn map_entities<M: bevy::ecs::entity::EntityMapper>(&mut self, _entity_mapper: &mut M) {}
}

// --- Protocol Plugin ---

/// Registers all protocol definitions (components, inputs, channels) with Lightyear.
#[derive(Clone)]
pub struct ProtocolPlugin;

impl Plugin for ProtocolPlugin {
    fn build(&self, app: &mut App) {
        // Register inputs
        app.add_plugins(lightyear::prelude::input::native::InputPlugin::<PlayerInput>::default());

        // Register replicated components
        app.register_component::<PlayerId>();

        app.register_component::<PlayerPosition>()
            .add_prediction()
            .add_linear_interpolation();

        app.register_component::<PlayerColor>();

        // Register channels
        app.add_channel::<GameChannel>(ChannelSettings {
            mode: ChannelMode::OrderedReliable(ReliableSettings::default()),
            ..default()
        });
    }
}

// --- Shared Movement ---

/// Shared movement behavior applied on both server and client (for prediction).
/// Takes the input and applies it to the player position.
pub fn shared_movement(mut position: Mut<PlayerPosition>, input: &PlayerInput) {
    let speed = if input.sprint {
        MOVE_SPEED * 3.0
    } else {
        MOVE_SPEED
    };
    position.0 += input.movement * speed;
}
