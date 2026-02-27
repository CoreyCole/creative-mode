# Creative Mode

An OpenClaw-powered game world builder. Create multiplayer games through conversation with friends on a shared, secured server.

Test the onboarding experience at [creative-mode.ai](https://creative-mode.ai).

[![Generated Image February 13, 2026 - 10_48PM](https://github.com/user-attachments/assets/668b3ea3-a467-4dad-b556-c0d1c32990aa)](https://youtu.be/OJRt62-ews0)

| | The demo harness server is locked behind a private tailnet. | |
|---|---|---|
| ⚠️⚠️⚠️ | **WARNING: The harness server runs OpenClaw agents. Do NOT run this on your personal computer. A VM or cloud VPS on a private network is HIGHLY recommended.** | ⚠️⚠️⚠️ |

Secured on-demand infrastructure coming soon.

## How It Works

```
You: "add green rolling hills with a castle on the highest one"
Mayor: *plans the changes, asks about castle style, composes a detailed build prompt*
Claude Code: *edits Rust/Bevy source, rebuilds WASM*
Browser: *reloads with your new terrain*
```

1. Create a world — a 4-step wizard gathers your world's identity and your AI mayor's personality
1. Chat with your mayor via Discord (from your phone) or the browser UI
1. The mayor understands your vision, asks clarifying questions, then orchestrates builds
1. Claude Code edits the game source, compiles to WASM, and the browser hot-reloads
1. Fork from any checkpoint to try different directions — every prompt creates a branch
1. Generate assets with Gemini Nano Banana image generation
1. Monitor everything from the Mayor Dashboard — memory, sessions, builds, activity

## Architecture

```
Discord Channel (source of truth)
   ^         |
   |         | OpenClaw listens via Discord adapter
   |         v
   |    OpenClaw Mayor Agent
   |    (SOUL.md = personality, MEMORY.md = world knowledge)
   |    (AGENTS.md = structured workflow, skills/ = capabilities)
   |         |
   |         | (when ready to build)
   |         v
   |    Harness API: POST /api/mayor/build
   |         |
   |         v
   |    Pipeline: ForkCheckpoint -> Claude Code -> hooks -> BuildCheckpoint
   |         |
   |         v (build complete/failed)
   |    Harness posts to Discord: "[BUILD COMPLETE] checkpoint abc123"
   |
   +--- discordgo listener mirrors ALL messages -> SQLite -> SSE -> browser UI
   +--- Mayor Dashboard (/mayor/:worldID) for full observability
```

| Component | Tech | Role |
|-----------|------|------|
| **Harness Server** | Go + Datastar + templ | World management, Claude Code orchestration, mayor API, SSE UI |
| **Game Server** | Rust + Bevy + Lightyear | Headless multiplayer server (one per active world) |
| **Game Client** | Rust + Bevy + Lightyear -> WASM | 3D/2D game running in the browser |
| **Mayor Agent** | OpenClaw + Claude | Per-world AI with personality, memory, and build skills |
| **Discord** | discordgo + discord.js | Primary chat interface + message bus |
| **Image Gen** | Gemini Nano Banana | AI-generated game assets |

### The Fork Model

Every prompt creates a new branch from the current checkpoint:

```
[starter template]
    +-- "add green hills" done
    |   +-- "castle on the highest hill" done
    |   +-- "dark forest with fog" building...
    +-- "flat desert with sand dunes" done
```

Each checkpoint is a complete Rust project. Build caches are preserved via hardlinks so forked builds are fast (~10-30s incremental).

### World Mayors

Every world gets an AI mayor agent — a persistent OpenClaw agent with:

- **Rich personality** gathered through a 4-step creation wizard
- **Evolving memory** that learns your preferences and remembers past builds
- **Structured workflow**: understand -> plan -> clarify -> build -> report
- **Discord integration** for phone-accessible chat from anywhere
- **Build skills** that call the harness API to trigger the full pipeline
- **Full observability** via the Mayor Dashboard

## Prerequisites

- Docker (Docker Desktop on macOS)
- Discord OAuth App + one Discord bot (shared by harness + site for mayor agents)
- Anthropic API key (for Claude Code + OpenClaw)
- Google AI API key (for Gemini image generation)

## Quick Start

```bash
cd harness

# Build and setup (first time)
just setup

# Start everything (Docker + file watcher + Tailwind)
just live

# Open browser
open http://localhost:8080
```

The first user to sign in becomes the server admin and can approve other users.

## Tech Stack

- **[Bevy](https://bevyengine.org/)** 0.18 - Rust game engine with WASM support
- **[Lightyear](https://github.com/cBournhonesque/lightyear)** 0.26 - Server-authoritative multiplayer networking
- **[OpenClaw](https://github.com/openclaw/openclaw)** - AI agent framework with Discord adapter
- **[Datastar](https://data-star.dev/)** - Hypermedia framework (SSE-based real-time UI)
- **[DatastarUI](https://github.com/CoreyCole/datastarui)** - shadcn/ui component library for Go/templ
- **[templ](https://templ.guide/)** - Go HTML templating
- **[Gemini](https://ai.google.dev/)** - Nano Banana image generation
- **[discordgo](https://github.com/bwmarrin/discordgo)** - Discord Gateway listener
- **SQLite** - World, checkpoint, message, and instrumentation tracking
- **tmux** - Claude Code session management

## License

Creative Mode is licensed under the [Elastic License 2.0 (ELv2)](./LICENSE). You're free to self-host, modify, and build with it. Games and worlds you create are yours — sell them, share them, do whatever you want. The one restriction: you can't offer Creative Mode itself as a competing for-profit service.

______________________________________________________________________

## Disclaimer

Creative Mode is experimental software built on top of other experimental software. The mayor is learning on the job and there is always a chance it borks your machine.

**Do not run this on your personal computer.** Use a dedicated virtual machine (UTM, VirtualBox, etc.) or a cloud VPS. If something goes wrong, you can roll back a VM snapshot or spin up a fresh server — your daily driver should NOT be in the blast radius.

**Make backups.** Take VM snapshots before major changes. Keep your `.env` file and database backups somewhere safe and not included in your mayor's default context. The bootstrap script sets up daily SQLite backups, but that only covers the database — your VM itself is your responsibility.

Large language models are probabilistic and can make mistakes. Your mileage may vary. Creative Mode will continue to improve, your usage and feedback make the system better at game dev over time.
