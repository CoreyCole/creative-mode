//! Click → select → highlight → move flow.

use bevy::prelude::*;
use bevy::window::PrimaryWindow;

use crate::board::{
    board_to_world, world_to_board, BoardEntity, GameStateRes, MoveHighlight, SelectedPiece,
    SelectionHighlight, ValidMoves, SQUARE_SIZE,
};

const HIGHLIGHT_COLOR: Color = Color::srgba(0.2, 0.8, 0.2, 0.5);
const SELECTION_COLOR: Color = Color::srgba(1.0, 1.0, 0.0, 0.4);

pub struct InteractionPlugin;

impl Plugin for InteractionPlugin {
    fn build(&self, app: &mut App) {
        app.add_systems(Update, (handle_click, update_highlights).chain());
    }
}

/// Handle mouse clicks: select a piece or execute a move.
fn handle_click(
    mouse: Res<ButtonInput<MouseButton>>,
    windows: Query<&Window, With<PrimaryWindow>>,
    cameras: Query<(&Camera, &GlobalTransform), With<Camera2d>>,
    mut game_state: ResMut<GameStateRes>,
    mut selected: ResMut<SelectedPiece>,
    mut valid_moves: ResMut<ValidMoves>,
) {
    if !mouse.just_pressed(MouseButton::Left) {
        return;
    }

    let Ok(window) = windows.single() else {
        return;
    };
    let Some(cursor_pos) = window.cursor_position() else {
        return;
    };
    let Ok((camera, camera_transform)) = cameras.single() else {
        return;
    };
    let Ok(world_pos) = camera.viewport_to_world_2d(camera_transform, cursor_pos) else {
        return;
    };

    let Some((row, col)) = world_to_board(world_pos) else {
        // Click outside board — deselect.
        selected.0 = None;
        valid_moves.0.clear();
        return;
    };

    // Game over — no interaction.
    if game_state.0.game_over.is_some() {
        return;
    }

    // Check if clicking a valid move destination.
    if let Some(m) = valid_moves.0.iter().find(|m| m.to == (row, col)).cloned() {
        let multi_jump = game_state.0.apply_move(&m);
        if multi_jump {
            // Keep the jumping piece selected, recompute valid moves.
            selected.0 = game_state.0.must_continue_jump;
            valid_moves.0 = game_state.0.legal_moves();
        } else {
            selected.0 = None;
            valid_moves.0.clear();
        }
        return;
    }

    // Check if clicking own piece.
    if let Some((color, _)) = game_state.0.get(row, col) {
        if color == game_state.0.current_turn {
            // If forced jump, only allow selecting the forced piece.
            if let Some(forced) = game_state.0.must_continue_jump {
                if (row, col) != forced {
                    return;
                }
            }
            selected.0 = Some((row, col));
            let all_moves = game_state.0.legal_moves();
            valid_moves.0 = all_moves
                .into_iter()
                .filter(|m| m.from == (row, col))
                .collect();
            return;
        }
    }

    // Click on empty or opponent piece — deselect.
    selected.0 = None;
    valid_moves.0.clear();
}

/// Spawn/despawn highlight entities based on selection and valid moves.
fn update_highlights(
    mut commands: Commands,
    selected: Res<SelectedPiece>,
    valid_moves: Res<ValidMoves>,
    sel_highlights: Query<Entity, With<SelectionHighlight>>,
    move_highlights: Query<Entity, With<MoveHighlight>>,
) {
    if !selected.is_changed() && !valid_moves.is_changed() {
        return;
    }

    // Despawn old highlights.
    for entity in sel_highlights.iter() {
        commands.entity(entity).despawn();
    }
    for entity in move_highlights.iter() {
        commands.entity(entity).despawn();
    }

    // Selection highlight.
    if let Some((row, col)) = selected.0 {
        let pos = board_to_world(row, col);
        commands.spawn((
            Sprite {
                color: SELECTION_COLOR,
                custom_size: Some(Vec2::splat(SQUARE_SIZE)),
                ..default()
            },
            Transform::from_xyz(pos.x, pos.y, 0.5),
            SelectionHighlight,
            BoardEntity,
        ));
    }

    // Move highlights.
    for m in &valid_moves.0 {
        let pos = board_to_world(m.to.0, m.to.1);
        commands.spawn((
            Sprite {
                color: HIGHLIGHT_COLOR,
                custom_size: Some(Vec2::splat(SQUARE_SIZE * 0.5)),
                ..default()
            },
            Transform::from_xyz(pos.x, pos.y, 0.5),
            MoveHighlight,
            BoardEntity,
        ));
    }
}
