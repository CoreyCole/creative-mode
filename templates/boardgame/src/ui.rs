//! Turn indicator, win message, and new game button.

use bevy::prelude::*;

use crate::board::{GameStateRes, SelectedPiece, ValidMoves, BOARD_SIZE};
use crate::rules::{BoardState, PieceColor};

pub struct UiPlugin;

impl Plugin for UiPlugin {
    fn build(&self, app: &mut App) {
        app.add_systems(Startup, spawn_ui);
        app.add_systems(Update, (update_turn_indicator, handle_new_game));
    }
}

/// Marker for the turn indicator text.
#[derive(Component)]
struct TurnIndicator;

/// Marker for the win message text.
#[derive(Component)]
struct WinMessage;

/// Spawn UI text elements.
fn spawn_ui(mut commands: Commands) {
    // Turn indicator at bottom of board.
    let y = -(BOARD_SIZE / 2.0 + 30.0);
    commands.spawn((
        Text2d::new("Red's turn"),
        TextFont {
            font_size: 24.0,
            ..default()
        },
        TextColor(Color::WHITE),
        Transform::from_xyz(0.0, y, 10.0),
        TurnIndicator,
    ));
}

/// Update turn indicator text based on game state.
#[allow(clippy::type_complexity)]
fn update_turn_indicator(
    game_state: Res<GameStateRes>,
    mut indicators: Query<
        (&mut Text2d, &mut TextColor),
        (With<TurnIndicator>, Without<WinMessage>),
    >,
    mut commands: Commands,
    win_messages: Query<Entity, With<WinMessage>>,
) {
    if !game_state.is_changed() {
        return;
    }

    for (mut text, mut color) in indicators.iter_mut() {
        if let Some(winner) = game_state.0.game_over {
            let winner_name = match winner {
                PieceColor::Red => "Red",
                PieceColor::Black => "Black",
            };
            text.0 = format!("{winner_name} wins!");
            color.0 = Color::srgb(1.0, 0.84, 0.0);

            // Spawn win message if not already present.
            if win_messages.is_empty() {
                commands.spawn((
                    Text2d::new("Click to start a new game"),
                    TextFont {
                        font_size: 18.0,
                        ..default()
                    },
                    TextColor(Color::srgba(1.0, 1.0, 1.0, 0.7)),
                    Transform::from_xyz(0.0, -(BOARD_SIZE / 2.0 + 60.0), 10.0),
                    WinMessage,
                ));
            }
        } else {
            let turn_name = match game_state.0.current_turn {
                PieceColor::Red => "Red",
                PieceColor::Black => "Black",
            };
            text.0 = format!("{turn_name}'s turn");
            color.0 = Color::WHITE;
        }
    }
}

/// Handle new game click when game is over.
fn handle_new_game(
    mouse: Res<ButtonInput<MouseButton>>,
    mut game_state: ResMut<GameStateRes>,
    mut selected: ResMut<SelectedPiece>,
    mut valid_moves: ResMut<ValidMoves>,
    win_messages: Query<Entity, With<WinMessage>>,
    mut commands: Commands,
) {
    if game_state.0.game_over.is_none() {
        return;
    }

    if !mouse.just_pressed(MouseButton::Left) {
        return;
    }

    // Reset game state.
    game_state.0 = BoardState::new();
    selected.0 = None;
    valid_moves.0.clear();

    // Despawn win message.
    for entity in win_messages.iter() {
        commands.entity(entity).despawn();
    }
}
