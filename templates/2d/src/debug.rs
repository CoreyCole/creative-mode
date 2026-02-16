//! Debug query system for the 2D template.
//!
//! Reads `window.__debugRequest` each frame, executes the query against ECS
//! state, and writes the JSON result to `window.__debugResponse`. The JS bridge
//! in `index.html` relays these between the harness parent frame and WASM.

use bevy::prelude::*;
use serde::Deserialize;
use serde_json::{json, Value};
use wasm_bindgen::prelude::*;

use crate::bridge::PendingBridgeAction;
use crate::room::{ActionDef, CurrentRoom, DialogText, Hotspot, PendingNavigation, RoomEntity};

// --- Query Protocol ---

#[derive(Deserialize)]
#[serde(tag = "type")]
enum DebugQuery {
    #[serde(rename = "room")]
    Room,
    #[serde(rename = "hotspots")]
    Hotspots,
    #[serde(rename = "dialog")]
    Dialog,
    #[serde(rename = "click")]
    Click { hotspot_id: String },
}

// --- JS Bridge ---

/// Checks for a pending JS debug request each frame.
/// No-op when no request is pending (one JS global read).
#[allow(clippy::too_many_arguments)]
pub fn process_debug_queries(
    mut commands: Commands,
    current_room: Option<Res<CurrentRoom>>,
    hotspots: Query<&Hotspot>,
    dialog_texts: Query<&Text2d, With<DialogText>>,
    dialog_entities: Query<Entity, With<DialogText>>,
    mut pending_nav: ResMut<PendingNavigation>,
    mut pending_bridge: ResMut<PendingBridgeAction>,
) {
    let Some(window) = web_sys::window() else {
        return;
    };

    // Read request from window.__debugRequest
    let Ok(request_val) = js_sys::Reflect::get(&window, &JsValue::from_str("__debugRequest"))
    else {
        return;
    };

    if !request_val.is_string() {
        return;
    }
    let request_str = request_val.as_string().unwrap();

    // Clear request immediately
    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugRequest"),
        &JsValue::NULL,
    );

    // Parse and execute
    let result = match serde_json::from_str::<DebugQuery>(&request_str) {
        Ok(DebugQuery::Click { hotspot_id }) => execute_click(
            &hotspot_id,
            &mut commands,
            &hotspots,
            &dialog_entities,
            &mut pending_nav,
            &mut pending_bridge,
        ),
        Ok(query) => execute_query(&query, current_room.as_deref(), &hotspots, &dialog_texts),
        Err(e) => json!({"error": format!("invalid query: {e}")}),
    };

    // Write response
    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugResponse"),
        &JsValue::from_str(&result.to_string()),
    );
}

// --- Query Engine ---

fn execute_query(
    query: &DebugQuery,
    current_room: Option<&CurrentRoom>,
    hotspots: &Query<&Hotspot>,
    dialog_texts: &Query<&Text2d, With<DialogText>>,
) -> Value {
    match query {
        DebugQuery::Room | DebugQuery::Hotspots => {
            let room_id = current_room.map(|r| r.0.as_str()).unwrap_or("unknown");

            let hotspot_list: Vec<Value> = hotspots
                .iter()
                .map(|h| {
                    json!({
                        "id": h.id,
                        "label": h.label,
                        "bounds_min": [h.bounds.min.x, h.bounds.min.y],
                        "bounds_max": [h.bounds.max.x, h.bounds.max.y],
                        "action": serde_json::to_value(&h.action).unwrap_or(json!(null)),
                    })
                })
                .collect();

            json!({
                "room_id": room_id,
                "hotspot_count": hotspot_list.len(),
                "hotspots": hotspot_list,
            })
        }

        DebugQuery::Dialog => {
            let text = dialog_texts.iter().next().map(|t| t.0.clone());
            json!({
                "visible": text.is_some(),
                "text": text.unwrap_or_default(),
            })
        }

        DebugQuery::Click { .. } => unreachable!("handled in process_debug_queries"),
    }
}

fn execute_click(
    hotspot_id: &str,
    commands: &mut Commands,
    hotspots: &Query<&Hotspot>,
    dialog_entities: &Query<Entity, With<DialogText>>,
    pending_nav: &mut ResMut<PendingNavigation>,
    pending_bridge: &mut ResMut<PendingBridgeAction>,
) -> Value {
    let Some(hotspot) = hotspots.iter().find(|h| h.id == hotspot_id) else {
        return json!({"error": format!("hotspot '{}' not found", hotspot_id)});
    };

    // Clear any existing dialog (same as interaction.rs hotspot_click).
    for entity in dialog_entities.iter() {
        commands.entity(entity).despawn();
    }

    let action_type = match &hotspot.action {
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
            "dialog"
        }
        ActionDef::NavigateRoom { room } => {
            pending_nav.0 = Some(room.clone());
            "navigate_room"
        }
        ActionDef::NavigateWorld { world_id } => {
            pending_bridge.0 = Some(crate::bridge::BridgeAction::NavigateWorld(world_id.clone()));
            "navigate_world"
        }
        ActionDef::NavigateCheckpoint {
            world_id,
            checkpoint_id,
        } => {
            pending_bridge.0 = Some(crate::bridge::BridgeAction::NavigateCheckpoint(
                world_id.clone(),
                checkpoint_id.clone(),
            ));
            "navigate_checkpoint"
        }
        ActionDef::OpenEmbed { url } => {
            pending_bridge.0 = Some(crate::bridge::BridgeAction::OpenEmbed(url.clone()));
            "open_embed"
        }
    };

    json!({
        "ok": true,
        "hotspot_id": hotspot_id,
        "action_type": action_type,
    })
}
