//! postMessage bridge to the harness parent frame.

use bevy::prelude::*;

pub struct BridgePlugin;

impl Plugin for BridgePlugin {
    fn build(&self, app: &mut App) {
        app.insert_resource(PendingBridgeAction(None));
        app.add_systems(Update, send_bridge_actions);
    }
}

/// Actions that get forwarded to the parent frame via postMessage.
#[derive(Clone, Debug)]
#[allow(dead_code)]
pub enum BridgeAction {
    NavigateWorld(String),
    NavigateCheckpoint(String, String),
    OpenEmbed(String),
}

/// Resource for pending bridge actions (set by interaction, consumed by bridge system).
#[derive(Resource)]
pub struct PendingBridgeAction(pub Option<BridgeAction>);

fn send_bridge_actions(mut pending: ResMut<PendingBridgeAction>) {
    let Some(action) = pending.0.take() else {
        return;
    };

    match &action {
        BridgeAction::NavigateWorld(world_id) => {
            post_message("navigate-world", world_id);
        }
        BridgeAction::NavigateCheckpoint(world_id, cp_id) => {
            post_message("navigate-checkpoint", &format!("{world_id}/{cp_id}"));
        }
        BridgeAction::OpenEmbed(url) => {
            post_message("open-embed", url);
        }
    }
}

/// Send a message to the parent window (harness overlay) via postMessage.
#[cfg(target_family = "wasm")]
fn post_message(msg_type: &str, data: &str) {
    use wasm_bindgen::prelude::*;

    let Some(window) = web_sys::window() else {
        return;
    };
    let Ok(Some(parent)) = window.parent() else {
        return;
    };
    let obj = js_sys::Object::new();
    let _ = js_sys::Reflect::set(
        &obj,
        &JsValue::from_str("type"),
        &JsValue::from_str(msg_type),
    );
    let _ = js_sys::Reflect::set(&obj, &JsValue::from_str("data"), &JsValue::from_str(data));
    let _ = parent.post_message(&obj, "*");
}

#[cfg(not(target_family = "wasm"))]
fn post_message(msg_type: &str, data: &str) {
    info!("bridge: {msg_type} -> {data}");
}
