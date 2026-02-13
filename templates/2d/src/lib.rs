mod bridge;
#[cfg(target_family = "wasm")]
mod debug;
mod interaction;
mod room;

use bevy::asset::{AssetMetaCheck, AssetPlugin};
use bevy::prelude::*;

pub fn run() {
    let mut app = App::new();

    app.add_plugins(
        DefaultPlugins
            .set(WindowPlugin {
                primary_window: Some(Window {
                    title: "Creative Mode - 2D World".to_string(),
                    resolution: (1280, 720).into(),
                    prevent_default_event_handling: true,
                    canvas: Some("#bevy-canvas".into()),
                    fit_canvas_to_parent: true,
                    ..default()
                }),
                ..default()
            })
            .set(AssetPlugin {
                meta_check: AssetMetaCheck::Never,
                ..default()
            }),
    );

    app.add_plugins(room::RoomPlugin);
    app.add_plugins(interaction::InteractionPlugin);
    app.add_plugins(bridge::BridgePlugin);

    #[cfg(target_family = "wasm")]
    app.add_systems(Update, debug::process_debug_queries);

    app.run();
}
