# Creative Mode - Board Game (Checkers)

A turn-based board game built with Bevy 0.18 WASM. Two players share the screen (hot-seat). No game server needed — all logic runs in the browser.

## Architecture

### How It Runs

The boardgame template is a **client-only WASM app** loaded inside an iframe by the harness. Same architecture as the 2D template — no game server.

```
Harness (Go server, port 8080)
  └─ World page → <iframe src="http://localhost:{trunkPort}/">
                    └─ Trunk-built HTML + WASM
                        └─ Bevy app (Camera2d, sprites, text, input)
                            ├─ board.rs       — board rendering & entity sync
                            ├─ rules.rs       — pure checkers logic (testable)
                            ├─ interaction.rs  — click→select→move flow
                            ├─ camera.rs      — fit board to window
                            ├─ bridge.rs      — postMessage to parent
                            ├─ ui.rs          — turn indicator, win message
                            └─ debug.rs       — runtime state inspection
```

### Crate Layout

Single crate with both library and binary target:

```toml
[lib]
crate-type = ["cdylib", "rlib"]
```

- **Binary** (`src/main.rs`): calls `checkers_game::run()` — this is what Trunk builds
- **Library** (`src/lib.rs`): defines `run()` which sets up the Bevy App

**CRITICAL — `index.html` must target the binary:**

```html
<link data-trunk rel="rust" data-bin="checkers-game" />
```

## Structure

| File | Purpose |
|------|---------|
| `src/main.rs` | Entry point — calls `checkers_game::run()` |
| `src/lib.rs` | App setup: DefaultPlugins + game plugins |
| `src/rules.rs` | Pure checkers logic — no Bevy deps, fully testable |
| `src/board.rs` | Board rendering, piece entity sync, coordinate conversion |
| `src/interaction.rs` | Mouse click → selection → move execution |
| `src/camera.rs` | 2D camera fit-to-board |
| `src/bridge.rs` | postMessage bridge to harness parent frame |
| `src/ui.rs` | Turn indicator text, win message, new game |
| `src/debug.rs` | Debug query system via JS bridge |
| `index.html` | Trunk entry point — canvas, JS bridge |

## Game Rules (Standard American Checkers)

- 8×8 board, pieces on dark squares only
- Red moves first, moves diagonally forward
- Capture by jumping over opponent piece to empty square beyond
- **Mandatory capture**: if a capture is available, you must take it
- **Multi-jump**: after a capture, if the same piece can capture again, it must
- **King promotion**: piece reaching the far row becomes a king (moves in all 4 diagonal directions)
- **Win condition**: opponent has no legal moves (no pieces or all blocked)

## ECS Architecture

### Resources
- `GameStateRes(BoardState)` — authoritative board state
- `SelectedPiece(Option<(u8, u8)>)` — currently selected position
- `ValidMoves(Vec<Move>)` — legal moves for selected piece

### Components
- `BoardEntity` — marker for bulk despawn on reset
- `Square { row, col }` — board square
- `Piece { row, col }` — checker piece
- `MoveHighlight` — valid move indicator
- `SelectionHighlight` — selected piece glow

### Board Layout
- 8×8 grid centered at origin, `SQUARE_SIZE = 80.0`, total 640×640
- Row 0 = bottom (Red's home), Row 7 = top (Black's home)
- Coordinate conversion: `board_to_world(row, col) -> Vec2`, `world_to_board(Vec2) -> Option<(u8, u8)>`

## Debug Queries

| Query | Response |
|-------|----------|
| `{"type": "board"}` | Full board state: pieces, turn, game_over |
| `{"type": "moves"}` | All legal moves for current player |
| `{"type": "moves", "position": "3,2"}` | Legal moves for piece at row 3, col 2 |
| `{"type": "reset"}` | Reset to new game |

## Building

- Client only: `trunk build --release` (from project root)
- Check: `cargo clippy --target wasm32-unknown-unknown -- -D warnings`

### wasm-bindgen Pin

`wasm-bindgen` is pinned to exactly `0.2.108` in both `Cargo.toml` and `Trunk.toml`. These MUST match.

### public_url

`Trunk.toml` sets `public_url = "./"` so that `trunk build` generates relative asset paths instead of root-absolute. This is required for static WASM serving at `/wasm/{worldID}/{cpID}/`.

## CHANGES.txt (Required)

Before you finish, ALWAYS write a brief summary of what you changed to `CHANGES.txt`.

## MEMORY.md

Read MEMORY.md for this world's design decisions and history.
