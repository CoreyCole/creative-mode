//! Hover and click detection on room hotspots.

use bevy::prelude::*;
use bevy::window::PrimaryWindow;

use crate::bridge::PendingBridgeAction;
use crate::camera::PendingTap;
use crate::room::{ActionDef, DialogText, HasImage, Hotspot, PendingNavigation, RoomEntity};

pub struct InteractionPlugin;

impl Plugin for InteractionPlugin {
    fn build(&self, app: &mut App) {
        app.add_systems(Update, (hotspot_hover, hotspot_click).chain());
    }
}

/// Highlight hotspots on hover by changing sprite opacity/tint.
fn hotspot_hover(
    windows: Query<&Window, With<PrimaryWindow>>,
    camera_q: Query<(&Camera, &GlobalTransform)>,
    mut color_hotspots: Query<(&Hotspot, &mut Sprite), Without<HasImage>>,
    mut image_hotspots: Query<(&Hotspot, &mut Sprite), With<HasImage>>,
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
        for (_, mut sprite) in color_hotspots.iter_mut() {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.1);
        }
        for (_, mut sprite) in image_hotspots.iter_mut() {
            sprite.color = Color::WHITE;
        }
        return;
    };

    // Color hotspots: opacity change
    for (hotspot, mut sprite) in color_hotspots.iter_mut() {
        if hotspot.bounds.contains(cursor_pos) {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.3);
        } else {
            sprite.color = Color::srgba(1.0, 1.0, 1.0, 0.1);
        }
    }

    // Image hotspots: brighten on hover
    for (hotspot, mut sprite) in image_hotspots.iter_mut() {
        if hotspot.bounds.contains(cursor_pos) {
            sprite.color = Color::srgba(1.2, 1.2, 1.2, 1.0);
        } else {
            sprite.color = Color::WHITE;
        }
    }
}

/// Handle clicks on hotspots, dispatching their action.
/// Accepts both mouse clicks and touch taps (via PendingTap resource).
#[allow(clippy::too_many_arguments)]
fn hotspot_click(
    mut commands: Commands,
    windows: Query<&Window, With<PrimaryWindow>>,
    camera_q: Query<(&Camera, &GlobalTransform)>,
    mouse: Res<ButtonInput<MouseButton>>,
    mut pending_tap: ResMut<PendingTap>,
    hotspots: Query<&Hotspot>,
    dialog_entities: Query<Entity, With<DialogText>>,
    mut pending_nav: ResMut<PendingNavigation>,
    mut pending_bridge: ResMut<PendingBridgeAction>,
) {
    // Determine click source: mouse click or touch tap.
    let tap_pos = pending_tap.0.take();
    let has_mouse_click = mouse.just_pressed(MouseButton::Left);

    if !has_mouse_click && tap_pos.is_none() {
        return;
    }

    let Ok(window) = windows.single() else {
        return;
    };
    let Ok((camera, camera_transform)) = camera_q.single() else {
        return;
    };

    // Use tap position if available, otherwise use cursor position.
    let screen_pos = tap_pos.or_else(|| window.cursor_position());
    let Some(cursor_pos) =
        screen_pos.and_then(|pos| camera.viewport_to_world_2d(camera_transform, pos).ok())
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
                // Dark backdrop behind dialog text.
                commands.spawn((
                    Sprite {
                        color: Color::srgba(0.0, 0.0, 0.0, 0.75),
                        custom_size: Some(Vec2::new(900.0, 60.0)),
                        ..default()
                    },
                    Transform::from_xyz(0.0, -300.0, 9.5),
                    DialogText,
                    RoomEntity,
                ));
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
