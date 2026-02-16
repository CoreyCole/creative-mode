---
date: 2026-02-16T01:52:35+00:00
researcher: CoreyCole
git_commit: 2b5d44db330481f3b115479311c79ec4aed5cda1
branch: main
repository: creative-mode
topic: "Debug Onboarding Discord Channel Creation Failure"
tags: [debugging, onboarding, discord, site, worldchannel]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Debug Onboarding — No Discord Channel Created

## Task(s)
- **Debugging onboarding flow** (work in progress): When hatching a world via the marketing site, no Discord channel is created. The harness server never receives the `world-hatched` webhook, and the hatched world does not appear in the harness dashboard.

## Critical References
- `site/CLAUDE.md` — full architecture of the onboarding flow
- `CLAUDE.md` (root) — system overview, agent system, environment variables

## Recent changes
None — this is a debugging session, no code changes made.

## Learnings

### Architecture (two separate servers)
- **Site server** (EC2 `ip-172-31-17-136`, Tailscale IP `100.89.48.98`): Handles onboarding, Discord channel creation, cover art generation, webhook to harness.
- **Harness server** (VPS `claude-2`): Receives webhook, provisions mayor agent, manages worlds.
- The site creates the Discord channel directly (not the harness). The harness only gets notified after.

### Onboarding flow traced
1. `GET /mayor` → builds system prompt, seeds greeting (`site/main.go:212-267`)
2. `POST /mayor/chat` → streams Claude response, detects `WORLD_READY|mayor|world|summary` marker (`site/internal/mayor/handler.go:60-290`)
3. `prepareCoverArtAndHatch` (`handler.go:295-349`) — three branches:
   - `wcClient == nil` → shows summary card only (NO channel creation) — **this is the most likely failure mode**
   - `imagegenClient == nil` → hatches immediately without cover art
   - Both available → generates cover art, shows preview with Hatch/Regenerate buttons
4. User clicks "Hatch" → `POST /mayor/hatch` → `HandleHatch` (`handler.go:489-517`) → `hatchWorldWithCover` (`handler.go:353-410`)
5. `hatchWorldWithCover` creates Discord channel via `wcClient.CreateChannel` (`pkg/worldchannel/channel.go:26-91`)
6. Fires webhook to harness in a goroutine (`handler.go:395`, `handler.go:521-564`)

### Key finding: harness received NO webhook
Searched harness logs (`journalctl -u creative-mode`) for the last 24 hours — zero mentions of `world-hatched`, `webhook`, `provision`, or `hatch`. This means the failure happened on the site side before/during the webhook send.

### Could not access site logs
- Site runs on EC2 (`100.89.48.98` via Tailscale), SSH requires Tailscale auth approval
- No site service logs on this VPS

### Most likely root causes (to investigate on EC2)
1. **`DISCORD_BOT_TOKEN` not set** → `wcClient` is nil → line 299-305 shows summary card only, no channel created, no webhook sent
2. **`DISCORD_WORLDS_CATEGORY_ID` not set** → `worldchannel.NewClient` may fail → same as above
3. **Channel creation failed** → logged but silent to user, shows summary card fallback (`handler.go:374-380`)
4. **`HARNESS_URL` not set** → webhook function returns early at line 523-525, channel may have been created but harness not notified
5. **`CM_HOOK_SECRET` mismatch** → webhook returns 401, logged but invisible

## Artifacts
- This handoff document

## Action Items & Next Steps

**These steps need to be run on the EC2 marketing site server** (`100.89.48.98` via Tailscale, SSH as `ubuntu`):

### 1. Check site service logs
```bash
sudo journalctl -u creative-mode-site --since "24 hours ago" --no-pager | tail -300
```
Look for:
- `WARNING: Failed to init Discord bot client` — means wcClient is nil
- `Channel creation failed` — Discord API error
- `world-hatched webhook failed` — webhook send error
- `world-hatched webhook returned unexpected status` — harness rejected it
- Any errors during the chat/hatch flow

### 2. Check environment variables
```bash
sudo cat /etc/systemd/system/creative-mode-site.service
# or
sudo cat ~/.config/creative-mode/site.env
```
Verify these are all set:
- `DISCORD_BOT_TOKEN` — required for channel creation
- `DISCORD_GUILD_ID` — required for channel creation
- `DISCORD_WORLDS_CATEGORY_ID` — required for channel creation
- `HARNESS_URL` — required for webhook (should be `https://claude-2.tailcdc985.ts.net` or similar)
- `CM_HOOK_SECRET` — must match the harness env var

### 3. Verify the deployed binary has the latest code
```bash
# Check when the binary was last built
ls -la /tmp/creative-mode-site

# Check the git commit on the site server
cd ~/creative-mode && git log --oneline -5

# Compare with the VPS (should match or be close to 2b5d44d)
```
The cover art flow was added in commit `9602e42`. If the deployed binary is older than this, the flow may differ.

### 4. Test the flow manually
If env vars look correct, try the onboarding flow again and watch the logs live:
```bash
sudo journalctl -u creative-mode-site -f
```
Then hatch a world in another terminal/browser and watch for errors.

### 5. Quick fix if env vars are missing
```bash
# Edit the env file
sudo nano ~/.config/creative-mode/site.env

# Rebuild and restart
cd ~/creative-mode/site
just build
cp site-linux /tmp/creative-mode-site
sudo systemctl restart creative-mode-site
```

## Other Notes

### Key code locations
- `site/main.go:73-87` — wcClient initialization (nil if DISCORD_BOT_TOKEN missing)
- `site/internal/mayor/handler.go:295-305` — the `wcClient == nil` early return (no channel, no webhook)
- `site/internal/mayor/handler.go:353-410` — `hatchWorldWithCover` (channel creation + webhook)
- `site/internal/mayor/handler.go:521-564` — `notifyHarnessWorldHatchedWithCover` (webhook sender)
- `pkg/worldchannel/channel.go:26-91` — `CreateChannel` (Discord API call)
- `site/site.env.example` — template showing all required env vars

### Harness database location
On the VPS, the harness DB is at `/home/deploy/creative-mode/data/creative-mode.db`. The `worlds` table contains all worlds. Only template worlds exist currently, confirming the webhook was never received.

### Error handling is silent
Almost all errors in the hatch flow are logged but NOT shown to the user. If `wcClient` is nil, the user just sees a summary card with no explanation. If channel creation fails, same thing. If the webhook fails, the user sees the "hatched" card but no world appears on the harness. Check logs carefully.
