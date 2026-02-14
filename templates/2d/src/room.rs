//! Room loading, spawning, and navigation.
//!
//! Rooms are loaded at runtime via HTTP as Bevy assets (`/assets/rooms/{id}.json`).
//! Each room has a background color, an optional background image, and a list of
//! hotspots with labels, optional images, and actions.

use bevy::asset::{io::Reader, AssetLoader, LoadContext};
use bevy::prelude::*;
use bevy::reflect::TypePath;
use serde::{Deserialize, Serialize};
use thiserror::Error;

pub struct RoomPlugin;

impl Plugin for RoomPlugin {
    fn build(&self, app: &mut App) {
        app.init_asset::<RoomAsset>();
        app.init_asset_loader::<RoomAssetLoader>();
        app.insert_resource(PendingNavigation(None));
        app.insert_resource(CurrentRoom("lobby".to_string()));
        app.insert_resource(RoomLoadState::Idle);
        app.add_systems(Startup, load_initial_room);
        app.add_systems(Update, (start_room_load, finish_room_load).chain());
        #[cfg(target_family = "wasm")]
        app.add_systems(Update, check_reload_request.before(start_room_load));
    }
}

// --- JSON Schema / Asset ---

#[derive(Asset, TypePath, Deserialize, Clone, Debug)]
#[allow(dead_code)]
pub struct RoomAsset {
    pub id: String,
    pub name: String,
    pub background_color: String,
    #[serde(default)]
    pub background_image: Option<String>,
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
    #[serde(default)]
    pub image: Option<String>,
    pub action: ActionDef,
}

#[derive(Deserialize, Serialize, Clone, Debug)]
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

// --- Asset Loader ---

#[derive(Default, TypePath)]
pub struct RoomAssetLoader;

#[derive(Debug, Error)]
pub enum RoomAssetLoaderError {
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
    #[error("JSON parse error: {0}")]
    Json(#[from] serde_json::error::Error),
}

impl AssetLoader for RoomAssetLoader {
    type Asset = RoomAsset;
    type Settings = ();
    type Error = RoomAssetLoaderError;

    async fn load(
        &self,
        reader: &mut dyn Reader,
        _settings: &(),
        _load_context: &mut LoadContext<'_>,
    ) -> Result<Self::Asset, Self::Error> {
        let mut bytes = Vec::new();
        reader.read_to_end(&mut bytes).await?;
        Ok(serde_json::from_slice(&bytes)?)
    }

    fn extensions(&self) -> &[&str] {
        &["room.json"]
    }
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

/// Marker for hotspots that use an image sprite (affects hover behavior).
#[derive(Component)]
pub struct HasImage;

/// Resource tracking the current room ID.
#[derive(Resource)]
pub struct CurrentRoom(pub String);

/// Resource for pending room navigation (set by interaction, consumed by room system).
#[derive(Resource)]
pub struct PendingNavigation(pub Option<String>);

/// Marker for the dialog text entity.
#[derive(Component)]
pub struct DialogText;

/// Async room loading state machine.
#[derive(Resource)]
pub enum RoomLoadState {
    Idle,
    Loading {
        handle: Handle<RoomAsset>,
        room_id: String,
    },
    Ready,
}

// --- Systems ---

fn load_initial_room(mut pending: ResMut<PendingNavigation>) {
    pending.0 = Some("lobby".to_string());
}

/// Consumes `PendingNavigation`, despawns old room entities, kicks off async load.
fn start_room_load(
    mut commands: Commands,
    mut pending: ResMut<PendingNavigation>,
    mut load_state: ResMut<RoomLoadState>,
    mut current_room: ResMut<CurrentRoom>,
    asset_server: Res<AssetServer>,
    room_entities: Query<Entity, With<RoomEntity>>,
) {
    let Some(room_id) = pending.0.take() else {
        return;
    };

    // Despawn all entities from the current room.
    for entity in room_entities.iter() {
        commands.entity(entity).despawn();
    }

    let path = format!("rooms/{room_id}.room.json");

    // If navigating to same room (reload), we need to force a re-fetch.
    let handle: Handle<RoomAsset> = asset_server.load(&path);

    current_room.0 = room_id.clone();
    *load_state = RoomLoadState::Loading { handle, room_id };
}

/// Polls the asset handle; when ready, spawns the room.
fn finish_room_load(
    mut commands: Commands,
    mut load_state: ResMut<RoomLoadState>,
    room_assets: Res<Assets<RoomAsset>>,
    asset_server: Res<AssetServer>,
) {
    let RoomLoadState::Loading { handle, room_id } = &*load_state else {
        return;
    };

    let Some(room) = room_assets.get(handle) else {
        return;
    };

    info!("Room loaded: {} ({})", room.name, room_id);
    spawn_room(&mut commands, room, &asset_server);
    *load_state = RoomLoadState::Ready;
}

/// WASM-only: checks `window.__reloadRoom` flag set by postMessage from harness.
#[cfg(target_family = "wasm")]
fn check_reload_request(current_room: Res<CurrentRoom>, mut pending: ResMut<PendingNavigation>) {
    use wasm_bindgen::prelude::*;

    let Some(window) = web_sys::window() else {
        return;
    };

    let Ok(val) = js_sys::Reflect::get(&window, &JsValue::from_str("__reloadRoom")) else {
        return;
    };

    if val.is_truthy() {
        // Clear the flag.
        let _ = js_sys::Reflect::set(&window, &JsValue::from_str("__reloadRoom"), &JsValue::FALSE);
        // Trigger re-navigation to the current room.
        pending.0 = Some(current_room.0.clone());
    }
}

/// Spawn all entities for a room: background, hotspot zones, labels.
pub fn spawn_room(commands: &mut Commands, room: &RoomAsset, asset_server: &AssetServer) {
    let bg_color = parse_hex_color(&room.background_color);

    // Background color — oversized so it fills the viewport at any zoom/aspect ratio,
    // preventing the HTML body (#111) from showing as gray letterbox bars.
    commands.spawn((
        Sprite {
            color: bg_color,
            custom_size: Some(Vec2::new(4000.0, 4000.0)),
            ..default()
        },
        Transform::from_xyz(0.0, 0.0, 0.0),
        RoomEntity,
    ));

    // Background image (if present)
    if let Some(ref bg_path) = room.background_image {
        commands.spawn((
            Sprite {
                image: asset_server.load(bg_path),
                custom_size: Some(Vec2::new(1280.0, 720.0)),
                ..default()
            },
            Transform::from_xyz(0.0, 0.0, 0.5),
            RoomEntity,
        ));
    }

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

        // Hotspot background — image or translucent color
        if let Some(ref img_path) = hotspot.image {
            commands.spawn((
                Sprite {
                    image: asset_server.load(img_path),
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
                HasImage,
                RoomEntity,
            ));
        } else {
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
        }

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
