//! Room loading, spawning, and navigation.
//!
//! Rooms are defined as JSON files in the `rooms/` directory. Each room has a
//! background color and a list of hotspots with labels and actions.

use bevy::prelude::*;
use serde::Deserialize;

pub struct RoomPlugin;

impl Plugin for RoomPlugin {
    fn build(&self, app: &mut App) {
        app.insert_resource(PendingNavigation(None));
        app.add_systems(Startup, setup_camera);
        app.add_systems(Startup, load_initial_room.after(setup_camera));
        app.add_systems(Update, handle_room_navigation);
    }
}

// --- JSON Schema ---

#[derive(Deserialize, Clone, Debug)]
#[allow(dead_code)]
pub struct RoomDef {
    pub id: String,
    pub name: String,
    pub background_color: String,
    pub hotspots: Vec<HotspotDef>,
}

#[derive(Deserialize, Clone, Debug)]
pub struct HotspotDef {
    pub id: String,
    pub label: String,
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
    pub action: ActionDef,
}

#[derive(Deserialize, Clone, Debug)]
#[serde(tag = "type")]
pub enum ActionDef {
    #[serde(rename = "dialog")]
    Dialog { text: String },
    #[serde(rename = "navigate_room")]
    NavigateRoom { room: String },
    #[serde(rename = "navigate_world")]
    NavigateWorld { world_id: String },
    #[serde(rename = "navigate_checkpoint")]
    NavigateCheckpoint {
        world_id: String,
        checkpoint_id: String,
    },
    #[serde(rename = "open_embed")]
    OpenEmbed { url: String },
}

// --- ECS Components ---

/// Marker for the current room's entities (despawned on navigation).
#[derive(Component)]
pub struct RoomEntity;

/// Hotspot component attached to interactive zone entities.
#[derive(Component, Clone)]
#[allow(dead_code)]
pub struct Hotspot {
    pub id: String,
    pub label: String,
    pub bounds: Rect,
    pub action: ActionDef,
}

/// Resource tracking the current room ID.
#[derive(Resource)]
pub struct CurrentRoom(pub String);

/// Resource for pending room navigation (set by interaction, consumed by room system).
#[derive(Resource)]
pub struct PendingNavigation(pub Option<String>);

/// Marker for the dialog text entity.
#[derive(Component)]
pub struct DialogText;

// --- Room Data ---

/// Built-in room JSON files. Add new rooms here.
const ROOM_DATA: &[(&str, &str)] = &[
    ("lobby", include_str!("../rooms/lobby.json")),
    ("garden", include_str!("../rooms/garden.json")),
];

pub fn find_room(id: &str) -> Option<RoomDef> {
    ROOM_DATA
        .iter()
        .find(|(name, _)| *name == id)
        .and_then(|(_, json)| serde_json::from_str(json).ok())
}

// --- Systems ---

fn setup_camera(mut commands: Commands) {
    commands.spawn(Camera2d);
}

fn load_initial_room(mut commands: Commands) {
    commands.insert_resource(CurrentRoom("lobby".to_string()));
    if let Some(room) = find_room("lobby") {
        spawn_room(&mut commands, &room);
    }
}

fn handle_room_navigation(
    mut commands: Commands,
    mut pending: ResMut<PendingNavigation>,
    room_entities: Query<Entity, With<RoomEntity>>,
    mut current_room: ResMut<CurrentRoom>,
) {
    let Some(room_id) = pending.0.take() else {
        return;
    };

    let Some(room) = find_room(&room_id) else {
        warn!("Room not found: {}", room_id);
        return;
    };

    // Despawn all entities from the current room.
    for entity in room_entities.iter() {
        commands.entity(entity).despawn();
    }

    current_room.0 = room_id;
    spawn_room(&mut commands, &room);
}

/// Spawn all entities for a room: background, hotspot zones, labels.
pub fn spawn_room(commands: &mut Commands, room: &RoomDef) {
    let bg_color = parse_hex_color(&room.background_color);

    // Background
    commands.spawn((
        Sprite {
            color: bg_color,
            custom_size: Some(Vec2::new(1280.0, 720.0)),
            ..default()
        },
        Transform::from_xyz(0.0, 0.0, 0.0),
        RoomEntity,
    ));

    // Room title
    commands.spawn((
        Text2d::new(&room.name),
        TextFont {
            font_size: 32.0,
            ..default()
        },
        TextColor(Color::WHITE),
        Transform::from_xyz(0.0, 320.0, 1.0),
        RoomEntity,
    ));

    // Hotspots
    for hotspot in &room.hotspots {
        // Convert from top-left screen coords to centered Bevy coords.
        let center_x = hotspot.x + hotspot.width / 2.0 - 640.0;
        let center_y = -(hotspot.y + hotspot.height / 2.0 - 360.0);

        let bounds = Rect::from_center_size(
            Vec2::new(center_x, center_y),
            Vec2::new(hotspot.width, hotspot.height),
        );

        // Hotspot background
        commands.spawn((
            Sprite {
                color: Color::srgba(1.0, 1.0, 1.0, 0.1),
                custom_size: Some(Vec2::new(hotspot.width, hotspot.height)),
                ..default()
            },
            Transform::from_xyz(center_x, center_y, 1.0),
            Hotspot {
                id: hotspot.id.clone(),
                label: hotspot.label.clone(),
                bounds,
                action: hotspot.action.clone(),
            },
            RoomEntity,
        ));

        // Hotspot label
        commands.spawn((
            Text2d::new(&hotspot.label),
            TextFont {
                font_size: 18.0,
                ..default()
            },
            TextColor(Color::WHITE),
            Transform::from_xyz(center_x, center_y, 2.0),
            RoomEntity,
        ));
    }
}

/// Parse a hex color string like "#1a1a2e" into a Bevy Color.
fn parse_hex_color(hex: &str) -> Color {
    let hex = hex.trim_start_matches('#');
    if hex.len() != 6 {
        return Color::srgb(0.1, 0.1, 0.1);
    }
    let r = u8::from_str_radix(&hex[0..2], 16).unwrap_or(26);
    let g = u8::from_str_radix(&hex[2..4], 16).unwrap_or(26);
    let b = u8::from_str_radix(&hex[4..6], 16).unwrap_or(46);
    Color::srgb(r as f32 / 255.0, g as f32 / 255.0, b as f32 / 255.0)
}
