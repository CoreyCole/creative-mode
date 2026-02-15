#!/bin/bash
set -euo pipefail

# =============================================================================
# harness-run.sh — systemd wrapper for the Creative Mode harness
# =============================================================================
# Sets up PATH so the harness (and tmux sessions it spawns for game servers
# and Claude Code) can find all required tools, then execs the harness binary.
#
# Called by: /etc/systemd/system/creative-mode.service
# =============================================================================

# Nix packages (go, tmux, jq, sqlite, gcc, pkg-config, etc.)
source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh

# Rust/Cargo (trunk, cargo-watch, wasm-bindgen-cli)
export RUSTUP_HOME=/usr/local/rustup
export CARGO_HOME=/usr/local/cargo
export PATH="/usr/local/cargo/bin:$PATH"

# Claude Code CLI
export PATH="/home/deploy/.local/bin:$PATH"

# Go tools (templ)
export PATH="/home/deploy/go/bin:$PATH"

# OpenClaw (agent framework for world mayors)
export OPENCLAW_HOME=/home/deploy/creative-mode/data/openclaw

# CGO for SQLite
export CGO_ENABLED=1

# Start tmux server so game/Claude sessions can be created
tmux start-server

cd /home/deploy/creative-mode/harness
exec air
