#!/bin/bash
set -euo pipefail

# =============================================================================
# setup-openclaw.sh — Initialize OpenClaw config for Creative Mode agents
# =============================================================================
# Creates $OPENCLAW_HOME/openclaw.json with Discord adapter config.
# Idempotent: skips if config already exists.
#
# Required env vars:
#   OPENCLAW_HOME            — OpenClaw data directory
#   DISCORD_BOT_TOKEN        — Discord bot token
#   DISCORD_GUILD_ID         — Discord guild (server) ID
# =============================================================================

OPENCLAW_HOME="${OPENCLAW_HOME:?OPENCLAW_HOME must be set}"
DISCORD_BOT_TOKEN="${DISCORD_BOT_TOKEN:?DISCORD_BOT_TOKEN must be set}"
DISCORD_GUILD_ID="${DISCORD_GUILD_ID:?DISCORD_GUILD_ID must be set}"

CONFIG_FILE="$OPENCLAW_HOME/openclaw.json"

if [ -f "$CONFIG_FILE" ]; then
    echo "OpenClaw config already exists at $CONFIG_FILE — skipping"
    exit 0
fi

mkdir -p "$OPENCLAW_HOME/workspaces"

cat > "$CONFIG_FILE" <<EOF
{
  "channels": {
    "discord": {
      "token": "${DISCORD_BOT_TOKEN}",
      "guilds": {
        "${DISCORD_GUILD_ID}": {
          "channels": { "*": { "allow": true } }
        }
      }
    }
  },
  "agents": { "list": [] },
  "bindings": []
}
EOF

echo "OpenClaw config created at $CONFIG_FILE"
