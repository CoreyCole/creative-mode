# Creative Mode - Implementation Plan

## Overview

Creative Mode is a claude-powered game world builder that lets anyone create multiplayer 3D games through natural language prompts. It consists of three components: a **Harness Server** (Go + DatastarUI) that provides the management UI overlay, a **Game Server** (Rust/Bevy, native binary) per world, and a **Game Client** (Rust/Bevy, compiled to WASM) that runs in the browser. The harness manages tmux sessions running claude code to edit game source code in response to user prompts.

The core metaphor: every prompt **forks** the current world at its latest checkpoint, producing a tree of game versions that users can browse, compare, and build on. Friends share a server and collaboratively shape game worlds together.

## Current State Analysis

- Empty repository with a README placeholder
- No existing source code, configs, or dependencies
- DatastarUI component library already exists at `github.com/coreycole/datastarui`
- Bevy 0.18 (released Jan 2026) has solid WASM support; Lightyear 0.26 is the best networking library for server-authoritative multiplayer with WASM (targets Bevy 0.18)
- **Target platform**: Linux (Ubuntu 24.04) server. Local macOS development uses Docker.
- **Scale**: <10 users on a shared, trusted server. SQLite is sufficient.

## Desired End State

A running system where:
1. Users open a browser and see a world browser
2. They can create a new world (from the starter template) or join an existing one
3. The game renders full-screen in a canvas, with a transparent harness UI overlay
4. Users type prompts to modify the game world → claude code edits the Rust source → the game rebuilds → the browser reloads the new WASM build
5. Users save checkpoints and fork from any checkpoint
6. Multiple users see each other in the same world (pill meshes, fly camera)
7. World/checkpoint switching is seamless from the harness UI

### Verification:
- Start harness server, create world, fly around in browser
- Submit a prompt, watch the build, see the changes
- Save checkpoint, submit different prompt, switch between checkpoints
- Two browser tabs see each other as pill meshes in the same world

## What We're NOT Doing

- Asset creation pipeline (textures, models - we use basic primitives and colors)
- Production deployment / SSL / domain setup
- Mobile support
- Persistent game state (worlds reset when server restarts)
- Voice chat (text chat is supported via the global notification log)
- Hot module reloading for Bevy (full rebuild + reload on each prompt)

## Docker Environment

The server runs on **Ubuntu 24.04**. Local macOS development uses Docker to match the production environment.

### Dockerfile

```dockerfile
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TERM=xterm-256color

# System dependencies for Bevy native (headless server) + WASM builds
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    git \
    pkg-config \
    libssl-dev \
    libasound2-dev \
    libudev-dev \
    libx11-dev \
    libxkbcommon-x11-0 \
    tmux \
    jq \
    sqlite3 \
    && rm -rf /var/lib/apt/lists/*

# Rust
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"
RUN rustup target add wasm32-unknown-unknown

# Trunk (pre-built binary)
ARG TRUNK_VERSION=0.21.14
RUN curl -L "https://github.com/trunk-rs/trunk/releases/download/v${TRUNK_VERSION}/trunk-x86_64-unknown-linux-gnu.tar.gz" \
    -o /tmp/trunk.tar.gz && \
    tar -xzf /tmp/trunk.tar.gz -C /usr/local/bin && \
    rm /tmp/trunk.tar.gz

# Go
ARG GO_VERSION=1.23.6
RUN curl -L "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -xz -C /usr/local
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Claude Code CLI
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g @anthropic-ai/claude-code@latest

WORKDIR /app
```

### docker-compose.yml

```yaml
services:
  creative-mode:
    build: .
    ports:
      - "8080:8080"       # Harness server
      - "9001-9020:9001-9020"  # Game server port range
    volumes:
      - .:/app
      - cargo-registry:/root/.cargo/registry
      - cargo-git:/root/.cargo/git
      - template-target:/app/template/target
    environment:
      - GITHUB_CLIENT_ID
      - GITHUB_CLIENT_SECRET
      - ANTHROPIC_API_KEY
      - HARNESS_URL=http://localhost:8080
    stdin_open: true
    tty: true  # Required for tmux

volumes:
  cargo-registry:
  cargo-git:
  template-target:
```

> **Note**: `libasound2-dev` retains its name on Ubuntu 24.04 (the runtime lib was renamed to
> `libasound2t64`, but the dev headers package is unchanged). Cargo registry and template target
> are volume-mounted to persist across container restarts and speed up rebuilds.

## Architecture

### System Diagram

```
┌─────────────── Server (Ubuntu 24.04 / Docker) ──────────┐
│                                                          │
│  ┌─────────────────────────────────────────────────┐     │
│  │           Harness Server (Go, :8080)            │     │
│  │  ┌──────────┐ ┌──────────┐ ┌───────────────┐   │     │
│  │  │ DatastarUI│ │ SQLite   │ │ tmux Manager  │   │     │
│  │  │ Overlay   │ │ Database │ │ (claude code) │   │     │
│  │  └──────────┘ └──────────┘ └───────────────┘   │     │
│  │  ┌──────────────────────────────────────────┐   │     │
│  │  │ Static File Server                       │   │     │
│  │  │ /wasm/{world}/{checkpoint}/  (game WASM) │   │     │
│  │  │ /assets/  (shared assets)                │   │     │
│  │  └──────────────────────────────────────────┘   │     │
│  └─────────────────────────────────────────────────┘     │
│                                                          │
│  ┌─────────────────────────────────────────────────┐     │
│  │              tmux Sessions                      │     │
│  │  cm-worldA-cp3: claude code editing game code   │     │
│  │  cm-worldB-cp1: claude code editing game code   │     │
│  └─────────────────────────────────────────────────┘     │
│                                                          │
│  ┌─────────────────────────────────────────────────┐     │
│  │           Game Servers (Rust native)            │     │
│  │  world-A server (:9001)                         │     │
│  │  world-B server (:9002)                         │     │
│  └─────────────────────────────────────────────────┘     │
│                                                          │
└──────────────────────────────────────────────────────────┘

┌─────────── Browser ───────────┐
│  ┌──────────────────────────┐ │
│  │   Harness UI Overlay     │ │  ← HTML/CSS, transparent bg
│  │   (DatastarUI + SSE)     │ │
│  ├──────────────────────────┤ │
│  │   Bevy Game Canvas       │ │  ← Full-screen WASM canvas
│  │   (WebSocket → server)   │ │
│  └──────────────────────────┘ │
└───────────────────────────────┘
```

### Directory Layout

```
/Users/coreycole/cdev/creative-mode/
├── README.md
├── Dockerfile                      # Ubuntu 24.04 build/runtime image
├── docker-compose.yml              # Local dev: harness + tmux + game servers
├── .dockerignore
├── harness/                        # Go harness server
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   ├── justfile
│   ├── internal/
│   │   ├── server/                 # HTTP server, Echo routing
│   │   │   └── server.go
│   │   ├── db/                     # SQLite database layer
│   │   │   ├── db.go
│   │   │   ├── migrations/
│   │   │   │   └── 001_initial.sql
│   │   │   └── queries.go
│   │   ├── logging/                # Structured logging (slog → file + stderr)
│   │   │   └── logger.go
│   │   ├── auth/                   # GitHub OAuth + session management
│   │   │   ├── auth.go             # OAuth flow handlers (login, callback, logout)
│   │   │   └── middleware.go       # Session auth middleware
│   │   ├── world/                  # World management (create, fork, checkpoint)
│   │   │   └── manager.go
│   │   ├── build/                  # Build pipeline (Trunk + cargo)
│   │   │   └── builder.go
│   │   ├── tmux/                   # tmux session management
│   │   │   └── session.go
│   │   └── claude/                 # Claude code integration
│   │       └── claude.go
│   ├── views/                      # templ templates
│   │   ├── layout.templ            # Base layout with game canvas + overlay
│   │   ├── lobby.templ             # World browser / lobby
│   │   ├── overlay.templ           # In-game overlay (prompt, status, nav)
│   │   ├── checkpoint_tree.templ   # Checkpoint tree visualization
│   │   └── components/
│   │       └── prompt_input.templ
│   └── static/                     # Static assets (CSS, JS)
│       └── styles.css
├── template/                       # Starter game template
│   ├── Cargo.toml                  # Workspace
│   ├── MEMORY.md                   # Claude code memory file
│   ├── CLAUDE.md                   # Claude code instructions for game dev
│   ├── .claude/
│   │   ├── settings.json           # Claude code hooks config (observability)
│   │   └── hooks/                  # Hook scripts for JSONL logging
│   │       ├── on-tool-use.sh      # Logs pre/post tool use events
│   │       ├── on-stop.sh          # Notifies harness when claude finishes
│   │       └── on-notification.sh  # Logs claude notifications
│   ├── shared/
│   │   ├── Cargo.toml
│   │   └── src/
│   │       ├── lib.rs
│   │       └── protocol.rs         # Lightyear protocol definitions
│   ├── server/
│   │   ├── Cargo.toml
│   │   └── src/
│   │       └── main.rs             # Headless Bevy server
│   ├── client/
│   │   ├── Cargo.toml
│   │   ├── Trunk.toml              # Trunk build configuration
│   │   ├── index.html              # Trunk source HTML (minimal)
│   │   └── src/
│   │       └── main.rs             # Bevy WASM client
│   └── .cargo/
│       └── config.toml             # WASM build settings
├── data/                           # Runtime data (gitignored)
│   ├── worlds/
│   │   ├── {world-id}/
│   │   │   └── {checkpoint-id}/    # Full Rust project copy
│   │   │       ├── Cargo.toml
│   │   │       ├── src/
│   │   │       ├── target/         # Build cache (hardlinked on fork)
│   │   │       └── MEMORY.md
│   │   └── ...
│   ├── wasm-builds/                # Built WASM artifacts (Trunk dist output)
│   │   └── {world-id}/
│   │       └── {checkpoint-id}/
│   │           ├── index.html
│   │           ├── client-{hash}_bg.wasm
│   │           └── client-{hash}.js
│   ├── logs/                       # All JSONL log files (tailable, parseable)
│   │   ├── harness.jsonl           # Harness server events + errors
│   │   └── worlds/
│   │       └── {world-id}/
│   │           └── {checkpoint-id}/
│   │               ├── claude.jsonl      # Claude code tool uses (via hooks)
│   │               ├── build.jsonl       # Trunk/cargo build output
│   │               └── game-server.jsonl # Game server stdout + stderr
│   ├── shared-assets/              # Shared between all worlds
│   └── creative-mode.db            # SQLite database
└── scripts/
    ├── build-game.sh               # Build pipeline script
    └── setup.sh                    # Install dependencies
```

### Database Schema (SQLite)

```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    github_id INTEGER UNIQUE NOT NULL,
    github_username TEXT NOT NULL,
    avatar_url TEXT,
    role TEXT NOT NULL DEFAULT 'pending',  -- 'admin', 'user', 'pending'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
-- First user to sign up is auto-promoted to 'admin'.
-- Subsequent users start as 'pending' until an admin approves them.

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,           -- crypto/rand 32-byte hex token
    user_id TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL  -- 7 days from creation
);

CREATE TABLE worlds (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    created_by TEXT REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    -- NO active_checkpoint_id here. That's per-user state in user_positions.
);

CREATE TABLE checkpoints (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL,
    parent_checkpoint_id TEXT,  -- NULL for root (initial template)
    name TEXT,
    prompt TEXT,                -- The prompt that created this checkpoint
    status TEXT DEFAULT 'building',  -- building, ready, failed
    build_log TEXT,
    work_summary TEXT,         -- Claude's human-readable summary (from CHANGES.txt)
    files_changed TEXT,        -- JSON array of file paths edited by Claude
    build_duration_ms INTEGER, -- Build time in milliseconds
    dir_path TEXT NOT NULL,    -- Absolute path to project directory
    wasm_path TEXT,            -- Path to built WASM artifacts
    server_port INTEGER,       -- Port for this checkpoint's game server
    created_by TEXT REFERENCES users(id),  -- Who submitted the prompt
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (world_id) REFERENCES worlds(id),
    FOREIGN KEY (parent_checkpoint_id) REFERENCES checkpoints(id)
);

-- Tracks where each user currently is (which world + checkpoint they're viewing)
CREATE TABLE user_positions (
    user_id TEXT NOT NULL REFERENCES users(id),
    world_id TEXT NOT NULL REFERENCES worlds(id),
    checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    last_accessed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, world_id)
);

CREATE TABLE prompt_history (
    id TEXT PRIMARY KEY,
    checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    world_id TEXT NOT NULL REFERENCES worlds(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    prompt_text TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Global chat + system notification log (shared across all players)
CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,              -- 'chat', 'build.started', 'build.completed', 'build.failed',
                                    -- 'player.joined', 'player.left'
    user_id TEXT REFERENCES users(id),  -- NULL for system messages
    world_id TEXT REFERENCES worlds(id),  -- context world (NULL for global messages)
    checkpoint_id TEXT REFERENCES checkpoints(id),  -- for build-related messages
    content TEXT NOT NULL,           -- chat text or system message description
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_messages_created_at ON messages(created_at);
```

### Forking Model

When a user submits a prompt:

```
Checkpoint Tree for "My RPG World":

  [cp-001] "starter template"
      │
      ├── [cp-002] "add green rolling hills terrain"  ✓ ready
      │       │
      │       ├── [cp-003] "add a castle on the highest hill"  ✓ ready
      │       │
      │       └── [cp-004] "add a dark forest with fog"  ⏳ building
      │
      └── [cp-005] "make it a flat desert with sand dunes"  ✓ ready
```

Fork process:
1. Identify source checkpoint (the one the user is currently viewing)
2. Create new checkpoint record in SQLite (status: `building`)
3. Copy project directory: `cp -r source_dir/ new_dir/`
4. Preserve build cache via hardlink cloning (see Build Cache Strategy below)
5. Create tmux session `cm-{world_id}-{checkpoint_id}`
6. In the tmux session, run claude code with the user's prompt
7. Claude code reads MEMORY.md for context, edits game code
8. Build pipeline runs (cargo build server + WASM client)
9. Copy WASM artifacts to `data/wasm-builds/{world}/{checkpoint}/`
10. Start game server on an available port
11. Update checkpoint status to `ready`
12. Notify browser clients via SSE → browser reloads WASM

### Logging System (JSONL)

All events are logged as **JSONL** (one JSON object per line) for easy parsing, UI display,
and debugging. Claude code agents can `tail -f` any log and pipe through `jq` for filtering.
The harness reads JSONL log files to display structured events in the build log viewer.

**Log file locations** (`data/logs/`):
```
data/logs/
├── harness.jsonl                        # Harness server: HTTP requests, SSE events,
│                                        #   world/checkpoint operations, errors
└── worlds/
    └── {worldID}/
        └── {checkpointID}/
            ├── claude.jsonl             # Claude code tool uses, edits, commands (via hooks)
            ├── build.jsonl              # Trunk + cargo build output (line-wrapped as JSONL)
            └── game-server.jsonl        # Game server structured log output
```

**JSONL event format** (consistent across all log files):
```jsonl
{"ts":"2026-02-10T12:00:00Z","level":"info","event":"checkpoint.forked","worldID":"abc","cpID":"cp2","parentCP":"cp1","prompt":"add hills"}
{"ts":"2026-02-10T12:00:05Z","level":"info","event":"claude.tool_use","worldID":"abc","cpID":"cp2","tool":"Edit","file":"client/src/main.rs","status":"success"}
{"ts":"2026-02-10T12:00:10Z","level":"info","event":"build.started","worldID":"abc","cpID":"cp2"}
{"ts":"2026-02-10T12:00:15Z","level":"error","event":"build.output","worldID":"abc","cpID":"cp2","line":"error[E0308]: mismatched types"}
{"ts":"2026-02-10T12:01:00Z","level":"info","event":"build.completed","worldID":"abc","cpID":"cp2","duration_ms":55000}
{"ts":"2026-02-10T12:01:01Z","level":"info","event":"game_server.started","worldID":"abc","cpID":"cp2","port":9001}
{"ts":"2026-02-10T12:05:00Z","level":"error","event":"game_server.crash","worldID":"abc","cpID":"cp2","error":"panicked at src/main.rs:42"}
```

**Harness server logging** (`harness/internal/logging/logger.go`):

The harness uses Go's `slog` with `slog.JSONHandler` to produce JSONL output. Writes to
both stderr (for the terminal) and `data/logs/harness.jsonl`:

```go
func NewLogger(logDir string) *slog.Logger {
    logFile, _ := os.OpenFile(
        filepath.Join(logDir, "harness.jsonl"),
        os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644,
    )
    multiWriter := io.MultiWriter(os.Stderr, logFile)
    return slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    }))
}
```

**Per-checkpoint log capture**:

Build and game server output is wrapped as JSONL before writing:

```go
// Build process - wrap each output line as a JSONL event
func (b *Builder) Build(cp *Checkpoint) error {
    buildLog, _ := os.Create(cp.BuildLogPath()) // .jsonl
    writer := &jsonlLineWriter{
        file:    buildLog,
        worldID: cp.WorldID,
        cpID:    cp.ID,
        event:   "build.output",
    }
    cmd := exec.Command("trunk", "build", "--release", "--dist", wasmDir)
    cmd.Stdout = writer
    cmd.Stderr = writer
    // ...
}

// jsonlLineWriter wraps each line of output as a JSONL event
type jsonlLineWriter struct {
    file    *os.File
    worldID string
    cpID    string
    event   string
}

func (w *jsonlLineWriter) Write(p []byte) (n int, err error) {
    lines := strings.Split(string(p), "\n")
    for _, line := range lines {
        if line == "" { continue }
        entry, _ := json.Marshal(map[string]any{
            "ts":      time.Now().UTC().Format(time.RFC3339),
            "level":   "info",
            "event":   w.event,
            "worldID": w.worldID,
            "cpID":    w.cpID,
            "line":    line,
        })
        w.file.Write(append(entry, '\n'))
    }
    return len(p), nil
}

// Game server - same pattern, event type "game_server.output"
func startGameServer(cp *Checkpoint) (*exec.Cmd, error) {
    serverLog, _ := os.Create(cp.GameServerLogPath()) // .jsonl
    writer := &jsonlLineWriter{
        file: serverLog, worldID: cp.WorldID, cpID: cp.ID, event: "game_server.output",
    }
    cmd := exec.Command(cp.ServerBinaryPath())
    cmd.Stdout = writer
    cmd.Stderr = writer
    // ...
}
```

### Claude Code Hooks (Observability)

Instead of polling tmux to monitor claude code sessions, we use **Claude Code hooks** to
get structured, event-driven observability. Hooks are shell commands that Claude Code runs
at specific lifecycle points, receiving event data on stdin as JSON.

Each game world template includes a `.claude/settings.json` with hooks configured, plus
hook scripts that write JSONL events and POST to the harness API.

**Template hook configuration** (`template/.claude/settings.json`):
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

**Hook script: tool use logging** (`template/.claude/hooks/on-tool-use.sh`):
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

**Hook script: session stop** (`template/.claude/hooks/on-stop.sh`):
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

**Hook script: notifications** (`template/.claude/hooks/on-notification.sh`):
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

**Harness receives hook events** (new API endpoint):

The harness exposes a POST endpoint that hook scripts fire-and-forget to. This replaces
the tmux polling approach with event-driven updates:

```go
// POST /api/claude-event - receives JSONL events from Claude Code hooks
func (s *Server) handleClaudeEvent(c echo.Context) error {
    var event map[string]any
    if err := c.Bind(&event); err != nil {
        return c.NoContent(400)
    }

    worldID, _ := event["worldID"].(string)
    eventType, _ := event["event"].(string)

    s.logger.Info("claude hook event",
        "worldID", worldID,
        "event", eventType,
    )

    // Publish to SSE subscribers for this world
    s.worldManager.Publish(worldID, event)

    // If claude stopped, trigger the build pipeline
    if eventType == "claude.session_stopped" {
        cpID, _ := event["cpID"].(string)
        go s.buildManager.BuildCheckpoint(worldID, cpID)
    }

    return c.NoContent(200)
}
```

**Environment variables set by harness** when launching a claude code tmux session:
```go
func (s *Session) Create(name, workDir, worldID, cpID string) error {
    logDir := filepath.Join(s.logsDir, "worlds", worldID, cpID)
    os.MkdirAll(logDir, 0755)

    env := fmt.Sprintf(
        "CM_WORLD_ID=%s CM_CHECKPOINT_ID=%s CM_HARNESS_URL=http://localhost:8080 CM_LOG_DIR=%s",
        worldID, cpID, logDir,
    )
    return exec.Command("tmux", "new-session", "-d",
        "-s", name, "-c", workDir,
        "-e", fmt.Sprintf("CM_WORLD_ID=%s", worldID),
        "-e", fmt.Sprintf("CM_CHECKPOINT_ID=%s", cpID),
        "-e", fmt.Sprintf("CM_HARNESS_URL=http://localhost:8080"),
        "-e", fmt.Sprintf("CM_LOG_DIR=%s", logDir),
    ).Run()
}
```

**What the harness UI sees** (via SSE, powered by hook events):

As claude code works, the UI gets a live stream of structured events:
```
[12:00:05] claude.tool_use.pre  → Edit  client/src/main.rs
[12:00:06] claude.tool_use.post → Edit  client/src/main.rs  ✓
[12:00:07] claude.tool_use.pre  → Edit  shared/src/protocol.rs
[12:00:08] claude.tool_use.post → Edit  shared/src/protocol.rs  ✓
[12:00:09] claude.tool_use.pre  → Bash  cargo check
[12:00:15] claude.tool_use.post → Bash  cargo check  ✓
[12:00:16] claude.session_stopped
[12:00:17] build.started
[12:00:45] build.completed (28s)
```

This gives users full visibility into what claude is doing without needing to SSH in. The
harness UI can show a real-time activity feed, file edit counts, and estimated progress.

**Claude code can still tail JSONL logs for debugging**:
```bash
# Pretty-print game server logs
tail -f data/logs/worlds/{worldID}/{cpID}/game-server.jsonl | jq .

# Filter for errors only
tail -f data/logs/worlds/{worldID}/{cpID}/build.jsonl | jq 'select(.level == "error")'

# Watch all events for a checkpoint
tail -f data/logs/worlds/{worldID}/{cpID}/claude.jsonl | jq .
```

**Log rotation**: For the hackathon, logs are unbounded. Each checkpoint gets fresh JSONL
files. Old checkpoint logs can be cleaned up with the checkpoint itself.

### Build Cache Strategy

Rust/Bevy incremental compilation is fast when the build cache exists. Key strategies:

- **Hardlink target/ on fork**: The `target/` directory can be 1-2GB. On Linux (Ubuntu 24.04), `cp -al source/target/ dest/target/` creates hardlinks — instant, shares disk space. When cargo builds, it overwrites only the changed files.
- **Shared cargo registry**: Set `CARGO_HOME` to a shared location so crate downloads aren't duplicated.
- **Consider sccache**: For additional cross-project caching if disk space becomes an issue.
- **Pre-build template deps**: The starter template should have its dependencies pre-compiled so the first checkpoint builds fast.

### Shared Assets

Assets (textures, models, sounds) live in `data/shared-assets/` and are served by the harness at `/assets/*`. Game code references assets via HTTP URLs rather than bundled files. Bevy 0.18 supports HTTP asset loading on WASM via the fetch API.

```rust
// In game client code, assets load from the harness server
asset_server.load("http://localhost:8080/assets/textures/grass.png")
```

### Build Pipeline: Trunk

Each checkpoint's client crate is built with [Trunk](https://trunkrs.dev/), the Rust WASM
bundler. Trunk handles the full pipeline: `cargo build` → `wasm-bindgen` → `wasm-opt` →
asset bundling → `index.html` generation. The output is a self-contained `dist/` folder
ready to serve as static files.

**Build command** (run from the checkpoint's client crate directory):
```bash
cd {checkpoint_dir}/client
trunk build --release --dist {wasm_builds_dir}/{worldID}/{cpID}/
```

**Trunk produces**:
```
data/wasm-builds/{worldID}/{cpID}/
├── index.html                          # Generated HTML with script tags
├── client-{hash}_bg.wasm               # Compiled WASM binary
├── client-{hash}.js                    # JS loader/glue code
└── snippets/                           # wasm-bindgen JS snippets (if any)
```

**Template client `index.html`** (Trunk source, lives in `template/client/index.html`):
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

Note: Shared assets are NOT bundled by Trunk. They live in `data/shared-assets/` and are
served by the harness at `/assets/*`. The Bevy client loads them via HTTP at runtime. This
avoids duplicating assets across every checkpoint build.

**Template client `Trunk.toml`** (lives in `template/client/Trunk.toml`):
```toml
[build]
target = "index.html"
filehash = true
minify = "on_release"

[tools]
wasm_bindgen = "0.2.108"
```

> **Version pinning**: The wasm-bindgen CLI version in `Trunk.toml` must **exactly match** the
> `wasm-bindgen` crate version in `Cargo.lock`. A mismatch causes build failures. Pin the crate
> version in the workspace `Cargo.toml`:
> ```toml
> [workspace.dependencies]
> wasm-bindgen = "=0.2.108"
> ```

### Serving Model: iframe Per World

The harness page uses an **iframe** to load the Trunk-built game. This gives us clean
isolation between builds - switching worlds or checkpoints just changes the iframe `src`.

```html
<!-- Harness page served at /world/:worldID -->
<body>
  <!-- Game iframe - full screen, behind the overlay -->
  <iframe id="game-frame"
          src="/wasm/{worldID}/{cpID}/index.html"
          style="position:fixed; inset:0; z-index:0; width:100%; height:100%; border:none;">
  </iframe>

  <!-- Harness overlay sits on top with transparent background -->
  <div id="harness-overlay"
       data-signals='{"overlayExpanded":true,"unreadCount":0,"buildStatus":"idle",
                      "currentWorldId":"...","currentCheckpointId":"...","chatText":""}'
       data-on:load="@get('/world/{worldID}/events')">

    <!-- === EXPANDED STATE === -->
    <div data-show="$overlayExpanded" class="overlay-expanded">

      <!-- Top bar (spans both columns) -->
      <header class="overlay-bar" style="grid-column: 1 / -1;">
        [World ▼] [Checkpoint ▼] [Save] [Tree] [← Lobby]
        <button data-on:click="$overlayExpanded = false">−</button>
      </header>

      <!-- Game area (left column) - transparent, clicks pass through -->
      <div class="game-area"></div>

      <!-- Chat/notification panel (right column) with scope tabs -->
      <div class="chat-panel">
        <!-- Tab selector -->
        <div class="tab-bar">
          <button data-on:click="$activeTab = 'global'"
                  data-class="{'tab-active': $activeTab === 'global'}">Global</button>
          <button data-on:click="$activeTab = 'world'"
                  data-class="{'tab-active': $activeTab === 'world'}">World</button>
          <button data-on:click="$activeTab = 'lineage'; loadLineage()"
                  data-class="{'tab-active': $activeTab === 'lineage'}">Lineage</button>
        </div>

        <!-- Global tab: all messages across all worlds -->
        <div data-show="$activeTab === 'global'" id="message-log-global" class="message-log">
          <!-- SSE PatchElements appends messages here -->
        </div>

        <!-- World tab: messages filtered to current world -->
        <div data-show="$activeTab === 'world'" id="message-log-world" class="message-log">
          <!-- SSE PatchElements appends world-scoped messages here -->
        </div>

        <!-- Lineage tab: prompt/response chain from root → current checkpoint -->
        <div data-show="$activeTab === 'lineage'" id="lineage-view" class="message-log">
          <!-- Populated via GET /world/{worldID}/lineage/{cpID} when tab is selected -->
        </div>

        <!-- Chat input (hidden on Lineage tab since it's read-only) -->
        <div data-show="$activeTab !== 'lineage'" class="chat-input-bar">
          <input data-bind="chatText" placeholder="Type a message..." />
          <button data-on:click="@post('/api/chat', {content: $chatText}); $chatText = ''">
            Send
          </button>
        </div>
      </div>

      <!-- Bottom bar (left column only) -->
      <footer class="overlay-bar">
        <input data-bind="promptText" placeholder="Describe what to build..." />
        <button data-on:click="@post('/world/{worldID}/prompt', {prompt: $promptText})">
          Build
        </button>
        <span class="status">[Build Status] [Players: 2] [60fps]</span>
      </footer>
    </div>

    <!-- === MINIMIZED STATE === -->
    <div data-show="!$overlayExpanded" class="overlay-minimized">
      <button data-on:click="$overlayExpanded = true; $unreadCount = 0">
        ⬛
        <span data-show="$unreadCount > 0" class="badge"
              data-text="$unreadCount"></span>
      </button>
    </div>
  </div>
</body>
```

**Why iframe?**
- Trunk produces a complete `index.html` per build - iframe serves it directly
- Switching is just `iframe.src = '/wasm/{worldID}/{cpID}/index.html'`
- WASM memory is fully freed when iframe unloads (no Bevy shutdown needed)
- Game input (mouse/keyboard) naturally scopes to the focused iframe
- Each build is completely isolated - no module cache conflicts

**Switching mechanism** (notification-driven):
1. Claude finishes editing → `trunk build --release` runs
2. Build completes → harness creates a `build.completed` message in the `messages` table
3. Harness publishes `build.completed` to the **global** event bus → all connected browsers receive it
4. The chat/notification log shows: "My RPG checkpoint ready: 'add a river' [▶ Play]"
5. User clicks `[▶ Play]` → JS calls `loadCheckpoint(worldID, cpID, serverPort)`
6. `loadCheckpoint()` sets `iframe.src` to the new checkpoint's WASM build
7. Game loads fresh in the iframe, connects to the game server
8. User is now in the new build
9. If the user is in a **different world**, clicking `[▶ Play]` navigates to
   `/world/{worldID}?checkpoint={cpID}` (full page load with new SSE connection)

**Key UX principle**: Builds never auto-switch. The game continues running uninterrupted.
Users see build completions in the chat log and choose when to switch. This means:
- Multiple builds can complete while a user is playing — they all appear in the log
- Users can click any completed build to hop to it, even builds from other worlds
- The chat log is the single place to discover what's new across all worlds

For switching **worlds** via the top bar selector:
1. User opens world selector dropdown
2. Selects a different world → harness navigates to `/world/{newWorldID}`
3. Full page load: new iframe src, new SSE connection, new overlay state

The overlay uses `pointer-events: none` on the container so mouse events pass through to
the game iframe. Interactive elements (buttons, inputs) have `pointer-events: auto`.

### Chat/Notification Panel (Three Scopes)

The chat panel has **three tabs** at the top, each showing a different scope of information.
The game continues running in the background at all times regardless of which tab is active.

#### Tab 1: Global

All players share a single feed that combines system events and player chat across all
worlds. This is the social/discovery view — "what is everyone doing?"

**Message types** (stored in `messages` table, pushed via SSE):
- `chat` — Player text messages ("hey check out the castle I made")
- `build.started` — "Alice started building in My RPG → 'add a river'"
- `build.completed` — "My RPG checkpoint ready: 'add a river'" [▶ Play]
- `build.failed` — "Build failed in My RPG: 'add a river'"
- `player.joined` — "Bob joined My RPG"
- `player.left` — "Bob left My RPG"

**Build completion entries are clickable** — clicking `[▶ Play]` navigates to that
world/checkpoint by changing the iframe src (same world) or full page navigation (cross
world). This is the primary way users discover and hop into each other's creations.

#### Tab 2: World

Same message types as Global, but **filtered to the current world only**. Use this when
you want to focus on activity in your world without noise from other worlds. Shows builds,
joins/leaves, and chat messages scoped to this world.

The filter is applied client-side (each message carries `worldID`) or server-side via a
signal: `data-show="$activeTab !== 'world' || msg.worldID === $currentWorldId"`.

#### Tab 3: Lineage

The **prompt/response decision history** along the path from root → current checkpoint.
This is NOT a live chat feed — it's a structured read-only view of how the current state
was built, step by step. Ideal context when crafting your next prompt.

Each entry in the lineage is a checkpoint in the ancestry:
```
[cp-001] Starter template
  ↳ Created automatically

[cp-002] "add green rolling hills terrain" — Alice, 5m ago
  ↳ Claude: Added Perlin noise terrain generation with green
    grass material and rolling hill geometry. Configurable
    amplitude and frequency for natural-looking hills.
    Files: client/src/main.rs, shared/src/lib.rs (+2)
    Build: ✓ ready (28s)

[cp-003] "add a castle on the highest hill" — Alice, 2m ago
  ↳ Claude: Built stone castle structure at terrain peak.
    Added battlements, main tower, and entrance gate using
    primitive meshes with gray stone material.
    Files: client/src/main.rs
    Build: ✓ ready (15s)

 ← you are here, type your next prompt below
```

**No chat messages in lineage** — this is strictly the decision history: what was asked,
what Claude did. Clean and focused context for the next prompt.

**Data source**: Walk `checkpoints.parent_checkpoint_id` from current → root. For each
checkpoint, show: prompt, submitter (from `prompt_history`), work summary (from
`checkpoints.work_summary`), files changed (auto-generated), build status + duration.

#### Checkpoint Work Summaries

Each checkpoint stores a **work summary** — a human-readable description of what Claude
did, plus structured metadata. This powers the Lineage tab.

**Two sources combined:**

1. **Claude-generated summary** (primary text): The `template/CLAUDE.md` instructs Claude
   to write a brief summary of changes to `CHANGES.txt` in the checkpoint directory before
   finishing. The harness reads this file after the `claude.session_stopped` event.
   Example: "Added Perlin noise terrain generation with green grass material and rolling
   hill geometry. Configurable amplitude and frequency for natural-looking hills."

2. **Auto-generated metadata** (secondary info): The harness parses `claude.jsonl` after
   the session stops to extract:
   - Files edited (from `Edit`/`Write` tool use events)
   - Commands run (from `Bash` tool uses, first 100 chars each)
   - Build result and duration
   Example: "Files: client/src/main.rs, shared/src/lib.rs, shared/src/protocol.rs.
   Build: ✓ ready (28s)"

**Storage**: Both are stored on the checkpoint record:
```sql
-- Added to checkpoints table:
work_summary TEXT,          -- Claude's CHANGES.txt content (human-readable)
files_changed TEXT,         -- JSON array of file paths edited by Claude
```

The harness populates these after the build completes:
```go
func (b *Builder) PostBuild(cp *Checkpoint) {
    // Read Claude's summary
    changesPath := filepath.Join(cp.DirPath, "CHANGES.txt")
    if summary, err := os.ReadFile(changesPath); err == nil {
        cp.WorkSummary = string(summary)
    }

    // Auto-generate file list from claude.jsonl
    cp.FilesChanged = parseEditedFiles(cp.ClaudeLogPath())

    b.db.UpdateCheckpointSummary(cp)
}
```

#### Overlay States

- **Expanded** (default): Full overlay with top bar, tabbed chat/notification panel,
  prompt bar, and status bar. The game canvas is fully visible and interactive behind
  transparent areas. The active tab (Global/World/Lineage) persists across sessions.
- **Minimized**: All overlay chrome is hidden. A single floating button in the corner
  shows an **unread badge count** for new chat messages and system notifications received
  while minimized. Click the button to expand.

#### SSE Delivery

The world SSE handler (`GET /world/:worldID/events`) subscribes to both the **global
event bus** (chat, system notifications for all worlds) and the **world event bus** (build
progress, checkpoint tree updates for the current world). This means:
- **Global tab**: receives all events from the global bus
- **World tab**: receives events from the global bus, filtered to current world
- **Lineage tab**: populated on tab switch by querying the checkpoint ancestry (not SSE-driven, since it's a static view of the tree path)

**Chat endpoint**: `POST /api/chat` accepts `{ "content": "..." }` from authenticated
users. The harness creates a `messages` row and publishes to the global event bus.

**Initial load**: When the SSE connection opens, the handler sends the last 50 messages
as an initial batch so the Global/World tabs aren't empty on page load. The Lineage tab
is populated by a separate `GET /world/:worldID/lineage/:cpID` endpoint that returns the
checkpoint ancestry with summaries.

---

### Environment Variables

Required env vars for the harness server:

```
GITHUB_CLIENT_ID=...               # GitHub OAuth App client ID
GITHUB_CLIENT_SECRET=...           # GitHub OAuth App client secret
ANTHROPIC_API_KEY=...              # Claude API key (used by Claude Code in tmux sessions)
HARNESS_URL=http://localhost:8080   # Base URL for OAuth redirect URI
```

These are read in `main.go` and passed to the auth config. For local development, create a
GitHub OAuth App at https://github.com/settings/developers with:
- Homepage URL: `http://localhost:8080`
- Authorization callback URL: `http://localhost:8080/auth/github/callback`

### Claude Code Auth (Server-Level)

Claude Code is authenticated once at the server level (e.g., `claude auth login` on the
machine during setup). All tmux sessions share this auth. The hook scripts POST to
`/api/claude-event` on localhost without any additional auth - they're internal
same-machine communication.

The `/api/claude-event` endpoint is unprotected but only accessible from the local machine.
Hook scripts just POST directly:
```bash
curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  -d "$JSONL" &>/dev/null &
```

---

## Implementation Phases

## Phase 1: Project Scaffolding

### Overview
Set up the monorepo structure, Go harness server skeleton, and basic tooling.

### Changes Required:

#### 1. Root project files
**Files**: `justfile`, `.gitignore`, `scripts/setup.sh`

The root justfile orchestrates both the harness and template builds:
```just
default:
    @just --list

harness:
    cd harness && just dev

setup:
    ./scripts/setup.sh
```

#### 2. Harness Go project
**Files**: `harness/go.mod`, `harness/main.go`, `harness/justfile`

Initialize Go module, install dependencies:
- `github.com/labstack/echo/v4` (HTTP framework / routing)
- `github.com/starfederation/datastar-go/datastar` (SSE/signals for live UI updates)
- `github.com/coreycole/datastarui` (component library)
- `github.com/a-h/templ` (HTML templating)
- `github.com/mattn/go-sqlite3` (SQLite driver)
- `github.com/google/uuid` (ID generation)
- `golang.org/x/oauth2` (GitHub OAuth flow)

`harness/main.go`:
```go
package main

import (
    "log"
    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
    "github.com/your/harness/internal/server"
    "github.com/your/harness/internal/db"
)

func main() {
    database, err := db.New("../data/creative-mode.db")
    if err != nil {
        log.Fatal(err)
    }
    defer database.Close()

    e := echo.New()
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    authCfg := &auth.Config{
        GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
        GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
        BaseURL:            os.Getenv("HARNESS_URL"),
    }

    server.RegisterRoutes(e, database, authCfg)

    // Graceful shutdown: stop game servers, kill tmux sessions, mark in-progress builds as interrupted
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    go func() {
        <-ctx.Done()
        log.Println("Shutting down...")
        server.Shutdown() // kills tmux sessions, stops game servers, closes SSE connections
        e.Shutdown(context.Background())
    }()

    log.Println("Harness server starting on :8080")
    e.Logger.Fatal(e.Start(":8080"))
}
```

#### 3. SQLite database layer
**Files**: `harness/internal/db/db.go`, `harness/internal/db/migrations/001_initial.sql`

Create database, run migrations, provide query methods for worlds, checkpoints, users,
sessions, and user positions.

**Database initialization** must enable WAL mode and set busy timeout to handle concurrent
access from multiple goroutines:
```go
func New(dbPath string) (*DB, error) {
    db, err := sql.Open("sqlite3", dbPath)
    if err != nil { return nil, err }
    db.Exec("PRAGMA journal_mode=WAL")
    db.Exec("PRAGMA busy_timeout=5000")
    // run migrations...
    return &DB{db: db}, nil
}
```

#### 4. GitHub OAuth + session auth
**Files**: `harness/internal/auth/auth.go`, `harness/internal/auth/middleware.go`

GitHub OAuth flow:
1. `GET /auth/github/login` → redirect to GitHub OAuth authorize URL with:
   - `client_id` from env `GITHUB_CLIENT_ID`
   - `redirect_uri` = `{HARNESS_URL}/auth/github/callback`
   - `scope` = `read:user` (minimal - just need username + avatar)
   - `state` = cryptographically random token stored in a short-lived cookie (CSRF protection)
2. `GET /auth/github/callback` → GitHub redirects back with `code` + `state`:
   - Validate `state` matches the cookie (CSRF check)
   - Exchange `code` for access token via `POST https://github.com/login/oauth/access_token`
   - Fetch user info via `GET https://api.github.com/user` with the access token
   - Upsert user in `users` table (github_id, github_username, avatar_url)
   - Create session in `sessions` table (crypto/rand 32-byte hex token, 7-day expiry)
   - Set secure session cookie: `HttpOnly`, `SameSite=Lax`, `Secure` (when not localhost)
   - Redirect to `/`
3. `POST /auth/logout` → delete session from DB, clear cookie

Session middleware (`auth.SessionMiddleware`):
- Reads `session` cookie from request
- Looks up session in DB, checks expiry
- If valid: sets user in Echo context (`c.Set("user", user)`)
- If invalid/missing: redirects to `/auth/github/login`

**Access control (admin approval model)**:
- On OAuth callback, upsert user with `role = 'pending'` (or `'admin'` if first user)
- `auth.SessionMiddleware` checks `user.role`:
  - `'admin'` or `'user'`: full access to all routes
  - `'pending'`: redirected to a "waiting for approval" page (`/auth/pending`)
- Admin panel (`/admin/users`): lists pending users, approve/reject buttons
- Endpoints:
  - `POST /admin/users/:userID/approve` — sets role to `'user'`
  - `POST /admin/users/:userID/reject` — deletes user record
  - `GET /admin/users` — admin-only page listing all users + pending requests
- Admin middleware: separate middleware that checks `role == 'admin'` for `/admin/*` routes

Security measures:
- Session tokens: 32 bytes from `crypto/rand`, stored as hex (64 chars)
- Cookie flags: `HttpOnly` (no JS access), `SameSite=Lax` (CSRF), `Secure` (HTTPS only, disabled for localhost dev)
- OAuth `state` parameter: random token in short-lived cookie, validated on callback (prevents CSRF on OAuth flow)
- Don't store GitHub access tokens - use them once to fetch user info, then discard
- Session expiry: 7 days, checked on every request
- Expired sessions cleaned up periodically

#### 5. HTTP server with Echo routing
**File**: `harness/internal/server/server.go`

Echo route registration with auth middleware and grouped routes:
```go
func RegisterRoutes(e *echo.Echo, database *db.DB, authCfg *auth.Config) {
    s := &Server{db: database}
    authHandler := auth.NewHandler(database, authCfg)

    // Static files (public, no auth)
    e.Static("/assets", "../data/shared-assets")
    e.Static("/static", "static")

    // Auth routes (no auth middleware)
    e.GET("/auth/github/login", authHandler.HandleLogin)
    e.GET("/auth/github/callback", authHandler.HandleCallback)
    e.POST("/auth/logout", authHandler.HandleLogout)

    // Claude Code hook events (internal, same machine - no auth needed)
    e.POST("/api/claude-event", s.handleClaudeEvent)

    // Auth-required routes (pending users see /auth/pending, approved users get full access)
    authed := e.Group("", auth.SessionMiddleware(database))
    authed.GET("/auth/pending", s.handlePendingApproval)

    // Approved users only (role = 'admin' or 'user')
    approved := e.Group("", auth.SessionMiddleware(database), auth.ApprovedMiddleware())

    // Admin routes
    admin := e.Group("/admin", auth.SessionMiddleware(database), auth.AdminMiddleware())
    admin.GET("/users", s.handleAdminUsers)
    admin.POST("/users/:userID/approve", s.handleApproveUser)
    admin.POST("/users/:userID/reject", s.handleRejectUser)

    approved.GET("/", s.handleLobby)
    approved.GET("/events", s.handleGlobalSSE)          // Global SSE (lobby: chat + notifications)
    approved.POST("/api/chat", s.handleChatMessage)     // Send chat message

    w := approved.Group("/world")
    w.POST("/create", s.handleCreateWorld)
    w.GET("/:worldID", s.handleWorldView)
    w.GET("/:worldID/checkpoint/:cpID", s.handleCheckpointView)
    w.POST("/:worldID/prompt", s.handlePrompt)
    w.POST("/:worldID/checkpoint", s.handleSaveCheckpoint)
    w.GET("/:worldID/events", s.handleSSEEvents)        // World SSE (global + world-specific events)
    w.GET("/:worldID/lineage/:cpID", s.handleLineage)   // Checkpoint ancestry with summaries (Lineage tab)
    w.GET("/:worldID/checkpoint/:cpID/logs/:logType", s.handleLogStream)

    // WASM artifacts (behind auth so only approved users can play)
    approved.GET("/wasm/:worldID/:cpID/*", s.handleWASMArtifacts)
}
```

Routes:
```
# Public (no auth)
GET  /auth/github/login                             → redirect to GitHub OAuth
GET  /auth/github/callback                          → OAuth callback, creates session
POST /auth/logout                                   → destroy session, clear cookie
POST /api/claude-event                              → receive hook events from claude code sessions (internal)
GET  /assets/*                                      → serve shared assets
GET  /static/*                                      → serve static CSS/JS

# Authenticated but pending approval
GET  /auth/pending                                  → "waiting for admin approval" page

# Admin only (role = 'admin')
GET  /admin/users                                   → user management (approve/reject)
POST /admin/users/:userID/approve                   → approve pending user
POST /admin/users/:userID/reject                    → reject pending user

# Approved users (role = 'admin' or 'user')
GET  /                                              → lobby (world browser)
GET  /events                                        → global SSE stream (chat + notifications, used in lobby)
POST /api/chat                                      → send chat message (global, shared with all players)
GET  /world/:worldID                                → game view (canvas + overlay)
GET  /world/:worldID/checkpoint/:cpID               → game view at specific checkpoint
POST /world/:worldID/prompt                         → submit prompt (forks + builds)
POST /world/:worldID/checkpoint                     → save checkpoint
POST /world/create                                  → create new world
GET  /world/:worldID/events                         → SSE stream (global events + world-specific build events)
GET  /world/:worldID/lineage/:cpID                  → checkpoint ancestry with work summaries (Lineage tab)
GET  /world/:worldID/checkpoint/:cpID/logs/:logType → stream JSONL logs (claude|build|game-server)
GET  /wasm/:worldID/:cpID/*                         → serve WASM build artifacts (Trunk dist)
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd harness && go build ./...` compiles successfully
- [ ] `cd harness && go test ./...` passes
- [ ] Echo server starts and responds on `:8080`
- [ ] Echo route table matches expected routes (auth + authed groups)
- [ ] SQLite database creates tables on first run (including users, sessions, user_positions)

#### Manual Verification:
- [ ] `curl localhost:8080/` redirects to `/auth/github/login` (no session)
- [ ] GitHub OAuth login flow completes and redirects back with session cookie
- [ ] First user auto-promoted to `admin` role
- [ ] Second user starts as `pending`, sees "waiting for approval" page
- [ ] Admin can approve pending users at `/admin/users`
- [ ] Approved user can access lobby and worlds
- [ ] Session cookie is `HttpOnly`, `SameSite=Lax`
- [ ] Echo logger middleware shows request logs
- [ ] SQLite file created at `data/creative-mode.db` with correct schema
- [ ] Logout clears session from DB and cookie

---

## Phase 2: Starter Game Template

### Overview
Create a minimal Bevy + Lightyear multiplayer game that compiles to WASM. Players can fly around an empty world and see each other as pill meshes.

### Changes Required:

#### 1. Cargo workspace
**File**: `template/Cargo.toml`

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

> **Version note**: Lightyear 0.26 targets Bevy 0.18 (released Jan 2026). Lightyear now
> uses [aeronet](https://github.com/aecsocket/aeronet) as its transport layer, which provides
> WebSocket and WebTransport implementations. The protocol registration, transport configuration,
> and plugin setup differ from older Lightyear versions - always reference the
> [Lightyear examples](https://github.com/cBournhonesque/lightyear/tree/main/examples) for
> current API patterns.

#### 2. Shared protocol
**Files**: `template/shared/Cargo.toml`, `template/shared/src/lib.rs`, `template/shared/src/protocol.rs`

Defines the Lightyear protocol shared between server and client:
- `PlayerPosition` component (replicated)
- `PlayerInput` (movement + look direction)
- `PlayerBundle` (mesh, transform, etc.)
- Channel configuration (unreliable for position, reliable for spawning)

> **Important**: Lightyear 0.26 uses the aeronet transport layer. The protocol registration
> and plugin setup APIs have changed from older versions. The code below is illustrative -
> reference the Lightyear `simple_box` example for the exact current API.

```rust
// template/shared/src/protocol.rs
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
// See: https://github.com/cBournhonesque/lightyear/tree/main/examples
```

#### 3. Server binary
**File**: `template/server/src/main.rs`

Headless Bevy app with Lightyear server plugin (aeronet transport layer):
- Listens on configurable port (from env var `GAME_PORT`)
- **Transport**: WebSocket server via aeronet — no TLS certificates needed for localhost
- Spawns player entity when client connects
- Replicates `PlayerPosition` to all clients
- Applies `PlayerInput` to update positions
- Fly movement: applies input as velocity in camera-relative direction
- No gravity, free 3D movement

#### 4. Client binary
**File**: `template/client/src/main.rs`

Bevy app with Lightyear client plugin (aeronet transport layer):
- Targets `#bevy-canvas` HTML element
- **Transport**: WebSocket via aeronet — works on WASM without TLS certificate setup
- Server address from query params or hardcoded for dev
- Fly camera: WASD + mouse look, shift for speed boost
- Renders other players as pill meshes (capsule)
- Ground plane with grid texture (simple procedural)
- Basic lighting (directional + ambient)
- Transparent clear color so the HTML background shows through edges (optional)

> **Why WebSocket instead of WebTransport?** WebTransport in browsers requires TLS
> certificates with a maximum 14-day validity period (per the WebTransport spec). Self-signed
> certs must be X.509v3 with ECDSA P-256 keys, and the certificate digest must be provided to
> the browser before connection. This adds significant operational complexity (certificate
> generation, rotation, distribution) that is not worth the latency improvement for a
> hackathon/prototype. WebSocket works on both native and WASM without any certificate setup.
> WebTransport can be added later for production deployment when proper TLS infrastructure exists.

#### 5. WASM build configuration
**File**: `template/.cargo/config.toml`

```toml
[target.wasm32-unknown-unknown]
rustflags = ['--cfg', 'getrandom_backend="wasm_js"']
```

#### 6. CLAUDE.md for game worlds
**File**: `template/CLAUDE.md`

Instructions for claude code when editing game worlds:
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
  - Trunk handles cargo build → wasm-bindgen → wasm-opt → index.html
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

#### 7. MEMORY.md template
**File**: `template/MEMORY.md`

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

### Success Criteria:

#### Automated Verification:
- [ ] `cd template && cargo build -p server` compiles
- [ ] `cd template/client && trunk build --release` compiles and produces `dist/`
- [ ] `dist/` contains `index.html`, `.wasm`, and `.js` files
- [ ] Server binary starts and listens on specified port

#### Manual Verification:
- [ ] Open WASM build in browser → see ground plane and sky
- [ ] WASD + mouse to fly around the world
- [ ] Two browser tabs connect → see each other as pill meshes
- [ ] Movement is smooth with prediction/interpolation

---

## Phase 3: World Management

### Overview
Implement world creation, checkpoint forking, and build pipeline in the harness server.

### Changes Required:

#### 1. World manager
**File**: `harness/internal/world/manager.go`

Core operations:
- `CreateWorld(name, description, userID) → World` - copies template dir, runs initial build, creates DB records
- `ForkCheckpoint(worldID, sourceCheckpointID, prompt, userID) → Checkpoint` - copies project dir with hardlinked target/, creates new checkpoint, records user in prompt_history
- `GetCheckpointTree(worldID) → []Checkpoint` - returns checkpoint tree for a world
- `GetUserPosition(userID, worldID) → checkpointID` - reads user's current checkpoint for a world from `user_positions`
- `SetUserPosition(userID, worldID, checkpointID)` - updates `user_positions` table

Fork implementation:
```go
func (m *Manager) ForkCheckpoint(worldID, sourceCPID, prompt string) (*Checkpoint, error) {
    newID := uuid.New().String()[:8]
    sourceDir := m.checkpointDir(worldID, sourceCPID)
    newDir := m.checkpointDir(worldID, newID)

    // Copy source files (excluding target/)
    exec.Command("rsync", "-a", "--exclude=target", sourceDir+"/", newDir+"/").Run()

    // Clone build cache using platform-appropriate method
    m.cloneBuildCache(sourceDir, newDir)

    // Create checkpoint record
    cp := &Checkpoint{
        ID:                 newID,
        WorldID:            worldID,
        ParentCheckpointID: sourceCPID,
        Prompt:             prompt,
        Status:             "building",
        DirPath:            newDir,
    }
    m.db.CreateCheckpoint(cp)
    return cp, nil
}

// cloneBuildCache hardlinks the target/ directory for instant, space-efficient copies.
// Linux: cp -al creates hardlinks (instant, shares disk space until modified by cargo).
func (m *Manager) cloneBuildCache(sourceDir, newDir string) error {
    src := filepath.Join(sourceDir, "target")
    dst := filepath.Join(newDir, "target")
    return exec.Command("cp", "-al", src, dst).Run()
}
```

#### 2. Build pipeline (Trunk + cargo)
**File**: `harness/internal/build/builder.go`

Orchestrates the build process:
1. Build game server (native): `cd {checkpoint_dir} && cargo build --release -p server`
2. Build game client (WASM via Trunk): `cd {checkpoint_dir}/client && trunk build --release --dist {wasm_builds_dir}/{worldID}/{cpID}/`
   - Trunk handles: cargo build → wasm-bindgen → wasm-opt → index.html generation
   - Output goes directly to the serving directory
3. Update checkpoint status in DB (`ready` or `failed`)
4. Publish to **world bus** (build progress updates for world-specific UI)
5. Call `createAndPublishMessage()` to persist `build.completed` or `build.failed`
   to the `messages` table and publish to the **global bus** (appears in all players'
   chat/notification logs with clickable `[▶ Play]` button)

```go
const (
    BuildTimeoutIncremental = 5 * time.Minute
    BuildTimeoutInitial     = 15 * time.Minute
)

func (b *Builder) Build(cp *Checkpoint, isInitial bool) error {
    timeout := BuildTimeoutIncremental
    if isInitial {
        timeout = BuildTimeoutInitial
    }
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    wasmDir := filepath.Join(b.wasmBuildsDir, cp.WorldID, cp.ID)

    // Step 1: Build game server (native binary)
    serverCmd := exec.CommandContext(ctx, "cargo", "build", "--release", "-p", "server")
    serverCmd.Dir = cp.DirPath
    serverCmd.Env = append(os.Environ(), "CARGO_HOME="+b.sharedCargoHome)
    if out, err := serverCmd.CombinedOutput(); err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("server build timed out after %v", timeout)
        }
        return fmt.Errorf("server build failed: %s\n%s", err, string(out))
    }

    // Step 2: Build game client (WASM via Trunk)
    clientCmd := exec.CommandContext(ctx, "trunk", "build", "--release", "--dist", wasmDir)
    clientCmd.Dir = filepath.Join(cp.DirPath, "client")
    if out, err := clientCmd.CombinedOutput(); err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("client build timed out after %v", timeout)
        }
        return fmt.Errorf("client build failed: %s\n%s", err, string(out))
    }

    return nil
}
```

Build output is captured and stored as `build_log` in the checkpoint record.
The harness streams build progress to connected browsers via SSE as it happens.

#### 3. Prompt rate limiting
**File**: `harness/internal/world/rate_limit.go`

Prevents resource exhaustion from rapid prompt submissions:
- **One active build per user**: If a user already has a checkpoint in `building` status, reject new prompts with a message: "You already have a build in progress."
- **Cooldown**: 30-second cooldown between prompts per user (prevents rapid-fire forking).
- **Max checkpoints per world**: 50 checkpoints per world (soft limit, admin can override).
- Prompt handler checks rate limits before forking. Returns HTTP 429 with a JSON body including `retryAfterSec` so the UI can show a countdown.

#### 4. Port allocator
**File**: `harness/internal/world/ports.go`

Simple port allocator for game servers:
- Range: 9001-9999
- Track which ports are in use
- Assign next available port when starting a game server

#### 4. Game server lifecycle (reference counting)
**File**: `harness/internal/world/game_server.go`

Since multiple users can be on different checkpoints simultaneously, multiple game servers
may need to run concurrently. The game server manager tracks connections with reference
counting:

```go
type GameServerManager struct {
    mu       sync.Mutex
    servers  map[string]*GameServer  // key: "{worldID}/{cpID}"
    refCount map[string]int          // active user count per server
    ports    *PortAllocator
}

func (m *GameServerManager) Connect(worldID, cpID string) (*GameServer, error) {
    key := worldID + "/" + cpID
    m.mu.Lock()
    defer m.mu.Unlock()

    if srv, ok := m.servers[key]; ok {
        m.refCount[key]++
        return srv, nil
    }
    // Start new game server, assign port
    srv, err := m.startServer(worldID, cpID)
    if err != nil { return nil, err }
    m.servers[key] = srv
    m.refCount[key] = 1
    return srv, nil
}

func (m *GameServerManager) Disconnect(worldID, cpID string) {
    key := worldID + "/" + cpID
    m.mu.Lock()
    defer m.mu.Unlock()

    m.refCount[key]--
    if m.refCount[key] <= 0 {
        // Stop after grace period (let users reconnect)
        go m.stopAfterDelay(key, 2*time.Minute)
    }
}
```

- Server binary path: `{checkpoint_dir}/target/release/server`
- Health check game servers periodically
- Grace period on disconnect prevents thrashing when users switch checkpoints

### Success Criteria:

#### Automated Verification:
- [ ] `POST /world/create` creates a world directory with template files
- [ ] Initial build completes and WASM artifacts exist
- [ ] Fork creates new directory with hardlinked target/
- [ ] Build pipeline produces working WASM
- [ ] Game server starts on assigned port

#### Manual Verification:
- [ ] Create world → build completes → WASM loads in browser
- [ ] Fork checkpoint → new build completes → can switch between versions
- [ ] Build logs are captured and accessible

---

## Phase 4: Claude Code Integration

### Overview
Connect the harness to claude code via tmux for AI-powered game development. Claude Code
hooks provide structured observability - no tmux polling needed. Hook scripts POST JSONL
events to the harness, which pushes them to browsers via Datastar SSE.

### Changes Required:

#### 1. tmux session manager
**File**: `harness/internal/tmux/session.go`

Creates tmux sessions with environment variables that the hook scripts use.

**Prompt delivery**: User prompts are written to a file and passed via `--input-file`
rather than interpolated into a shell command via `tmux send-keys`. This prevents prompt
injection — since claude code runs with `--dangerously-skip-permissions`, a crafted prompt
that escaped shell quoting could execute arbitrary commands.

```go
type Session struct {
    Name    string  // cm-{worldID}-{cpID}
    WorkDir string  // checkpoint directory
}

func (s *Session) Create(worldID, cpID, workDir, logsDir, harnessURL string) error {
    logDir := filepath.Join(logsDir, "worlds", worldID, cpID)
    os.MkdirAll(logDir, 0755)

    return exec.Command("tmux", "new-session", "-d",
        "-s", s.Name, "-c", workDir,
        "-e", "CM_WORLD_ID="+worldID,
        "-e", "CM_CHECKPOINT_ID="+cpID,
        "-e", "CM_HARNESS_URL="+harnessURL,
        "-e", "CM_LOG_DIR="+logDir,
    ).Run()
}

func (s *Session) SendPrompt(prompt string) error {
    // Write prompt to a temp file to avoid shell injection via tmux send-keys.
    // The prompt file is the ONLY user-controlled input that reaches claude code.
    promptFile := filepath.Join(s.WorkDir, ".claude-prompt.txt")
    if err := os.WriteFile(promptFile, []byte(prompt), 0644); err != nil {
        return fmt.Errorf("writing prompt file: %w", err)
    }

    // Use --input-file to pass the prompt safely, avoiding shell escaping issues.
    // --dangerously-skip-permissions is required for unattended operation in tmux.
    cmd := fmt.Sprintf("claude --dangerously-skip-permissions --input-file %s",
        shellescape.Quote(promptFile))
    return exec.Command("tmux", "send-keys", "-t", s.Name, cmd, "Enter").Run()
}

func (s *Session) Kill() error {
    return exec.Command("tmux", "kill-session", "-t", s.Name).Run()
}
```

> **Security note**: Even with file-based prompt delivery, `--dangerously-skip-permissions`
> allows claude code to execute arbitrary commands. This is an accepted risk for the managed
> tmux sessions — the alternative (`--allowedTools`) would need a curated tool allowlist
> that may be too restrictive for open-ended game development. The key improvement is that
> user input never passes through shell interpolation.

#### 2. Claude code orchestrator (hooks-driven)
**File**: `harness/internal/claude/claude.go`

The prompt-to-build pipeline, now event-driven via hooks instead of polling:

1. Fork checkpoint (Phase 3)
2. Update MEMORY.md with the new prompt context
3. Create tmux session with `CM_*` env vars (hooks use these)
4. Write user's prompt to `.claude-prompt.txt` in the checkpoint directory
5. Send claude code command with `--input-file` flag (safe prompt delivery, no shell interpolation)
6. **Hooks fire events** as claude works → `POST /api/claude-event` → SSE to browsers
7. When `claude.session_stopped` event arrives → harness triggers build pipeline
8. If build succeeds → update checkpoint status to `ready`, start game server
9. If build fails → send build errors back to claude code for fixing (retry loop, max 3)
10. **Rate limit handling**: If Claude API returns 429 (rate limited), parse `retry-after` header, surface to user via SSE with countdown until rate limit lifts. Show a banner: "Rate limited — resuming in X:XX". Auto-retry when the window expires.
11. All events logged to JSONL files for debugging

```go
// handleClaudeEvent receives JSONL events POSTed by Claude Code hook scripts.
// Hook events are published to BOTH the world-specific bus (for build progress UI)
// and the global bus (for chat/notification log entries visible to all players).
func (s *Server) handleClaudeEvent(c echo.Context) error {
    var event map[string]any
    if err := json.NewDecoder(c.Request().Body).Decode(&event); err != nil {
        return c.NoContent(400)
    }

    worldID, _ := event["worldID"].(string)
    cpID, _ := event["cpID"].(string)
    eventType, _ := event["event"].(string)

    // Log to harness JSONL
    s.logger.Info("claude hook event", "worldID", worldID, "event", eventType)

    // Publish to world-specific bus (build progress, claude activity)
    s.eventBus.Publish(worldID, event)

    // If claude stopped, trigger the build pipeline
    if eventType == "claude.session_stopped" {
        // Notify all players that a build started
        s.createAndPublishMessage("build.started", worldID, cpID,
            fmt.Sprintf("Building in %s...", s.worldName(worldID)))
        go s.buildManager.BuildCheckpoint(worldID, cpID)
    }

    return c.NoContent(200)
}

// createAndPublishMessage persists a message to the DB and publishes to the global bus.
// Called for build lifecycle events so they appear in every player's chat/notification log.
func (s *Server) createAndPublishMessage(msgType, worldID, cpID, content string) {
    msg := &Message{
        ID: uuid.New().String()[:8],
        Type: msgType, WorldID: worldID, CheckpointID: cpID, Content: content,
    }
    s.db.CreateMessage(msg)
    s.eventBus.PublishGlobal(map[string]any{
        "event":        msgType,
        "worldID":      worldID,
        "cpID":         cpID,
        "content":      content,
        "ts":           time.Now().UTC().Format(time.RFC3339),
    })
}
```

#### 3. MEMORY.md management
**File**: `harness/internal/claude/memory.go`

Before each claude code session:
- Read current MEMORY.md
- Append the new prompt and any context
- After claude finishes, extract key decisions from the session and update MEMORY.md

#### 4. SSE event stream (Datastar, driven by hook events)
**File**: `harness/internal/server/events.go`

The SSE endpoint bridges Claude Code hook events to the browser UI. When hook scripts POST
to `/api/claude-event`, those events are published to a per-world event bus, which the SSE
handler reads and pushes to connected browsers via Datastar.

The world SSE handler subscribes to **both** the global event bus (chat, system
notifications) and the world-specific event bus (build progress, checkpoint tree updates).
This means users see chat messages and build completions from ALL worlds while playing.

On initial connection, the handler sends the last 50 messages from the `messages` table
so the chat/notification log isn't empty on page load.

```go
func (s *Server) handleSSEEvents(c echo.Context) error {
    w := c.Response().Writer
    r := c.Request()
    sse := datastar.NewSSE(w, r)

    worldID := c.Param("worldID")
    user := c.Get("user").(*User)

    // Subscribe to both global and world-specific events
    globalCh := s.eventBus.SubscribeGlobal()
    defer s.eventBus.UnsubscribeGlobal(globalCh)
    worldCh := s.eventBus.Subscribe(worldID)
    defer s.eventBus.Unsubscribe(worldID, worldCh)

    // Send recent message history on connect (last 50 messages)
    recentMessages, _ := s.db.GetRecentMessages(50)
    sse.PatchElements(renderMessageLog(recentMessages))

    // Record player joined
    s.eventBus.PublishGlobal(map[string]any{
        "event": "player.joined", "username": user.GitHubUsername,
        "worldID": worldID, "avatarURL": user.AvatarURL,
    })

    for {
        select {
        case event := <-globalCh:
            // Global events: chat messages, system notifications (all worlds)
            e, _ := event.(map[string]any)
            eventType, _ := e["event"].(string)

            switch eventType {
            case "chat.message":
                sse.PatchElements(renderChatMessage(e))
                sse.MarshalAndPatchSignals(map[string]any{
                    "unreadCount": "$unreadCount + 1",
                })
            case "build.completed", "build.started", "build.failed",
                 "player.joined", "player.left":
                sse.PatchElements(renderNotification(e))
            }

        case event := <-worldCh:
            // World-specific events: build progress, claude activity
            e, _ := event.(map[string]any)
            eventType, _ := e["event"].(string)

            switch {
            case eventType == "claude.tool_use.pre":
                tool, _ := e["tool"].(string)
                file, _ := e["file"].(string)
                sse.PatchElements(renderClaudeActivity(tool, file))
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "editing",
                })

            case eventType == "claude.tool_use.post":
                sse.PatchElements(renderClaudeActivityDone(e))

            case eventType == "claude.session_stopped":
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "compiling",
                })

            case eventType == "build.output":
                line, _ := e["line"].(string)
                sse.PatchElements(renderBuildLogLine(line))

            case eventType == "build.completed":
                cpID, _ := e["cpID"].(string)
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "ready",
                })
                sse.PatchElements(renderCheckpointTree(worldID, s.db))

            case eventType == "build.failed":
                errMsg, _ := e["error"].(string)
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus": "failed",
                })
                sse.PatchElements(renderBuildStatus("failed", errMsg))

            case eventType == "claude.rate_limited":
                retryAfter, _ := e["retryAfterSec"].(float64)
                sse.MarshalAndPatchSignals(map[string]any{
                    "buildStatus":       "rate_limited",
                    "rateLimitRetryAt":  time.Now().Add(time.Duration(retryAfter) * time.Second).Unix(),
                })
                sse.PatchElements(renderRateLimitBanner(int(retryAfter)))
            }

        case <-r.Context().Done():
            s.eventBus.PublishGlobal(map[string]any{
                "event": "player.left", "username": user.GitHubUsername,
                "worldID": worldID,
            })
            return nil
        }
    }
}

// handleChatMessage receives a chat message from an authenticated user
func (s *Server) handleChatMessage(c echo.Context) error {
    user := c.Get("user").(*User)
    var body struct{ Content string `json:"content"` }
    if err := c.Bind(&body); err != nil || body.Content == "" {
        return c.NoContent(400)
    }

    msg := &Message{
        ID:      uuid.New().String()[:8],
        Type:    "chat",
        UserID:  user.ID,
        Content: body.Content,
    }
    s.db.CreateMessage(msg)

    s.eventBus.PublishGlobal(map[string]any{
        "event":    "chat.message",
        "username": user.GitHubUsername,
        "avatar":   user.AvatarURL,
        "content":  body.Content,
        "ts":       time.Now().UTC().Format(time.RFC3339),
    })
    return c.NoContent(200)
}
```

The event bus supports both global (all players) and per-world subscriptions:
```go
type EventBus struct {
    mu              sync.RWMutex
    worldSubs       map[string][]chan any  // worldID → subscriber channels
    globalSubs      []chan any             // all-player subscribers
}

func (b *EventBus) SubscribeGlobal() chan any {
    b.mu.Lock()
    defer b.mu.Unlock()
    ch := make(chan any, 100)
    b.globalSubs = append(b.globalSubs, ch)
    return ch
}

func (b *EventBus) PublishGlobal(event any) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for _, ch := range b.globalSubs {
        select {
        case ch <- event:
        default: // drop if subscriber is slow
        }
    }
}

func (b *EventBus) Subscribe(worldID string) chan any {
    b.mu.Lock()
    defer b.mu.Unlock()
    ch := make(chan any, 100)
    b.worldSubs[worldID] = append(b.worldSubs[worldID], ch)
    return ch
}

func (b *EventBus) Publish(worldID string, event any) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for _, ch := range b.worldSubs[worldID] {
        select {
        case ch <- event:
        default:
        }
    }
}
```

### Success Criteria:

#### Automated Verification:
- [ ] tmux sessions create with correct CM_* env vars
- [ ] Claude code receives prompts in the tmux session
- [ ] Hook scripts write JSONL to claude.jsonl
- [ ] `POST /api/claude-event` publishes events to SSE subscribers
- [ ] Build pipeline triggers when `claude.session_stopped` event arrives
- [ ] Build logs written as JSONL to build.jsonl
- [ ] Game server output written as JSONL to game-server.jsonl

#### Manual Verification:
- [ ] Submit prompt → see build progress in UI → game updates
- [ ] MEMORY.md is updated with new design decisions
- [ ] Power user can `tmux attach -t cm-{world}-{cp}` to inspect claude code session
- [ ] Build retry works when claude produces code that doesn't compile

---

## Phase 5: Harness UI

### Overview
Build the DatastarUI overlay that sits on top of the game canvas. The overlay has two
states: **expanded** (full chrome with chat/notification log) and **minimized** (floating
button with unread badge). The game canvas is always visible and interactive behind the
overlay's transparent areas.

### Changes Required:

#### 1. Base layout
**File**: `harness/views/layout.templ`

The page structure:
- Full-screen iframe for Bevy game (z-index: 0)
- Fixed-position overlay with transparent background (z-index: 10)
- Chat/notification log panel (right side)
- DatastarUI components for interactive elements
- Datastar signals for reactive state, updated in real-time via SSE
- Datastar `data-on:load` attribute connects to the SSE endpoint on page load

Key signals (initialized server-side, updated via SSE as events arrive):
```go
type OverlaySignals struct {
    CurrentWorldID      string `json:"currentWorldId"`
    CurrentCheckpointID string `json:"currentCheckpointId"`
    BuildStatus         string `json:"buildStatus"`    // idle, editing, compiling, ready, failed, rate_limited
    PromptText          string `json:"promptText"`
    ChatText            string `json:"chatText"`
    OverlayExpanded     bool   `json:"overlayExpanded"` // true = full overlay, false = minimized
    ActiveTab           string `json:"activeTab"`       // "global", "world", "lineage"
    ShowCheckpointTree  bool   `json:"showCheckpointTree"`
    ShowBuildLog        bool   `json:"showBuildLog"`
    UnreadCount         int    `json:"unreadCount"`     // badge count when overlay is minimized
    RateLimitRetryAt    int64  `json:"rateLimitRetryAt"`// Unix timestamp, 0 if not rate limited
}
```

The SSE connection is established via Datastar's `data-on:load` attribute:
```html
<div id="harness-overlay"
     data-signals='{"overlayExpanded":true,"unreadCount":0,"buildStatus":"idle",...}'
     data-on:load="@get('/world/{worldID}/events')">

  <!-- Expanded overlay: shown when overlayExpanded is true -->
  <div data-show="$overlayExpanded" class="overlay-expanded">
    <!-- Top bar, chat/notification panel, prompt bar, status bar -->
  </div>

  <!-- Minimized overlay: floating button with unread badge -->
  <div data-show="!$overlayExpanded" class="overlay-minimized">
    <button data-on:click="$overlayExpanded = true; $unreadCount = 0">
      <span data-show="$unreadCount > 0" class="badge" data-text="$unreadCount"></span>
    </button>
  </div>
</div>
```

The SSE connection stays open in both expanded and minimized states. When minimized,
incoming chat messages and notifications increment `unreadCount` (the badge). When
the user expands the overlay, the badge resets to 0.

#### 2. Login page
**File**: `harness/views/login.templ`

Shown when user is not authenticated (redirected by auth middleware):
- "Sign in with GitHub" button → links to `/auth/github/login`
- Clean, minimal design with Creative Mode branding

#### 2b. Pending approval page
**File**: `harness/views/pending.templ`

Shown when user is authenticated but `role = 'pending'`:
- User's avatar + username
- "Your request to join has been submitted. An admin will approve your access."
- Polls `/auth/pending/status` via Datastar SSE — auto-redirects to lobby when approved

#### 2c. Admin user management page
**File**: `harness/views/admin_users.templ`

Admin-only page at `/admin/users`:
- Lists all users with role, avatar, username, joined date
- Pending users highlighted with "Approve" / "Reject" buttons
- Live-updates via Datastar SSE when new users request access

#### 3. Lobby / world browser
**File**: `harness/views/lobby.templ`

Full-screen view (no game canvas) showing:
- User avatar + username in top-right corner (from `c.Get("user")`)
- Logout button (`POST /auth/logout`)
- List of existing worlds with thumbnails (we can screenshot canvases later, placeholder for now)
- "Create New World" button with name input
- For each world: name, description, created by (username + avatar), # checkpoints, last modified

#### 4. In-game overlay (expanded state)
**File**: `harness/views/overlay.templ`

```
┌───────────────────────────────────────────┬──────────────────────┐
│ Creative Mode │ World: My RPG ▼ │ [−]   │ [Global][World][Lin.] │
│ CP: castle ▼ │ [Save] [Tree] [← Lobby]   │──────────────────────│
├───────────────────────────────────────────│                      │
│                                            │ Global tab:          │
│         (transparent - game visible)       │ [sys] Build ready:   │
│                                            │   "add river" [▶]    │
│                                            │ [alice] nice castle! │
│                                            │ [sys] Bob joined     │
│                                            │                      │
│                                            │ Lineage tab:         │
│                                            │ [cp-001] starter     │
│                                            │ [cp-002] "add hills" │
│                                            │   ↳ Claude: Added... │
│                                            │ [cp-003] "castle"    │
│                                            │   ↳ Claude: Built... │
│                                            │  ← you are here     │
├───────────────────────────────────────────│──────────────────────│
│ > add a river...                  [Build] │ > type a message...  │
│ ⏳ Building...  Players: 2         60fps │ [Send]               │
└───────────────────────────────────────────┴──────────────────────┘
```

**Minimized state** (floating button, bottom-right corner):
```
                                                          ┌─────┐
                                                          │ CM 3│
                                                          └─────┘
                                                            ↑ unread badge
```

Components:
- **Top bar**: world selector, checkpoint selector, save button, tree toggle, lobby link, minimize button `[−]`
- **Tabbed panel** (right side, ~320px): three tabs at the top — Global | World | Lineage
  - **Global tab**: all chat + system notifications across all worlds. Build completions have `[▶ Play]`.
  - **World tab**: same types but filtered to current world only.
  - **Lineage tab**: read-only prompt/response chain from root → current checkpoint. Each entry shows the prompt, who submitted it, Claude's work summary, files changed, and build result. No chat messages.
- **Chat input**: text input at bottom of panel (hidden on Lineage tab since it's read-only)
- **Prompt bar** (left, bottom): text area + "Build" button. Sends to Claude Code (forks + builds). Disabled while building.
- **Status bar**: build status, player count (with avatars), FPS
- **Checkpoint tree panel**: slides in from the left, shows fork tree (toggled via `[Tree]` button)
- **Minimize button** `[−]`: collapses all overlay chrome to a floating corner button
- **Unread badge**: shown on minimized button, counts chat + notification messages received while minimized
- User avatar from GitHub (stored in `users.avatar_url`)

The panel has two separate inputs serving different purposes:
- **Prompt bar** (left column, bottom): sends to Claude Code → forks checkpoint → triggers build
- **Chat input** (right column, bottom): sends to all players via `POST /api/chat` → appears in Global/World tabs

#### 4b. Chat/notification panel components
**File**: `harness/views/chat_panel.templ`

The panel renders different content depending on the active tab.

**Global/World tabs** — `id="message-log-global"` and `id="message-log-world"` containers.
The SSE handler appends new messages by patching elements into both containers (world tab
messages also carry a `data-world-id` for filtering). Each message is a templ component:

```go
// renderChatMessage produces a chat message HTML fragment
templ ChatMessage(username, avatarURL, content, timestamp, worldID string) {
    <div class="message chat-message" data-world-id={ worldID }>
        <img src={ avatarURL } class="avatar-sm" />
        <span class="username">{ username }</span>
        <span class="content">{ content }</span>
        <time class="ts">{ timestamp }</time>
    </div>
}

// renderNotification produces a system notification entry
templ SystemNotification(eventType, worldID, worldName, cpID, content string) {
    <div class="message system-notification" data-world-id={ worldID }>
        <span class="sys-badge">[sys]</span>
        <span class="content">{ content }</span>
        if eventType == "build.completed" {
            <button class="play-btn"
                    data-on:click={ fmt.Sprintf("loadCheckpoint('%s','%s')", worldID, cpID) }>
                ▶ Play
            </button>
        }
    </div>
}
```

**Lineage tab** — populated by `GET /world/:worldID/lineage/:cpID` which returns the
checkpoint ancestry as HTML fragments. Fetched when the tab is selected (and re-fetched
when the user switches checkpoints):

```go
// handleLineage returns the checkpoint ancestry as HTML for the Lineage tab
func (s *Server) handleLineage(c echo.Context) error {
    worldID := c.Param("worldID")
    cpID := c.Param("cpID")
    ancestry, _ := s.db.GetCheckpointAncestry(worldID, cpID) // walks parent_checkpoint_id → root
    return render(c, LineageView(ancestry))
}

// LineageView renders the prompt/response chain
templ LineageView(checkpoints []Checkpoint) {
    for _, cp := range checkpoints {
        <div class="lineage-entry">
            <div class="lineage-header">
                <span class="cp-id">[{ cp.ID[:8] }]</span>
                if cp.Prompt != "" {
                    <span class="prompt">"{ truncate(cp.Prompt, 60) }"</span>
                } else {
                    <span class="prompt">Starter template</span>
                }
                if cp.CreatedBy != "" {
                    <span class="author">— { cp.CreatedByUsername }</span>
                }
                <time class="ts">{ timeAgo(cp.CreatedAt) }</time>
            </div>
            if cp.WorkSummary != "" {
                <div class="lineage-summary">
                    <span class="claude-label">↳ Claude:</span>
                    { cp.WorkSummary }
                    if cp.FilesChanged != "" {
                        <div class="files-changed">Files: { cp.FilesChanged }</div>
                    }
                    <div class="build-result">
                        Build: { cp.StatusIcon() } { cp.Status }
                        if cp.BuildDurationMs > 0 {
                            ({ formatDuration(cp.BuildDurationMs) })
                        }
                    </div>
                </div>
            }
        </div>
    }
    <div class="lineage-cursor">← you are here</div>
}
```

```javascript
// game-loader.js — fetch lineage when tab is selected or checkpoint changes
window.loadLineage = function() {
    const worldID = document.body.dataset.worldId;
    const cpID = document.body.dataset.checkpointId;
    fetch(`/world/${worldID}/lineage/${cpID}`)
        .then(r => r.text())
        .then(html => {
            document.getElementById('lineage-view').innerHTML = html;
        });
};
```

#### 5. Checkpoint tree visualization
**File**: `harness/views/checkpoint_tree.templ`

A side panel showing the checkpoint tree for the current world:
- Tree structure with indentation
- Each node shows: name/prompt snippet, status icon (✓/⏳/✗)
- Click a checkpoint to switch to it
- Current checkpoint highlighted
- "Fork from here" button on each ready checkpoint

#### 6. Build log viewer
**File**: `harness/views/build_log.templ`

Expandable panel showing:
- Claude code output (streamed from tmux)
- Build output (cargo build logs)
- Error messages with line numbers

#### 8. CSS for transparent overlay
**File**: `harness/static/styles.css`

```css
#harness-overlay {
    position: fixed;
    inset: 0;
    z-index: 10;
    pointer-events: none;
}

/* Expanded overlay: two-column layout (game area left, chat right) */
.overlay-expanded {
    display: grid;
    grid-template-columns: 1fr 320px;
    grid-template-rows: auto 1fr auto;
    height: 100vh;
    pointer-events: none;
}

.overlay-expanded > * {
    pointer-events: auto;
}

.overlay-bar {
    background: rgba(15, 15, 15, 0.85);
    backdrop-filter: blur(10px);
    color: white;
    padding: 8px 16px;
}

/* Chat/notification panel (right column, full height) */
.chat-panel {
    grid-column: 2;
    grid-row: 1 / -1;
    background: rgba(15, 15, 15, 0.90);
    backdrop-filter: blur(10px);
    color: white;
    display: flex;
    flex-direction: column;
    pointer-events: auto;
}

/* Tab bar */
.tab-bar {
    display: flex;
    border-bottom: 1px solid rgba(255,255,255,0.1);
    padding: 0 4px;
}
.tab-bar button {
    flex: 1;
    padding: 8px 4px;
    background: none;
    border: none;
    color: #888;
    cursor: pointer;
    font-size: 12px;
    border-bottom: 2px solid transparent;
}
.tab-bar .tab-active {
    color: white;
    border-bottom-color: #2563eb;
}

.message-log {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
}

.message { padding: 4px 8px; font-size: 13px; }
.system-notification { color: #888; }

/* Lineage view */
.lineage-entry {
    padding: 8px;
    border-left: 2px solid rgba(255,255,255,0.1);
    margin-left: 8px;
    margin-bottom: 4px;
}
.lineage-header { font-size: 13px; }
.lineage-header .cp-id { color: #666; font-family: monospace; font-size: 11px; }
.lineage-header .prompt { color: #e0e0e0; }
.lineage-header .author { color: #888; font-size: 12px; }
.lineage-summary {
    margin-top: 4px;
    padding-left: 8px;
    font-size: 12px;
    color: #aaa;
}
.lineage-summary .claude-label { color: #2563eb; font-weight: 600; }
.lineage-summary .files-changed { color: #666; font-size: 11px; margin-top: 2px; }
.lineage-summary .build-result { color: #666; font-size: 11px; }
.lineage-cursor { color: #2563eb; font-style: italic; padding: 8px; font-size: 13px; }
.play-btn {
    background: #2563eb;
    color: white;
    border: none;
    border-radius: 4px;
    padding: 2px 8px;
    cursor: pointer;
    font-size: 12px;
}

/* Minimized overlay: floating button */
.overlay-minimized {
    position: fixed;
    bottom: 24px;
    right: 24px;
    pointer-events: auto;
}

.overlay-minimized button {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    background: rgba(15, 15, 15, 0.85);
    color: white;
    border: 1px solid rgba(255,255,255,0.2);
    cursor: pointer;
    position: relative;
}

.badge {
    position: absolute;
    top: -4px;
    right: -4px;
    background: #ef4444;
    color: white;
    border-radius: 50%;
    width: 20px;
    height: 20px;
    font-size: 11px;
    display: flex;
    align-items: center;
    justify-content: center;
}

/* Game area (left column) is transparent - clicks pass through to iframe */
.game-area {
    grid-column: 1;
    pointer-events: none;
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `templ generate` succeeds
- [ ] `cd harness && go build ./...` compiles with views
- [ ] All templ files render without errors

#### Manual Verification:
- [ ] Login page shows "Sign in with GitHub" button
- [ ] Unauthenticated users are redirected to login
- [ ] Lobby shows user avatar, username, and logout button
- [ ] Overlay renders on top of game canvas with user avatar in top bar
- [ ] Game canvas receives mouse/keyboard input through transparent areas
- [ ] World selector switches between worlds
- [ ] Checkpoint selector switches between checkpoints
- [ ] Prompt input submits and triggers build
- [ ] Build status updates in real-time via SSE
- [ ] Checkpoint tree shows correct fork structure
- [ ] Build log streams output
- [ ] Logout clears session and redirects to login page
- [ ] Chat messages sent by one user appear in all connected browsers
- [ ] Build completion notifications appear in chat log with clickable [▶ Play] button
- [ ] Clicking [▶ Play] on a build notification switches iframe to that world/checkpoint
- [ ] Minimizing overlay hides all chrome, shows floating button
- [ ] New messages while minimized increment the unread badge count
- [ ] Expanding overlay resets badge to 0 and shows full chat history
- [ ] Player joined/left notifications appear when users connect/disconnect
- [ ] Chat log loads recent history (last 50 messages) on page load

---

## Phase 6: End-to-End Integration

### Overview
Wire everything together and polish the end-to-end flow.

### Changes Required:

#### 1. World/checkpoint switching via iframe + notifications
**File**: `harness/static/game-loader.js`

Minimal JS for switching between Trunk-built game worlds. Called from two places:
1. The `[▶ Play]` button on build completion notifications in the chat log
2. The checkpoint tree panel when clicking a checkpoint node

```javascript
// Load a checkpoint within the current world (same-world switch)
// Called from chat log [▶ Play] buttons and checkpoint tree clicks
window.loadCheckpoint = function(worldID, checkpointID, serverPort) {
    const currentWorldID = document.body.dataset.worldId;

    if (worldID !== currentWorldID) {
        // Cross-world navigation: full page load to set up new SSE connection
        window.location.href = `/world/${worldID}?checkpoint=${checkpointID}`;
        return;
    }

    // Same-world: just swap the iframe src (instant, no page reload)
    const iframe = document.getElementById('game-frame');
    iframe.src = `/wasm/${worldID}/${checkpointID}/index.html?server_port=${serverPort}`;

    // Update user position on the server
    fetch(`/world/${worldID}/checkpoint/${checkpointID}/select`, { method: 'POST' });
};

// Called when user selects a world from the top bar dropdown
window.switchWorld = function(worldID) {
    window.location.href = `/world/${worldID}`;
};
```

The iframe approach means WASM memory is fully freed when the `src` changes - no Bevy
shutdown gymnastics needed. Each Trunk build is self-contained and loads cleanly.

**Cross-world navigation from notifications**: When a user clicks `[▶ Play]` on a build
notification for a different world than the one they're currently in, `loadCheckpoint()`
detects the world mismatch and does a full page navigation. This establishes a new SSE
connection scoped to the target world while keeping the global notification stream.

#### 2. Game server connection string
The game client needs to know which server to connect to. The harness passes this via
query parameters on the iframe URL (included in the `loadCheckpoint()` call above).

The Bevy client reads the query parameter on startup:
```rust
fn get_server_port() -> u16 {
    let window = web_sys::window().unwrap();
    let search = window.location().search().unwrap();
    let params = web_sys::UrlSearchParams::new_with_str(&search).unwrap();
    params.get("server_port")
        .and_then(|p| p.parse().ok())
        .unwrap_or(9001)
}
```

#### 3. Multi-user support
- Each user's position (world + checkpoint) tracked in `user_positions` table
- Users independently browse different checkpoints without affecting each other
- `handleWorldView` reads user's position from `user_positions`, defaults to root checkpoint
- `handleCheckpointView` updates `user_positions` for the current user
- `handlePrompt` forks from user's current checkpoint (per `user_positions`), records `user_id` in `prompt_history`
- Harness tracks connected users via SSE connections
- Player count shown in status bar with user avatars
- Game server handles multiple WebTransport connections natively via Lightyear
- `GameServerManager` reference counting ensures servers stay alive while users are connected

#### 4. Setup script
**File**: `scripts/setup.sh`

For local macOS development, everything runs inside Docker:
```bash
#!/bin/bash
set -e

echo "Setting up Creative Mode..."

# Verify Docker is installed
if ! command -v docker &>/dev/null; then
    echo "Error: Docker is required. Install Docker Desktop for macOS."
    exit 1
fi

# Build the dev image
docker compose build

# Pre-build template dependencies inside the container
docker compose run --rm creative-mode bash -c \
    "cd /app/template && cargo build --release -p server && cd client && trunk build --release"

echo "Setup complete! Run 'docker compose up' to start."
```

For direct Linux server deployment (Ubuntu 24.04), dependencies are installed by the Dockerfile.
The same Dockerfile is used in both environments.

#### 5. Pre-build template dependencies
Part of setup: run an initial `cargo build` in the template directory so that all Bevy/Lightyear dependencies are compiled once. New worlds copy this pre-built `target/` directory.

### Success Criteria:

#### Automated Verification:
- [ ] `docker compose build` succeeds
- [ ] `docker compose up` starts the harness server on :8080
- [ ] Template pre-build completes inside container

#### Manual Verification:
- [ ] Full flow: create world → fly around → submit prompt → see changes → save checkpoint → fork → compare
- [ ] Two users in different browsers can see each other
- [ ] Switching between worlds/checkpoints works
- [ ] Claude code sessions are visible via `tmux ls`
- [ ] MEMORY.md accumulates design decisions across prompts

---

## Testing Strategy

### Unit Tests:
- Database CRUD operations
- World manager fork logic (directory operations)
- Port allocator
- tmux session naming/escaping

### Integration Tests:
- Create world → verify directory structure
- Fork checkpoint → verify hardlinked target/ exists
- Build pipeline → verify WASM output
- SSE event stream → verify events fire

### Manual Testing Steps:
1. Start harness, create "Test World"
2. Wait for initial build, open in browser
3. Fly around with WASD + mouse, verify fly camera
4. Open second browser tab, verify both players visible as pill meshes
5. Submit prompt: "change the ground color to blue"
6. Watch build progress in status bar, verify "Building..." notification in chat log
7. Verify `build.completed` notification appears in chat log with `[▶ Play]` button
8. Click `[▶ Play]` → verify ground changes to blue
9. Send a chat message → verify it appears in both browser tabs
10. Save checkpoint "blue ground"
11. Submit prompt: "add 10 randomly placed red cubes"
12. Minimize the overlay → verify floating button appears
13. Verify unread badge increments as build progresses and completes
14. Expand overlay → verify badge resets, chat log shows full history
15. Click `[▶ Play]` on the build notification → verify cubes appear
16. Go back to "blue ground" checkpoint via checkpoint tree
17. Submit different prompt: "add a large sphere in the center"
18. Switch between the two forks using chat log `[▶ Play]` buttons
19. Create a second world, verify build notifications from first world appear in second world's chat
20. Click cross-world `[▶ Play]` → verify full page navigation to other world
21. SSH in, run `tmux ls`, verify sessions exist
22. Run `tmux attach -t cm-{world}-{cp}` to inspect claude code

## Performance Considerations

- **Build times**: Pre-compiled dependencies + hardlinked target directories keep incremental builds to ~10-30 seconds
- **Disk space**: Each checkpoint is ~1-2GB (target dir). Hardlinks help but worlds will accumulate. Consider a cleanup strategy for old checkpoints.
- **Memory**: Each game server is a separate Rust process (~50-100MB). Limit concurrent game servers.
- **WASM size**: ~15-30MB per build. Serve with gzip/brotli compression.
- **SSE connections**: Lightweight, one per connected browser. Datastar handles reconnection automatically.

## Open Questions (Resolved)

- **Q: How to handle concurrent prompts?** A: Fork model - each prompt creates a new branch from the current checkpoint. No conflicts.
- **Q: How to share assets?** A: Central `shared-assets/` dir served by harness. Games load via HTTP.
- **Q: How to preserve build cache on fork?** A: `cp -al` (hardlinks) on Linux. Instant, shares disk space until cargo overwrites changed files.
- **Q: How to handle game server lifecycle?** A: Harness starts/stops server processes. One game server per active checkpoint.
- **Q: Target platform?** A: Linux (Ubuntu 24.04) server. Docker for local macOS development.
- **Q: Scale expectations?** A: <10 users on a shared, trusted server. SQLite is sufficient.
- **Q: Deletion support?** A: Not needed for initial implementation.
- **Q: Claude API rate limiting?** A: Surface rate limits to user via SSE with countdown until lifted. Auto-retry when window expires.
- **Q: Access control?** A: Request-to-join + admin approval. First user becomes admin. Subsequent users start as `pending` until approved.
- **Q: wasm-bindgen version?** A: Pin `wasm-bindgen = "=0.2.108"` in workspace Cargo.toml. Match with `wasm_bindgen = "0.2.108"` in Trunk.toml. Versions must be exact match.
- **Q: How do build completions surface to users?** A: No auto-switching. Build completions appear as entries in a global chat/notification log shared by all players. Each `build.completed` entry has a clickable `[▶ Play]` button that navigates to that world/checkpoint. Same-world switches swap the iframe; cross-world switches do a full page navigation.
- **Q: How do users communicate?** A: Global text chat in the same notification log panel. System events (builds, joins, leaves) and player chat messages are interleaved in a single scrollable feed.
- **Q: What happens when the overlay is minimized?** A: All chrome is hidden. A floating button in the corner shows an unread badge count. New chat messages and system notifications increment the badge. Expanding the overlay resets the badge and shows full history.
- **Q: Are notifications per-world or global?** A: Global. All players see all notifications regardless of which world they're in. This enables cross-world discovery — you can see someone's build complete in another world and click to hop there.
- **Q: How does the chat panel handle different scopes?** A: Three tabs at the top of the panel: Global (all worlds), World (current world only), Lineage (prompt/response chain from root → current checkpoint). Tab selection is always visible.
- **Q: Does the Lineage tab include chat messages?** A: No. Lineage is strictly the decision history: prompts + Claude's work summaries. Clean and focused context for the next prompt.
- **Q: How are Claude's work summaries generated?** A: Both combined. Claude writes a human-readable `CHANGES.txt` (primary text, 2-4 sentences). Harness auto-generates structured metadata from hook events (files changed, build duration). Both stored on the checkpoint record.

## References

- [Bevy Engine](https://bevyengine.org/) - v0.18 (Jan 2026), Rust game engine
- [Lightyear](https://github.com/cBournhonesque/lightyear) - v0.26, Bevy multiplayer networking (targets Bevy 0.18)
- [Aeronet](https://github.com/aecsocket/aeronet) - Transport layer used by Lightyear (WebSocket, WebTransport, UDP, Steam)
- [Trunk](https://trunkrs.dev/) - Rust WASM bundler (cargo build → wasm-bindgen → wasm-opt → index.html)
- [Datastar](https://data-star.dev/) - Hypermedia framework
- [DatastarUI](https://github.com/CoreyCole/datastarui) - Go/templ component library
- [Echo](https://echo.labstack.com/) - Go HTTP framework
- [Bevy WASM Guide](https://bevy-cheatbook.github.io/platforms/wasm.html)
- [templ](https://templ.guide/) - Go HTML templating
- [Bevy Game Template](https://github.com/NiklasEi/bevy_game_template) - Reference Trunk + Bevy project
