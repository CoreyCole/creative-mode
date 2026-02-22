# OpenClaw Gateway Installation — Updated Plan

## Context

We want the harness to talk directly to the OpenClaw gateway HTTP API as the orchestrator for mayor agents. The first step is getting OpenClaw installed and running on the VPS. The previous plan (2026-02-20) proposed copying source to `/opt/openclaw/` and hand-writing config — this update uses the proper installation tools and integrates into the VPS bootstrap script.

## Current State

- **OpenClaw source** already at `/opt/openclaw/` (copied earlier in this session), dependencies installed, CLI works: `node /opt/openclaw/openclaw.mjs --version` → `2026.2.20`
- **No data directory** — `data/openclaw/` doesn't exist, no config, no workspaces
- **No systemd service** — gateway not running, health endpoint unreachable
- **No `.bin/openclaw` symlink** — the Go code's hardcoded path `/opt/openclaw/node_modules/.bin/openclaw` is broken
- **`systemctl --user` doesn't work** — no D-Bus session, Linger=no. Must use system-level service.
- **Discord env vars now set** — `DISCORD_BOT_TOKEN`, `DISCORD_GUILD_ID`, `DISCORD_WORLDS_CATEGORY_ID` in `.env`

## What We're Doing

1. Add OpenClaw installation step (15e) to `scripts/vps-bootstrap.sh`
2. Rewrite `harness/scripts/setup-openclaw.sh` to use `openclaw onboard --non-interactive` + `openclaw config set`
3. Create system-level systemd service for the gateway
4. Fix the Go CLI path (`resolveOpenclawPaths` in `main.go`)
5. Update `scripts/harness-run.sh` with `OPENCLAW_BIN`

## What We're NOT Doing

- Mayor widget UI (separate effort)
- OpenClaw HTTP client in Go (Phase 3, after gateway is running)
- Mayor provisioning bug fix (Phase 2)
- President agent setup

---

## Phase 1: Fix Go CLI Path

**File**: `harness/main.go:341-348`

```go
func resolveOpenclawPaths(dataDir string) (home, bin string) {
    home = os.Getenv("OPENCLAW_HOME")
    if home == "" {
        home = filepath.Join(dataDir, "openclaw")
    }
    bin = os.Getenv("OPENCLAW_BIN")
    if bin == "" {
        bin = "/opt/openclaw/openclaw.mjs"
    }
    return home, bin
}
```

**File**: `scripts/harness-run.sh` — add after line 28:
```bash
export OPENCLAW_BIN=/opt/openclaw/openclaw.mjs
```

**Rationale**: `openclaw.mjs` has `#!/usr/bin/env node` shebang and execute permissions. Node is on PATH via `nix-daemon.sh` sourced earlier in the script. Making it configurable via `OPENCLAW_BIN` env var follows the existing `OPENCLAW_HOME` pattern.

---

## Phase 2: Rewrite `setup-openclaw.sh`

**File**: `harness/scripts/setup-openclaw.sh`

Replace the raw JSON writing with proper CLI commands:

```bash
#!/bin/bash
set -euo pipefail

# Required env vars
OPENCLAW_HOME="${OPENCLAW_HOME:?OPENCLAW_HOME must be set}"
OPENCLAW_BIN="${OPENCLAW_BIN:-/opt/openclaw/openclaw.mjs}"
ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY must be set}"

# Optional env vars (for Discord adapter)
DISCORD_BOT_TOKEN="${DISCORD_BOT_TOKEN:-}"
DISCORD_GUILD_ID="${DISCORD_GUILD_ID:-}"

# Generate gateway token if not set
OPENCLAW_GATEWAY_TOKEN="${OPENCLAW_GATEWAY_TOKEN:-$(openssl rand -hex 32)}"

CONFIG_DIR="$OPENCLAW_HOME"
mkdir -p "$CONFIG_DIR/workspaces"

# Step 1: Run non-interactive onboarding if no config exists
if [ ! -f "$CONFIG_DIR/openclaw.json" ]; then
    "$OPENCLAW_BIN" onboard \
        --non-interactive \
        --accept-risk \
        --mode local \
        --auth-choice apiKey \
        --anthropic-api-key "$ANTHROPIC_API_KEY" \
        --gateway-port 18789 \
        --gateway-auth token \
        --gateway-token "$OPENCLAW_GATEWAY_TOKEN" \
        --no-install-daemon \
        --skip-skills \
        --skip-channels \
        --skip-ui
    echo "OpenClaw onboarding complete"
else
    echo "OpenClaw config already exists — skipping onboard"
fi

# Step 2: Enable chatCompletions endpoint
"$OPENCLAW_BIN" config set gateway.http.endpoints.chatCompletions.enabled true

# Step 3: Configure Discord adapter (if env vars set)
if [ -n "$DISCORD_BOT_TOKEN" ] && [ -n "$DISCORD_GUILD_ID" ]; then
    "$OPENCLAW_BIN" config set channels.discord.token "$DISCORD_BOT_TOKEN"
    "$OPENCLAW_BIN" config set "channels.discord.guilds.$DISCORD_GUILD_ID.channels.*" '{"allow": true}' --strict-json
    echo "Discord adapter configured"
fi

echo "OPENCLAW_GATEWAY_TOKEN=$OPENCLAW_GATEWAY_TOKEN"
echo "Setup complete. Start gateway with: systemctl start openclaw-gateway"
```

---

## Phase 3: Add Step 15e to `vps-bootstrap.sh`

Insert between Step 15d (playwright-cli) and Step 16 (start server):

```bash
# ============================================================================
# Step 15e: Configure OpenClaw gateway
# ============================================================================
# OpenClaw is the agent framework for world mayors. The gateway runs as a
# separate systemd service on port 18789, providing the /v1/chat/completions
# API that the harness uses for mayor chat.
# Pre-requisite: /opt/openclaw/ must exist with built source.
# ============================================================================
section "Step 15e: Configure OpenClaw gateway"

OPENCLAW_BIN="/opt/openclaw/openclaw.mjs"
OPENCLAW_DATA="$CREATIVE_MODE_DIR/data/openclaw"

if [ ! -f "$OPENCLAW_BIN" ]; then
    warn "OpenClaw not found at $OPENCLAW_BIN — skipping gateway setup"
    warn "To install: cp -r context/openclaw /opt/openclaw && cd /opt/openclaw && pnpm install && pnpm build"
else
    # Generate gateway token if not already in .env
    if ! grep -q 'OPENCLAW_GATEWAY_TOKEN' "$ENV_FILE" 2>/dev/null; then
        if $DRY_RUN; then
            info "Would generate OPENCLAW_GATEWAY_TOKEN and append to .env"
        else
            OC_TOKEN=$(openssl rand -hex 32)
            echo "" >> "$ENV_FILE"
            echo "# OpenClaw gateway auth token" >> "$ENV_FILE"
            echo "OPENCLAW_GATEWAY_TOKEN=$OC_TOKEN" >> "$ENV_FILE"
            ok "Generated OPENCLAW_GATEWAY_TOKEN"
        fi
    else
        skip "OPENCLAW_GATEWAY_TOKEN already in .env"
    fi

    # Run setup-openclaw.sh (idempotent — checks for existing config)
    if [ -f "$OPENCLAW_DATA/openclaw.json" ]; then
        skip "OpenClaw config already exists"
    else
        if $DRY_RUN; then
            info "Would run setup-openclaw.sh to configure gateway"
        else
            sudo -u deploy bash -lc "
                source $CREATIVE_MODE_DIR/harness/.env && \
                export OPENCLAW_HOME=$OPENCLAW_DATA && \
                export OPENCLAW_BIN=$OPENCLAW_BIN && \
                bash $CREATIVE_MODE_DIR/harness/scripts/setup-openclaw.sh
            "
            ok "OpenClaw configured"
        fi
    fi

    # Create systemd service
    if [ -f /etc/systemd/system/openclaw-gateway.service ]; then
        skip "openclaw-gateway.service already exists"
    else
        if $DRY_RUN; then
            info "Would create openclaw-gateway.service"
        else
            NODE_BIN=$(sudo -u deploy bash -lc 'source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh && which node')
            cat > /etc/systemd/system/openclaw-gateway.service << SVCEOF
[Unit]
Description=OpenClaw Gateway
After=network-online.target
Wants=network-online.target
Before=creative-mode.service

[Service]
Type=simple
User=deploy
WorkingDirectory=/opt/openclaw
ExecStart=$NODE_BIN /opt/openclaw/openclaw.mjs gateway run
Restart=always
RestartSec=5
KillMode=process
Environment=HOME=/home/deploy
Environment=OPENCLAW_HOME=$OPENCLAW_DATA
Environment=NODE_ENV=production
EnvironmentFile=$CREATIVE_MODE_DIR/harness/.env

[Install]
WantedBy=multi-user.target
SVCEOF
            systemctl daemon-reload
            systemctl enable openclaw-gateway
            ok "Created and enabled openclaw-gateway.service"
        fi
    fi

    # Start the gateway
    if systemctl is-active --quiet openclaw-gateway; then
        skip "openclaw-gateway is already running"
    else
        if $DRY_RUN; then
            info "Would start openclaw-gateway"
        else
            systemctl start openclaw-gateway
            sleep 2
            if curl -sf http://localhost:18789/health >/dev/null 2>&1; then
                ok "OpenClaw gateway running (health check passed)"
            else
                warn "OpenClaw gateway started but health check failed — check: journalctl -u openclaw-gateway -n 20"
            fi
        fi
    fi
fi
```

Also update Step 16 (creative-mode.service) to depend on the gateway:
- Add `After=openclaw-gateway.service` and `Wants=openclaw-gateway.service` to the `[Unit]` section

---

## Phase 4: Env Var Flow

**New env var**: `OPENCLAW_GATEWAY_TOKEN` — auto-generated in bootstrap Step 15e, appended to `.env`

**Flow**:
```
harness/.env (single source of truth for secrets)
  ├─→ creative-mode.service (EnvironmentFile) → harness reads OPENCLAW_GATEWAY_TOKEN
  └─→ openclaw-gateway.service (EnvironmentFile) → gateway reads OPENCLAW_GATEWAY_TOKEN
                                                    (via --token default: env if set)

harness-run.sh (static paths, not secrets)
  ├─→ OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw
  └─→ OPENCLAW_BIN=/opt/openclaw/openclaw.mjs
```

Gateway reads `OPENCLAW_GATEWAY_TOKEN` from env automatically (confirmed: `--token` flag docs say "default: OPENCLAW_GATEWAY_TOKEN env if set"). No need to pass `--token` in ExecStart.

---

## Verification

After all phases:

```bash
# Gateway is running
systemctl is-active openclaw-gateway  # → active
curl http://localhost:18789/health     # → 200

# CLI works from harness context
OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw \
  /opt/openclaw/openclaw.mjs agents list

# Config has chatCompletions enabled
OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw \
  /opt/openclaw/openclaw.mjs config get gateway.http.endpoints.chatCompletions.enabled
# → true

# Harness builds with new paths
cd /home/deploy/creative-mode && just check

# Gateway survives restart
sudo systemctl restart openclaw-gateway
sleep 2 && curl -sf http://localhost:18789/health
```

---

## Execution Order (for this session)

Script-first approach: update all scripts, then test by running them.

1. Fix `resolveOpenclawPaths` in `main.go` + update `harness-run.sh`
2. Rewrite `harness/scripts/setup-openclaw.sh`
3. Add Step 15e to `vps-bootstrap.sh` (includes source copy, pnpm build, config, systemd service)
4. Run the bootstrap Step 15e manually to test it
5. Verify health + `just check`

## References

- Previous plan: `thoughts/CoreyCole/plans/2026-02-20_21-51-08_openclaw-setup-and-mayor-widget.md`
- Review: `thoughts/CoreyCole/reviews/2026-02-20_21-58-45_openclaw-setup-and-mayor-widget_review.md`
- OpenClaw CLI help: `node /opt/openclaw/openclaw.mjs onboard --help`, `gateway run --help`
- Go CLI path: `harness/main.go:341-348` (`resolveOpenclawPaths`)
- systemd wrapper: `scripts/harness-run.sh`
- Existing setup: `harness/scripts/setup-openclaw.sh`
- Bootstrap: `scripts/vps-bootstrap.sh` (Step 15d ends at line 908, Step 16 at line 917)
