//! Debug query system for the boardgame template.
//!
//! Reads `window.__debugRequest` each frame, executes the query against ECS
//! state, and writes the JSON result to `window.__debugResponse`.

use bevy::prelude::*;
use serde::Deserialize;
use serde_json::{json, Value};
use wasm_bindgen::prelude::*;

use crate::board::{GameStateRes, SelectedPiece, ValidMoves};
use crate::rules::{BoardState, PieceColor, PieceKind};

#[derive(Deserialize)]
#[serde(tag = "type")]
enum DebugQuery {
    #[serde(rename = "board")]
    Board,
    #[serde(rename = "moves")]
    Moves { position: Option<String> },
    #[serde(rename = "reset")]
    Reset,
}

/// Checks for a pending JS debug request each frame.
pub fn process_debug_queries(
    mut game_state: ResMut<GameStateRes>,
    mut selected: ResMut<SelectedPiece>,
    mut valid_moves: ResMut<ValidMoves>,
) {
    let Some(window) = web_sys::window() else {
        return;
    };

    let Ok(request_val) = js_sys::Reflect::get(&window, &JsValue::from_str("__debugRequest"))
    else {
        return;
    };

    if !request_val.is_string() {
        return;
    }
    let request_str = request_val.as_string().unwrap();

    // Clear request immediately.
    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugRequest"),
        &JsValue::NULL,
    );

    let result = match serde_json::from_str::<DebugQuery>(&request_str) {
        Ok(DebugQuery::Board) => board_state_json(&game_state.0),
        Ok(DebugQuery::Moves { position }) => moves_json(&game_state.0, position.as_deref()),
        Ok(DebugQuery::Reset) => {
            game_state.0 = BoardState::new();
            selected.0 = None;
            valid_moves.0.clear();
            json!({"ok": true, "action": "reset"})
        }
        Err(e) => json!({"error": format!("invalid query: {e}")}),
    };

    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugResponse"),
        &JsValue::from_str(&result.to_string()),
    );
}

fn board_state_json(state: &BoardState) -> Value {
    let mut pieces = Vec::new();
    for row in 0..8u8 {
        for col in 0..8u8 {
            if let Some((color, kind)) = state.get(row, col) {
                pieces.push(json!({
                    "row": row,
                    "col": col,
                    "color": match color {
                        PieceColor::Red => "red",
                        PieceColor::Black => "black",
                    },
                    "kind": match kind {
                        PieceKind::Man => "man",
                        PieceKind::King => "king",
                    },
                }));
            }
        }
    }

    json!({
        "current_turn": match state.current_turn {
            PieceColor::Red => "red",
            PieceColor::Black => "black",
        },
        "game_over": state.game_over.map(|c| match c {
            PieceColor::Red => "red",
            PieceColor::Black => "black",
        }),
        "must_continue_jump": state.must_continue_jump.map(|(r, c)| format!("{r},{c}")),
        "piece_count": pieces.len(),
        "pieces": pieces,
    })
}

fn moves_json(state: &BoardState, position: Option<&str>) -> Value {
    let moves = if let Some(pos) = position {
        let parts: Vec<&str> = pos.split(',').collect();
        if parts.len() == 2 {
            if let (Ok(row), Ok(col)) = (parts[0].parse::<u8>(), parts[1].parse::<u8>()) {
                state.piece_moves(row, col)
            } else {
                return json!({"error": "invalid position format, use 'row,col'"});
            }
        } else {
            return json!({"error": "invalid position format, use 'row,col'"});
        }
    } else {
        state.legal_moves()
    };

    let move_list: Vec<Value> = moves
        .iter()
        .map(|m| {
            json!({
                "from": format!("{},{}", m.from.0, m.from.1),
                "to": format!("{},{}", m.to.0, m.to.1),
                "capture": m.capture.map(|(r, c)| format!("{r},{c}")),
            })
        })
        .collect();

    json!({
        "move_count": move_list.len(),
        "moves": move_list,
    })
}
