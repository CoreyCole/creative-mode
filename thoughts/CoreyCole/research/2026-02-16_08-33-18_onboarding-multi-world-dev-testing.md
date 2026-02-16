---
date: 2026-02-16T08:33:18-08:00
researcher: CoreyCole
git_commit: 829d88abce4ee7b1121712ee02afed5ccc0b34a1
branch: main
repository: creative-mode
topic: "Debugging Onboarding Flow E2E — Multi-World Hatching for Dev Testing"
tags: [research, codebase, onboarding, dev-mode, multi-world, site, hatching]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: Debugging Onboarding Flow E2E — Multi-World Hatching for Dev Testing

**Date**: 2026-02-16T08:33:18-08:00
**Researcher**: CoreyCole
**Git Commit**: 829d88abce4ee7b1121712ee02afed5ccc0b34a1
**Branch**: main
**Repository**: creative-mode

## Research Question

How can we debug the site onboarding flow end-to-end with dev credentials, and allow the same Discord user to hatch multiple worlds for repeated testing? Should we add a dev-mode flag, or allow multi-world hatching in general?

## Summary

**There is NO persistent constraint preventing a Discord user from owning multiple worlds.** The harness `worlds` table has no unique constraint on `created_by`, and the code has no per-user world limit check. The only practical barrier is that the site's `ConversationManager` persists the chat history in SQLite keyed by `discord_id` — so when a user returns to `/mayor`, they see their old conversation instead of starting fresh.

**Recommended approach: Add a dev-mode "Reset Conversation" endpoint** that clears the user's chat history (SQLite + in-memory transient state), enabling repeated E2E testing of the full onboarding flow without server restarts or username changes. The harness and Discord both already support multiple worlds per user — no changes needed there.

## Detailed Findings

### Current Onboarding Flow (Site)

The site (`site/`) implements a 5-step funnel:

| Step | Route | Gate |
|------|-------|------|
| 1. Landing | `GET /` | None |
| 2. Discord OAuth | `/auth/discord/login` → `/auth/discord/callback` | None |
| 3. Guild Membership | `GET/POST /join-discord` | SessionMiddleware |
| 4. Invite Code | `GET/POST /invite` | SessionMiddleware |
| 5. Mayor Chat | `GET /mayor`, `POST /mayor/chat` | Session + Guild + Invite middleware |

In dev mode (`DEV_MODE=true`), `POST /dev/auth/login` creates a session with:
- Deterministic fake Discord ID: `dev-{fnv32(username)}` (`site/internal/auth/auth.go:316-320`)
- `GuildMemberVerified: true` — bypasses guild check
- `InviteCodeVerified: true` — bypasses invite code
- Redirects directly to `/mayor`

### Why The Same User Can't Easily Re-Hatch

After hatching, three things persist:

1. **Conversation messages in SQLite** (`conversation_messages` table, keyed by `discord_id`) — `site/internal/db/db.go:49-55`
2. **In-memory transient state** (`ConversationManager.transient` map) — `pkg/mayorchat/conversation.go:36`
3. **The `Hatched` flag** — but this resets to `false` after each successful hatch via `ClearWorldReady` (`conversation.go:185`)

When the user returns to `GET /mayor`, `convMgr.GetMessages(session.DiscordID)` returns the old conversation (`site/main.go:310`), so the greeting is not re-seeded and the user continues from where they left off. There's no "start over" mechanism.

### What's NOT a Barrier

| Layer | Constraint | Multi-World Impact |
|-------|-----------|-------------------|
| Harness DB | No unique constraint on `worlds.created_by` | Multiple worlds allowed |
| Harness code | `ProvisionFromWebhook` only deduplicates by `discord_channel_id` (`mayor.go:72`) | Different channels = new worlds |
| Discord | Channels can have identical names | No conflict |
| Site `SetHatched()` | Resets to `false` after successful hatch (`conversation.go:185`) | Not a persistent block |
| Mayor name uniqueness | Appends roman numeral suffix if taken (`handler.go:362-371`) | Auto-resolved |

### Approach Analysis

#### Option A: Dev-Mode "Reset Conversation" Endpoint (Recommended)

Add to the site:

1. **`DeleteUserMessages(userID string)` on `MessageStore` interface** (`pkg/mayorchat/conversation.go:11-15`) — new method to clear a specific user's messages from SQLite
2. **`SQLiteMessageStore.DeleteUserMessages()`** (`site/internal/mayor/store.go`) — `DELETE FROM conversation_messages WHERE discord_id = ?`
3. **`ConversationManager.ResetConversation(userID string)`** — clears both DB messages and in-memory transient state
4. **`POST /dev/reset-conversation` route** (dev mode only) — calls `ResetConversation(session.DiscordID)`, redirects to `/mayor`
5. **"Start Over" button on `/mayor` page** (dev mode only) — posts to the reset endpoint

**Pros**: Simple (~30 lines of code), non-destructive, tests the real E2E flow repeatedly, doesn't require server restarts, works with production harness.

**Cons**: Dev-only feature, doesn't help production users who might want to create a second world (but that's a separate product decision).

#### Option B: Allow Multi-World in Production

Since there's already no DB constraint preventing it, the only change needed is giving users a way to start a new conversation after hatching. This could be:
- A "Create Another World" button on the `WorldHatched` card
- Automatic conversation reset after hatching

**Pros**: Users can create multiple worlds.
**Cons**: Product implications (cost of Claude API calls, Discord channel sprawl, mayor agent provisioning). Better as a separate product decision.

#### Option C: Different Dev Usernames (Workaround, No Code Changes)

Since `dev-{fnv32(username)}` is deterministic, logging in as "testuser1", "testuser2", etc. creates completely independent identities.

**Pros**: Zero code changes.
**Cons**: Doesn't test the same-user-multi-world scenario, clutters Discord with channels for fake users, can't test conversation persistence.

### Dev Mode Testing Topology

```
Local Machine                         VPS (Production)
┌─────────────────┐                  ┌─────────────────┐
│ Site (Docker)    │                  │ Harness (air)    │
│ DEV_MODE=true    │─── webhook ────▶│ /api/world-hatched│
│ localhost:3000   │  (Tailscale)    │ 100.x.x.x:8080  │
│                  │                  │                  │
│ Dev Login ───────│──▶ /mayor       │ Creates world    │
│ (no Discord     │    (chat flow)   │ Provisions mayor │
│  OAuth needed)  │                  │ (OpenClaw agent) │
└─────────────────┘                  └─────────────────┘
        │                                    │
        │ Creates Discord channel            │ Reads onboarding
        ▼ (via bot token)                    ▼ (from pinned msg)
    ┌──────────┐
    │ Discord  │
    │ REST API │
    └──────────┘
```

**Required env vars for local site testing**:
- `DEV_MODE=true`
- `DISCORD_BOT_TOKEN` — for channel creation
- `DISCORD_GUILD_ID` — for guild membership (auto-bypassed in dev)
- `DISCORD_WORLDS_CATEGORY_ID` — for channel creation
- `ANTHROPIC_API_KEY` — for Claude chat (or omit for scripted fallback)
- `HARNESS_URL=http://100.x.x.x:8080` — VPS Tailscale IP
- `CM_HOOK_SECRET` — shared secret with harness
- `INVITE_CODES` — not needed (bypassed by dev login)
- `GEMINI_API_KEY` — optional, for cover art generation

## Code References

- `site/main.go:66` — `DEV_MODE` env var check
- `site/main.go:181-183` — Dev login route registration
- `site/internal/auth/auth.go:276-313` — `HandleDevLogin` (fake Discord ID, auto-verify gates)
- `site/internal/auth/auth.go:316-320` — `devDiscordID()` — FNV-32a hash
- `site/internal/mayor/handler.go:279-354` — `prepareCoverArtAndHatch()` — hatching entry point
- `site/internal/mayor/handler.go:358-421` — `hatchWorldWithCover()` — Discord channel + webhook
- `site/internal/mayor/handler.go:550-593` — `notifyHarnessWorldHatchedWithCover()` — fires webhook to harness
- `site/internal/mayor/store.go:10-55` — `SQLiteMessageStore` — conversation persistence
- `pkg/mayorchat/conversation.go:11-15` — `MessageStore` interface (missing `DeleteUserMessages`)
- `pkg/mayorchat/conversation.go:162-192` — `SetHatched()` / `ClearWorldReady()` — in-memory hatch guard
- `pkg/mayorchat/conversation.go:206-223` — cleanup loop (24-hour stale conversation removal)
- `harness/internal/server/mayor_api.go:16-79` — `/api/world-hatched` webhook handler
- `harness/internal/mayor/mayor.go:54-191` — `ProvisionFromWebhook()` — world creation + agent provisioning
- `harness/internal/db/migrations/001_initial.sql:20-26` — `worlds` table schema (no unique on `created_by`)

## Architecture Insights

- **No persistent one-world-per-user limit exists anywhere** — this is an accidental product constraint caused by conversation stickiness, not an intentional design decision.
- **The site and harness are architecturally decoupled** — the site handles Discord channel creation and fires a webhook; the harness handles world DB records and agent provisioning. Multi-world support requires no harness changes.
- **Dev mode is well-designed but incomplete** — it bypasses OAuth, guild check, and invite codes, but doesn't provide a way to reset the conversation for repeated testing. This is the gap to fill.
- **The `ConversationManager` splits state across two stores** — persistent messages in SQLite via `MessageStore`, transient flags in memory. Both must be cleared for a clean reset.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/handoffs/general/2026-02-16_01-52-35_debug-onboarding-discord-channel.md` — Recent debugging of onboarding channel creation issues
- `thoughts/CoreyCole/handoffs/general/2026-02-11_12-37-55_dev-auth-multi-user-testing.md` — Previous dev auth implementation for E2E testing
- `thoughts/CoreyCole/plans/2026-02-14_03-02-42_meet-the-mayor-site-page.md` — Original Meet the Mayor implementation plan
- `thoughts/CoreyCole/research/2026-02-16_08-44-34_launch-readiness-audit.md` — Launch readiness audit covering onboarding flow

## Open Questions

1. **Should multi-world be a production feature?** Currently it's architecturally supported but there's no UI to start a new conversation. This is a product decision (API cost, Discord sprawl, etc.)
2. **Should the reset button also appear in production?** Could be useful as "Create Another World" post-hatch, but needs product consideration.
3. **Conversation cleanup timing** — The 24-hour cleanup in `ConversationManager.cleanupLoop()` means old conversations auto-delete. Should dev mode shorten this, or is the explicit reset button sufficient?
