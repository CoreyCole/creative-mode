# Creative Mode

A claude-powered game world builder. Create multiplayer 3D games through natural language prompts.

**The problem**: Game development is powerful but locked behind deep technical expertise. Creative Mode breaks that barrier - anyone can describe what they want and watch it come to life.

**The magic**: Every prompt forks the current game world, creating a tree of versions you can browse, compare, and build on. Friends share a server and shape game worlds together, just like Minecraft creative mode but for building the games themselves.

## How It Works

```
You: "add green rolling hills with a castle on the highest one"
Claude: *edits Rust/Bevy source code, rebuilds WASM*
Browser: *reloads with your new terrain*
```

1. Open the browser and create a new game world (starts from a multiplayer 3D template)
2. Fly around the empty world - you'll see other players as pill meshes
3. Type a prompt describing what to build
4. Claude Code edits the Rust game source, rebuilds, and the browser reloads with your changes
5. Save checkpoints at good states, fork from any checkpoint to try different directions
6. Switch between worlds and checkpoints instantly from the UI

## Architecture

Three components:

| Component | Tech | Role |
|-----------|------|------|
| **Harness Server** | Go + DatastarUI + templ | Management UI overlay, world/checkpoint management, claude code orchestration |
| **Game Server** | Rust + Bevy + Lightyear | Headless multiplayer server (one per active world) |
| **Game Client** | Rust + Bevy + Lightyear → WASM | 3D game running in the browser |

```
┌─────────── Browser ───────────┐
│  Harness UI (transparent)     │  ← prompts, world switching, status
│  Bevy Canvas (full screen)    │  ← 3D multiplayer game
└───────────────────────────────┘
         │ SSE              │ WebSocket
┌────────┴──────────────────┴───────────┐
│    Server (Ubuntu 24.04 / Docker)      │
│  Harness (Go :8080)                   │
│  tmux → claude code (per world)       │
│  Game Servers (Rust :9001+)           │
│  SQLite (worlds, checkpoints, prompts)│
└────────────────────────────────────────┘
```

### The Fork Model

Every prompt creates a new branch from the current checkpoint:

```
[starter template]
    ├── "add green hills" ✓
    │   ├── "castle on the highest hill" ✓
    │   └── "dark forest with fog" ⏳ building...
    └── "flat desert with sand dunes" ✓
```

Each checkpoint is its own complete Rust project. Build caches are preserved via hardlinks so forked builds are fast (~10-30s incremental).

### Shared Assets

Assets (textures, models, sounds) live in a central directory served by the harness. Game worlds reference them via HTTP URLs, avoiding duplication across worlds and checkpoints.

### Claude Code Sessions

Each active build runs in a tmux session with claude code. A `MEMORY.md` file per world preserves design decisions across compaction. Power users can SSH in and `tmux attach` to inspect or intervene.

## Prerequisites

- Docker (Docker Desktop on macOS)
- GitHub OAuth App (for auth)
- Anthropic API key (for Claude Code)

## Quick Start

```bash
# Build and setup (first time)
./scripts/setup.sh

# Start everything
docker compose up

# Open browser
open http://localhost:8080
```

The first user to sign in becomes the server admin and can approve other users.

## Project Structure

```
creative-mode/
├── Dockerfile                  # Ubuntu 24.04 build/runtime image
├── docker-compose.yml          # Local dev setup
├── harness/                    # Go harness server
│   ├── internal/
│   │   ├── server/             # HTTP routing
│   │   ├── db/                 # SQLite layer
│   │   ├── world/              # World/checkpoint management
│   │   ├── build/              # Cargo build pipeline
│   │   ├── tmux/               # tmux session management
│   │   └── claude/             # Claude code integration
│   └── views/                  # templ templates (DatastarUI overlay)
├── template/                   # Starter Bevy+Lightyear game
│   ├── shared/                 # Multiplayer protocol (shared)
│   ├── server/                 # Headless game server
│   ├── client/                 # WASM game client
│   ├── CLAUDE.md               # Instructions for claude code
│   └── MEMORY.md               # World memory (design decisions)
├── data/                       # Runtime (gitignored)
│   ├── worlds/                 # World project directories
│   ├── wasm-builds/            # Compiled WASM artifacts
│   └── creative-mode.db        # SQLite database
└── scripts/
    ├── setup.sh                # Install dependencies
    └── build-game.sh           # Build pipeline
```

## Tech Stack

- **[Bevy](https://bevyengine.org/)** 0.18 - Rust game engine with WASM support
- **[Lightyear](https://github.com/cBournhonesque/lightyear)** 0.26 - Server-authoritative multiplayer networking (WebSocket via aeronet)
- **[Datastar](https://data-star.dev/)** - Hypermedia framework (SSE-based real-time UI)
- **[DatastarUI](https://github.com/CoreyCole/datastarui)** - shadcn/ui component library for Go/templ
- **[templ](https://templ.guide/)** - Go HTML templating
- **SQLite** - World/checkpoint/prompt tracking
- **tmux** - Claude code session management
