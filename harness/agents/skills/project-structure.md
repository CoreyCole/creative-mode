---
name: project-structure
description: Directory layout, key packages, data flow, entry points, Go server structure
tags: [project, structure, packages, directories, architecture]
last_verified: 2026-03-08
---

# Project Structure

## Top-Level Directories

| Directory | Purpose |
|-----------|---------|
| `harness/` | Go server (Echo + SQLite + Datastar + templ) |
| `templates/3d/` | 3D Bevy/Lightyear game template |
| `templates/2d/` | 2D Bevy room-based template |
| `templates/boardgame/` | Board game (Checkers) Bevy/WASM template |
| `scripts/` | Build, format, setup, infrastructure scripts |
| `site/` | Marketing site + onboarding (Echo + templ) |
| `pkg/` | Shared Go packages: `worldchannel`, `mayorchat`, `markdown`, `imagegen` |
| `thoughts/` | Plans, reviews, research docs, handoffs |
| `context/` | Reference code (gitignored) — pi-mono, northstar, datastarui |

## Harness Key Packages

| Package | Purpose |
|---------|---------|
| `internal/server/` | HTTP handlers, routes, SSE events |
| `internal/auth/` | Discord OAuth, session middleware, role checks |
| `internal/db/` | SQLite wrapper, migrations, sqlc queries |
| `internal/events/` | EventBus: global + per-world pub/sub channels |
| `internal/world/` | World creation, checkpoints, game server management |
| `internal/claude/` | Claude Code orchestrator: prompt-to-build pipeline |
| `internal/builder/` | Build pipeline: fork → Claude Code → compile → deploy |
| `internal/mayor/` | Mayor agent lifecycle: OpenClaw provisioning |
| `internal/discord/` | Discord Gateway listener: mirrors messages to DB |
| `internal/tmux/` | Tmux session management for Claude Code and game servers |
| `internal/president/` | President agent: provisioning, repo-level operations |
| `internal/swarmorch/` | Swarm orchestrator: agent primitives types and Temporal integration |
| `internal/logging/` | Structured JSON logger |
| `internal/gemini/` | Gemini image generation integration |

## Data Flow

```
Browser <--SSE--> Echo handlers <--EventBus--> Claude orchestrator
                       |              |              |
                    SQLite DB         |          Game servers
                       ^              |
                       |         EventMayorMessage
                       |              |
Discord <--Gateway--> Listener -------+
```

## Stack

Go + Echo + SQLite (sqlc) + templ (HTML) + Datastar (hypermedia/SSE) + static CSS/JS

## Entry Point

`harness/main.go` — creates DB, EventBus, Server, GameServerManager, MayorManager, wires routes.
