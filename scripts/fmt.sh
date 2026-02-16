#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
failed=0

# Run formatters in parallel, capture output only on failure.
go_out=$(mktemp)
templ_out=$(mktemp)
site_go_out=$(mktemp)
site_templ_out=$(mktemp)
rs_3d_out=$(mktemp)
rs_2d_out=$(mktemp)
rs_boardgame_out=$(mktemp)
trap 'rm -f "$go_out" "$templ_out" "$site_go_out" "$site_templ_out" "$rs_3d_out" "$rs_2d_out" "$rs_boardgame_out"' EXIT

(cd "$ROOT/harness" && golangci-lint fmt ./... 2>&1) >"$go_out" 2>&1 &
pid_go=$!

(cd "$ROOT/harness" && templ fmt . 2>&1) >"$templ_out" 2>&1 &
pid_templ=$!

(cd "$ROOT/site" && golangci-lint fmt ./... 2>&1) >"$site_go_out" 2>&1 &
pid_site_go=$!

(cd "$ROOT/site" && templ fmt . 2>&1) >"$site_templ_out" 2>&1 &
pid_site_templ=$!

(cd "$ROOT/templates/3d" && cargo fmt 2>&1) >"$rs_3d_out" 2>&1 &
pid_rs_3d=$!

(cd "$ROOT/templates/2d" && cargo fmt 2>&1) >"$rs_2d_out" 2>&1 &
pid_rs_2d=$!

(cd "$ROOT/templates/boardgame" && cargo fmt 2>&1) >"$rs_boardgame_out" 2>&1 &
pid_rs_boardgame=$!

# Wait and report.
if wait $pid_go; then
  echo "  ok  harness (go fmt)"
else
  echo "FAIL  harness (go fmt)"
  cat "$go_out"
  failed=1
fi

if wait $pid_templ; then
  echo "  ok  harness (templ fmt)"
else
  echo "FAIL  harness (templ fmt)"
  cat "$templ_out"
  failed=1
fi

if wait $pid_site_go; then
  echo "  ok  site (go fmt)"
else
  echo "FAIL  site (go fmt)"
  cat "$site_go_out"
  failed=1
fi

if wait $pid_site_templ; then
  echo "  ok  site (templ fmt)"
else
  echo "FAIL  site (templ fmt)"
  cat "$site_templ_out"
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

if wait $pid_rs_boardgame; then
  echo "  ok  templates/boardgame (cargo fmt)"
else
  echo "FAIL  templates/boardgame (cargo fmt)"
  cat "$rs_boardgame_out"
  failed=1
fi

exit $failed
