#!/usr/bin/env bash
# Setup Graphite CLI for stacked PRs.
# Idempotent — safe to re-run.
set -euo pipefail

echo "==> Installing Graphite CLI..."
if command -v gt &>/dev/null; then
    echo "    gt already installed: $(gt --version 2>/dev/null || echo 'unknown')"
else
    npm install -g @withgraphite/graphite-cli
    echo "    installed: $(gt --version 2>/dev/null || echo 'unknown')"
fi

GT_BIN="$(command -v gt)"
echo "    binary: $GT_BIN"

echo "==> Initializing repo (if needed)..."
REPO_DIR="${REPO_DIR:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "$REPO_DIR"

if [ ! -f .graphite_repo_config ]; then
    gt repo init --trunk main 2>/dev/null || echo "    repo init skipped (may need auth first)"
fi

echo "==> Graphite setup complete"
echo "    Run 'gt auth --token <token>' to authenticate with Graphite."
echo "    Run 'gt repo init --trunk main' if repo init was skipped."
