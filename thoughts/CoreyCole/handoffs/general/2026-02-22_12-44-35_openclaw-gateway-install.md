---
date: 2026-02-22T12:44:35-08:00
researcher: CoreyCole
git_commit: a9ad7f5a52eabdedeff48af51c45921e3b258def
branch: main
repository: creative-mode
topic: "OpenClaw Gateway Installation and VPS Bootstrap Integration"
tags: [implementation, openclaw, gateway, vps-bootstrap, mayor-agents]
status: complete
last_updated: 2026-02-22
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: OpenClaw Gateway Installation and VPS Bootstrap Integration

## Task(s)

**Phase 1 of the OpenClaw integration plan: Install and configure the OpenClaw gateway on the VPS. STATUS: COMPLETE.**

Working from the plan at `thoughts/CoreyCole/plans/2026-02-22_openclaw-gateway-install.md` (updated version of the original `2026-02-20_21-51-08_openclaw-setup-and-mayor-widget.md`). Phase 1 covers getting the gateway running so the harness can talk to it as an orchestrator for mayor agents via the `/v1/chat/completions` HTTP API.

The broader goal (discussed but not started): use the OpenClaw gateway as the orchestrator — the harness talks directly to the gateway HTTP API for mayor chat and agent management, replacing CLI `exec.CommandContext` calls.

## Critical References

- `thoughts/CoreyCole/plans/2026-02-22_openclaw-gateway-install.md` — Updated installation plan (this session's work)
- `thoughts/CoreyCole/plans/2026-02-20_21-51-08_openclaw-setup-and-mayor-widget.md` — Full 5-phase plan (OpenClaw setup + mayor widget UI)
- `thoughts/CoreyCole/reviews/2026-02-20_21-58-45_openclaw-setup-and-mayor-widget_review.md` — Staff eng review of the full plan

## Recent changes

- `harness/main.go:341-348` — `resolveOpenclawPaths` now reads `OPENCLAW_BIN` env var with default `/opt/openclaw/openclaw.mjs` (was hardcoded to broken path `/opt/openclaw/node_modules/.bin/openclaw`)
- `scripts/harness-run.sh:29` — Added `export OPENCLAW_BIN=/opt/openclaw/openclaw.mjs`
- `harness/scripts/setup-openclaw.sh` — Full rewrite: uses `openclaw onboard --non-interactive` + `openclaw config set` instead of hand-writing JSON
- `harness/internal/mayor/openclaw.go:170-172` — Health check now accepts HTTP 503 (Control UI assets not built, gateway still functional)
- `scripts/vps-bootstrap.sh` — Added Step 15e: full OpenClaw install (copy source, pnpm install+build, generate token, run setup, create systemd service, start gateway)
- `harness/.env` — Fixed Anthropic API key comment, removed duplicate HARNESS_URL, added OPENCLAW_GATEWAY_TOKEN

## Learnings

1. **Config file location**: OpenClaw `onboard` writes config to `$OPENCLAW_HOME/.openclaw/openclaw.json`, NOT `$OPENCLAW_HOME/openclaw.json`. The `.openclaw/` subdirectory is canonical.

2. **Build step required**: Copying the source from `context/openclaw/` is not enough — `pnpm build` is required to create `dist/entry.mjs`. Without it, the CLI throws "missing dist/entry.(m)js".

3. **`--skip-health` flag needed**: The onboard command tries to verify the gateway is running at the end. Since we're configuring before starting the service, `--skip-health` is required.

4. **Discord guild allowlist doesn't work via `config set`**: Setting `channels.discord.guilds.$GUILD_ID.channels.*` via dot-path fails with validation errors (numeric guild ID treated as array index). Solution: use `groupPolicy: "open"` instead — the harness manages which channels to use, not OpenClaw.

5. **Health endpoint returns 503 without Control UI**: The `/health` endpoint is served by the Control UI handler. Without built UI assets, it returns 503 with "Control UI assets not found". Disabling `controlUi.enabled` makes `/health` return 404 (worse). Keep UI enabled and accept 503 as "gateway is functional."

6. **`systemctl --user` doesn't work on this VPS**: No D-Bus session, `Linger=no`. OpenClaw's `--install-daemon` creates user-level services which won't work. Must use system-level `/etc/systemd/system/openclaw-gateway.service`.

7. **Gateway token flow**: The gateway reads `OPENCLAW_GATEWAY_TOKEN` from environment automatically (confirmed in `gateway run --help`: "default: OPENCLAW_GATEWAY_TOKEN env if set"). No `--token` flag needed in ExecStart — the `EnvironmentFile` directive handles it.

8. **Anthropic API key usage**: `ANTHROPIC_API_KEY` is used by the create-world onboarding chat (`mayorchat.NewClient`) and the OpenClaw gateway. Claude Code sessions use CLI OAuth, not this key.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-22_openclaw-gateway-install.md` — Updated installation plan
- `harness/scripts/setup-openclaw.sh` — Rewritten setup script
- `scripts/vps-bootstrap.sh:910-1010` — Step 15e (OpenClaw install + config + systemd)
- `/etc/systemd/system/openclaw-gateway.service` — Created on VPS (not in repo)
- `/opt/openclaw/` — Built OpenClaw v2026.2.20 installation on VPS
- `/home/deploy/creative-mode/data/openclaw/.openclaw/openclaw.json` — Gateway config on VPS

## Action Items & Next Steps

1. **Phase 2: Fix mayor provisioning + Discord binding** — `provisionAgent()` in `harness/internal/mayor/openclaw.go:21-46` creates agents but never calls `BindAgentToDiscord()`. Fix is described in the full plan. Also add `OnProvision` callback for dynamic listener registration.

2. **Phase 3: Build OpenClaw HTTP client in Go** — Create `harness/internal/openclaw/client.go` that calls `/v1/chat/completions` with streaming. Key details: use `model: "openclaw/<agentId>"` for agent routing, `user` field for deterministic session keys (`agent:<agentId>:openai-user:<userID>`). Wire into `server.go`.

3. **Phase 4: Mayor widget UI** — Persistent bottom-left FAB + chat panel using Datastar SSE. See full plan for detailed UI spec.

4. **Phase 5: Build mode integration** — Chat vs Build mode toggle in the widget.

5. **Commit the changes** — The current changes (Go code, scripts, bootstrap) are uncommitted.

## Other Notes

- The OpenClaw gateway is live on the VPS at `localhost:18789`, running as `openclaw-gateway.service`
- Chat completions endpoint verified working: `curl -X POST http://localhost:18789/v1/chat/completions -H "Authorization: Bearer $OPENCLAW_GATEWAY_TOKEN" -H "Content-Type: application/json" -d '{"model":"openclaw","messages":[{"role":"user","content":"hello"}],"stream":false}'`
- The gateway auto-detected and connected to Discord on startup (logged in as bot 1472078170965413898)
- OpenClaw reference source is at `context/openclaw/` (v2026.2.20) — useful for understanding gateway internals
- The gateway exposes 95 WebSocket RPC methods beyond just HTTP — `chat.send`, `agents.create`, `sessions.*`, `config.*` etc. These could replace CLI calls in future phases.
- The full OpenClaw gateway API analysis is available in the conversation context (from the `codebase-analyzer` agent) — covers all HTTP endpoints, WS methods, auth modes, session management
