#!/bin/bash
EVENT_JSON=$(cat)
WORLD_ID="${CM_WORLD_ID}"
CP_ID="${CM_CHECKPOINT_ID}"
HARNESS_URL="${CM_HARNESS_URL:-http://localhost:8080}"
LOG_FILE="${CM_LOG_DIR}/claude.jsonl"

JSONL=$(jq -n \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg worldID "$WORLD_ID" \
  --arg cpID "$CP_ID" \
  '{ts: $ts, level: "info", event: "claude.session_stopped", worldID: $worldID, cpID: $cpID}')

echo "$JSONL" >> "$LOG_FILE"

# Notify harness that claude is done - triggers the build pipeline
curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  -d "$JSONL" &>/dev/null &
