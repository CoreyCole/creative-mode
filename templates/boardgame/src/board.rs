//! Board rendering and entity synchronization.

use bevy::prelude::*;

use crate::rules::{BoardState, PieceColor, PieceKind};

pub const SQUARE_SIZE: f32 = 80.0;
pub const BOARD_SIZE: f32 = SQUARE_SIZE * 8.0; // 640.0

const LIGHT_SQUARE: Color = Color::srgb(0.941, 0.851, 0.710); // #F0D9B5
const DARK_SQUARE: Color = Color::srgb(0.710, 0.533, 0.388); // #B58863
const RED_PIECE: Color = Color::srgb(0.85, 0.2, 0.15);
const BLACK_PIECE: Color = Color::srgb(0.25, 0.25, 0.25);
const CROWN_COLOR: Color = Color::srgb(1.0, 0.84, 0.0); // Gold

/// Marker for all board entities (bulk despawn on reset).
#[derive(Component)]
pub struct BoardEntity;

/// Marks a board square.
#[derive(Component)]
#[allow(dead_code)]
pub struct Square {
    pub row: u8,
    pub col: u8,
}

/// Marks a piece entity, storing its logical position.
#[derive(Component)]
#[allow(dead_code)]
pub struct Piece {
    pub row: u8,
    pub col: u8,
}

/// Marks a crown sprite on a king piece.
#[derive(Component)]
pub struct CrownMarker;

/// Marks a valid-move highlight.
#[derive(Component)]
pub struct MoveHighlight;

/// Marks the selection highlight.
#[derive(Component)]
pub struct SelectionHighlight;

pub struct BoardPlugin;

impl Plugin for BoardPlugin {
    fn build(&self, app: &mut App) {
        app.insert_resource(GameStateRes(BoardState::new()));
        app.insert_resource(SelectedPiece(None));
        app.insert_resource(ValidMoves(Vec::new()));
        app.add_systems(Startup, spawn_board);
        app.add_systems(Update, sync_pieces_to_entities);
    }
}

/// Wraps `BoardState` as a Bevy resource.
#[derive(Resource)]
pub struct GameStateRes(pub BoardState);

/// Currently selected piece position.
#[derive(Resource)]
pub struct SelectedPiece(pub Option<(u8, u8)>);

/// Valid moves for the currently selected piece.
#[derive(Resource)]
pub struct ValidMoves(pub Vec<crate::rules::Move>);

/// Convert board (row, col) to world coordinates (center of square).
pub fn board_to_world(row: u8, col: u8) -> Vec2 {
    let x = (col as f32 - 3.5) * SQUARE_SIZE;
    let y = (row as f32 - 3.5) * SQUARE_SIZE;
    Vec2::new(x, y)
}

/// Convert world coordinates to board (row, col), if within bounds.
pub fn world_to_board(pos: Vec2) -> Option<(u8, u8)> {
    let col = ((pos.x / SQUARE_SIZE) + 4.0).floor();
    let row = ((pos.y / SQUARE_SIZE) + 4.0).floor();
    if (0.0..8.0).contains(&row) && (0.0..8.0).contains(&col) {
        Some((row as u8, col as u8))
    } else {
        None
    }
}

/// Spawn the 8x8 board squares.
fn spawn_board(mut commands: Commands) {
    for row in 0..8u8 {
        for col in 0..8u8 {
            let color = if (row + col) % 2 == 0 {
                LIGHT_SQUARE
            } else {
                DARK_SQUARE
            };
            let pos = board_to_world(row, col);
            commands.spawn((
                Sprite {
                    color,
                    custom_size: Some(Vec2::splat(SQUARE_SIZE)),
                    ..default()
                },
                Transform::from_xyz(pos.x, pos.y, 0.0),
                Square { row, col },
                BoardEntity,
            ));
        }
    }
}

/// Reconcile piece entities with the authoritative `GameStateRes`.
fn sync_pieces_to_entities(
    mut commands: Commands,
    game_state: Res<GameStateRes>,
    pieces: Query<(Entity, &Piece)>,
    crowns: Query<(Entity, &ChildOf), With<CrownMarker>>,
) {
    if !game_state.is_changed() {
        return;
    }

    // Despawn all existing pieces and re-create from state.
    // Simple approach — fine for an 8x8 board (max 24 entities).
    for (entity, _) in pieces.iter() {
        commands.entity(entity).despawn();
    }
    for (entity, _) in crowns.iter() {
        commands.entity(entity).despawn();
    }

    for row in 0..8u8 {
        for col in 0..8u8 {
            if let Some((color, kind)) = game_state.0.get(row, col) {
                let pos = board_to_world(row, col);
                let piece_color = match color {
                    PieceColor::Red => RED_PIECE,
                    PieceColor::Black => BLACK_PIECE,
                };

                let piece_entity = commands
                    .spawn((
                        Sprite {
                            color: piece_color,
                            custom_size: Some(Vec2::splat(SQUARE_SIZE * 0.8)),
                            ..default()
                        },
                        Transform::from_xyz(pos.x, pos.y, 1.0),
                        Piece { row, col },
                        BoardEntity,
                    ))
                    .id();

                if kind == PieceKind::King {
                    commands.spawn((
                        Sprite {
                            color: CROWN_COLOR,
                            custom_size: Some(Vec2::splat(SQUARE_SIZE * 0.3)),
                            ..default()
                        },
                        Transform::from_xyz(0.0, 0.0, 0.1),
                        ChildOf(piece_entity),
                        CrownMarker,
                        BoardEntity,
                    ));
                }
            }
        }
    }
}
