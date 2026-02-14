//! Camera auto-fit, touch pan/zoom, tap detection, and scroll-wheel zoom.

use bevy::ecs::message::MessageReader;
use bevy::input::mouse::MouseWheel;
use bevy::input::touch::Touches;
use bevy::prelude::*;
use bevy::window::PrimaryWindow;

/// Room dimensions in world units.
const ROOM_WIDTH: f32 = 1280.0;
const ROOM_HEIGHT: f32 = 720.0;

pub struct CameraPlugin;

impl Plugin for CameraPlugin {
    fn build(&self, app: &mut App) {
        app.insert_resource(CameraFitScale(1.0));
        app.insert_resource(PendingTap(None));
        app.insert_resource(TouchState::default());
        app.add_systems(Startup, (spawn_camera, fit_camera_to_window).chain());
        app.add_systems(
            Update,
            (
                resize_camera,
                touch_camera_system,
                touch_tap_system,
                scroll_wheel_zoom,
                clamp_camera_bounds,
            )
                .chain(),
        );
    }
}

/// The scale at which the full room fits the window. Used as max zoom-out.
#[derive(Resource)]
pub struct CameraFitScale(pub f32);

/// A pending tap position in screen/viewport coordinates.
/// Set by touch_tap_system, consumed by hotspot_click in interaction.rs.
#[derive(Resource)]
pub struct PendingTap(pub Option<Vec2>);

/// Tracking state for two-finger pinch-to-zoom.
#[derive(Resource, Default)]
struct TouchState {
    prev_distance: Option<f32>,
}

/// Single-finger tap and drag tracking.
#[derive(Default)]
struct TapTracker {
    start_pos: Option<Vec2>,
    prev_pos: Option<Vec2>,
    start_time: Option<f64>,
    cancelled: bool,
    dragging: bool,
}

/// Spawn the 2D camera.
fn spawn_camera(mut commands: Commands) {
    commands.spawn(Camera2d);
}

/// Compute the fit scale for current window dimensions.
fn compute_fit_scale(width: f32, height: f32) -> f32 {
    let scale = (ROOM_WIDTH / width).max(ROOM_HEIGHT / height);
    if scale > 2.0 {
        // On very narrow screens (portrait phones), use sqrt to avoid making room too tiny.
        // Shows center portion; user pans to edges.
        scale.sqrt()
    } else {
        scale
    }
}

/// Helper: get mutable reference to OrthographicProjection from a Projection query.
fn get_ortho_scale(projection: &Projection) -> Option<f32> {
    match projection {
        Projection::Orthographic(ortho) => Some(ortho.scale),
        _ => None,
    }
}

/// Helper: set OrthographicProjection scale on a Projection component.
fn set_ortho_scale(projection: &mut Projection, scale: f32) {
    if let Projection::Orthographic(ref mut ortho) = projection {
        ortho.scale = scale;
    }
}

/// Set camera scale on startup to fit the room.
fn fit_camera_to_window(
    windows: Query<&Window, With<PrimaryWindow>>,
    mut projection_q: Query<&mut Projection, With<Camera2d>>,
    mut fit_scale: ResMut<CameraFitScale>,
) {
    let Ok(window) = windows.single() else {
        return;
    };
    let Ok(mut projection) = projection_q.single_mut() else {
        return;
    };
    let scale = compute_fit_scale(window.width(), window.height());
    set_ortho_scale(&mut projection, scale);
    fit_scale.0 = scale;
}

/// Re-fit camera when window is resized.
fn resize_camera(
    mut resize_events: MessageReader<bevy::window::WindowResized>,
    mut projection_q: Query<&mut Projection, With<Camera2d>>,
    mut fit_scale: ResMut<CameraFitScale>,
    mut camera_transform: Query<&mut Transform, With<Camera2d>>,
) {
    for event in resize_events.read() {
        let scale = compute_fit_scale(event.width, event.height);
        fit_scale.0 = scale;
        if let Ok(mut projection) = projection_q.single_mut() {
            set_ortho_scale(&mut projection, scale);
        }
        if let Ok(mut transform) = camera_transform.single_mut() {
            transform.translation.x = 0.0;
            transform.translation.y = 0.0;
        }
    }
}

/// Two-finger pinch-to-zoom.
fn touch_camera_system(
    touches: Res<Touches>,
    mut touch_state: ResMut<TouchState>,
    mut projection_q: Query<&mut Projection, With<Camera2d>>,
    fit_scale: Res<CameraFitScale>,
) {
    let active: Vec<_> = touches.iter().collect();

    if active.len() < 2 {
        touch_state.prev_distance = None;
        return;
    }

    let p0 = active[0].position();
    let p1 = active[1].position();
    let distance = p0.distance(p1);

    let Ok(mut projection) = projection_q.single_mut() else {
        return;
    };
    let Some(current_scale) = get_ortho_scale(&projection) else {
        return;
    };

    // Pinch: adjust scale by ratio of distances.
    if let Some(prev_dist) = touch_state.prev_distance {
        if prev_dist > 1.0 && distance > 1.0 {
            let ratio = prev_dist / distance;
            let new_scale = (current_scale * ratio).clamp(0.5, fit_scale.0);
            set_ortho_scale(&mut projection, new_scale);
        }
    }

    touch_state.prev_distance = Some(distance);
}

/// Detect single-finger taps and drag-to-pan.
fn touch_tap_system(
    touches: Res<Touches>,
    mut tap_tracker: Local<TapTracker>,
    mut pending_tap: ResMut<PendingTap>,
    mut camera_transform: Query<&mut Transform, With<Camera2d>>,
    projection_q: Query<&Projection, With<Camera2d>>,
    time: Res<Time>,
) {
    let active_count = touches.iter().count();

    // Cancel if second finger appears.
    if active_count > 1 {
        tap_tracker.cancelled = true;
        tap_tracker.dragging = false;
        return;
    }

    // Detect new press.
    for touch in touches.iter_just_pressed() {
        tap_tracker.start_pos = Some(touch.position());
        tap_tracker.prev_pos = Some(touch.position());
        tap_tracker.start_time = Some(time.elapsed_secs_f64());
        tap_tracker.cancelled = false;
        tap_tracker.dragging = false;
    }

    // Track movement for single-finger drag-to-pan.
    if active_count == 1 && !tap_tracker.cancelled {
        if let Some(touch) = touches.iter().next() {
            let current_pos = touch.position();

            // Check if we should start dragging (>10px from start).
            if !tap_tracker.dragging {
                if let Some(start_pos) = tap_tracker.start_pos {
                    if current_pos.distance(start_pos) > 10.0 {
                        tap_tracker.dragging = true;
                    }
                }
            }

            // Apply pan delta while dragging.
            if tap_tracker.dragging {
                if let Some(prev_pos) = tap_tracker.prev_pos {
                    let delta = current_pos - prev_pos;
                    if let (Ok(mut transform), Ok(projection)) =
                        (camera_transform.single_mut(), projection_q.single())
                    {
                        if let Some(scale) = get_ortho_scale(projection) {
                            // Screen delta → world delta: multiply by projection scale.
                            // Y is inverted (screen Y down, world Y up).
                            transform.translation.x -= delta.x * scale;
                            transform.translation.y += delta.y * scale;
                        }
                    }
                }
            }

            tap_tracker.prev_pos = Some(current_pos);
        }
    }

    // Detect release.
    for touch in touches.iter_just_released() {
        if tap_tracker.cancelled || tap_tracker.dragging {
            *tap_tracker = TapTracker::default();
            continue;
        }

        if let (Some(start_pos), Some(start_time)) = (tap_tracker.start_pos, tap_tracker.start_time)
        {
            let distance = touch.position().distance(start_pos);
            let duration = time.elapsed_secs_f64() - start_time;

            if distance < 10.0 && duration < 0.3 {
                pending_tap.0 = Some(touch.position());
            }
        }

        *tap_tracker = TapTracker::default();
    }
}

/// Desktop scroll-wheel zoom.
fn scroll_wheel_zoom(
    mut scroll_events: MessageReader<MouseWheel>,
    mut projection_q: Query<&mut Projection, With<Camera2d>>,
    fit_scale: Res<CameraFitScale>,
) {
    for event in scroll_events.read() {
        let Ok(mut projection) = projection_q.single_mut() else {
            continue;
        };
        let Some(current_scale) = get_ortho_scale(&projection) else {
            continue;
        };
        // Positive scroll_y = scroll up = zoom in (reduce scale).
        let factor = if event.y > 0.0 { 0.9 } else { 1.1 };
        let new_scale = (current_scale * factor).clamp(0.5, fit_scale.0);
        set_ortho_scale(&mut projection, new_scale);
    }
}

/// Clamp camera position so the viewport stays within room bounds.
fn clamp_camera_bounds(
    windows: Query<&Window, With<PrimaryWindow>>,
    projection_q: Query<&Projection, With<Camera2d>>,
    mut camera_transform: Query<&mut Transform, With<Camera2d>>,
) {
    let Ok(window) = windows.single() else {
        return;
    };
    let Ok(projection) = projection_q.single() else {
        return;
    };
    let Some(scale) = get_ortho_scale(projection) else {
        return;
    };
    let Ok(mut transform) = camera_transform.single_mut() else {
        return;
    };

    // Visible half-extents in world units.
    let half_w = window.width() * scale / 2.0;
    let half_h = window.height() * scale / 2.0;

    // Room half-extents.
    let room_half_w = ROOM_WIDTH / 2.0;
    let room_half_h = ROOM_HEIGHT / 2.0;

    // If the viewport is larger than the room, center the camera.
    if half_w >= room_half_w {
        transform.translation.x = 0.0;
    } else {
        transform.translation.x = transform
            .translation
            .x
            .clamp(-(room_half_w - half_w), room_half_w - half_w);
    }

    if half_h >= room_half_h {
        transform.translation.y = 0.0;
    } else {
        transform.translation.y = transform
            .translation
            .y
            .clamp(-(room_half_h - half_h), room_half_h - half_h);
    }
}
