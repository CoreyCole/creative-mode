#!/bin/bash

# Ensure Rust + Claude CLI are on PATH for tmux child sessions.
export PATH="/usr/local/cargo/bin:/root/.local/bin:$PATH"

# Clean stale Cargo incremental build artifacts that can break WASM builds.
# macOS ↔ Docker bind mounts can lose sync on container restart, leaving
# .rcgu.o references in metadata that point to deleted files.
find /app/templates -path '*/target/wasm32-unknown-unknown/debug/incremental' -type d -exec rm -rf {} + 2>/dev/null || true

# Pre-start tmux server so game/Claude sessions can be created.
tmux start-server

echo "=== Creative Mode Harness — Dev Container ==="
echo ""
echo "  Browse:  http://localhost:8080"
echo "  Air watches .go and .templ files for hot-reload"
echo ""

# Air handles: initial build → run → watch → rebuild → restart
exec air
