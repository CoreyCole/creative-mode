---
date: 2026-02-15T19:09:08-08:00
researcher: CoreyCole
git_commit: 94c164d27eda4d417743093ed6dc2a672bbb9278
branch: main
repository: creative-mode
topic: "Boardgame Template (Checkers) — Design & Multiplayer Next Steps"
tags: [implementation, boardgame, checkers, bevy, wasm, multiplayer]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Boardgame Template (Checkers) — Design & Multiplayer

## Task(s)

**Completed:**
- Added new `"boardgame"` template type to the harness (Go server recognizes it, builds it, serves it in iframes)
- Created `templates/boardgame/` with a full checkers game in Bevy 0.18 WASM
- Updated build/format scripts (`check.sh`, `fmt.sh`) to include boardgame clippy/fmt
- All checks pass (`just check` green)
- Committed and pushed to `main` (commit `94c164d`)

**Not yet tested:**
- Creating a boardgame world from the lobby UI (Docker harness had a pre-existing `just` issue; planned to test on VPS)
- Visual verification of the board rendering and gameplay

## Critical References
- `templates/boardgame/CLAUDE.md` — template dev guide with ECS architecture, rules, debug queries
- `templates/2d/CLAUDE.md` — the 2D template this was modeled after (same client-only WASM pattern)

## Recent changes

**New files (templates/boardgame/):**
- `Cargo.toml` — single crate "checkers-game", Bevy 0.18 `2d` feature, wasm-bindgen `=0.2.108`
- `Trunk.toml`, `index.html` — identical pattern to 2D, `data-bin="checkers-game"`
- `src/rules.rs` — pure checkers logic, no Bevy deps, with unit tests
- `src/board.rs` — 8x8 board rendering (80px squares, 640x640 total), piece entity sync
- `src/interaction.rs` — click-to-select-to-move flow with forced jump enforcement
- `src/camera.rs` — simplified 2D camera fit-to-board
- `src/bridge.rs` — postMessage bridge (copied from 2D)
- `src/ui.rs` — turn indicator, win message, click-to-restart
- `src/debug.rs` — debug queries: `board`, `moves`, `reset`
- `src/lib.rs` — Bevy app setup (800x800 window, all plugins registered)
- `src/main.rs` — entry point calling `checkers_game::run()`

**Modified harness files:**
- `harness/main.go:50` — added `"boardgame"` to template dir loop
- `harness/internal/server/server.go:277` — added `"boardgame"` to validation
- `harness/internal/builder/builder.go:87` — changed `!= "2d"` to `== "3d"` (future-proof)
- `harness/internal/claude/claude.go:95,214` — two places changed `!= "2d"` to `== "3d"`
- `harness/internal/world/manager.go:188` — added `|| templateType == "boardgame"` to skip game server
- `harness/views/lobby/lobby.templ:76` — added `<option value="boardgame">Board Game</option>`
- `harness/views/world/world.templ:12` — added `|| w.TemplateType == "boardgame"` to iframe logic
- `scripts/check.sh` — added parallel boardgame clippy WASM job
- `scripts/fmt.sh` — added parallel boardgame cargo fmt job

## Learnings

### Checkers Game Design (ECS Architecture)

**Separation of concerns:** The game logic lives entirely in `rules.rs` with zero Bevy dependencies. This makes it unit-testable and portable. The Bevy ECS layer (`board.rs`, `interaction.rs`, `ui.rs`) only handles rendering and input.

**State flow:**
1. `GameStateRes(BoardState)` is the single source of truth (resource)
2. `interaction.rs` handles clicks: converts screen coords → board coords, checks valid moves, calls `BoardState::apply_move()`
3. `board.rs` watches `GameStateRes` changes and reconciles piece entities (full despawn+respawn — fine for 24 max entities)
4. `ui.rs` watches `GameStateRes` changes and updates turn indicator text

**Key resources:**
- `GameStateRes` — wraps `BoardState` (the 8x8 cell array + turn + game_over + must_continue_jump)
- `SelectedPiece(Option<(u8, u8)>)` — which piece the current player has selected
- `ValidMoves(Vec<Move>)` — legal moves for the selected piece (filtered from `legal_moves()`)

**Coordinate system:** Board is centered at origin. `board_to_world(row, col)` maps `(0,0)` (bottom-left, Red's home) to `(-280, -280)` world coords. `SQUARE_SIZE = 80.0`, so the board spans -320 to +320 on each axis.

**Mandatory capture rule:** `legal_moves()` computes all moves for the current player. If any captures exist, only captures are returned. This enforces mandatory capture at the rules level.

**Multi-jump:** `apply_move()` returns `true` if the jumping piece has further captures available. The interaction system keeps the piece selected and only allows continuing the jump chain.

### Harness Integration Pattern

The boardgame template follows the exact same pattern as 2D: client-only WASM, no game server. The key harness changes were:
- **Template type validation** — added `"boardgame"` to the allow-list
- **Game server skip** — boardgame worlds skip game server startup (same as 2D)
- **Future-proofing** — changed `templateType != "2d"` checks to `templateType == "3d"` so new client-only templates don't need to update every check

### Docker Issue (Pre-existing)

The Docker harness container fails because `.air.toml` calls `just build-tailwind` but `just` is not installed in the Dockerfile (`harness/Dockerfile`). This is pre-existing and unrelated to boardgame changes.

## Artifacts

- `templates/boardgame/` — complete checkers game template (13 files + Cargo.lock)
- `templates/boardgame/CLAUDE.md` — template developer guide
- `templates/boardgame/src/rules.rs` — pure game logic with 5 unit tests
- This handoff document

## Action Items & Next Steps

### Immediate: Test on VPS
1. Deploy to VPS (`just vps-deploy` from harness dir on server)
2. Create a boardgame world from the lobby UI
3. Verify: board renders with 24 pieces, clicking works, captures work, king promotion, win detection
4. Test debug queries: `just debug <worldID> board`

### Next: Multiplayer via WebSocket/SSE
The current implementation is **hot-seat** (two players share one screen). To make it networked multiplayer:

**Option A: Harness-mediated turns (simplest, fits existing architecture)**
- Server holds authoritative `BoardState` in a per-world resource (Go struct or SQLite)
- Players send moves via `POST /api/world/:worldID/move` (validated server-side)
- Board state changes broadcast to all clients via existing SSE/EventBus
- WASM client becomes view-only + move sender (no local game logic)
- Each player is assigned a color based on join order
- Pros: reuses existing SSE infrastructure, no new protocols
- Cons: ~100ms latency per move (fine for turn-based)

**Option B: Lightyear networking (like 3D template)**
- Add a server binary to the boardgame template (becomes a workspace like 3D)
- Use Lightyear for state replication
- Overkill for turn-based but would support real-time board games later
- Cons: significant complexity, needs game server management

**Recommended: Option A** — turn-based games don't need real-time networking. The harness SSE system already handles per-world event broadcasting.

**Implementation sketch for Option A:**
1. Add `board_state` column to worlds table (JSON blob) or a new `game_state` table
2. New handler: `POST /world/:worldID/move` — accepts `{from: "2,1", to: "4,3"}`, validates via rules logic (port to Go or call WASM), updates state, broadcasts via EventBus
3. New SSE event: `EventBoardMove` — carries the new board state
4. WASM client: on SSE board update, replace local `GameStateRes`; on click, POST move to server instead of applying locally
5. Player assignment: first visitor = Red, second = Black (stored in session or world metadata)

### Other Enhancements
- **Visual polish:** piece animations (slide to destination), capture animation, king promotion effect
- **Sound effects:** click, capture, win (Bevy `bevy_audio` feature works on WASM)
- **Game variants:** add options for different checkers rulesets (international draughts on 10x10, etc.)
- **Spectator mode:** read-only view for non-players

## Other Notes

- The `rules.rs` module is designed to be easily portable to Go if server-side validation is needed for multiplayer. The data structures are simple (8x8 array, enum for color/kind, struct for moves).
- The `debug.rs` queries (`board`, `moves`, `reset`) are useful for automated testing — `just debug <worldID> board` returns full board state JSON.
- Pieces are rendered as colored squares with a smaller gold square for kings (no sprite assets needed). This can be upgraded to actual checker piece sprites later.
- The camera uses a fixed fit-to-board approach (no pan/zoom unlike the 2D template) since the board always fits on screen.
