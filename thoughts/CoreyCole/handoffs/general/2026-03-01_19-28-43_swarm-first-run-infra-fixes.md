---
date: 2026-03-01T19:28:43-08:00
researcher: CoreyCole
git_commit: 5e4b5b1c95631c50a40cf1a009293a237db7de19
branch: feature/agent-swarm
repository: creative-mode
topic: "Swarm First Run — Infrastructure Fixes and End-to-End Test"
tags: [swarm, infrastructure, deployment, first-run, CRE-5]
status: in_progress
last_updated: 2026-03-01
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Swarm First Run — Infrastructure Fixes and E2E Test

## Task(s)

### Completed
1. **Resumed handoff** from `thoughts/CoreyCole/handoffs/general/2026-03-01_18-12-35_swarm-implementation-gaps.md` — all 7 gap-closure phases were already implemented.
2. **Ran `just check`** — all lints pass.
3. **Reviewed and simplified** — found/removed one redundant `m.loadConfig(ctx)` call in `advanceWorkflow`.
4. **Updated `harness/CLAUDE.md`** — documented RevisionTarget, MaxProjectVerifyRetries, token capture, retrospectives, high retry alert, severity-weighted decay.
5. **Committed** as `5e4b5b1` on `feature/agent-swarm`.
6. **Set up swarm infrastructure** — added env vars, created Linear ticket, enabled gateProjectReview.
7. **Fixed 4 critical infrastructure bugs** blocking swarm execution (see Learnings).

### In Progress
8. **CRE-5 tech debt project workflow** (`124c06f6`) — research phase completed successfully, wrote output to `thoughts/swarm/research/2026-03-02_02-48-02_CRE-5_tech-debt-audit.md`. However, the Stop hook failed (see below), so the workflow is stuck in `status: running` even though research is done. The **localhost hook fix is uncommitted** — air will pick it up on next restart but the current workflow needs manual intervention.
9. **Uncommitted fixes** — 4 files changed but not yet committed (db.go, manager.go, temporal.go, main.go).

### Not Started
10. **Verify Linear integration** — Linear comments show "ticket not found" for CRE-5. The `LINEAR_TEAM_KEY=CRE` is set but the Linear client may need the full ticket identifier format, or the ticket isn't being found by the API. Needs debugging.
11. **Verify Discord alerts** — First alert attempt got HTTP 403 "Missing Access" on channel `1477854966348644362`. The bot likely needs permissions to post in that channel.

## Critical References

- `thoughts/CoreyCole/handoffs/general/2026-03-01_18-12-35_swarm-implementation-gaps.md` — previous handoff with all 7 implementation phases
- `thoughts/swarm/research/2026-03-02_02-48-02_CRE-5_tech-debt-audit.md` — research output from the swarm's first successful run
- `harness/CLAUDE.md` — full swarm architecture documentation (updated this session)

## Recent changes

- `harness/internal/db/db.go:110-112` — Added migrations 007, 008, 009 to the migration runner list (they existed as files but weren't registered)
- `harness/internal/swarmorch/manager.go:319-332` — Changed from `CLAUDE_CONFIG_DIR` override to `--settings <path>` flag for hooks config (fixes auth breakage)
- `harness/internal/swarmorch/manager.go:1108-1130` — Rewrote `sendSkillPrompt` to pass prompt as positional arg instead of `--input-file` (flag removed in claude CLI v2.1.63)
- `harness/internal/swarmorch/manager.go:322` — Hardcoded `http://localhost:8080` for hooks URL (Claude Code blocks private/link-local addresses in HTTP hooks)
- `harness/main.go:253-257` — Changed alert manager to use `DISCORD_SWARM_CHANNEL_ID` env var (falls back to `DISCORD_PRESIDENT_CHANNEL_ID`)
- `harness/.env` — Added `DISCORD_SWARM_CHANNEL_ID`, `LINEAR_API_KEY`, `LINEAR_TEAM_KEY=CRE`
- `harness/internal/swarmorch/manager.go:43` — Added `envTrue` const to fix goconst lint
- `harness/internal/swarmorch/temporal.go:41` — Use `envTrue` const

## Learnings

- **`CLAUDE_CONFIG_DIR` breaks auth**: Setting this env var on tmux sessions overrides where claude looks for `.credentials.json`. The swarm was setting it to `/tmp/swarm-hooks-{sessionID}/` which only had `settings.json`. Fix: use `--settings <path>` CLI flag instead, which merges settings without overriding the config dir.
- **`--input-file` removed from claude CLI**: Claude Code v2.1.63 no longer has this flag. The prompt must be passed as a positional argument (interactive mode) or via `-p` (print mode). Swarm needs interactive mode for hooks to fire.
- **Claude Code HTTP hooks block private IPs**: Hooks to Tailscale addresses (`100.x.x.x`) are blocked. Only `127.0.0.1`/`::1` are allowed. The harness URL (`https://claude-2.tailcdc985.ts.net`) resolves to a Tailscale IP, so hooks must use `http://localhost:8080`.
- **Migrations not auto-discovered**: The migration runner in `db.go` has a hardcoded list. New migration files must be added to `migrationFiles` slice manually.
- **Linear API team key**: The Linear workspace has team key `CRE` (not `CM` as defaulted). But even with correct key, `linear comment skip: ticket not found` appears — the Linear client may need investigation.
- **Discord bot permissions**: The bot token doesn't have access to the swarm alerts channel (403 Missing Access). The channel needs bot permissions configured in Discord.
- **Swarm config in DB**: `gateProjectReview: true` was set via direct SQLite update. This persists across restarts. Check with: `sqlite3 data/creative-mode.db "SELECT config FROM swarm_config WHERE id = 'default';"`

## Artifacts

- `harness/internal/db/db.go:110-112` — migration list fix
- `harness/internal/swarmorch/manager.go` — multiple fixes (hooks config, sendSkillPrompt, localhost URL, envTrue const)
- `harness/internal/swarmorch/temporal.go:41` — envTrue const
- `harness/main.go:252-270` — DISCORD_SWARM_CHANNEL_ID support
- `harness/.env` — new env vars (DISCORD_SWARM_CHANNEL_ID, LINEAR_API_KEY, LINEAR_TEAM_KEY)
- `thoughts/swarm/research/2026-03-02_02-48-02_CRE-5_tech-debt-audit.md` — swarm research output

## Action Items & Next Steps

1. **Commit uncommitted fixes** — 4 files with infrastructure fixes need to be committed: `harness/internal/db/db.go`, `harness/internal/swarmorch/manager.go`, `harness/internal/swarmorch/temporal.go`, `harness/main.go`. All pass `just check`.

2. **Unstick workflow 124c06f6** — The research session completed but the Stop hook failed (private IP block). Options:
   - Cancel workflow `124c06f6` via `POST /api/swarm/cancel` with hook secret, then start a new one
   - Or manually update the DB: mark session complete, advance workflow to `project_plan` phase
   - The localhost hook fix is in the uncommitted code — air should have already picked it up

3. **Fix Discord bot permissions** — The bot got 403 on channel `1477854966348644362`. Either:
   - Add the bot to that channel in Discord server settings
   - Or create a new channel the bot already has access to and update `DISCORD_SWARM_CHANNEL_ID` in `.env`

4. **Debug Linear integration** — `linear comment skip: ticket not found` for CRE-5. Check `harness/internal/linear/` client to understand how it looks up tickets. The team key is `CRE` and ticket is `CRE-5` — verify the API query format.

5. **Monitor the next workflow run** — Once fixes are committed and a new workflow started for CRE-5:
   - Watch tmux session: `tmux attach -t cm-swarm-CRE-5-research`
   - Check status: `curl -s -H "X-Hook-Secret: $CM_HOOK_SECRET" http://localhost:8080/api/swarm/status/<workflow_id> | python3 -m json.tool`
   - Check logs: `sudo journalctl -u creative-mode --since "5 min ago" --no-pager | grep -iE 'swarm|CRE-5'`
   - Dashboard: accessible at harness URL `/swarm/<workflow_id>`

6. **Verify project_plan → project_review gate** — Once research completes and project_plan runs, the workflow should pause at `project_review` because `gateProjectReview: true`. Verify it enters `awaiting_review` status and shows the gate review panel on the dashboard.

## Other Notes

- **Hook secret for API calls**: stored in `.env` as `CM_HOOK_SECRET`
- **Linear API key**: stored in `.env` as `LINEAR_API_KEY`
- **Linear team**: key=`CRE`, id=`152e9cd6-534c-408c-a0d1-dfcaa3665864`
- **CRE-5 Linear URL**: https://linear.app/creative-mode/issue/CRE-5/tech-debt-audit-identify-and-consolidate-duplicated-patterns-improve
- **Previous failed workflows**: `8e46f8e6` (failed — auth issue), `1336f8a3` (failed — auth issue still). `124c06f6` is the first one that actually ran research successfully.
- **Swarm DB location**: `/home/deploy/creative-mode/data/creative-mode.db`
- **Service name**: `creative-mode` (systemd), runs air which hot-reloads on file changes
- **The `site (golangci-lint)` failure** in check output is pre-existing and unrelated to swarm changes.
