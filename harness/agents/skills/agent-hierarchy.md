---
name: agent-hierarchy
description: President mayors Claude Code hierarchy, OpenClaw workspace, Discord integration, build pipeline
tags: [agents, mayor, president, openclaw, discord, claude-code]
last_verified: 2026-03-08
---

# Agent Hierarchy

## Architecture

```
President (global, optional)
├── Oversees all mayors and the repo
├── Skills: mayor-status, repo-build, template-update, deploy
├── Channel: DISCORD_PRESIDENT_CHANNEL_ID
└── Auto-provisions on startup if env vars set

Mayors (per-world)
├── OpenClaw agent with personality from onboarding
├── Workspace: {OPENCLAW_HOME}/workspaces/world-{worldID}/
│   ├── SOUL.md, AGENTS.md, IDENTITY.md, USER.md, MEMORY.md
│   └── skills/ (world-build, world-status, contribute-learning)
├── Discord channel: one per world (private)
└── Triggers Claude Code builds via POST /api/mayor/build

Claude Code (per-build session)
├── Runs in tmux: cm-{worldID}-{cpID}
├── Guided by templates/*/CLAUDE.md + MEMORY.md
├── Hook scripts POST events to /api/claude-event
└── Pipeline: ForkCheckpoint → edit → BuildCheckpoint → deploy
```

## Single Bot Architecture

One `DISCORD_BOT_TOKEN` for all operations via separate `discordgo.Session` instances:
- **REST** (`pkg/worldchannel.Client`): Channel creation, welcome messages
- **Gateway** (`internal/discord.Listener`): Real-time message mirroring
- **Mayor init** (`internal/mayor.Manager`): Discord API operations

## President Details

Workspace: `{OPENCLAW_HOME}/workspaces/president/` — `SOUL.md`, `AGENTS.md`, `IDENTITY.md`, `USER.md`, `MEMORY.md`, `HEARTBEAT.md`, `skills/` (4 skills: `mayor-status`, `repo-build`, `template-update`, `deploy`).

**Currently disabled in production** — `PRESIDENT_SECRET` and `DISCORD_PRESIDENT_CHANNEL_ID` env vars are not configured. Auto-provisions on startup only when both are set.

## OpenClaw

Installed at `/opt/openclaw/`, CLI at `/opt/openclaw/node_modules/.bin/openclaw`.

## Build Flow

Site onboarding → Discord channel created → `POST /api/world-hatched` webhook → harness provisions OpenClaw agent → mayor responds in Discord → can trigger builds via `POST /api/mayor/build`.
