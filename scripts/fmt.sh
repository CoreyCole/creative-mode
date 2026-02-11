#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
failed=0

# Run formatters in parallel, capture output only on failure.
go_out=$(mktemp)
rs_out=$(mktemp)
trap 'rm -f "$go_out" "$rs_out"' EXIT

(cd "$ROOT/harness" && golangci-lint fmt ./... 2>&1) >"$go_out" 2>&1 &
pid_go=$!

(cd "$ROOT/template" && cargo fmt 2>&1) >"$rs_out" 2>&1 &
pid_rs=$!

# Wait and report.
if wait $pid_go; then
  echo "  ok  harness (go fmt)"
else
  echo "FAIL  harness (go fmt)"
  cat "$go_out"
  failed=1
fi

if wait $pid_rs; then
  echo "  ok  template (cargo fmt)"
else
  echo "FAIL  template (cargo fmt)"
  cat "$rs_out"
  failed=1
fi

exit $failed
