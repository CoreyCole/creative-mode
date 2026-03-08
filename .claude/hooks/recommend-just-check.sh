#!/bin/bash
# PreToolUse hook: Recommend `just check` instead of direct go/cargo/templ commands.
# These commands should go through the justfile to use isolated build dirs
# and avoid corrupting incremental builds (especially on macOS with Docker).

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

if [ -z "$COMMAND" ]; then
  exit 0
fi

# Extract the "real" command: strip any leading cd/pushd, look at each
# &&/; separated segment for actual build tool invocations.
# We split on && and ; then check if any segment starts with a build command
# (after optional cd prefix).

BLOCKED=false
REASON=""

# Process each command segment separated by && or ;
while IFS= read -r seg; do
  # Trim whitespace
  seg="$(echo "$seg" | xargs)"
  [ -z "$seg" ] && continue

  # Skip segments that are clearly not build commands (echo, cat, grep, pipe targets, etc.)
  case "$seg" in
    echo\ *|cat\ *|grep\ *|printf\ *|jq\ *|sed\ *|awk\ *|head\ *|tail\ *|curl\ *|test\ *|\[\ *)
      continue
      ;;
  esac

  # Strip leading "cd ... &&" or "cd ..." prefix within the segment
  stripped="$seg"
  if echo "$stripped" | grep -qE '^cd '; then
    # This segment is just a cd, skip it
    continue
  fi

  # Check for blocked commands
  if echo "$stripped" | grep -qE '^go (build|vet|test|run)( |$)'; then
    BLOCKED=true
    REASON="go ${BASH_REMATCH[0]}"
    break
  fi
  if echo "$stripped" | grep -qE '^cargo (build|clippy|check)( |$)'; then
    BLOCKED=true
    break
  fi
  if echo "$stripped" | grep -qE '^templ generate'; then
    BLOCKED=true
    break
  fi
  if echo "$stripped" | grep -qE '^just generate'; then
    BLOCKED=true
    break
  fi
done <<< "$(echo "$COMMAND" | sed 's/ *&& */\n/g; s/ *; */\n/g')"

if [ "$BLOCKED" = true ]; then
  cat <<HOOKEOF
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Direct build/test commands are not allowed — they can corrupt incremental builds when Docker is bind-mounting the project. Use \`just check\` from the project root instead (it uses an isolated CARGO_TARGET_DIR). On VPS, \`just vps-build\` from harness/ is also acceptable. See CLAUDE.md for details."
  }
}
HOOKEOF
  exit 0
fi

exit 0
