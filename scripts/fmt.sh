#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
failed=0

# Run formatters in parallel, capture output only on failure.
go_out=$(mktemp)
rs_3d_out=$(mktemp)
rs_2d_out=$(mktemp)
trap 'rm -f "$go_out" "$rs_3d_out" "$rs_2d_out"' EXIT

(cd "$ROOT/harness" && golangci-lint fmt ./... 2>&1) >"$go_out" 2>&1 &
pid_go=$!

(cd "$ROOT/templates/3d" && cargo fmt 2>&1) >"$rs_3d_out" 2>&1 &
pid_rs_3d=$!

(cd "$ROOT/templates/2d" && cargo fmt 2>&1) >"$rs_2d_out" 2>&1 &
pid_rs_2d=$!

# Wait and report.
if wait $pid_go; then
  echo "  ok  harness (go fmt)"
else
  echo "FAIL  harness (go fmt)"
  cat "$go_out"
  failed=1
fi

if wait $pid_rs_3d; then
  echo "  ok  templates/3d (cargo fmt)"
else
  echo "FAIL  templates/3d (cargo fmt)"
  cat "$rs_3d_out"
  failed=1
fi

if wait $pid_rs_2d; then
  echo "  ok  templates/2d (cargo fmt)"
else
  echo "FAIL  templates/2d (cargo fmt)"
  cat "$rs_2d_out"
  failed=1
fi

exit $failed
