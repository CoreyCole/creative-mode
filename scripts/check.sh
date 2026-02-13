#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
failed=0

# Phase 1: Format (modifies files, must run first).
"$ROOT/scripts/fmt.sh"

# Phase 2: Lint in parallel (each linter includes compilation).
go_out=$(mktemp)
rs_clippy_server_out=$(mktemp)
rs_clippy_client_out=$(mktemp)
rs_clippy_2d_out=$(mktemp)
trap 'rm -f "$go_out" "$rs_clippy_server_out" "$rs_clippy_client_out" "$rs_clippy_2d_out"' EXIT

(cd "$ROOT/harness" && golangci-lint run ./... 2>&1) >"$go_out" 2>&1 &
pid_go=$!

(cd "$ROOT/templates/3d" && cargo clippy -p server -p shared -- -D warnings 2>&1) >"$rs_clippy_server_out" 2>&1 &
pid_clippy_server=$!

(cd "$ROOT/templates/3d" && cargo clippy -p client --target wasm32-unknown-unknown -- -D warnings 2>&1) >"$rs_clippy_client_out" 2>&1 &
pid_clippy_client=$!

(cd "$ROOT/templates/2d" && cargo clippy --target wasm32-unknown-unknown -- -D warnings 2>&1) >"$rs_clippy_2d_out" 2>&1 &
pid_clippy_2d=$!

# Wait and report.
if wait $pid_go; then
  echo "  ok  harness (golangci-lint)"
else
  echo "FAIL  harness (golangci-lint)" >&2
  cat "$go_out" >&2
  failed=1
fi

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

# Exit code 2 = blocking hook failure (forces Claude to fix issues before stopping)
if [ "$failed" -ne 0 ]; then
  exit 2
fi
