#!/bin/bash
EVENT_JSON=$(cat)
WORLD_ID="${CM_WORLD_ID}"
CP_ID="${CM_CHECKPOINT_ID}"
LOG_FILE="${CM_LOG_DIR}/claude.jsonl"
HARNESS_URL="${CM_HARNESS_URL:-http://localhost:8080}"

MESSAGE=$(echo "$EVENT_JSON" | jq -r '.message // ""')

JSONL=$(jq -n \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg worldID "$WORLD_ID" \
  --arg cpID "$CP_ID" \
  --arg message "$MESSAGE" \
  '{ts: $ts, level: "info", event: "claude.notification", worldID: $worldID, cpID: $cpID, message: $message}')

echo "$JSONL" >> "$LOG_FILE"

curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  -d "$JSONL" &>/dev/null &
