mod board;
mod bridge;
mod camera;
#[cfg(target_family = "wasm")]
mod debug;
mod interaction;
mod rules;
mod ui;

use bevy::asset::AssetMetaCheck;
use bevy::prelude::*;

pub fn run() {
    let mut app = App::new();

    app.add_plugins(
        DefaultPlugins
            .set(WindowPlugin {
                primary_window: Some(Window {
                    title: "Creative Mode - Checkers".to_string(),
                    resolution: (800, 800).into(),
                    prevent_default_event_handling: true,
                    canvas: Some("#bevy-canvas".into()),
                    fit_canvas_to_parent: true,
                    ..default()
                }),
                ..default()
            })
            .set(bevy::asset::AssetPlugin {
                file_path: "/assets".to_string(),
                meta_check: AssetMetaCheck::Never,
                ..default()
            }),
    );

    app.add_plugins(board::BoardPlugin);
    app.add_plugins(camera::CameraPlugin);
    app.add_plugins(interaction::InteractionPlugin);
    app.add_plugins(bridge::BridgePlugin);
    app.add_plugins(ui::UiPlugin);

    #[cfg(target_family = "wasm")]
    app.add_systems(Update, debug::process_debug_queries);

    app.run();
}
