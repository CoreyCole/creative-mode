#!/bin/bash

# Ensure Rust + Claude CLI are on PATH for tmux child sessions.
export PATH="/usr/local/cargo/bin:/root/.local/bin:$PATH"

# Pre-start tmux server so game/Claude sessions can be created.
tmux start-server

echo "=== Creative Mode Harness — Dev Container ==="
echo ""
echo "  Browse:  http://localhost:8080"
echo "  Air watches .go and .templ files for hot-reload"
echo ""

# Air handles: initial build → run → watch → rebuild → restart
exec air
