---
date: 2026-02-16T03:38:09-08:00
researcher: CoreyCole
git_commit: a0ec990bc4e46f159864fb5051470bc35126be23
branch: main
repository: creative-mode
topic: "Status Page SSE Not Working on Production"
tags: [debugging, sse, deployment, status-page, api-gateway]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Root-cause SSE failure on deployed /status page

## Task(s)

**Root-cause why `/status` page shows no live data on production** — IN PROGRESS

The deployed site at `creative-mode.ai/status` renders the page HTML correctly (all sections visible: Metrics, Site Stats, System, Database, Harness) but NO SSE data populates. Every field shows `--` or placeholder text. The SSE endpoint at `/status/events` is not delivering data to the browser.

## Critical References

- `site/internal/monitor/handler.go` — SSE handler (`HandleEvents` at line ~189) and all send* functions
- `site/main.go:173-178` — route registration (routes are PUBLIC, not behind auth middleware)
- `site/internal/db/db.go:82-87` — DB migrations (recently added missing ALTER TABLEs)

## Recent changes

Recent commits pushed to main that affect the status page:

1. `a0ec990` — Synced handoff doc
2. `6e0acdf` — Added missing ALTER TABLE migrations for `discord_members` and `worlds_hatched` columns in `site/internal/db/db.go:86-87`
3. `61a113c` — Added `go mod tidy` to `site/justfile:6` build recipe
4. `5bbe76d` — Major status page enhancement: timestamp-based graph X positioning, background snapshot writer, Discord poller, retention cleanup, stats overview, disk stats, page view middleware, select UI component

## Learnings

### Two SSE patterns in this codebase
- **Mayor chat** (`POST /mayor/chat` in `site/internal/mayor/handler.go:102`): Short-lived SSE — streams Claude's response then handler returns. This reportedly works on production.
- **Status events** (`GET /status/events` in `site/internal/monitor/handler.go:211`): Long-lived SSE — loops indefinitely, sending events every 2 seconds via tickers. Never closes until client disconnects.

### Leading hypothesis: AWS API Gateway timeout
The production traffic flow is: `Browser → Route 53 → API Gateway (TLS) → EC2:80 → site binary`

API Gateway has a ~30s integration timeout and buffers responses. Long-lived SSE connections (like `/status/events`) would be killed after 30s. Short-lived SSE (like mayor chat) would work because the response completes within the timeout.

**However, this hypothesis needs confirmation.** The mayor chat SSE working through API Gateway suggests SSE itself isn't entirely blocked — it's specifically the long-lived nature of the status SSE that may be the issue.

### Other possible causes to investigate
1. **Binary not rebuilt** — The EC2 site may not have been rebuilt after the latest pushes. Check if the running binary includes the status page code.
2. **Startup crash** — `NewHandler` starts 3 goroutines (`runSnapshotWriter`, `runDiscordPoller`, `runRetentionCleanup`). If any panic, it could affect the handler.
3. **Missing DB columns** — Before commit `6e0acdf`, the `metrics_snapshots` table on production was missing `discord_members` and `worlds_hatched` columns. The `writeSnapshot` INSERT would fail every 30s. This was fixed but may not be deployed yet.
4. **`PageViewMiddleware`** — Added globally in `site/main.go:107`. If the `page_views` table is missing the `visitor_hash` column, every request would fail. (Migration exists at `db.go:84`.)

## Artifacts

- `thoughts/CoreyCole/handoffs/general/2026-02-16_03-26-29_status-page-graph-deploy-fix.md` — previous handoff with full context on code changes

## Action Items & Next Steps

### 1. Check deploy state
```bash
# Is the binary up to date?
journalctl -u creative-mode-site -n 200 --no-pager

# When was the binary last modified?
ls -la /tmp/creative-mode-site

# What commit is checked out?
cd ~/creative-mode && git log --oneline -3
```

### 2. Rebuild and deploy if needed
```bash
cd ~/creative-mode/site && git pull && just build && cp site-linux /tmp/creative-mode-site && sudo systemctl restart creative-mode-site
```

### 3. Test SSE locally on EC2 (bypass API Gateway)
```bash
# This is the critical test — does SSE work when hitting the binary directly?
curl -N http://localhost:80/status/events
```
- If events stream: the app works, API Gateway is the problem
- If connection hangs with no output: server-side bug
- If connection refused: binary isn't running or listening on wrong port

### 4. Check for errors in logs after restart
```bash
journalctl -u creative-mode-site -f
```
Look for:
- Panic stack traces
- "snapshot: failed to insert" errors (missing DB columns)
- "failed to patch" errors (SSE write failures)

### 5. Test from browser through API Gateway
Open `https://creative-mode.ai/status` in browser dev tools → Network tab. Look at the `/status/events` request:
- Does it connect? What status code?
- Does it receive any data before closing?
- How long until it closes? (30s = API Gateway timeout)

## Other Notes

- The site runs as a systemd service `creative-mode-site` on EC2. Binary at `/tmp/creative-mode-site`, env at `~/.config/creative-mode/site.env`, working dir `~/creative-mode/site`.
- The systemd unit is at `site/creative-mode-site.service` — uses `CAP_NET_BIND_SERVICE` to bind port 80.
- Default port is 80 (env var `PORT`, defaults in `site/main.go:381`).
- If the root cause IS API Gateway, the planned fix is to add Caddy as a reverse proxy on EC2 for TLS termination and SSE streaming support, replacing API Gateway for creative-mode.ai only. Plan at `.claude/plans/steady-painting-kernighan.md`.
