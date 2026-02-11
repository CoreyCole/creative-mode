# Component 4: Bevy Game Template

## Overview

Create a minimal Bevy 0.18 + Lightyear 0.26 multiplayer game template that compiles to WASM. Players fly around an empty world and see each other as pill meshes. This is the starter template that gets copied for every new world.

**Dependencies**: None (start immediately, can develop and test independently)
**Depends on this**: Components 5, 7

## Directory Layout

```
template/
├── Cargo.toml                  # Workspace
├── MEMORY.md                   # Claude code memory file (template for new worlds)
├── CLAUDE.md                   # Claude code instructions for game dev
├── .claude/
│   ├── settings.json           # Claude code hooks config (observability)
│   └── hooks/                  # Hook scripts for JSONL logging
│       ├── on-tool-use.sh      # Logs pre/post tool use events
│       ├── on-stop.sh          # Notifies harness when claude finishes
│       └── on-notification.sh  # Logs claude notifications
├── shared/
│   ├── Cargo.toml
│   └── src/
│       ├── lib.rs
│       └── protocol.rs         # Lightyear protocol definitions
├── server/
│   ├── Cargo.toml
│   └── src/
│       └── main.rs             # Headless Bevy server
├── client/
│   ├── Cargo.toml
│   ├── Trunk.toml              # Trunk build configuration
│   ├── index.html              # Trunk source HTML (minimal)
│   └── src/
│       └── main.rs             # Bevy WASM client
└── .cargo/
    └── config.toml             # WASM build settings
```

## Implementation Details

### 1. Cargo Workspace (`template/Cargo.toml`)

```toml
[workspace]
members = ["shared", "server", "client"]
resolver = "2"

[workspace.dependencies]
bevy = "0.18"
lightyear = "0.26"
serde = { version = "1", features = ["derive"] }
wasm-bindgen = "=0.2.108"  # Must exactly match Trunk.toml [tools] wasm_bindgen version
```

> **Version note**: Lightyear 0.26 targets Bevy 0.18 (released Jan 2026). Lightyear uses
> [aeronet](https://github.com/aecsocket/aeronet) as its transport layer, providing
> WebSocket and WebTransport implementations. Always reference the
> [Lightyear examples](https://github.com/cBournhonesque/lightyear/tree/main/examples)
> for current API patterns.

### 2. Shared Protocol (`template/shared/`)

**`shared/Cargo.toml`:**
```toml
[package]
name = "shared"
version = "0.1.0"
edition = "2021"

[dependencies]
bevy = { workspace = true }
lightyear = { workspace = true }
serde = { workspace = true }
```

**`shared/src/lib.rs`:**
```rust
pub mod protocol;
```

**`shared/src/protocol.rs`:**

Defines the Lightyear protocol shared between server and client:
- `PlayerPosition` component (replicated) — `Vec3`
- `PlayerInput` — movement vector + look direction
- Channel configuration (unreliable for position, reliable for spawning)

```rust
use bevy::prelude::*;
use lightyear::prelude::*;
use serde::{Deserialize, Serialize};

#[derive(Component, Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct PlayerPosition(pub Vec3);

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct PlayerInput {
    pub movement: Vec3,
    pub look: Vec2,
}

// Protocol registration uses Lightyear 0.26 API (aeronet-based transport)
// IMPORTANT: The exact API has changed from older versions.
// Reference: https://github.com/cBournhonesque/lightyear/tree/main/examples
// Use the simple_box example as the primary reference.
```

> **Critical**: Lightyear 0.26 uses the aeronet transport layer. Protocol registration,
> transport configuration, and plugin setup differ significantly from older Lightyear versions.
> The code above is illustrative — the implementing agent MUST reference Lightyear's current
> examples (especially `simple_box`) for the exact API. Do not copy-paste from old tutorials.

### 3. Server Binary (`template/server/`)

**`server/Cargo.toml`:**
```toml
[package]
name = "server"
version = "0.1.0"
edition = "2021"

[dependencies]
bevy = { workspace = true }
lightyear = { workspace = true }
shared = { path = "../shared" }
```

**`server/src/main.rs`:**

Headless Bevy app with Lightyear server plugin:
- Listens on configurable port from env var `GAME_PORT` (default 9001)
- **Transport**: WebSocket server via aeronet — no TLS certificates needed
- Spawns player entity when client connects
- Replicates `PlayerPosition` to all clients
- Applies `PlayerInput` to update positions
- Fly movement: applies input as velocity in camera-relative direction
- No gravity, free 3D movement
- Uses `ScheduleRunnerPlugin` for headless operation (no window/rendering)

### 4. Client Binary (`template/client/`)

**`client/Cargo.toml`:**
```toml
[package]
name = "client"
version = "0.1.0"
edition = "2021"

[dependencies]
bevy = { workspace = true }
lightyear = { workspace = true }
shared = { path = "../shared" }
wasm-bindgen = { workspace = true }
web-sys = { version = "0.3", features = ["Window", "Location", "UrlSearchParams"] }
```

**`client/src/main.rs`:**

Bevy app with Lightyear client plugin:
- **Transport**: WebSocket via aeronet — works on WASM without TLS setup
- Server address: reads `server_port` from URL query params, connects to `ws://localhost:{port}`
- Fly camera: WASD + mouse look, shift for speed boost
- Renders other players as pill meshes (capsule geometry)
- Ground plane with simple grid or color
- Basic lighting (directional + ambient)

```rust
// Read server port from URL query parameters
fn get_server_port() -> u16 {
    let window = web_sys::window().unwrap();
    let search = window.location().search().unwrap();
    let params = web_sys::UrlSearchParams::new_with_str(&search).unwrap();
    params.get("server_port")
        .and_then(|p| p.parse().ok())
        .unwrap_or(9001)
}
```

> **Why WebSocket instead of WebTransport?** WebTransport requires TLS certificates with
> max 14-day validity per spec. Self-signed certs need X.509v3 with ECDSA P-256, and the
> cert digest must be provided to the browser. This operational complexity isn't worth it
> for a prototype. WebSocket works on both native and WASM without any certificate setup.

### 5. WASM Build Configuration

**`template/.cargo/config.toml`:**
```toml
[target.wasm32-unknown-unknown]
rustflags = ['--cfg', 'getrandom_backend="wasm_js"']
```

**`template/client/Trunk.toml`:**
```toml
[build]
target = "index.html"
filehash = true
minify = "on_release"

[tools]
wasm_bindgen = "0.2.108"
```

> **Version pinning**: The wasm-bindgen CLI version in `Trunk.toml` must **exactly match**
> the `wasm-bindgen` crate version in `Cargo.lock`. A mismatch causes build failures.

**`template/client/index.html`:**
```html
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Creative Mode</title>
    <style>
        html, body { margin: 0; padding: 0; overflow: hidden; width: 100%; height: 100%; }
        canvas { width: 100%; height: 100%; display: block; }
    </style>
</head>
<body></body>
</html>
```

### 6. CLAUDE.md (`template/CLAUDE.md`)

```markdown
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
```

### 7. MEMORY.md Template (`template/MEMORY.md`)

```markdown
# World Memory

## Design Decisions
(Design decisions made by users will be recorded here)

## Architecture Notes
- Bevy 0.18 + Lightyear 0.26 multiplayer (aeronet WebSocket transport)
- Server-authoritative with client prediction
- WASM client, native server

## Current State
- Starter template: empty world with ground plane, fly camera, pill player meshes
```

### 8. Claude Code Hooks

**`template/.claude/settings.json`:**
```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": ".*",
        "command": ".claude/hooks/on-tool-use.sh pre"
      }
    ],
    "PostToolUse": [
      {
        "matcher": ".*",
        "command": ".claude/hooks/on-tool-use.sh post"
      }
    ],
    "Stop": [
      {
        "command": ".claude/hooks/on-stop.sh"
      }
    ],
    "Notification": [
      {
        "command": ".claude/hooks/on-notification.sh"
      }
    ]
  }
}
```

**`template/.claude/hooks/on-tool-use.sh`:**
```bash
#!/bin/bash
# Receives tool use event JSON on stdin from Claude Code
# Arg $1: "pre" or "post"
PHASE="$1"
EVENT_JSON=$(cat)

# Extract fields from the Claude Code hook payload
TOOL=$(echo "$EVENT_JSON" | jq -r '.tool_name // .tool // "unknown"')
FILE=$(echo "$EVENT_JSON" | jq -r '.input.file_path // .input.command // ""' | head -c 200)

# Read world/checkpoint IDs from env vars (set by harness before launching session)
WORLD_ID="${CM_WORLD_ID}"
CP_ID="${CM_CHECKPOINT_ID}"
HARNESS_URL="${CM_HARNESS_URL:-http://localhost:8080}"
LOG_FILE="${CM_LOG_DIR}/claude.jsonl"

# Build JSONL event
JSONL=$(jq -n \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg phase "$PHASE" \
  --arg tool "$TOOL" \
  --arg file "$FILE" \
  --arg worldID "$WORLD_ID" \
  --arg cpID "$CP_ID" \
  '{ts: $ts, level: "info", event: ("claude.tool_use." + $phase), worldID: $worldID, cpID: $cpID, tool: $tool, file: $file}')

# Append to JSONL log file
echo "$JSONL" >> "$LOG_FILE"

# POST to harness for live SSE updates (fire-and-forget)
curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  -d "$JSONL" &>/dev/null &
```

**`template/.claude/hooks/on-stop.sh`:**
```bash
#!/bin/bash
EVENT_JSON=$(cat)
WORLD_ID="${CM_WORLD_ID}"
CP_ID="${CM_CHECKPOINT_ID}"
HARNESS_URL="${CM_HARNESS_URL:-http://localhost:8080}"
LOG_FILE="${CM_LOG_DIR}/claude.jsonl"

JSONL=$(jq -n \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg worldID "$WORLD_ID" \
  --arg cpID "$CP_ID" \
  '{ts: $ts, level: "info", event: "claude.session_stopped", worldID: $worldID, cpID: $cpID}')

echo "$JSONL" >> "$LOG_FILE"

# Notify harness that claude is done - triggers the build pipeline
curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  -d "$JSONL" &>/dev/null &
```

**`template/.claude/hooks/on-notification.sh`:**
```bash
#!/bin/bash
EVENT_JSON=$(cat)
WORLD_ID="${CM_WORLD_ID}"
CP_ID="${CM_CHECKPOINT_ID}"
LOG_FILE="${CM_LOG_DIR}/claude.jsonl"
HARNESS_URL="${CM_HARNESS_URL:-http://localhost:8080}"

MESSAGE=$(echo "$EVENT_JSON" | jq -r '.message // ""')

JSONL=$(jq -n \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg worldID "$WORLD_ID" \
  --arg cpID "$CP_ID" \
  --arg message "$MESSAGE" \
  '{ts: $ts, level: "info", event: "claude.notification", worldID: $worldID, cpID: $cpID, message: $message}')

echo "$JSONL" >> "$LOG_FILE"

curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  -d "$JSONL" &>/dev/null &
```

All hook scripts must be executable: `chmod +x template/.claude/hooks/*.sh`

## Interface Contract

This component provides to other components:

1. **Template directory** (`template/`) — Component 3 copies this to create new worlds
2. **Build commands** — `cargo build --release -p server` and `cd client && trunk build --release`
3. **Hook scripts** — Component 5 relies on these to receive claude code events
4. **Server port convention** — server reads `GAME_PORT` env var (Component 3 sets this)
5. **Client port convention** — client reads `server_port` URL query param (Component 6 sets this in iframe URL)
6. **CHANGES.txt convention** — Claude writes this; Component 5 reads it for work summaries
7. **MEMORY.md convention** — Component 5 manages this file before/after claude sessions

## Key Research Notes

- **Lightyear 0.26 API**: The protocol and transport APIs have changed significantly from older versions. The implementing agent MUST consult the [Lightyear examples](https://github.com/cBournhonesque/lightyear/tree/main/examples), particularly `simple_box`, for correct API usage.
- **Aeronet transport**: Lightyear uses aeronet for networking. WebSocket is the correct transport for this project (not WebTransport).
- **wasm-bindgen version pinning**: `Trunk.toml` tools version and `Cargo.toml` dependency version MUST match exactly.
- The `getrandom_backend="wasm_js"` cfg flag is required for random number generation in WASM.

## Success Criteria

### Automated Verification
- [ ] `cd template && cargo build -p server` compiles
- [ ] `cd template/client && trunk build --release` compiles and produces `dist/`
- [ ] `dist/` contains `index.html`, `.wasm`, and `.js` files
- [ ] Server binary starts and listens on specified port
- [ ] All hook scripts are executable and valid bash

### Manual Verification
- [ ] Open WASM build in browser (serve `dist/` with any HTTP server) -> see ground plane and sky
- [ ] WASD + mouse to fly around the world
- [ ] Start server, connect two browser tabs -> see each other as pill meshes
- [ ] Movement is smooth with prediction/interpolation
