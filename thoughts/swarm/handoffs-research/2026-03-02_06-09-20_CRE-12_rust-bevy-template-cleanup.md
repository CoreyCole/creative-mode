---
ticket: CRE-12
phase: research
result: success
session: a5610e73
workflow: 996b4552
timestamp: 2026-03-02T06:09:20Z
---

## BLUF
Completed exhaustive Rust/Bevy template cleanup audit: cataloged ~133 magic numbers across all 3 templates, mapped 3D client main.rs structure for module split, audited 3 dead code annotations in boardgame, and proposed naming conventions. Research doc ready for planning phase.

## What Was Done
- Cataloged ~60 magic numbers in templates/2d/ across 6 source files
- Cataloged ~38 magic numbers in templates/3d/ across 3 source files (only 5 of 38 are named constants)
- Cataloged ~35 magic numbers in templates/boardgame/ across 6 source files
- Mapped all 12 systems, resources, plugins, and dependencies in 3D client main.rs
- Proposed 5-module split for 3D client (camera, input, scene, player, connection)
- Audited 3 `#[allow(dead_code)]` annotations — all truly dead (Square/Piece fields, BridgeAction enum)
- Proposed naming conventions: SCREAMING_SNAKE_CASE with domain prefix and unit suffix
- Identified key duplication patterns (dialog rendering copy-paste, shared BridgePlugin)
- Wrote complete research document at thoughts/swarm/research/2026-03-02_06-09-20_CRE-12_rust-bevy-template-cleanup.md

## What Was NOT Done
- Did not verify Bevy 0.18 `const Color` support (needed before extracting color constants)
- Did not check if any CLAUDE.md templates reference boardgame Square/Piece .row/.col fields
- Did not catalog string-based magic values beyond the obvious ones (e.g., asset paths in 3D template)

## Key Files
- `templates/2d/src/camera.rs` — has named ROOM_WIDTH/HEIGHT constants but they're re-hardcoded elsewhere
- `templates/2d/src/interaction.rs` + `debug.rs` — dialog rendering block is copy-pasted (9 identical values)
- `templates/3d/client/src/main.rs` — 623 lines, 30 magic numbers, needs 5-module split
- `templates/3d/shared/src/protocol.rs` — good example of named constants (5 already defined)
- `templates/boardgame/src/rules.rs` — board dimension `8` appears 14 times with no constant
- `templates/boardgame/src/board.rs` — Square/Piece structs have dead fields
- `templates/boardgame/src/bridge.rs` — BridgeAction enum entirely unused scaffolding

## Gotchas
- Linear API key has limited scopes — couldn't query issues directly, had to use local SQLite DB and logs for ticket context
- CRE-12 ticket description was empty in local DB; title found in service logs ("Rust/Bevy template cleanup audit")
- The 2D template's ROOM_WIDTH/ROOM_HEIGHT are defined in camera.rs but other files hardcode 1280.0/720.0 directly

## Next Steps
- Planning phase should create implementation tasks grouped by template (2D, 3D, boardgame)
- Start with boardgame BOARD_DIMENSION constant (highest impact-to-effort ratio: 1 constant eliminates 14 magic numbers)
- 3D module split should be a separate task from constant extraction (different risk profile)
- Verify Bevy 0.18 const Color support before designing color constant extraction approach
