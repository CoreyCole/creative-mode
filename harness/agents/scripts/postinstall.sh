#!/usr/bin/env bash
# Patch: mapCodexEvents hangs after response.completed when HTTP body stays open
# PR: https://github.com/CoreyCole/pi-mono/tree/fix/codex-stream-hang
# Remove this script once the fix is released upstream in @mariozechner/pi-ai

set -euo pipefail

FILE="node_modules/@mariozechner/pi-ai/dist/providers/openai-codex-responses.js"

if [ ! -f "$FILE" ]; then
  exit 0
fi

if grep -q 'type: "response.completed", response: normalizedResponse' "$FILE"; then
  if grep -A1 'type: "response.completed", response: normalizedResponse' "$FILE" | grep -q 'continue;'; then
    sed -i '/type: "response.completed", response: normalizedResponse/{n;s/continue;/return;/}' "$FILE"
    echo "[postinstall] Patched mapCodexEvents: continue -> return after response.completed"
  else
    echo "[postinstall] mapCodexEvents patch no longer needed — consider removing scripts/postinstall.sh"
  fi
fi
