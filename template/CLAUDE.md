# Creative Mode - Game World

This is a Bevy 0.18 + Lightyear 0.26 multiplayer game (using aeronet WebSocket transport).

## Structure
- `shared/` - Protocol definitions (shared between server + client)
- `server/` - Headless game server (native binary)
- `client/` - Game client (compiles to WASM via Trunk)

## Building
- Server: `cargo build --release -p server`
- Client: `cd client && trunk build --release`
  - Trunk handles cargo build -> wasm-bindgen -> wasm-opt -> index.html
  - Output goes to client/dist/ by default

## Debugging
All logs are JSONL format. Log files are at `$CM_LOG_DIR/` (set by the harness):
- Game server logs (tail for runtime issues):
  `tail -f $CM_LOG_DIR/game-server.jsonl | jq .`
- Build logs (check for compile errors):
  `tail -f $CM_LOG_DIR/build.jsonl | jq .`
- Filter for errors only:
  `tail -f $CM_LOG_DIR/game-server.jsonl | jq 'select(.level == "error")'`
- Harness server log:
  `tail -f data/logs/harness.jsonl | jq .`

When debugging a crash or unexpected behavior, ALWAYS check game-server.jsonl first.

## Key Patterns
- All replicated components go in `shared/src/protocol.rs`
- Server is authoritative - client sends inputs, server applies them
- Use Lightyear's `Replicate` bundle for entity sync
- Assets load from HTTP: `asset_server.load("http://{harness_host}/assets/...")`
- Do NOT use `copy-dir` in client/index.html for assets - they are served separately

## CHANGES.txt (Required)
Before you finish, ALWAYS write a brief summary of what you changed to `CHANGES.txt`
in the project root. This is shown to users in the UI as context for their next prompt.

Keep it concise (2-4 sentences). Describe WHAT you built/changed and WHY, not which
files you edited. Example:
```
Added Perlin noise terrain generation with green grass material and rolling hill
geometry. Hills have configurable amplitude and frequency for natural-looking terrain.
The tallest hill is tracked so future prompts can reference "the highest point."
```

## MEMORY.md
Read MEMORY.md for this world's design decisions and history.
