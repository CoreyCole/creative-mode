//! Hover and click detection on room hotspots.

use bevy::prelude::*;
use bevy::window::PrimaryWindow;

use crate::bridge::PendingBridgeAction;
use crate::room::{ActionDef, DialogText, Hotspot, PendingNavigation, RoomEntity};

pub struct InteractionPlugin;

impl Plugin for InteractionPlugin {
    fn build(&self, app: &mut App) {
        app.add_systems(Update, (hotspot_hover, hotspot_click).chain());
    }
}

/// Highlight hotspots on hover by changing sprite opacity.
fn hotspot_hover(
    windows: Query<&Window, With<PrimaryWindow>>,
    camera_q: Query<(&Camera, &GlobalTransform)>,
    mut hotspots: Query<(&Hotspot, &mut Sprite)>,
) {
    let Ok(window) = windows.single() else {
        return;
    };
    let Ok((camera, camera_transform)) = camera_q.single() else {
        return;
    };
    let Some(cursor_pos) = window
        .cursor_position()
        .and_then(|pos| camera.viewport_to_world_2d(camera_transform, pos).ok())
    else {
        // No cursor — reset all to default.
        for (_, mut sprite) in hotspots.iter_mut() {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.1);
        }
        return;
    };

    for (hotspot, mut sprite) in hotspots.iter_mut() {
        if hotspot.bounds.contains(cursor_pos) {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.3);
        } else {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.1);
        }
    }
}

/// Handle clicks on hotspots, dispatching their action.
#[allow(clippy::too_many_arguments)]
fn hotspot_click(
    mut commands: Commands,
    windows: Query<&Window, With<PrimaryWindow>>,
    camera_q: Query<(&Camera, &GlobalTransform)>,
    mouse: Res<ButtonInput<MouseButton>>,
    hotspots: Query<&Hotspot>,
    dialog_entities: Query<Entity, With<DialogText>>,
    mut pending_nav: ResMut<PendingNavigation>,
    mut pending_bridge: ResMut<PendingBridgeAction>,
) {
    if !mouse.just_pressed(MouseButton::Left) {
        return;
    }

    let Ok(window) = windows.single() else {
        return;
    };
    let Ok((camera, camera_transform)) = camera_q.single() else {
        return;
    };
    let Some(cursor_pos) = window
        .cursor_position()
        .and_then(|pos| camera.viewport_to_world_2d(camera_transform, pos).ok())
    else {
        return;
    };

    // Clear any existing dialog.
    for entity in dialog_entities.iter() {
        commands.entity(entity).despawn();
    }

    for hotspot in hotspots.iter() {
        if !hotspot.bounds.contains(cursor_pos) {
            continue;
        }

        match &hotspot.action {
            ActionDef::Dialog { text } => {
                // Spawn dialog text at bottom of screen.
                commands.spawn((
                    Text2d::new(text),
                    TextFont {
                        font_size: 20.0,
                        ..default()
                    },
                    TextColor(Color::srgba(1.0, 1.0, 1.0, 0.9)),
                    Transform::from_xyz(0.0, -300.0, 10.0),
                    DialogText,
                    RoomEntity,
                ));
            }
            ActionDef::NavigateRoom { room } => {
                pending_nav.0 = Some(room.clone());
            }
            ActionDef::NavigateWorld { world_id } => {
                pending_bridge.0 =
                    Some(crate::bridge::BridgeAction::NavigateWorld(world_id.clone()));
            }
            ActionDef::NavigateCheckpoint {
                world_id,
                checkpoint_id,
            } => {
                pending_bridge.0 = Some(crate::bridge::BridgeAction::NavigateCheckpoint(
                    world_id.clone(),
                    checkpoint_id.clone(),
                ));
            }
            ActionDef::OpenEmbed { url } => {
                pending_bridge.0 = Some(crate::bridge::BridgeAction::OpenEmbed(url.clone()));
            }
        }

        break; // Only handle the first hit.
    }
}
