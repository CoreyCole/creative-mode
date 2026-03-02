#!/usr/bin/env bash
# Captures token usage from a Claude Code tmux session's output.
# Usage: swarm-capture-tokens.sh <session_name> <session_id>
#
# Writes total token count to /tmp/swarm-tokens-<session_id> if found.
# Returns 0 regardless — token capture is best-effort.

set -euo pipefail

SESSION_NAME="${1:-}"
SESSION_ID="${2:-}"

if [[ -z "$SESSION_NAME" || -z "$SESSION_ID" ]]; then
    exit 0
fi

OUTPUT_FILE="/tmp/swarm-tokens-${SESSION_ID}"

# Capture the last 200 lines of tmux pane output.
PANE_OUTPUT=$(tmux capture-pane -t "$SESSION_NAME" -p -S -200 2>/dev/null || true)

if [[ -z "$PANE_OUTPUT" ]]; then
    exit 0
fi

# Try to extract token count from Claude Code's cost summary.
# Claude Code displays: "Total tokens: 123,456" or "tokens: 123456"
TOKENS=$(echo "$PANE_OUTPUT" | grep -oP '[Tt]otal.?[Tt]okens:?\s*[\d,]+' | tail -1 | grep -oP '[\d,]+' | tr -d ',' || true)

if [[ -n "$TOKENS" ]]; then
    echo "$TOKENS" > "$OUTPUT_FILE"
fi

exit 0
