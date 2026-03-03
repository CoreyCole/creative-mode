#!/bin/bash
# PreToolUse hook: intercept denied build commands and suggest `just check`
INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command')

DENIED_PATTERNS=(
  "cargo build"
  "cargo clippy"
  "cargo check"
  "go build"
  "templ generate"
  "just generate"
)

for pattern in "${DENIED_PATTERNS[@]}"; do
  if [[ "$COMMAND" == "$pattern"* ]]; then
    jq -n '{
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: "This command is denied because it corrupts Docker bind-mount builds. Use `just check` from the project root instead (it uses an isolated CARGO_TARGET_DIR)."
      }
    }'
    exit 0
  fi
done

exit 0
