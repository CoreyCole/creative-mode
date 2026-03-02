#!/usr/bin/env bash
set -euo pipefail

# --- Swarm session context protection ---
# Claude Code passes JSON on stdin to Stop hooks. On the second stop attempt
# (after a blocking exit 2), the JSON includes "stop_hook_active": true.
# For swarm sessions, allow stop immediately on second attempt to avoid
# burning context on lint fix loops.

SWARM_SESSION="${CM_SWARM_SESSION_ID:-}"
STOP_HOOK_ACTIVE="false"

# Read stdin JSON (non-blocking). Claude Code sends hook payload on stdin.
if read -t 1 -r STDIN_LINE 2>/dev/null; then
  STOP_HOOK_ACTIVE=$(echo "$STDIN_LINE" | jq -r '.stop_hook_active // false' 2>/dev/null || echo "false")
fi

# Second stop in a swarm session: exit immediately to preserve context.
if [ -n "$SWARM_SESSION" ] && [ "$STOP_HOOK_ACTIVE" = "true" ]; then
  echo "swarm: allowing stop on second attempt (preserving context)"
  exit 0
fi

IS_SWARM="false"
if [ -n "$SWARM_SESSION" ]; then
  IS_SWARM="true"
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
failed=0

# Phase 1: Format (modifies files, must run first).
"$ROOT/scripts/fmt.sh"

# Phase 2: Generate (sqlc + templ + tailwind — catches template errors early).
(cd "$ROOT/harness" && sqlc generate 2>&1)
(cd "$ROOT/harness" && templ generate 2>&1)
(cd "$ROOT/harness" && just build-tailwind 2>&1)
(cd "$ROOT/site" && templ generate 2>&1)

# Phase 3: Lint in parallel (each linter includes compilation).
# CARGO_TARGET_DIR isolates host-side clippy from Docker's trunk builds.
# Without this, clippy writes to templates/*/target/ on the bind mount,
# corrupting incremental state that trunk is using inside the container.
CHECK_TARGET="/tmp/cm-check-target"

go_out=$(mktemp)
go_exit_file=$(mktemp)
site_go_out=$(mktemp)
site_go_exit_file=$(mktemp)
rs_clippy_server_out=$(mktemp)
rs_clippy_client_out=$(mktemp)
rs_clippy_2d_out=$(mktemp)
rs_clippy_boardgame_out=$(mktemp)
trap 'rm -f "$go_out" "$go_exit_file" "$site_go_out" "$site_go_exit_file" "$rs_clippy_server_out" "$rs_clippy_client_out" "$rs_clippy_2d_out" "$rs_clippy_boardgame_out"' EXIT

# golangci-lint uses a file lock — run harness and site sequentially, then
# combine into one background job so clippy can still run in parallel.
(
  (cd "$ROOT/harness" && golangci-lint run ./... 2>&1) >"$go_out" 2>&1
  echo $? > "$go_exit_file"
  (cd "$ROOT/site" && golangci-lint run ./... 2>&1) >"$site_go_out" 2>&1
  echo $? > "$site_go_exit_file"
) &
pid_go_all=$!

if [ "$IS_SWARM" = "false" ]; then
  (cd "$ROOT/templates/3d" && CARGO_TARGET_DIR="$CHECK_TARGET/3d" cargo clippy -p server -p shared -- -D warnings 2>&1) >"$rs_clippy_server_out" 2>&1 &
  pid_clippy_server=$!

  (cd "$ROOT/templates/3d" && CARGO_TARGET_DIR="$CHECK_TARGET/3d" cargo clippy -p client --target wasm32-unknown-unknown -- -D warnings 2>&1) >"$rs_clippy_client_out" 2>&1 &
  pid_clippy_client=$!

  (cd "$ROOT/templates/2d" && CARGO_TARGET_DIR="$CHECK_TARGET/2d" cargo clippy --target wasm32-unknown-unknown -- -D warnings 2>&1) >"$rs_clippy_2d_out" 2>&1 &
  pid_clippy_2d=$!

  (cd "$ROOT/templates/boardgame" && CARGO_TARGET_DIR="$CHECK_TARGET/boardgame" cargo clippy --target wasm32-unknown-unknown -- -D warnings 2>&1) >"$rs_clippy_boardgame_out" 2>&1 &
  pid_clippy_boardgame=$!
fi

# Wait and report.
# Wait for the combined Go lint job, then check exit codes.
wait $pid_go_all || true

if [ "$(cat "$go_exit_file" 2>/dev/null)" = "0" ]; then
  echo "  ok  harness (golangci-lint)"
else
  echo "FAIL  harness (golangci-lint)" >&2
  cat "$go_out" >&2
  failed=1
fi

if [ "$(cat "$site_go_exit_file" 2>/dev/null)" = "0" ]; then
  echo "  ok  site (golangci-lint)"
else
  echo "FAIL  site (golangci-lint)" >&2
  cat "$site_go_out" >&2
  failed=1
fi

if [ "$IS_SWARM" = "false" ]; then
  if wait $pid_clippy_server; then
    echo "  ok  templates/3d/server+shared (clippy)"
  else
    echo "FAIL  templates/3d/server+shared (clippy)" >&2
    cat "$rs_clippy_server_out" >&2
    failed=1
  fi

  if wait $pid_clippy_client; then
    echo "  ok  templates/3d/client (clippy wasm)"
  else
    echo "FAIL  templates/3d/client (clippy wasm)" >&2
    cat "$rs_clippy_client_out" >&2
    failed=1
  fi

  if wait $pid_clippy_2d; then
    echo "  ok  templates/2d (clippy wasm)"
  else
    echo "FAIL  templates/2d (clippy wasm)" >&2
    cat "$rs_clippy_2d_out" >&2
    failed=1
  fi

  if wait $pid_clippy_boardgame; then
    echo "  ok  templates/boardgame (clippy wasm)"
  else
    echo "FAIL  templates/boardgame (clippy wasm)" >&2
    cat "$rs_clippy_boardgame_out" >&2
    failed=1
  fi
fi

# Exit code 2 = blocking hook failure (forces Claude to fix issues before stopping)
if [ "$failed" -ne 0 ]; then
  exit 2
fi
