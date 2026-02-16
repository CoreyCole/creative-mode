#!/bin/bash
# Receives tool use event JSON on stdin from Claude Code
# Arg $1: "pre" or "post"
PHASE="$1"
EVENT_JSON=$(cat)

# Extract fields from the Claude Code hook payload
TOOL=$(echo "$EVENT_JSON" | jq -r '.tool_name // .tool // "unknown"')
FILE=$(echo "$EVENT_JSON" | jq -r '.input.file_path // .input.command // ""' | head -c 200)

# Read world/checkpoint IDs from env vars (set by harness before launching session)
WORLD_ID="${CM_WORLD_ID}"
CP_ID="${CM_CHECKPOINT_ID}"
HARNESS_URL="${CM_HARNESS_URL:-http://localhost:8080}"
LOG_FILE="${CM_LOG_DIR}/claude.jsonl"

# Build JSONL event
JSONL=$(jq -n \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg phase "$PHASE" \
  --arg tool "$TOOL" \
  --arg file "$FILE" \
  --arg worldID "$WORLD_ID" \
  --arg cpID "$CP_ID" \
  '{ts: $ts, level: "info", event: ("claude.tool_use." + $phase), worldID: $worldID, cpID: $cpID, tool: $tool, file: $file}')

# Append to JSONL log file
echo "$JSONL" >> "$LOG_FILE"

# POST to harness for live SSE updates (fire-and-forget)
curl -s -X POST "$HARNESS_URL/api/claude-event" \
  -H "Content-Type: application/json" \
  ${CM_HOOK_SECRET:+-H "X-Hook-Secret: $CM_HOOK_SECRET"} \
  -d "$JSONL" &>/dev/null &
