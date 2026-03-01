#!/bin/bash
set -euo pipefail

# =============================================================================
# setup-openclaw.sh — Initialize OpenClaw config for Creative Mode agents
# =============================================================================
# Uses `openclaw onboard --non-interactive` for initial config, then applies
# additional settings (chatCompletions endpoint, Discord adapter) via
# `openclaw config set`.
#
# Idempotent: skips onboarding if config already exists, config set is safe
# to re-run.
#
# Required env vars:
#   OPENCLAW_HOME            — OpenClaw data directory
#   ANTHROPIC_API_KEY        — Anthropic API key (for agent reasoning)
#
# Optional env vars:
#   OPENCLAW_BIN             — Path to openclaw CLI (default: /opt/openclaw/openclaw.mjs)
#   OPENCLAW_GATEWAY_TOKEN   — Gateway auth token (auto-generated if not set)
#   DISCORD_BOT_TOKEN        — Discord bot token (for Discord adapter)
#   DISCORD_GUILD_ID         — Discord guild ID (for Discord adapter)
# =============================================================================

OPENCLAW_HOME="${OPENCLAW_HOME:?OPENCLAW_HOME must be set}"
ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY must be set}"
OPENCLAW_BIN="${OPENCLAW_BIN:-/opt/openclaw/openclaw.mjs}"

DISCORD_BOT_TOKEN="${DISCORD_BOT_TOKEN:-}"
DISCORD_GUILD_ID="${DISCORD_GUILD_ID:-}"
OPENCLAW_GATEWAY_TOKEN="${OPENCLAW_GATEWAY_TOKEN:-$(openssl rand -hex 32)}"

export OPENCLAW_HOME

# Config lives at $OPENCLAW_HOME/.openclaw/openclaw.json (created by onboard)
CONFIG_FILE="$OPENCLAW_HOME/.openclaw/openclaw.json"
mkdir -p "$OPENCLAW_HOME/workspaces"

# Step 1: Run non-interactive onboarding if no config exists
if [ -f "$CONFIG_FILE" ]; then
    echo "OpenClaw config already exists at $CONFIG_FILE — skipping onboard"
else
    echo "Running OpenClaw onboarding (non-interactive)..."
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
        --skip-health \
        --skip-ui
    echo "OpenClaw onboarding complete"
fi

# Step 2: Enable chatCompletions endpoint (idempotent)
echo "Enabling chatCompletions endpoint..."
"$OPENCLAW_BIN" config set gateway.http.endpoints.chatCompletions.enabled true

# Step 3: Configure Discord adapter (if bot token set)
if [ -n "$DISCORD_BOT_TOKEN" ]; then
    echo "Configuring Discord adapter..."
    "$OPENCLAW_BIN" config set channels.discord.token "$DISCORD_BOT_TOKEN"
    # Use "open" policy — the harness manages which channels to use, not OpenClaw
    "$OPENCLAW_BIN" config set channels.discord.groupPolicy open
    echo "Discord adapter configured"
else
    echo "DISCORD_BOT_TOKEN not set — skipping Discord adapter"
fi

echo ""
echo "OpenClaw setup complete."
echo "  Config: $CONFIG_FILE"
echo "  Gateway token: ${OPENCLAW_GATEWAY_TOKEN:0:8}..."
echo "  Start gateway: systemctl start openclaw-gateway"
