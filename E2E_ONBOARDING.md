# E2E Onboarding Flow — Full Application Test Playbook

End-to-end testing for the entire Creative Mode onboarding pipeline, from marketing site through world creation to the harness. Uses `playwright-cli` in headed mode on macOS against live production servers.

## Servers Under Test

| Server | URL | Auth |
|--------|-----|------|
| Marketing Site | `https://creative-mode.ai` | Discord OAuth → invite code → guild check |
| Harness | `https://claude-2.tailcdc985.ts.net` | Discord OAuth → admin approval |

Both share a single Discord bot and Discord OAuth application. The user authenticates separately on each server (different session cookies, different domains).

## Architecture: The Onboarding Pipeline

```
Marketing Site                         Harness
─────────────                         ───────
1. GET /
   └─ "Meet the Mayor" CTA
2. Discord OAuth dialog
   └─ Continue with Discord
3. GET /auth/discord/login
   └─ Discord OAuth flow
4. GET /auth/discord/callback
   └─ Session created
5. GET /join-discord (if not in guild)
   └─ "I've joined, check again"
6. GET /invite
   └─ Enter invite code
7. POST /invite
   └─ Redirect to /mayor
8. GET /mayor
   └─ Greeting seeded, chat UI shown
9. POST /mayor/chat (SSE)
   └─ Streaming conversation
10. WORLD_READY marker emitted
    ├─ Discord channel created
    ├─ Onboarding data pinned
    ├─ Webhook: POST /api/world-hatched ──► 11. Harness receives webhook
    └─ WorldHatched card shown                   ├─ World record created
       ├─ "Enter Your World" link ──────────►    ├─ OpenClaw agent provisioned
       └─ "Open Discord Channel" link            └─ Mayor bound to channel

                                       12. GET / (harness)
                                           └─ Discord OAuth → lobby
                                       13. World appears in lobby
                                       14. GET /world/:worldID
                                           └─ Game iframe + overlay
                                       15. GET /mayor/:worldID
                                           └─ Mayor dashboard
```

## Prerequisites

### 1. playwright-cli installed

```bash
just setup-playwright   # from project root
```

### 2. Discord test account

You need a real Discord account that:
- Is a member of the Creative Mode guild
- Can complete OAuth (not a bot account)

### 3. Valid invite code

One of the codes configured in the site's `INVITE_CODES` env var. Check with `ssh` or ask the admin.

### 4. Servers are running

```bash
# Marketing site health
curl -sf https://creative-mode.ai/health

# Harness health
curl -sf https://claude-2.tailcdc985.ts.net/health
```

---

## Session Management

We use two persistent sessions — one per server — to preserve cookies across test runs.

```bash
# Marketing site session
playwright-cli -s=site open https://creative-mode.ai --headed --persistent

# Harness session
playwright-cli -s=harness open https://claude-2.tailcdc985.ts.net --headed --persistent
```

With `--persistent`, the session cookie survives browser restarts. After one successful Discord OAuth login, subsequent runs reuse the session for up to 7 days (site) or 7 days (harness).

**Fresh session** (to re-test auth from scratch):

```bash
playwright-cli -s=site cookie-delete session
playwright-cli -s=harness cookie-delete session
```

---

## S1: Marketing Site — Landing Page

**URL**: `https://creative-mode.ai/`
**Source**: `site/pages/home.templ`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S1.1 | Navigate | `playwright-cli -s=site goto https://creative-mode.ai` | Page loads |
| S1.2 | Snapshot | `playwright-cli -s=site snapshot` | "Meet the Mayor" heading, hero section, features grid |
| S1.3 | Screenshot | `playwright-cli -s=site screenshot` | Dark theme, centered hero, CTA buttons |
| S1.4 | Console check | `playwright-cli -s=site console error` | No errors |
| S1.5 | Click CTA | `playwright-cli -s=site click <meet-mayor-btn-ref>` | Discord OAuth dialog appears |
| S1.6 | Snapshot dialog | `playwright-cli -s=site snapshot` | "Sign in with Discord" heading, "Continue with Discord" button |

**DOM structure**:
- "Meet the Mayor" button: `data-on:click="$discord_dialog_open = true"`
- Dialog: `data-show="$discord_dialog_open"` with "Continue with Discord" link to `/mayor`
- The `/mayor` link goes through `SessionMiddleware` → redirects to `/auth/discord/login` if no session

---

## S2: Discord OAuth Flow (Marketing Site)

**Trigger**: Clicking "Continue with Discord" in the dialog
**Source**: `site/internal/auth/auth.go`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S2.1 | Click Continue | `playwright-cli -s=site click <discord-continue-ref>` | Redirects to Discord OAuth |
| S2.2 | Screenshot | `playwright-cli -s=site screenshot` | Discord authorize page (or auto-redirect if previously authorized) |
| S2.3 | Authorize | `playwright-cli -s=site click <authorize-btn-ref>` | Redirects back to site |
| S2.4 | Wait for redirect | `playwright-cli -s=site screenshot` | Lands on `/join-discord` or `/invite` |

**Notes**:
- If already authorized on Discord, the OAuth page may auto-redirect (no button to click)
- After callback: if user is NOT in guild → `/join-discord`; if in guild → `/invite`
- With `--persistent`, after first OAuth the session cookie is stored. Future runs skip OAuth entirely.

**Expected redirect chain**:
```
/mayor → SessionMiddleware redirect → /auth/discord/login
→ Discord OAuth → /auth/discord/callback
→ 200 HTML with meta-refresh to /invite or /join-discord
```

---

## S3: Join Discord (if needed)

**URL**: `https://creative-mode.ai/join-discord`
**Source**: `site/pages/join_discord.templ`
**Middleware**: `SessionMiddleware` (requires session cookie)

Only reached if the user's Discord account is not a member of the guild.

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S3.1 | Snapshot | `playwright-cli -s=site snapshot` | "Join the Discord" heading, Discord invite link, "I've joined, check again" button |
| S3.2 | Screenshot | `playwright-cli -s=site screenshot` | Card with Discord join link + verification button |
| S3.3 | Click join link | Manual — open Discord invite in another tab | User joins guild |
| S3.4 | Click verify | `playwright-cli -s=site click <verify-btn-ref>` | POST /join-discord → redirects to /invite if verified |
| S3.5 | Verify redirect | `playwright-cli -s=site snapshot` | Now on /invite page |

**If verification fails**: error message "We couldn't find you in the server yet" appears. The bot checks membership via `GET /guilds/{id}/members/{userId}`.

---

## S4: Invite Code Entry

**URL**: `https://creative-mode.ai/invite`
**Source**: `site/pages/invite.templ`
**Middleware**: `SessionMiddleware` (requires session cookie)

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S4.1 | Snapshot | `playwright-cli -s=site snapshot` | "Enter Invite Code" heading, code input, "Continue" button |
| S4.2 | Screenshot | `playwright-cli -s=site screenshot` | Clean card with form |
| S4.3 | Fill invalid code | `playwright-cli -s=site fill <code-input-ref> "wrong-code"` | Input populated |
| S4.4 | Submit invalid | `playwright-cli -s=site click <continue-btn-ref>` | Error: "Invalid invite code" |
| S4.5 | Screenshot error | `playwright-cli -s=site screenshot` | Error message visible in red |
| S4.6 | Fill valid code | `playwright-cli -s=site fill <code-input-ref> "<INVITE_CODE>"` | Input populated |
| S4.7 | Submit valid | `playwright-cli -s=site click <continue-btn-ref>` | Redirects to /mayor |
| S4.8 | Verify redirect | `playwright-cli -s=site snapshot` | "Meet the Mayor" heading, chat UI |

**DOM**: Standard HTML form (`<form method="POST" action="/invite">`), input `name="code"`, standard submit.

---

## S5: Meet the Mayor — Chat UI

**URL**: `https://creative-mode.ai/mayor`
**Source**: `site/pages/mayor.templ`, `site/internal/mayor/handler.go`
**Middleware**: `SessionMiddleware` → `GuildMemberMiddleware` → `InviteCodeMiddleware`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S5.1 | Snapshot | `playwright-cli -s=site snapshot` | "Meet the Mayor" heading, greeting message from mayor ("Hey <username>..."), input field, "Send" button |
| S5.2 | Screenshot | `playwright-cli -s=site screenshot` | Chat UI with mayor greeting in card, input at bottom |
| S5.3 | Console check | `playwright-cli -s=site console error` | No errors |

**Mayor greeting** (seeded on page load):
> "Hey {username}. I'm the Mayor — though I don't have a real name yet. I just came online and this world is... empty. Which is actually kind of exciting. So. What are we building?"

**DOM structure**:
- `#chat-messages` — scrollable container with message cards
- Mayor messages: `.rounded-lg.border` card with "M" avatar
- Input: `data-bind:mayor_input`, `data-on:keydown` (Enter to send)
- Send button: `data-on:click` → `@post('/mayor/chat')`
- `#mayor-signup` — empty div, populated with WorldHatched card after hatch

---

## S6: Mayor Conversation — World Design

**URL**: `https://creative-mode.ai/mayor` (SSE via `POST /mayor/chat`)
**Source**: `site/internal/mayor/handler.go`

The mayor collects four things: **world setting**, **gameplay**, **world name**, **mayor name**.

### Sending Messages

Datastar's `data-bind:mayor_input` may not sync with playwright `fill`. Two approaches:

**Approach A — Direct fill + click** (try first):
```bash
playwright-cli -s=site fill <input-ref> "A cyberpunk city with neon lights"
playwright-cli -s=site click <send-btn-ref>
```

**Approach B — page.evaluate fetch** (if signals don't sync):
```bash
playwright-cli -s=site run-code "async page => {
  await page.evaluate(async () => {
    await fetch('/mayor/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({datastar: {mayor_input: 'A cyberpunk city with neon lights'}})
    });
  });
}"
```

### Test Steps

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S6.1 | Send setting msg | fill + click (or fetch) | User message appears, "Thinking..." shown, then streaming response |
| S6.2 | Wait for response | `playwright-cli -s=site snapshot` (after ~5-10s) | Mayor response rendered with markdown |
| S6.3 | Screenshot | `playwright-cli -s=site screenshot` | Both user + mayor messages visible |
| S6.4 | Send gameplay msg | fill + click | Second user message + response |
| S6.5 | Continue conversation | Repeat 2-3 more messages | Mayor asks for world name and mayor name |
| S6.6 | Console check | `playwright-cli -s=site console error` | No errors during streaming |

**Conversation flow** (typical 4-6 messages):
1. User describes the world setting
2. Mayor asks about gameplay
3. User describes gameplay
4. Mayor suggests a name or asks for one
5. User confirms name + mayor name
6. Mayor emits `WORLD_READY|<mayor_name>|<world_name>|<summary>` (hidden from user)

**Timing**: Each Claude response takes 5-15 seconds to stream. Use snapshots/screenshots to check progress rather than waiting for a specific element.

**Scripted fallback**: If the Anthropic API returns 402/429/529, the conversation silently falls back to scripted responses (`site/internal/mayor/scripted.go`). The test flow is identical — just faster responses.

---

## S7: World Hatching

**Trigger**: Mayor emits `WORLD_READY|<mayor>|<world>|<summary>` in its response
**Source**: `site/internal/mayor/handler.go:hatchWorld()`

After the final mayor response, the server:
1. Creates a Discord channel in the "Worlds" category
2. Sends a welcome message to the channel
3. Pins onboarding conversation data
4. Fires webhook to harness (`POST /api/world-hatched`)
5. Patches `WorldHatched` card into `#mayor-signup`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S7.1 | Wait for hatch | `playwright-cli -s=site snapshot` (after final mayor msg) | `#mayor-signup` contains WorldHatched card |
| S7.2 | Screenshot | `playwright-cli -s=site screenshot` | Card shows: world name, mayor name, avatar, "Enter Your World" + "Open Discord Channel" links |
| S7.3 | Verify links | `playwright-cli -s=site snapshot` | "Enter Your World" → `https://claude-2.tailcdc985.ts.net`, "Open Discord Channel" → Discord URL |
| S7.4 | Console check | `playwright-cli -s=site console error` | No errors |

**WorldHatched card DOM** (`site/pages/mayor_fragments.templ`):
- `#mayor-signup` — container
- `<h2>` — "{worldName} Has Been Born"
- `<strong>` — mayor name
- "Enter Your World" link → `harnessURL` (https://claude-2.tailcdc985.ts.net)
- "Open Your World in Discord" link → `discord.com/channels/{guildID}/{channelID}`

---

## S8: Discord Channel Verification

**Manual step** — verify the Discord side effects of world hatching.

| # | Check | How | Pass Criteria |
|---|-------|-----|---------------|
| S8.1 | Channel exists | Open Discord → "Worlds" category | New channel named after the world |
| S8.2 | Welcome message | Read channel | Welcome message mentioning creator |
| S8.3 | Pinned data | Check pinned messages | JSON onboarding data pinned |
| S8.4 | Permissions | Check channel settings | Creator has access, channel is private |

---

## S9: Harness — Discord OAuth

**URL**: `https://claude-2.tailcdc985.ts.net`
**Source**: `harness/internal/auth/auth.go`

The harness has its own Discord OAuth flow (separate session from the marketing site).

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S9.1 | Navigate | `playwright-cli -s=harness goto https://claude-2.tailcdc985.ts.net` | Login page or lobby (if session exists) |
| S9.2 | Snapshot | `playwright-cli -s=harness snapshot` | If logged out: "Creative Mode" heading, "Sign in with Discord" link |
| S9.3 | Click login | `playwright-cli -s=harness click <login-ref>` | Discord OAuth flow |
| S9.4 | Authorize | `playwright-cli -s=harness click <authorize-ref>` | Redirects to harness |
| S9.5 | Screenshot | `playwright-cli -s=harness screenshot` | Lobby page (if approved) or pending page |
| S9.6 | Console check | `playwright-cli -s=harness console error` | No errors (except favicon 404) |

**Note**: New users land on a "pending" page until an admin approves them. If testing with a new Discord account, an admin must approve via `/admin/users`.

---

## S10: Harness — Lobby + World Verification

**URL**: `https://claude-2.tailcdc985.ts.net` (authenticated, approved)
**Source**: `harness/views/lobby/lobby.templ`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S10.1 | Snapshot | `playwright-cli -s=harness snapshot` | "Worlds" heading, world list, chat panel |
| S10.2 | Find hatched world | `playwright-cli -s=harness snapshot` | World created in S7 appears in the list |
| S10.3 | Screenshot | `playwright-cli -s=harness screenshot` | Lobby with world cards |
| S10.4 | SSE connected | `playwright-cli -s=harness network` | Active GET to `/events` |
| S10.5 | Click world | `playwright-cli -s=harness click <world-card-ref>` | Navigates to `/world/<id>` |

**Note**: The world created by the site webhook (`/api/world-hatched`) should appear in the lobby. If it doesn't, check:
- Harness logs: `ssh` into VPS and `journalctl -u creative-mode`
- Webhook secret mismatch between site and harness
- Network connectivity between EC2 (site) and VPS (harness) over Tailscale

---

## S11: Harness — World View

**URL**: `https://claude-2.tailcdc985.ts.net/world/<worldID>`
**Source**: `harness/views/world/world.templ`, `harness/views/world/overlay.templ`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S11.1 | Snapshot | `playwright-cli -s=harness snapshot` | `#game-frame` iframe, `#harness-overlay`, chat tabs |
| S11.2 | Screenshot | `playwright-cli -s=harness screenshot` | World page with overlay |
| S11.3 | SSE connected | `playwright-cli -s=harness network` | Active GET to `/world/<id>/events` |
| S11.4 | Chat tabs | `playwright-cli -s=harness snapshot` | "Global", "World", "Lineage" tabs |
| S11.5 | Prompt bar | `playwright-cli -s=harness snapshot` | "Describe what to build..." input, "Build" button |
| S11.6 | Console check | `playwright-cli -s=harness console error` | No errors |

---

## S12: Harness — Mayor Dashboard

**URL**: `https://claude-2.tailcdc985.ts.net/mayor/<worldID>`
**Source**: `harness/internal/server/mayor_dashboard.go`, `harness/views/mayor/dashboard.templ`

| # | Action | Command | Pass Criteria |
|---|--------|---------|---------------|
| S12.1 | Navigate | `playwright-cli -s=harness goto https://claude-2.tailcdc985.ts.net/mayor/<worldID>` | Dashboard loads |
| S12.2 | Snapshot | `playwright-cli -s=harness snapshot` | Dashboard with tabs: builds, activity, messages, sessions |
| S12.3 | Screenshot | `playwright-cli -s=harness screenshot` | Dashboard rendered |
| S12.4 | Check workspace files | Click "Memory" or file tab | SOUL.md visible with onboarding conversation |
| S12.5 | Console check | `playwright-cli -s=harness console error` | No errors |

**Note**: The mayor dashboard requires Phase 7 of the master plan to be implemented. If not yet built, this section can be skipped.

---

## Cross-System Verification

These checks verify the full pipeline worked end-to-end:

| # | Check | How | Pass Criteria |
|---|-------|-----|---------------|
| X1 | World in harness DB | Harness lobby shows the hatched world | World name matches |
| X2 | Discord channel linked | World has Discord channel association | Channel URL in world metadata |
| X3 | OpenClaw agent | `ssh` to VPS → `openclaw agents list` | Agent for world exists |
| X4 | Mayor responds | Type in Discord channel | Mayor responds with personality from onboarding |
| X5 | Webhook trace | Check harness logs | `world-hatched` webhook received and processed |

---

## Error Cases

### E1: Expired Session

```bash
playwright-cli -s=site cookie-delete session
playwright-cli -s=site goto https://creative-mode.ai/mayor
# Expected: redirect to /auth/discord/login
```

### E2: Missing Guild Membership

Test with a Discord account that's not in the guild:
```bash
# After OAuth, should land on /join-discord
playwright-cli -s=site snapshot
# "Join the Discord" heading visible
```

### E3: Invalid Invite Code

```bash
playwright-cli -s=site goto https://creative-mode.ai/invite
playwright-cli -s=site fill <code-ref> "invalid"
playwright-cli -s=site click <submit-ref>
# "Invalid invite code" error message
```

### E4: Rate Limiting (Mayor Chat)

Send two messages within 2 seconds:
```bash
playwright-cli -s=site click <send-ref>
# Immediately:
playwright-cli -s=site fill <input-ref> "another message"
playwright-cli -s=site click <send-ref>
# "Please wait a moment" error in #rate-limit-error
```

### E5: API Fallback (Scripted Mode)

When the Anthropic API returns 402/429/529, the conversation falls back to scripted responses. The flow is identical but responses are pre-written. This is transparent to the user and harder to trigger on purpose, but can be verified by checking that world hatching still works when the API is degraded.

---

## Cleanup

```bash
playwright-cli -s=site close
playwright-cli -s=harness close
```

World cleanup:
- Discord channels can be deleted manually
- Harness world records persist in SQLite (no automated cleanup)
- OpenClaw agent workspaces persist at `$OPENCLAW_HOME/workspaces/`

---

## Quick Reference

### Onboarding Funnel Routes (Marketing Site)

| Step | Route | Method | Auth Gate | Source |
|------|-------|--------|-----------|--------|
| Landing | `/` | GET | None | `site/pages/home.templ` |
| OAuth start | `/auth/discord/login` | GET | None | `site/internal/auth/auth.go` |
| OAuth callback | `/auth/discord/callback` | GET | None | `site/internal/auth/auth.go` |
| Join Discord | `/join-discord` | GET/POST | Session | `site/pages/join_discord.templ` |
| Invite code | `/invite` | GET/POST | Session | `site/pages/invite.templ` |
| Mayor chat | `/mayor` | GET | Session + Guild + Invite | `site/pages/mayor.templ` |
| Mayor SSE | `/mayor/chat` | POST | Session + Guild + Invite | `site/internal/mayor/handler.go` |

### Harness Routes

| Step | Route | Method | Auth Gate | Source |
|------|-------|--------|-----------|--------|
| Login | `/` | GET | None (or session → lobby) | `harness/views/login/login.templ` |
| OAuth | `/auth/discord/*` | GET | None | `harness/internal/auth/auth.go` |
| Lobby | `/` | GET | Approved | `harness/views/lobby/lobby.templ` |
| World view | `/world/:worldID` | GET | Approved | `harness/views/world/world.templ` |
| World SSE | `/world/:worldID/events` | GET | Approved | `harness/internal/server/server.go` |
| Mayor dashboard | `/mayor/:worldID` | GET | Approved | `harness/views/mayor/dashboard.templ` |
| World hatched | `/api/world-hatched` | POST | Hook secret | `harness/internal/server/mayor_api.go` |

### Session Cookie Details

| Server | Cookie name | TTL | Secure | SameSite |
|--------|-------------|-----|--------|----------|
| Marketing site | `session` | 7 days | Yes (HTTPS) | Lax |
| Harness | `session` | 7 days | Yes (HTTPS) | Lax |

### Key DOM Selectors

| Element | Selector / Signal | Page |
|---------|-------------------|------|
| Discord dialog | `$discord_dialog_open` | Home |
| Invite code input | `input[name="code"]` | Invite |
| Guild verify button | `button[type="submit"]` | Join Discord |
| Mayor chat input | `data-bind:mayor_input` | Mayor |
| Chat messages | `#chat-messages` | Mayor |
| World hatched card | `#mayor-signup` | Mayor (after hatch) |
| Rate limit error | `#rate-limit-error` | Mayor |

### Datastar + Playwright Interop

For the mayor chat, Datastar's `data-bind:mayor_input` may not update when playwright uses `fill`. If messages don't send after fill + click, use `page.evaluate(fetch(...))`:

```bash
playwright-cli -s=site run-code "async page => {
  await page.evaluate(async () => {
    await fetch('/mayor/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({datastar: {mayor_input: 'your message here'}})
    });
  });
}"
```

Note: This bypasses SSE rendering. The response will be returned but not rendered to the page. For visual testing, prefer fill + click and fall back to fetch only if signals don't sync.
