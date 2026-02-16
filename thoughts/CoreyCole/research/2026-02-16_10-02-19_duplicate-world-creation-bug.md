---
date: 2026-02-16T10:02:19-08:00
researcher: CoreyCole
git_commit: c2a845e5b7f4c3be4dea9fa48119fa5e217b8964
branch: main
repository: creative-mode
topic: "Duplicate world creation bug — onboarding site flow"
tags: [research, codebase, onboarding, mayor, world-creation, deduplication, site]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: Duplicate World Creation Bug — Onboarding Site Flow

**Date**: 2026-02-16T10:02:19-08:00
**Researcher**: CoreyCole
**Git Commit**: c2a845e5b7f4c3be4dea9fa48119fa5e217b8964
**Branch**: main
**Repository**: creative-mode

## Research Question

We have a bug where multiple worlds can be created for the same user. Does the onboarding site check if a Discord user has already created a world? How does the "Create World" button interact with Claude's autonomous world creation? Can we disable the button as soon as Claude starts creating the world, and re-enable it if the mayor decides they aren't ready?

## Summary

**The "Create World" button is NOT disabled when Claude autonomously decides to create a world.** The button only disables during an active SSE request (`$_sending` indicator). After Claude's response completes and `prepareCoverArtAndHatch` runs, the SSE stream ends, `_sending` goes back to `false`, and the button becomes clickable again — even though hatching is already in progress.

There is **no persistent check** that a Discord user has already created a world. The only deduplication is an in-memory `Hatched` boolean flag that resets on server restart. A user can create multiple worlds by reloading the page.

### Recommended Fix

1. **Add a `world_creating` signal** — When Claude emits `WORLD_READY` (or when the user clicks "Create World"), patch a signal like `$world_creating = true` to disable the button immediately. The button should check `$_sending || $world_creating`.

2. **If the mayor is not ready** (no `WORLD_READY` marker emitted), don't patch the signal — the button stays enabled by default.

3. **Persistent dedup** — Check Discord channels (via `worldchannel.Client`) on page load to see if the user already has a channel. If so, skip the chat and show the "world already exists" card.

## Detailed Findings

### 1. How World Creation Is Triggered

Claude decides autonomously to create a world by emitting a `WORLD_READY|<mayor>|<world>|<summary>` text marker in its response. This is NOT a tool/function call — it's a plain-text convention in the system prompt (`pkg/mayorchat/prompt.go:78-80`).

The "Create World" button (`site/pages/mayor.templ:73-80`) sets `$create_world = true` and POSTs to `/mayor/chat`. The handler reads this signal and:
- Replaces the user's message with "I'm ready -- let's create the world!" (`handler.go:81-83`)
- Appends `ForceCreatePromptSuffix` to the system prompt, which tells Claude to fill in gaps and emit the marker (`prompt.go:94-98`)

So there are two paths to WORLD_READY: Claude decides organically, or user forces it via button.

### 2. Current Button State Management

The "Create World" button at `site/pages/mayor.templ:73-80`:
```html
<button
    data-on:click={ `$create_world = true; ` + string(datastar.PostSSE("/mayor/chat")) }
    data-indicator:_sending
    data-attr:disabled="$_sending"
    class="..."
>
    Create World
</button>
```

The button is disabled ONLY while `$_sending` is true (during an active SSE request). Once the SSE stream ends — even if `prepareCoverArtAndHatch` was called and is creating a Discord channel — `_sending` resets to `false` and the button becomes clickable again.

**The Send button and input field have the same issue** — they also only check `$_sending`, so a user could type another message during the cover art generation or hatch flow.

### 3. Current Deduplication Layers

| Layer | Scope | Location | Mechanism | Limitation |
|-------|-------|----------|-----------|------------|
| `SetHatched()` | Same session, same process | `pkg/mayorchat/conversation.go:162-171` | In-memory mutex boolean, first-caller-wins | Resets on server restart |
| `GetWorldByDiscordChannel` | Same Discord channel | `harness/internal/mayor/mayor.go:72-82` | DB lookup, skips if exists | Doesn't prevent new channel creation |
| `CheckMayorNameUnique` | Mayor name across guild | `pkg/worldchannel/uniqueness.go:35-60` | Discord channel topic scan | Only prevents name collision, not user dedup |
| **None** | Same user, multiple worlds | N/A | **Not implemented** | Users can create unlimited worlds |

### 4. The `SetHatched` Flow

When `WORLD_READY` is detected (`handler.go:269-271`):
```go
if worldInfo != nil {
    h.prepareCoverArtAndHatch(c, sse, session, ...)
}
```

Inside `prepareCoverArtAndHatch` (`handler.go:281`):
```go
if !h.convMgr.SetHatched(session.DiscordID) {
    h.logger.Warn("duplicate hatch attempt blocked", "user", session.DiscordID)
    return
}
```

This prevents concurrent requests within the same server process, but:
- The button isn't disabled on the frontend, so the user can still click it
- After server restart, the flag resets
- A page reload starts a fresh conversation

### 5. What the Site Database Knows

The site's SQLite schema (`site/internal/db/db.go:48-55`) has:
- `conversation_messages` — keyed by `discord_id`, stores chat messages
- `metrics_snapshots` — has a `worlds_hatched` counter (for analytics)
- **No `worlds` table** and **no per-user "has hatched" persistent flag**

The site does NOT track which users have created worlds. It relies entirely on the in-memory `transientState.Hatched` flag.

### 6. The SSE Stream Lifecycle

The SSE stream for `/mayor/chat` ends when `HandleChat` returns. This happens AFTER `prepareCoverArtAndHatch` completes all its SSE patches (cover art spinner, cover art preview, or hatched card). The `_sending` indicator goes back to `false` at this point.

However, if the cover art generation takes time (3-10 seconds), the stream stays open during that time. But after the response ends, the button becomes clickable again.

For the cover art preview flow:
1. Claude emits WORLD_READY → `prepareCoverArtAndHatch` called
2. Cover art generating spinner shown via SSE → user waits
3. Cover art preview with "Hatch World" button shown → SSE stream ends → `_sending` = false
4. "Create World" button becomes clickable again (BUG)
5. User could click "Create World" again, but `SetHatched()` would block it on the server

### 7. Discord Channel as Persistent State

The site could check for existing worlds by querying Discord channels. `worldchannel.Client.ListExistingMayors()` (`pkg/worldchannel/uniqueness.go:63-83`) already iterates all guild channels — it would be straightforward to also check if a channel was created by the current user (the channel topic includes the creator info, or we could check permission overwrites).

## Code References

- `site/pages/mayor.templ:73-80` — "Create World" button with `$_sending` only
- `site/pages/mayor.templ:47` — Datastar signals: `{"mayor_input": "", "create_world": false}`
- `site/internal/mayor/handler.go:63-274` — `HandleChat` SSE handler
- `site/internal/mayor/handler.go:75-86` — `create_world` signal reading and force-create
- `site/internal/mayor/handler.go:104-107` — Signal patching (clears input + create_world)
- `site/internal/mayor/handler.go:255-271` — WORLD_READY detection and `prepareCoverArtAndHatch` call
- `site/internal/mayor/handler.go:279-354` — `prepareCoverArtAndHatch` with `SetHatched` guard
- `site/internal/mayor/handler.go:358-421` — `hatchWorldWithCover` (Discord channel + webhook)
- `site/internal/mayor/handler.go:512-546` — `HandleHatch` (cover art confirm button)
- `pkg/mayorchat/conversation.go:17-29` — `transientState` struct (in-memory, resets on restart)
- `pkg/mayorchat/conversation.go:162-171` — `SetHatched()` atomic guard
- `pkg/mayorchat/prompt.go:78-80` — WORLD_READY marker instruction in system prompt
- `pkg/mayorchat/prompt.go:94-98` — `ForceCreatePromptSuffix` for force-create
- `pkg/mayorchat/stream.go:21-57` — `ParseWorldReady` marker parser
- `site/internal/db/db.go:48-55` — Site DB schema (no worlds table)
- `harness/internal/db/migrations/001_initial.sql:20-26` — Harness worlds table (no unique constraint on `created_by`)

## Architecture Insights

1. **The "Create World" button lives in the input area** (`mayor.templ:73`), separate from the `#mayor-signup` placeholder div (`mayor.templ:30`) where the hatched card appears. Patching `#mayor-signup` with a cover art preview or hatched card does NOT affect the button's visibility or disabled state.

2. **All UI state flows through Datastar signals and SSE patches.** To disable the button when hatching starts, the server should patch a new signal (e.g., `$world_creating`) via `sse.MarshalAndPatchSignals`. The button would check `$_sending || $world_creating`.

3. **The site has no persistent world tracking.** The site DB has `conversation_messages` and `metrics_snapshots` but no `worlds` table. Any persistent dedup would need to either: (a) add a column/table to the site DB, (b) query Discord channels for existing user worlds, or (c) query the harness via API.

4. **The fire-and-forget webhook means the site doesn't know if the harness successfully created the world.** The webhook to `/api/world-hatched` runs in a goroutine and errors are only logged. Adding a persistent flag on the site side would be the most reliable approach.

## Open Questions

1. Should we add a `hatched_discord_id` column to the site DB to persistently track which users have hatched?
2. Should the "Create World" button be hidden entirely after a certain number of messages (e.g., after 4+ exchanges)?
3. Should we also disable the Send button and input field during the hatching flow, or just the "Create World" button?
4. For the cover art preview flow, should the entire input area be replaced/hidden when the cover art preview is shown?
