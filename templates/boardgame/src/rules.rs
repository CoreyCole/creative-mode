//! Pure checkers logic — no Bevy dependency, fully testable.

use serde::{Deserialize, Serialize};

/// Piece color (Red moves first, goes up the board).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum PieceColor {
    Red,
    Black,
}

impl PieceColor {
    pub fn opponent(self) -> Self {
        match self {
            PieceColor::Red => PieceColor::Black,
            PieceColor::Black => PieceColor::Red,
        }
    }
}

/// Piece kind.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum PieceKind {
    Man,
    King,
}

/// A move on the board.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Move {
    pub from: (u8, u8),
    pub to: (u8, u8),
    /// Position of the captured piece, if any.
    pub capture: Option<(u8, u8)>,
}

/// Full board state.
#[derive(Clone, Debug)]
pub struct BoardState {
    /// cells[row][col] — row 0 is bottom (Red's home).
    pub cells: [[Option<(PieceColor, PieceKind)>; 8]; 8],
    pub current_turn: PieceColor,
    pub game_over: Option<PieceColor>,
    /// If set, the piece at this position must continue jumping.
    pub must_continue_jump: Option<(u8, u8)>,
}

impl Default for BoardState {
    fn default() -> Self {
        Self::new()
    }
}

impl BoardState {
    /// Create a new board with standard starting positions.
    pub fn new() -> Self {
        let mut cells = [[None; 8]; 8];

        // Red pieces on rows 0-2 (bottom).
        for row in 0..3u8 {
            for col in 0..8u8 {
                if (row + col) % 2 == 1 {
                    cells[row as usize][col as usize] = Some((PieceColor::Red, PieceKind::Man));
                }
            }
        }

        // Black pieces on rows 5-7 (top).
        for row in 5..8u8 {
            for col in 0..8u8 {
                if (row + col) % 2 == 1 {
                    cells[row as usize][col as usize] = Some((PieceColor::Black, PieceKind::Man));
                }
            }
        }

        Self {
            cells,
            current_turn: PieceColor::Red,
            game_over: None,
            must_continue_jump: None,
        }
    }

    /// Get the piece at a position.
    pub fn get(&self, row: u8, col: u8) -> Option<(PieceColor, PieceKind)> {
        self.cells[row as usize][col as usize]
    }

    /// All legal moves for the current player.
    /// If any captures exist, only captures are returned (mandatory capture rule).
    pub fn legal_moves(&self) -> Vec<Move> {
        let mut captures = Vec::new();
        let mut simple = Vec::new();

        for row in 0..8u8 {
            for col in 0..8u8 {
                if let Some((color, _)) = self.get(row, col) {
                    if color != self.current_turn {
                        continue;
                    }
                    if let Some(forced) = self.must_continue_jump {
                        if (row, col) != forced {
                            continue;
                        }
                    }
                    for m in self.piece_moves(row, col) {
                        if m.capture.is_some() {
                            captures.push(m);
                        } else {
                            simple.push(m);
                        }
                    }
                }
            }
        }

        if !captures.is_empty() {
            captures
        } else {
            simple
        }
    }

    /// Legal moves for a specific piece.
    pub fn piece_moves(&self, row: u8, col: u8) -> Vec<Move> {
        let Some((color, kind)) = self.get(row, col) else {
            return Vec::new();
        };

        let directions = match (color, kind) {
            (PieceColor::Red, PieceKind::Man) => vec![(1i8, -1i8), (1, 1)],
            (PieceColor::Black, PieceKind::Man) => vec![(-1, -1), (-1, 1)],
            (_, PieceKind::King) => vec![(1, -1), (1, 1), (-1, -1), (-1, 1)],
        };

        let mut moves = Vec::new();

        for (dr, dc) in &directions {
            let nr = row as i8 + dr;
            let nc = col as i8 + dc;

            if !in_bounds(nr, nc) {
                continue;
            }

            let nr = nr as u8;
            let nc = nc as u8;

            match self.get(nr, nc) {
                None => {
                    // Simple move to empty square.
                    moves.push(Move {
                        from: (row, col),
                        to: (nr, nc),
                        capture: None,
                    });
                }
                Some((other_color, _)) if other_color != color => {
                    // Jump over opponent piece.
                    let jr = nr as i8 + dr;
                    let jc = nc as i8 + dc;
                    if in_bounds(jr, jc) && self.get(jr as u8, jc as u8).is_none() {
                        moves.push(Move {
                            from: (row, col),
                            to: (jr as u8, jc as u8),
                            capture: Some((nr, nc)),
                        });
                    }
                }
                _ => {}
            }
        }

        moves
    }

    /// Apply a move. Returns `true` if a multi-jump is available (piece must continue).
    pub fn apply_move(&mut self, m: &Move) -> bool {
        let piece = self.cells[m.from.0 as usize][m.from.1 as usize].take();
        let Some((color, mut kind)) = piece else {
            return false;
        };

        // Remove captured piece.
        if let Some((cr, cc)) = m.capture {
            self.cells[cr as usize][cc as usize] = None;
        }

        // King promotion.
        match color {
            PieceColor::Red if m.to.0 == 7 => kind = PieceKind::King,
            PieceColor::Black if m.to.0 == 0 => kind = PieceKind::King,
            _ => {}
        }

        self.cells[m.to.0 as usize][m.to.1 as usize] = Some((color, kind));

        // Check for multi-jump.
        if m.capture.is_some() {
            let follow_up: Vec<Move> = self
                .piece_moves(m.to.0, m.to.1)
                .into_iter()
                .filter(|mv| mv.capture.is_some())
                .collect();

            if !follow_up.is_empty() {
                self.must_continue_jump = Some(m.to);
                return true;
            }
        }

        // End turn.
        self.must_continue_jump = None;
        self.current_turn = color.opponent();

        // Check game over.
        if self.legal_moves().is_empty() {
            self.game_over = Some(color);
        }

        false
    }
}

fn in_bounds(r: i8, c: i8) -> bool {
    (0..8).contains(&r) && (0..8).contains(&c)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn initial_piece_count() {
        let state = BoardState::new();
        let mut red = 0;
        let mut black = 0;
        for row in 0..8 {
            for col in 0..8 {
                match state.cells[row][col] {
                    Some((PieceColor::Red, _)) => red += 1,
                    Some((PieceColor::Black, _)) => black += 1,
                    None => {}
                }
            }
        }
        assert_eq!(red, 12);
        assert_eq!(black, 12);
    }

    #[test]
    fn red_moves_first() {
        let state = BoardState::new();
        assert_eq!(state.current_turn, PieceColor::Red);
        let moves = state.legal_moves();
        assert!(!moves.is_empty());
        // All moves should be simple (no captures at start).
        assert!(moves.iter().all(|m| m.capture.is_none()));
    }

    #[test]
    fn simple_move() {
        let mut state = BoardState::new();
        let moves = state.legal_moves();
        let m = moves.first().unwrap().clone();
        let multi = state.apply_move(&m);
        assert!(!multi);
        assert_eq!(state.current_turn, PieceColor::Black);
    }

    #[test]
    fn capture_and_multi_jump() {
        // Set up a board where Red can capture.
        let mut state = BoardState {
            cells: [[None; 8]; 8],
            current_turn: PieceColor::Red,
            game_over: None,
            must_continue_jump: None,
        };
        // Red at (2,1), Black at (3,2) and (5,4).
        state.cells[2][1] = Some((PieceColor::Red, PieceKind::Man));
        state.cells[3][2] = Some((PieceColor::Black, PieceKind::Man));
        state.cells[5][4] = Some((PieceColor::Black, PieceKind::Man));

        let moves = state.legal_moves();
        // Should only have captures (mandatory).
        assert!(moves.iter().all(|m| m.capture.is_some()));

        let m = moves
            .iter()
            .find(|m| m.to == (4, 3))
            .expect("should be able to jump to (4,3)");

        let multi = state.apply_move(m);
        assert!(multi, "should have multi-jump available");
        assert_eq!(state.must_continue_jump, Some((4, 3)));

        // Continue the jump.
        let moves = state.legal_moves();
        assert_eq!(moves.len(), 1);
        assert_eq!(moves[0].from, (4, 3));
        assert_eq!(moves[0].to, (6, 5));
    }

    #[test]
    fn king_promotion() {
        let mut state = BoardState {
            cells: [[None; 8]; 8],
            current_turn: PieceColor::Red,
            game_over: None,
            must_continue_jump: None,
        };
        state.cells[6][1] = Some((PieceColor::Red, PieceKind::Man));

        let m = Move {
            from: (6, 1),
            to: (7, 2),
            capture: None,
        };
        state.apply_move(&m);
        assert_eq!(state.cells[7][2], Some((PieceColor::Red, PieceKind::King)));
    }

    #[test]
    fn game_over_detection() {
        let mut state = BoardState {
            cells: [[None; 8]; 8],
            current_turn: PieceColor::Red,
            game_over: None,
            must_continue_jump: None,
        };
        // Red king vs no black pieces — after Red moves, Black has no moves.
        state.cells[3][2] = Some((PieceColor::Red, PieceKind::King));

        let moves = state.legal_moves();
        assert!(!moves.is_empty());

        let m = moves.first().unwrap().clone();
        state.apply_move(&m);
        // Black has no pieces, so game over.
        assert_eq!(state.game_over, Some(PieceColor::Red));
    }
}
