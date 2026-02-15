---
date: 2026-02-15T18:19:42+0000
reviewer: Claude (Staff Eng Review)
git_commit: 440705f4470e6c4ec02c018aebf8a327b4d9feea
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-15_07-49-26_world-mayors-vps-plan.md
prior_reviews:
  - thoughts/CoreyCole/reviews/2026-02-14_11-45-00_world-mayors-master-plan_review.md
  - thoughts/CoreyCole/reviews/2026-02-15_07-39-39_world-mayors-master-plan_review.md
status: complete
type: plan_review
context: VPS (Ubuntu, Nix + systemd, native binary)
---

# Plan Review: World Mayors VPS Implementation Plan

### Summary

This plan is a well-structured rewrite addressing many issues from the prior two reviews (Docker→Nix, Phase 0 prerequisites, single bot decision, site↔harness bridge). However, it contains a **critical factual error**: the plan repeatedly states the harness uses "GitHub OAuth" while it actually uses **Discord OAuth** — this invalidates the core architectural assumption that drives the site↔harness bridge design. The plan also takes OpenClaw's CLI/gateway API as given without verification, creating risk across all 6 phases. Overall design (OpenClaw orchestration, SOUL.md from onboarding conversation, instrumentation, dashboard) remains strong.

---

### Critical Issues (Must Address Before Implementation)

#### 1. The Harness Already Uses Discord OAuth — NOT GitHub

**Problem**: The plan states in multiple places that the harness uses GitHub OAuth and that Discord OAuth should only be in the site:

- Line 24: "Site handles: Discord OAuth"
- Line 64: "Discord OAuth in harness — site handles Discord auth; harness keeps GitHub OAuth"
- Line 298: "No OAuth changes — the harness keeps GitHub OAuth"
- Line 1019: "Auth | Discord OAuth replaces GitHub in harness | Harness keeps GitHub OAuth"
- Line 1025: "Users table | Recreated with discord_id | Unchanged (GitHub auth preserved)"

**Verified reality** (`harness/internal/auth/auth.go`, `harness/.env`):
- `HandleLogin` redirects to `https://discord.com/api/oauth2/authorize?client_id=...`
- `HandleCallback` exchanges code at `https://discord.com/api/oauth2/token`
- `fetchDiscordUser` calls `https://discord.com/api/users/@me`
- `.env` has `DISCORD_CLIENT_ID` and `DISCORD_CLIENT_SECRET` configured and active
- `main.go` reads `DISCORD_CLIENT_ID` / `DISCORD_CLIENT_SECRET` env vars
- Users table (`001_initial.sql`) has `discord_id TEXT UNIQUE NOT NULL` as the primary identity
- GitHub columns (`github_id`, `github_username`) exist but are optional/unused

**Risk**: The entire site↔harness bridge architecture is predicated on "site handles Discord auth, harness handles GitHub auth, bridge via Discord channel pinned data." But since both systems use Discord OAuth, users already have `discord_id` in the harness DB. This means:
1. The harness already knows each user's Discord identity
2. The "bridge" between site and harness may be unnecessary — or at least much simpler
3. The harness could potentially do onboarding itself (the plan explicitly rules this out based on the false premise)
4. The `world-hatched` webhook from site→harness could carry the Discord user ID directly, matching against the harness's own user record

**Suggestion**: Acknowledge that both systems use Discord OAuth and redesign the bridge accordingly. Options:
- **Option A (simpler)**: Since harness users already have `discord_id`, the harness can directly create Discord channels and run onboarding. No site dependency needed.
- **Option B (keep site)**: Keep the site for onboarding conversation but simplify the bridge — the harness can match users by `discord_id` instead of needing a separate discovery mechanism.

This is not a blocking issue for Phase 0-1, but Phases 2-3 need the bridge design corrected before implementation.

#### 2. OpenClaw CLI/Gateway API Is Entirely Unverified

**Problem**: The plan references OpenClaw's API extensively but OpenClaw isn't installed and these interfaces aren't verified:

| Assumed API | Where Used | Risk if Wrong |
|-------------|-----------|---------------|
| `openclaw agents add --non-interactive --json --workspace {dir}` | Phase 3 (`openclaw.go`) | Agent provisioning breaks |
| `openclaw config get bindings --json` | Phase 3 (`openclaw.go`) | Channel binding breaks |
| `openclaw config set bindings --json '{...}'` | Phase 3 (`openclaw.go`) | Channel binding breaks |
| `openclaw agents list --json` | Phase 3 (success criteria) | Status checking breaks |
| `openclaw agents delete --force {agentID}` | Phase 3 (`openclaw.go`) | Cleanup breaks |
| `src/gateway/server.js` | Phase 1 (systemd service) | Gateway won't start |
| `http://localhost:18789/health` | Phase 1 (success criteria) | Health check breaks |
| `openclaw.json` schema: `{channels, agents, bindings}` | Phase 1 (setup script) | Config invalid |
| Discord adapter: `{kind: "discord", token: "..."}` | Phase 1 (setup script) | Bot won't connect |
| Workspace files: SOUL.md, AGENTS.md in specific locations | Phase 3 | Agent has no personality |

**Risk**: If OpenClaw's actual API differs from these assumptions (different flag names, different JSON schema, different gateway path), every phase after 0 needs rework. The prior review's "context/openclaw/" directory that documented verified behaviors doesn't exist on this VPS.

**Suggestion**: Add to Phase 0:
1. Clone OpenClaw
2. Run `openclaw --help`, `openclaw agents --help`, `openclaw config --help`
3. Read `src/gateway/server.js` to verify entry point
4. Document actual CLI flags and config schema
5. Update Phase 1 and 3 if they differ from plan assumptions

Alternatively, pin an OpenClaw commit SHA in the plan so the API surface is known.

#### 3. `hookSecretMiddleware` Signature Mismatch

**Problem**: Phase 2 (line 479) shows:
```go
e.POST("/api/world-hatched", s.handleWorldHatched, hookSecretMiddleware(hookSecret))
```

But the actual `hookSecretMiddleware()` in `server.go:570` takes **no arguments** — it reads `CM_HOOK_SECRET` from `os.Getenv()` internally:
```go
func hookSecretMiddleware() echo.MiddlewareFunc {
    secret := os.Getenv("CM_HOOK_SECRET")
    // ...
}
```

**Risk**: Code won't compile. Minor but blocks Phase 2 build.

**Suggestion**: Use `hookSecretMiddleware()` (no args), matching the existing pattern at `server.go:128`.

---

### Concerns (Should Address)

#### 1. `CM_HOOK_SECRET` Is Disabled on VPS

**Observation**: `harness/.env` has `# CM_HOOK_SECRET=` commented out. This means the `/api/claude-event` endpoint (and the proposed `/api/world-hatched`) accept requests from anyone on the Tailnet without authentication.

**Suggestion**: Generate and enable `CM_HOOK_SECRET` before adding the world-hatched webhook. This is especially important since the webhook triggers agent provisioning, which could be abused.

#### 2. `DEV_MODE=true` on VPS

**Observation**: `harness/.env` has `DEV_MODE=true`. This enables dev-only endpoints (`/dev/sse`, `/dev/rebuild`, `/dev/auth/login`) on the production VPS. The `/dev/auth/login` endpoint allows creating arbitrary user sessions without Discord OAuth.

**Suggestion**: Consider disabling `DEV_MODE` on the VPS before adding mayor functionality. The dev auth bypass is a security concern if the Tailnet isn't fully trusted.

#### 3. Discord Gateway vs REST Session Lifecycle

**Observation**: `pkg/worldchannel/Client` uses REST-only (`discordgo.New()` without `session.Open()`). Phase 5's Discord listener needs Gateway WebSocket (`session.Open()` to receive `MESSAGE_CREATE` events). These are different `discordgo.Session` lifecycles.

**Risk**: If both share a session, calling `Open()` changes behavior of all REST calls and introduces reconnection logic. If separate, two bot connections consume two Discord identify slots.

**Suggestion**: Use two `discordgo.Session` instances:
1. REST-only (`worldchannel.Client`) for channel creation, message sending, onboarding data
2. Gateway (`discord.Listener`) for real-time message mirroring

Document this in the plan so implementers don't accidentally share the session.

#### 4. No Automated Tests

**Observation**: 6 phases, ~28 new files, ~10 modified files, but no test files in the plan. The system has multiple integration points (Discord API, OpenClaw CLI, SQLite, tmux) that are error-prone.

**Suggestion**: At minimum, add:
- `harness/internal/mayor/mayor_test.go` — test `ProvisionAgent` with mock OpenClaw CLI
- `harness/internal/discord/listener_test.go` — test message classification logic
- `harness/internal/server/mayor_api_test.go` — test webhook auth + world-hatched flow

#### 5. Discord API Rate Limits

**Observation**: The plan has multiple Discord API touchpoints:
- Build events → `PostToDiscord()` (Phase 4)
- Browser prompts → Discord channel message (Phase 5)
- Listener → mirrors every message to SQLite (Phase 5)
- Channel creation during onboarding (site)

Discord's rate limits are ~5 messages/5s per channel, ~50 requests/s globally. A rapid build-edit-build cycle could hit limits.

**Suggestion**: Add rate-limit awareness to `PostToDiscord()`. Use `discordgo`'s built-in rate limiter (enabled by default), but document the expected throughput.

#### 6. SELECT * in New Queries Won't Work After ALTER TABLE

**Observation**: The existing world queries (`worlds.sql`) use explicit column lists:
```sql
SELECT id, name, description, created_by, created_at, template_type FROM worlds WHERE id = ?;
```

But after Phase 2 adds 5 new columns to `worlds`, the `sqlc`-generated `World` model struct will have those new fields. The existing queries that list specific columns will work fine — BUT the plan's new queries use `SELECT *`:
```sql
-- name: GetWorldByMayorSecret :one
SELECT * FROM worlds WHERE mayor_secret = ?;
```

`SELECT *` is fine for sqlc but creates a maintenance burden — any future column addition changes all `SELECT *` queries. The existing codebase uses explicit column lists.

**Suggestion**: Follow the existing pattern — use explicit column lists in new queries for consistency.

#### 7. OpenClaw Gateway Lifecycle Under systemd

**Observation**: The plan creates `openclaw-gateway.service` with `EnvironmentFile=/home/deploy/creative-mode/harness/.env`. This means the OpenClaw gateway reads ALL harness env vars, including `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, `ANTHROPIC_API_KEY`, etc. The gateway only needs `DISCORD_BOT_TOKEN` (for the Discord adapter) and `OPENCLAW_HOME`.

**Suggestion**: Either:
- Create a separate `/home/deploy/creative-mode/data/openclaw/.env` with only the vars the gateway needs
- Or document that sharing `harness/.env` is intentional and the gateway ignores unrecognized vars

---

### Questions (Need Clarification)

1. **Does the harness need the site at all for onboarding?** Given that the harness already has Discord OAuth and users have `discord_id`, could the harness run the onboarding conversation directly? The site's "Meet the Mayor" page exists on EC2, but if the harness can do it, the bridge complexity disappears.

2. **Is site/ deployed on this VPS?** The plan references "Site handles onboarding, harness reads pinned data" but never clarifies whether the site is running on this VPS alongside the harness. If not, the webhook from site→harness needs to cross network boundaries (EC2→Tailnet).

3. **What if OpenClaw doesn't exist as described?** The plan takes OpenClaw as a given, but it's a relatively new/unknown project. What's the fallback if OpenClaw's CLI doesn't support the assumed flags, or if the Discord adapter doesn't work as expected? Is there a plan B (e.g., direct `discordgo` bot without OpenClaw)?

4. **Should `CM_HOOK_SECRET` be enabled?** The world-hatched webhook and claude-event endpoint are unprotected. Is this intentional for dev, or should it be secured before adding mayor functionality?

5. **What is the "president" role?** (User's new requirement — not in this plan.) The user wants a "president" agent who orchestrates the overall repo (harness + templates) while mayors focus on their individual worlds. How does this fit into the architecture? Is the president an OpenClaw agent? Does it have repo-level access?

---

### Suggestions (Nice to Have)

1. **Pin OpenClaw to a specific commit** in Phase 0 (`git clone --depth 1 --branch <tag> ...`). This prevents upstream changes from breaking the plan's assumptions.

2. **Add `just vps-openclaw-status`** command to check gateway health alongside `just vps-status`.

3. **Consolidate all systemd services into a target**: Create `creative-mode.target` that includes both `creative-mode.service` and `openclaw-gateway.service`. This enables `systemctl restart creative-mode.target` for a single-command full restart.

4. **Add startup validation in `main.go`**: Before creating the mayor manager, verify OpenClaw gateway is reachable (`http://localhost:18789/health`). Log a clear warning if not, so operators know builds won't be mayor-orchestrated.

5. **Use the existing `events.EventBus` for mayor events**: The plan adds `mayor_activity` logging in the build handler, but could also publish to the EventBus for real-time dashboard updates without polling.

---

### What's Good

- **Phase 0 is a major improvement** over the original plan. Explicitly listing every prerequisite with exact commands prevents the "assumed available" trap.

- **Single bot decision** (line 32-37) eliminates the two-bot ambiguity from the original plan. Message classification by content prefix (`[BUILD`, `[SYSTEM`) is simple and debuggable.

- **SOUL.md from onboarding conversation** (Phase 3, line 533-575) is significantly better than the original plan's form-field approach. The conversation IS the personality — this gives each mayor authentic character.

- **Site↔harness bridge via Discord pinned data** (Phase 3, line 521) is clever. Using Discord itself as the data transport avoids a custom API between site and harness. The `pkg/worldchannel` package already implements `PinOnboardingData` / `ReadOnboardingData` with chunking for Discord's 2000-char limit.

- **OpenClaw as separate systemd service** (Phase 1) is the right call. Independent lifecycle, journalctl logs, automatic restart. Much better than the original plan's "background process in entrypoint."

- **Graceful degradation pattern** throughout: mayor manager only creates if `DISCORD_BOT_TOKEN` is set (Phase 3, line 652), gateway health check with fallback (Phase 5, line 847), non-fatal provisioning. The harness keeps working even if Discord/OpenClaw are broken.

- **File inventory tables** (lines 956-1008) are comprehensive and make implementation planning concrete. Every file, its phase, and its purpose.

- **Key Differences table** (lines 1012-1026) makes it easy to understand what changed from the original plan. Good documentation practice.

---

### Recommended Next Steps

1. **Fix the auth misconception**: Acknowledge that the harness uses Discord OAuth. Decide whether the site bridge is still needed or whether the harness can handle onboarding directly. This affects Phases 2-5.

2. **Install Phase 0 prerequisites**: Rust, Node.js, pnpm. These are independent of the auth question and unblock everything else.

3. **Verify OpenClaw's actual API**: Clone it, read the help, document the real CLI flags. Update Phases 1 and 3 accordingly.

4. **Enable `CM_HOOK_SECRET`**: Generate a secret and uncomment it in `.env`. Add the same secret to the site's env for the world-hatched webhook.

5. **Consider the "president" concept**: The user wants a repo-level orchestrator agent. This could be Phase 7, or it could reshape the entire architecture. Get clarity before starting implementation.

6. **Resolve the site deployment question**: Is the site running on this VPS? If not, how does the site→harness webhook work across EC2→Tailnet? Document the network path.

7. **Copy 2D template hooks**: `templates/2d/.claude/` doesn't exist. This is independent work that can happen immediately (Phase 1 item).
