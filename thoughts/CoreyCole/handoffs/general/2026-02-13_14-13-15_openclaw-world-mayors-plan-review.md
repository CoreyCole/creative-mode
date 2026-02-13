---
date: 2026-02-13T14:13:15-08:00
researcher: CoreyCole
git_commit: 8910e0342a92e75eaf295a8919382f344758eb24
branch: main
repository: creative-mode
topic: "OpenClaw World Mayors Plan — Final Review & Verification"
tags: [implementation, strategy, openclaw, discord, mayors, plan-review]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: OpenClaw World Mayors — Plan Review & Deep Research

## Task(s)

### Completed
1. **Plan review and feedback integration** — Reviewed the full OpenClaw World Mayors implementation plan. Identified 14 issues (bugs, security, architecture, missing code) and integrated all fixes into the plan document.
2. **Two-bot architecture** — Updated the plan to use two Discord bots (Mayor bot for OpenClaw persona, Harness bot for infrastructure/listener) instead of one shared bot.
3. **Private channels + invite system** — Updated Phase 3 to create private Discord channels with `permission_overwrites` and added invite/revoke API endpoints.
4. **Discord OAuth as primary auth** — Updated Phase 2 to replace GitHub OAuth with Discord OAuth for login, keeping GitHub as optional account linking. Updated DB schema, auth handler, routes, and views.

### Planned (Next Session)
5. **Deep research on `context/openclaw/`** — Verify assumptions about OpenClaw's isolated agent workspaces, config hot-reload, CLI semantics (`agents add`, `config set` — especially whether `config set bindings` replaces or appends), and identify any missed features that could be useful (e.g., MCP tool bridging, session management APIs).
6. **Full plan review pass** — One more end-to-end read of the plan with the OpenClaw research findings incorporated. Look for inconsistencies, gaps, and opportunities.

## Critical References

- **Implementation plan**: `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — the primary document, heavily updated in this session
- **OpenClaw reference code**: `context/openclaw/` — gitignored reference code that needs deep research to verify plan assumptions
- **Current auth implementation**: `harness/internal/auth/auth.go` — the GitHub OAuth code being replaced with Discord OAuth

## Recent changes

All changes were to the plan document only (no code changes):

- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — extensive updates across all sections

## Learnings

### Bugs Fixed in Plan
1. **Data race on `channelMap`** — `Listener.channelMap` needs `sync.RWMutex` (written by `RegisterChannel` from HTTP goroutine, read by `handleMessage` from discordgo goroutine)
2. **`bindAgentToDiscord` replace vs append** — `openclaw config set bindings` semantics are UNKNOWN. Plan now uses read-modify-write (read current, append, write back) to be safe. **This needs verification against actual OpenClaw CLI behavior in `context/openclaw/`.**
3. **Dedup race** — `PostToDiscordAndMirror` and discordgo listener both insert. Fixed with `UNIQUE(discord_message_id)` + `INSERT OR IGNORE`.
4. **Discord REST error handling** — `createDiscordChannel` wasn't checking HTTP status codes. Fixed.
5. **`GetMayorMessageByDiscordID`** dedup check — was treating all errors as "exists". Fixed to check `sql.ErrNoRows` explicitly.
6. **Message timestamps** — listener was using `time.Now()` instead of Discord's `m.Timestamp`. Fixed.

### Architecture Decisions Made This Session
- **Two bots**: Mayor bot (OpenClaw discord.js) + Harness bot (discordgo listener, REST API, channel management). Avoids dual-gateway session conflicts.
- **Discord OAuth primary**: Users authenticate with Discord, giving us their Discord user ID for channel permission management. GitHub is optional linking for code attribution.
- **Private channels**: `permission_overwrites` on creation deny @everyone, allow both bots + creator. Invite system adds overwrites per invited user.
- **Mayor build API auth**: `X-Mayor-Secret` header with per-world secret stored in DB and embedded in skill `SKILL.md`.
- **Gateway health check**: `IsGatewayHealthy()` pings OpenClaw before routing prompts through Discord. Falls back to direct pipeline if unhealthy.

### Current Auth System (Being Replaced)
- `harness/internal/auth/auth.go` — GitHub OAuth with CSRF state cookie, 7-day sessions
- DB: `users` table has `github_id INTEGER UNIQUE NOT NULL`, `github_username TEXT NOT NULL`
- Roles: `admin` (first user), `user` (approved), `pending` (new signups)
- Dev mode: `POST /dev/auth/login` with arbitrary username
- Middleware chain: `SessionMiddleware` → `ApprovedMiddleware` → `AdminMiddleware`
- The middleware/session/role system stays the same — only the OAuth provider changes

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — the full implementation plan (5 phases, ~1900 lines)

## Action Items & Next Steps

1. **Deep research `context/openclaw/`** — This is the primary next step. Specifically verify:
   - How `openclaw agents add --non-interactive --json` works (exact CLI args, output format, what it creates)
   - How `openclaw config set bindings --json` works (REPLACE or APPEND? — critical for multi-world)
   - How `openclaw config get bindings --json` works (does it exist?)
   - Agent workspace isolation: does each agent truly get isolated SOUL.md, AGENTS.md, MEMORY.md, IDENTITY.md, USER.md?
   - Config hot-reload: how does the gateway pick up new agents without restart?
   - Discord adapter: how does binding route messages to specific agents?
   - MCP tool bridging: is `openclaw-claude-code-skill` useful or do we stick with curl-based skills?
   - Session management: does OpenClaw expose any APIs for checking agent status, conversation history?
   - Any features we missed that would simplify the plan

2. **Final plan review** — After OpenClaw research, do one more end-to-end read:
   - Check all code snippets for consistency with the two-bot / Discord OAuth changes
   - Verify Phase 3 `ProvisionAgent` has all necessary parameters after the Discord auth changes
   - Ensure the `world_invites` table and invite API are fully wired into the UI (Phase 5 chat component should show invite controls for world creator)
   - Check that dev mode auth (`HandleDevLogin`) is updated for Discord-primary schema

3. **Consider**: Should the plan include a "Phase 0" for Discord OAuth migration? It's currently bundled into Phase 2 with mayor/DB schema, but it's a significant standalone change that could be shipped and tested independently.

## Other Notes

### Discord API References Used
- Channel creation: `POST /guilds/{guild_id}/channels` with `permission_overwrites`
- Permission editing: `PUT /channels/{channel_id}/permissions/{overwrite_id}`
- Permission bits: `VIEW_CHANNEL=0x400`, `SEND_MESSAGES=0x800`, `READ_MESSAGE_HISTORY=0x10000`, `MANAGE_CHANNELS=0x10`
- OAuth2: `https://discord.com/oauth2/authorize` → `https://discord.com/api/oauth2/token` → `https://discord.com/api/users/@me`

### Key File Locations
- Auth: `harness/internal/auth/auth.go`, `harness/internal/auth/middleware.go`
- DB migrations: `harness/internal/db/migrations/`
- SQL queries: `harness/internal/db/queries/`
- World manager: `harness/internal/world/manager.go`
- Server routes: `harness/internal/server/server.go:119-171`
- Login view: `harness/views/login/login.templ`
- Build pipeline: `harness/internal/claude/claude.go`

### OpenClaw Reference Code Location
- `context/openclaw/` — gitignored, contains the actual OpenClaw source code for research
- This directory needs thorough exploration to validate the plan's assumptions about CLI behavior, agent isolation, and config management
