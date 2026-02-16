---
date: 2026-02-16T08:44:34+0000
researcher: CoreyCole
git_commit: 59c61aab63c7df584cf0d6c2508836274933afe3
branch: main
repository: creative-mode
topic: "Launch Readiness Audit: Marketing Site, Onboarding Flow, and Discord Messaging"
tags: [research, codebase, site, onboarding, discord, launch, security, traffic]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: Launch Readiness Audit

**Date**: 2026-02-16T08:44:34+0000
**Researcher**: CoreyCole
**Git Commit**: 59c61aab63c7df584cf0d6c2508836274933afe3
**Branch**: main
**Repository**: creative-mode

## Research Question
We are releasing to the public tomorrow. We need to: (1) ensure the marketing site can handle increased traffic, (2) find all bugs in the onboarding experience from mayor chat to Discord, and (3) review Discord message formatting -- currently raw JSON is posted, we want file attachments and human-friendly messages instead.

## Summary

The audit found **14 traffic/scalability issues**, **10 onboarding bugs/edge cases**, and **1 major Discord formatting improvement** (pinned onboarding data as raw JSON). The most critical issues for launch day are: SQLite `MaxOpenConns(1)` serializing all DB operations, no server timeouts (Slowloris vulnerability), wildcard CORS, missing security headers, Datastar JS loaded from unpinned CDN `@main` branch, and the ability for users to hatch unlimited worlds.

---

## Detailed Findings

### Area 1: Traffic & Scalability

#### CRITICAL: SQLite MaxOpenConns(1)
- **File**: `site/internal/db/db.go:23`
- `db.SetMaxOpenConns(1)` serializes ALL database operations through a single connection
- Every authenticated request hits `GetSession()`, every chat hits `AddMessage()` + `GetMessages()`
- Under concurrent users, this becomes the primary bottleneck
- **Fix**: Increase to `MaxOpenConns(4)` minimum for WAL mode (WAL supports concurrent reads)

#### CRITICAL: No Server Timeouts
- **File**: `site/main.go:311`
- No `ReadTimeout`, `WriteTimeout`, or `IdleTimeout` on Echo server
- Vulnerable to Slowloris attacks (slow clients hold connections indefinitely)
- Claude API streams that hang hold HTTP connections forever
- **Fix**: Set `ReadTimeout: 30s`, `WriteTimeout: 120s` (for SSE streams), `IdleTimeout: 120s`

#### CRITICAL: Datastar JS from Unpinned CDN
- **File**: `site/layouts/root.templ:44`
- `src="https://cdn.jsdelivr.net/gh/starfederation/datastar@main/bundles/datastar.js"`
- Breaking changes upstream or CDN outage kills the entire interactive UI
- No SRI (Subresource Integrity) hash
- **Fix**: Pin to specific commit hash or version tag, add SRI hash

#### HIGH: Wildcard CORS
- **File**: `site/main.go:60`
- `middleware.CORS()` with no config allows ALL origins
- **Fix**: Configure to only allow `creative-mode.ai` origin

#### HIGH: No Static Asset Caching
- **File**: `site/main.go:145`
- `e.Static("/", "static/")` serves files without `Cache-Control`, `ETag`, or `Last-Modified` headers
- Browsers re-request banner image, CSS, avatar image on every page load
- **Fix**: Add cache headers middleware for static files (e.g., `Cache-Control: public, max-age=86400`)

#### HIGH: Missing Security Headers
- **File**: `site/main.go:58-60`
- No `X-Frame-Options` (clickjacking risk on chat/invite forms)
- No `X-Content-Type-Options: nosniff`
- No `Strict-Transport-Security` (HSTS)
- No `Content-Security-Policy`
- **Fix**: Add security headers middleware

#### MEDIUM: No Global Rate Limit on Anthropic API
- **File**: `site/main.go:112`, `site/internal/mayor/handler.go:161`
- Single shared API key with no global throttle
- Per-user rate limit is only 2 seconds, in-memory only
- Botnet with many Discord accounts could exhaust API rate limits
- **Fix**: Add global concurrent request semaphore (e.g., max 10 concurrent Claude streams)

#### MEDIUM: No Rate Limiting on Cover Art / Hatch / Invite Endpoints
- `POST /mayor/generate-cover` -- no rate limit (handler.go:432)
- `POST /mayor/hatch` -- no rate limit (handler.go:491)
- `POST /invite` -- no rate limit, brute-forceable (main.go:164)
- **Fix**: Apply same 2s per-user rate limit, add attempt counting for invite codes

#### MEDIUM: No Request Body Size Limit
- No `BodyLimit` middleware configured
- Chat messages have no length validation
- Could bloat SQLite DB and increase Anthropic API costs
- **Fix**: Add `e.Use(middleware.BodyLimit("1M"))` and message length validation (e.g., 2000 chars)

#### LOW: filepath.Glob on Every Page Render
- **File**: `site/layouts/root_templ.go:41`
- CSS filename resolution hits filesystem on every GET request
- **Fix**: Cache the result at startup

#### LOW: Transient State + Cover Art Lost on Restart
- **File**: `site/internal/mayor/session.go:18-27`
- WorldReady, Scripted flag, cover art paths all in-memory
- Server restart orphans pending cover art files on disk
- **Fix**: Accept this for now; users can retry

#### LOW: Shared Anthropic API Key
- Single API key for all users, no per-user quota
- High traffic could trigger Anthropic rate limits (429/529)
- Scripted fallback handles this gracefully (auto-switches to pre-written responses)

---

### Area 2: Onboarding Bugs & Edge Cases

#### CRITICAL: No Re-Hatch Protection
- **File**: `site/internal/mayor/handler.go:264-291`
- After hatching, `ClearWorldReady` clears transient state but conversation persists
- User can keep chatting and Claude can emit another `WORLD_READY` marker
- "Create World" button remains visible after hatching
- **Fix**: Add `HasHatched` flag to session, check before processing WORLD_READY

#### HIGH: Malformed WORLD_READY Silently Fails
- **File**: `site/internal/mayor/handler.go:264-273`
- If Claude emits `WORLD_READY|Name|World` (missing summary), `SplitN` returns 2 parts
- `len(parts) == 3` check fails, hatching is silently skipped
- User sees Claude's response but no "World Has Been Born" card -- they're stuck
- **Fix**: Handle 2-part case (use empty summary), or show error message

#### HIGH: Partial Stream + Scripted Fallback Jarring Transition
- **File**: `site/internal/mayor/handler.go:243-248`
- If API fails mid-stream, partial Claude text is replaced by unrelated scripted response
- User sees a flash -- partial text disappears, scripted response appears
- **Fix**: On mid-stream failure, show error inline instead of switching to scripted

#### HIGH: Channel Creation Failure Shows Wrong Card
- **File**: `site/internal/mayor/handler.go:376-382`
- On channel creation error, shows `WorldSummaryCard` with "Join Discord to Get Started"
- No explanation of what went wrong, no retry mechanism
- User thinks world was created but it wasn't
- **Fix**: Show explicit error with retry button

#### MEDIUM: Mayor Name Suffix Exhaustion
- **File**: `site/internal/mayor/handler.go:360-367`
- Only tries suffixes II through V (4 attempts)
- If all 5 variants taken, creates channel with duplicate mayor name in topic
- **Fix**: Extend suffix range or generate random suffix

#### MEDIUM: No Concurrent Request Guard Per User
- Client-side `data-indicator:_sending` disables button, but no server-side guard
- Two simultaneous requests create duplicate user messages and parallel Claude API calls
- Corrupts conversation history
- **Fix**: Add per-user mutex in ConversationManager

#### MEDIUM: 24-Hour Conversation Cleanup Without Notice
- **File**: `site/internal/mayor/session.go:209`
- Users returning after 24 hours find their conversation deleted silently
- They get a fresh greeting with no explanation
- **Fix**: Extend to 7 days (match session TTL), or show "conversation expired" message

#### MEDIUM: Empty INVITE_CODES Blocks All Onboarding
- **File**: `site/internal/auth/invite.go:15-23`
- If env var is empty, all codes are rejected silently
- No startup warning logged
- **Fix**: Log warning at startup if INVITE_CODES is empty

#### LOW: No Startup Validation of Critical Env Vars
- **File**: `site/main.go:63-69`
- Missing `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, `DISCORD_REDIRECT_URI` cause silent runtime failures
- Only DB init and markdown renderer failures cause `log.Fatalf`
- **Fix**: Validate required env vars at startup with clear error messages

#### LOW: Browser Close Mid-Stream Leaves Orphaned User Message
- User message is saved to DB (handler.go:88) before API call
- If browser closes mid-stream, partial assistant response is NOT saved
- On return, user sees their message without a response; Claude retries
- Mostly self-healing but could confuse users

---

### Area 3: Discord Message Formatting

#### Current Discord Messages Inventory

| # | What | File | Format | Human-Friendly? |
|---|------|------|--------|-----------------|
| 1 | Welcome message | `pkg/worldchannel/welcome.go:34` | Discord Markdown (headers, mentions, quotes) | YES |
| 2 | Onboarding data (main) | `pkg/worldchannel/onboarding.go:142` | `🥚` marker + **raw JSON code block** | NO |
| 3 | Onboarding data (continued) | `pkg/worldchannel/onboarding.go:142` | `🥚` marker + **raw JSON array** | NO |
| 4 | Build complete | `pkg/worldchannel/client.go:60` | `[BUILD COMPLETE] Checkpoint...` plain text | PARTIAL |
| 5 | Build failed | `pkg/worldchannel/client.go:60` | `[BUILD FAILED] Checkpoint...` plain text | PARTIAL |
| 6 | OpenClaw mayor messages | External (OpenClaw framework) | Natural language | YES |

#### THE MAIN ISSUE: Pinned Onboarding Data is Raw JSON

**File**: `pkg/worldchannel/onboarding.go:53-96`

When a world is hatched, the entire onboarding conversation is pinned as JSON in the Discord channel:

```
🥚 Onboarding Conversation
```json
{"version":1,"creator":{"discord_id":"123","username":"alice"},"world":{"name":"Duskhollow","summary":"A cozy village..."},"mayor":{"name":"Bramble"},"conversation":[{"role":"user","content":"I want a forest village"},{"role":"assistant","content":"Tell me more..."}]}
```

For long conversations, this can be **multiple pinned messages of ~2000 characters each**, all raw JSON.

#### Why It's Done This Way

The JSON serves as a **data bridge** between `site/` and `harness/`:
- Site writes it via `PinOnboardingData` (`pkg/worldchannel/onboarding.go:53`)
- Harness reads it via `ReadOnboardingData` (`pkg/worldchannel/onboarding.go:100`)
- Used to bootstrap the OpenClaw mayor agent's personality files

The `ReadOnboardingData` function at line 100-138:
1. Fetches pinned messages via `ChannelMessagesPinned`
2. Looks for the `🥚` marker prefix
3. Extracts JSON from the code block
4. Unmarshals into `OnboardingData` struct

#### Recommended Fix: JSON as File Attachment + Human-Friendly Message

Replace the raw JSON pinned message with:

1. **Human-friendly pinned message** with world summary, creator mention, mayor name
2. **JSON file attachment** (e.g., `onboarding.json`) containing the full data

Update `PinOnboardingData` to use `ChannelMessageSendComplex` with:
```go
discordgo.MessageSend{
    Content: fmt.Sprintf("🥚 **%s** — World created by <@%s>\n\n**Mayor**: %s\n**Summary**: %s",
        data.World.Name, data.Creator.DiscordID, data.Mayor.Name, data.World.Summary),
    Files: []*discordgo.File{{
        Name:        "onboarding.json",
        ContentType: "application/json",
        Reader:      bytes.NewReader(jsonData),
    }},
}
```

Update `ReadOnboardingData` to:
1. Check pinned messages for `🥚` marker
2. If message has attachments, download the JSON file
3. Fall back to extracting from code block for backwards compatibility

#### Build Notifications Could Be Improved Too

Current: `[BUILD COMPLETE] Checkpoint 'abc123' — Added a tavern`
Could use Discord embeds with color coding (green for success, red for failure).

---

## Priority Matrix for Launch Day

### Must Fix Before Launch (P0)

1. **Pin Datastar JS version** -- CDN outage or breaking change kills entire UI
2. **Add security headers** -- X-Frame-Options, X-Content-Type-Options minimum
3. **Configure CORS** -- restrict to `creative-mode.ai`
4. **Add re-hatch protection** -- prevent users from creating unlimited worlds
5. **Add server timeouts** -- prevent Slowloris and hung connections
6. **Validate critical env vars at startup** -- don't silently break

### Should Fix Before Launch (P1)

7. **Increase SQLite MaxOpenConns** -- at least 4 for concurrent reads
8. **Add static asset caching headers** -- reduce bandwidth under load
9. **Add message length limit** -- prevent DB bloat and API abuse
10. **Handle malformed WORLD_READY** -- don't silently fail
11. **Add invite code rate limiting** -- prevent brute force
12. **Fix onboarding data Discord formatting** -- JSON as file attachment

### Fix Soon After Launch (P2)

13. **Add global Claude API semaphore** -- prevent API rate limiting
14. **Add per-user concurrent request guard** -- prevent duplicate messages
15. **Extend conversation cleanup to 7 days** -- match session TTL
16. **Improve build notifications** -- Discord embeds with color coding
17. **Add error pages** -- custom 404/500 instead of Echo JSON defaults
18. **Fix partial-stream scripted fallback** -- show error inline, not switch

---

## Code References

- `site/internal/db/db.go:23` -- `SetMaxOpenConns(1)` bottleneck
- `site/main.go:58-60` -- middleware stack (missing security headers)
- `site/main.go:145` -- static file serving (no cache headers)
- `site/main.go:311` -- server start (no timeouts)
- `site/layouts/root.templ:44` -- Datastar CDN `@main` dependency
- `site/internal/mayor/handler.go:264-273` -- WORLD_READY parsing
- `site/internal/mayor/handler.go:243-248` -- scripted fallback after partial stream
- `site/internal/mayor/handler.go:360-367` -- mayor name suffix loop
- `site/internal/mayor/handler.go:376-382` -- channel creation failure handling
- `site/internal/mayor/handler.go:523-566` -- harness webhook (fire-and-forget)
- `site/internal/mayor/session.go:114-128` -- rate limiting (in-memory, per-user only)
- `site/internal/mayor/session.go:205-222` -- 24h conversation cleanup
- `site/internal/auth/invite.go:15-31` -- invite code validation (no rate limit)
- `site/internal/auth/auth.go:230-238` -- session cookie settings
- `site/internal/auth/auth.go:407-409` -- guild membership bypass if bot token empty
- `site/internal/markdown/renderer.go:85-86` -- unescaped link href (XSS surface)
- `pkg/worldchannel/onboarding.go:53-96` -- PinOnboardingData (raw JSON)
- `pkg/worldchannel/onboarding.go:100-138` -- ReadOnboardingData (JSON extraction)
- `pkg/worldchannel/welcome.go:13-44` -- welcome message (already human-friendly)

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/handoffs/general/2026-02-16_01-52-35_debug-onboarding-discord-channel.md` -- Recent debugging of Discord channel creation during onboarding
- `thoughts/CoreyCole/handoffs/general/2026-02-15_14-20-24_sqlite-persistence-sessions-conversations.md` -- SQLite persistence implementation for sessions and conversations
- `thoughts/CoreyCole/handoffs/general/2026-02-15_17-32-11_mayor-name-required-before-hatch.md` -- Mayor name requirement added before hatching
- `thoughts/CoreyCole/reviews/2026-02-14_09-27-22_meet-the-mayor-implementation_review.md` -- Implementation review of Meet the Mayor page
- `thoughts/CoreyCole/reviews/2026-02-14_03-15-25_meet-the-mayor-site-page_review.md` -- Plan review of the Meet the Mayor page design
- `thoughts/CoreyCole/research/2026-02-15_23-42-38_documentation-audit-agent-hierarchy.md` -- Documentation audit of agent hierarchy

## Open Questions

1. Should we add a global concurrent user cap for the mayor chat (e.g., max 50 simultaneous conversations)?
2. What is the expected traffic volume for launch day? This affects whether SQLite is sufficient or if we need to consider a different database.
3. Should invite codes become single-use or have a limited number of redemptions?
4. Do we want to add a "coming soon" / waitlist page that bypasses the full onboarding flow for overflow traffic?
