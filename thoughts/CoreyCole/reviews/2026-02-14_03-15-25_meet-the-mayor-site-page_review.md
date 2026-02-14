---
date: 2026-02-14T03:15:25-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 8b99b1d1a8e7e6dda5771198b086cf5c455b169c
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-14_03-02-42_meet-the-mayor-site-page.md
status: complete
type: plan_review
---

# Plan Review: "Meet the Mayor" Conversational Page

### Summary

A thorough, well-structured plan that correctly identifies existing patterns and builds on them. However, it has several implementation gaps (missing methods, stub functions, Docker config), a streaming bug where `SIGNUP_READY` leaks to the user mid-stream, and critically **does not address Discord world channel bootstrapping** — the "hatch" action should create a Discord channel for the user's world (per the master plan architecture), not just show a static summary card.

### Critical Issues (Must Address Before Implementation)

1. **Missing Discord Channel Bootstrap on World Hatch**
   - Problem: The plan's "world summary card" is a dead end — it shows a static card with "We'll reach out on Discord when your world is ready." But per the master plan (`thoughts/CoreyCole/plans/2026-02-13_16-05-13_world-mayors-master-plan.md`), each world gets a private Discord channel. The marketing site's "hatch" moment should actually create this channel (or enqueue it) so the user lands in their world's Discord channel immediately.
   - Risk: Users see a promise with no follow-through. The marketing site and harness should share the same Discord channel bootstrapping code, but neither the plan nor the master plan specifies this shared flow from the site context.
   - Suggestion: Add a Phase 5.5 or extend Phase 5: after `SIGNUP_READY` is detected, POST to an internal endpoint that (a) creates a Discord channel in the Creative Mode guild under a "Worlds" category, (b) sets permission overwrites (deny @everyone, grant creator + bots), (c) provisions the OpenClaw mayor agent bound to that channel. The world summary card should then link directly to the new Discord channel. This code should live in a shared package so the harness can reuse it for additional world creation. Define the `DISCORD_BOT_TOKEN` and `DISCORD_GUILD_ID` env vars needed. Consider using `discordgo` for channel creation — it's already in the master plan's dependency list.

2. **`SIGNUP_READY` Marker Leaks During Streaming**
   - Problem: During the streaming loop (Phase 5, lines 968-1018), the plan renders `fullContent` incrementally as markdown. If Claude writes `SIGNUP_READY: A moonlit forest...`, this text will appear in the chat bubble in real-time as rendered markdown. The marker is only stripped in the post-stream final render (line 1032-1037).
   - Risk: Users see the raw `SIGNUP_READY:` text flicker briefly before it's cleaned up.
   - Suggestion: Check for `SIGNUP_READY` inside the streaming loop's render path. If the accumulated `fullContent` contains the marker, strip it before rendering the delta. Alternatively, instruct the LLM to place `SIGNUP_READY` on the absolute last line and only render up to the last complete paragraph during streaming.

3. **Missing Methods Referenced in main.go**
   - Problem: `main.go` (Phase 6) calls `sessionMgr.HandleLogout()` and `sessionMgr.SetInviteVerified(session.ID)`, but neither method is defined anywhere in the plan. The `SessionManager` struct (Phase 2) only has `HandleCallback`, `SessionMiddleware`, and `cleanupLoop`.
   - Risk: Won't compile — blocking.
   - Suggestion: Add `HandleLogout` (clear cookie, delete from map) and `SetInviteVerified` (lock, set `InviteCodeVerified = true`, unlock) to Phase 2's `SessionManager`.

4. **Docker Compose Missing Environment Variables**
   - Problem: `site/docker-compose.yml` only passes `CGO_ENABLED`, `GOOS`, `GO111MODULE`. The plan adds 5 new env vars (`DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, `DISCORD_REDIRECT_URI`, `ANTHROPIC_API_KEY`, `INVITE_CODES`) but never updates `docker-compose.yml` to forward them.
   - Risk: All env vars will be empty inside Docker. OAuth, Claude, and invite codes won't work.
   - Suggestion: Add these to the `environment` block in `docker-compose.yml` using `${VAR}` syntax, and create a `.env.example` documenting them.

5. **Discord OAuth URL Not URL-Encoded**
   - Problem: In `HandleLogin` (Phase 2, line 166-169), the OAuth redirect URL is constructed with `fmt.Sprintf` without URL-encoding parameters. Compare with the harness's `auth.go:76-81` which uses `url.QueryEscape()`.
   - Risk: If `RedirectURI` contains special characters (e.g., `http://localhost:3000/auth/discord/callback` has colons and slashes), the OAuth flow will break.
   - Suggestion: Use `url.QueryEscape()` for `ClientID`, `RedirectURI`, and `state` — same pattern as `harness/internal/auth/auth.go:77-80`.

6. **Stub Functions Without Implementation**
   - Problem: `exchangeCode` and `fetchDiscordUser` in Phase 2 are just comments with `// ...`. These are the core of the OAuth flow — without them, nothing works.
   - Risk: Implementation gap will block all downstream phases.
   - Suggestion: Provide full implementations. The harness's `exchangeCode` (`auth.go:326-372`) is a working reference. Discord's token endpoint is `https://discord.com/api/oauth2/token` with `application/x-www-form-urlencoded` body; user endpoint is `https://discord.com/api/users/@me` with `Bearer` token.

### Concerns (Should Address)

1. **In-Memory Sessions Reset on Every Hot Reload**
   - Observation: The site uses `air` for hot-reloading (`.air.toml` runs `just build` on file change). Every rebuild restarts the binary, wiping all in-memory sessions. During active development, the developer will need to re-authenticate through Discord OAuth after every code change.
   - Suggestion: Accept this for now but document it. Consider adding a `DEV_MODE` bypass (similar to `harness/internal/auth/auth.go:422-501`'s `HandleDevLogin`) that auto-creates a session with fake Discord user data.

2. **Air Config Doesn't Watch `internal/` Directory**
   - Observation: `.air.toml` `include_dir` is `["pages", "layouts", "static"]`. New code in `site/internal/auth/`, `site/internal/mayor/`, `site/internal/markdown/` won't trigger rebuilds.
   - Suggestion: Add `"internal"` to `include_dir` in `.air.toml`.

3. **Missing `uuid` Dependency**
   - Observation: Phase 5 handler uses `github.com/google/uuid` but Phase 1 doesn't list it in the `go get` command.
   - Suggestion: Add `github.com/google/uuid` to the Phase 1 dependency install.

4. **Missing `strings` Import in auth.go**
   - Observation: The `isSecure` function (Phase 2, line 259) calls `strings.HasPrefix` but the import block doesn't include `"strings"`.
   - Suggestion: Add `"strings"` to the import.

5. **Model Constant May Not Exist**
   - Observation: Phase 4 uses `anthropic.ModelClaude4_5Sonnet`. The cn-agents reference code uses `anthropic.ModelClaudeOpus4_5_20251101`. The exact constant name depends on the SDK version — this should be verified.
   - Suggestion: Check `github.com/anthropics/anthropic-sdk-go` for the actual constant name, or use the string literal `"claude-sonnet-4-5-20250929"` as a fallback.

6. **Conversation Not Seeded with Initial Greeting**
   - Observation: The initial mayor greeting is rendered on page load (`main.go` line 1176-1177) but is never added to the `ConversationSession.Messages`. When the user sends their first message, Claude won't see the greeting in its context, causing an unanchored conversation.
   - Suggestion: In the `GET /mayor` handler, after creating the greeting, also call `convMgr.AddMessage(session.DiscordID, "assistant", greetingText)` with the raw markdown (not HTML) to seed the conversation.

7. **No CSRF Protection on Chat Endpoint**
   - Observation: `/api/mayor/chat` accepts POST requests authenticated only by session cookie. While `SameSite: Lax` prevents most cross-origin attacks, it doesn't protect against all CSRF vectors.
   - Suggestion: For an invite-code-gated marketing page, this is acceptable risk. Note it as a future hardening item.

8. **Cleanup Goroutine Has No Shutdown Path**
   - Observation: `SessionManager.cleanupLoop()` and `ConversationManager.cleanupLoop()` run forever with no `context.Context` or stop channel. This causes goroutine leaks in tests and prevents clean shutdown.
   - Suggestion: Accept a `context.Context` in the constructors and select on `ctx.Done()` in the cleanup loops.

### Questions (Need Clarification)

1. When the world is "hatched" via `SIGNUP_READY`, should the site actually create a Discord channel and provision a mayor agent? Or is this marketing-only and the world is created later when the user joins the harness?
2. Should invite codes be single-use per code (one code = one user) or reusable? The plan's `VerifyCode` allows the same user to reuse their code but blocks other users — is this the intended behavior?
3. Should the conversation persist across page refreshes (using the in-memory session) or start fresh? The plan says "conversation resets" in the testing section but the conversation manager keeps sessions for 1 hour.
4. Should the Datastar CDN import in `root.templ:38` be pinned to a specific version? Currently using `@main` which could break at any time.

### Suggestions (Nice to Have)

1. **Add a `dev-login` bypass** — Port the harness's `HandleDevLogin` pattern to skip Discord OAuth during development. Create a mock Discord user (name: "Dev User", avatar: placeholder) and session in one click.
2. **Consider rate limiting at the middleware level** — The current per-handler rate limit check could be extracted into a middleware that runs before the handler, keeping the handler focused on business logic.
3. **Add `data-on:keydown` for Enter-to-submit** — The chat form currently only submits via the button. Add `data-on:keydown="evt.key === 'Enter' && !evt.shiftKey && @post('/api/mayor/chat', {contentType: 'form'})"` to the input for a more natural chat UX.
4. **Streaming cursor animation** — The plan defines `.chat-message-content.streaming` CSS but the streaming container's cursor (`animate-pulse` span) will remain visible after streaming completes. The final `MayorMessageComplete` replaces the whole container, which handles this — but there's a brief flash. Consider removing the cursor via SSE before the final render.

### What's Good

- **Correct identification of existing patterns**: The plan accurately references `harness/internal/auth/auth.go`, `context/cn-agents/server/services/chat/handler.go`, and `context/cn-agents/server/services/markdown/renderer.go` as models. The streaming loop is faithfully adapted from cn-agents.
- **Well-scoped "What We're NOT Doing" section**: Explicitly excludes persistent storage, image gen, token-by-token streaming, and Discord bot integration. This prevents scope creep.
- **Proper Datastar patterns**: Uses `PatchElementTempl` for server-rendered fragments, `ExecuteScript` for scroll-to-bottom, and colon syntax for attributes (`data-on:submit__prevent`, `data-indicator:fetching`). Follows the Tao of Datastar.
- **Clean middleware chain**: Session → InviteCode → Handler mirrors the harness's Session → Approved → Admin pattern.
- **Graceful degradation**: No `ANTHROPIC_API_KEY` → "Coming Soon" page instead of a crash.
- **Comprehensive success criteria per phase**: Each phase has specific automated and manual verification steps.

### Recommended Next Steps

1. **Decide on Discord channel bootstrapping scope** — Should the marketing site create real Discord channels on "hatch", or just capture interest? This fundamentally changes Phase 5's end state and adds Discord bot token as a dependency.
2. **Fill in stub functions** — Write full `exchangeCode` and `fetchDiscordUser` implementations before starting Phase 2.
3. **Add missing methods** — Define `HandleLogout` and `SetInviteVerified` on `SessionManager`.
4. **Fix streaming `SIGNUP_READY` leak** — Add marker detection to the streaming render path.
5. **Update Docker config** — Forward env vars in `docker-compose.yml`, add `internal/` to `.air.toml` watch dirs.
6. **Seed conversation with initial greeting** — Add the greeting to the conversation manager on page load.
7. **Add dev auth bypass** — Saves significant time during development iteration.
