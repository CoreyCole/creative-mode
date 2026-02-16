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

# Notify harness — retry up to 5 times in case harness is restarting.
for attempt in 1 2 3 4 5; do
  HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$HARNESS_URL/api/claude-event" \
    -H "Content-Type: application/json" \
    ${CM_HOOK_SECRET:+-H "X-Hook-Secret: $CM_HOOK_SECRET"} \
    -d "$JSONL" 2>/dev/null)

  if [ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "204" ]; then
    echo "{\"ts\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"level\":\"info\",\"event\":\"hook.on_stop.delivered\",\"attempt\":$attempt}" >> "$LOG_FILE"
    exit 0
  fi

  echo "{\"ts\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"level\":\"warn\",\"event\":\"hook.on_stop.retry\",\"attempt\":$attempt,\"http_code\":\"$HTTP_CODE\"}" >> "$LOG_FILE"
  sleep 2
done

echo "{\"ts\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"level\":\"error\",\"event\":\"hook.on_stop.failed\",\"attempts\":5}" >> "$LOG_FILE"
