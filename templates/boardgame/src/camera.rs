//! 2D camera fit-to-board.

use bevy::ecs::message::MessageReader;
use bevy::prelude::*;
use bevy::window::PrimaryWindow;

use crate::board::BOARD_SIZE;

pub struct CameraPlugin;

impl Plugin for CameraPlugin {
    fn build(&self, app: &mut App) {
        app.add_systems(Startup, (spawn_camera, fit_camera).chain());
        app.add_systems(Update, resize_camera);
    }
}

/// Spawn the 2D camera.
fn spawn_camera(mut commands: Commands) {
    commands.spawn(Camera2d);
}

/// Compute the scale to fit the board in the window with padding.
fn compute_fit_scale(window_w: f32, window_h: f32) -> f32 {
    let padded = BOARD_SIZE + 80.0; // 40px padding each side
    (padded / window_w).max(padded / window_h)
}

/// Fit the camera to the board on startup.
fn fit_camera(
    windows: Query<&Window, With<PrimaryWindow>>,
    mut projection_q: Query<&mut Projection, With<Camera2d>>,
) {
    let Ok(window) = windows.single() else {
        return;
    };
    let Ok(mut projection) = projection_q.single_mut() else {
        return;
    };
    let scale = compute_fit_scale(window.width(), window.height());
    if let Projection::Orthographic(ref mut ortho) = *projection {
        ortho.scale = scale;
    }
}

/// Re-fit camera when window is resized.
fn resize_camera(
    mut resize_events: MessageReader<bevy::window::WindowResized>,
    mut projection_q: Query<&mut Projection, With<Camera2d>>,
) {
    for event in resize_events.read() {
        let scale = compute_fit_scale(event.width, event.height);
        if let Ok(mut projection) = projection_q.single_mut() {
            if let Projection::Orthographic(ref mut ortho) = *projection {
                ortho.scale = scale;
            }
        }
    }
}
