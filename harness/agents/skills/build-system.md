---
name: build-system
description: Nix deps, just commands, WASM 5GB constraint, deployment topology, VPS vs macOS
tags: [nix, just, build, wasm, deploy, systemd, air]
last_verified: 2026-03-08
---

# Build System

## VPS (Production) — Nix + systemd

Harness runs via `air` (hot-reload) under systemd. Nix provides build/runtime deps.

| Command | Purpose |
|---------|---------|
| `just vps-build` | Build harness binary (templ + tailwind + go build) |
| `just vps-deploy` | Pull + build + restart systemd service |
| `just vps-logs` | Stream service logs (journalctl) |
| `just vps-status` | Check service status |

## macOS (Local Dev) — Docker

**Never run `cargo build/clippy/check`, `go build`, `templ generate` directly on macOS host.** Docker bind-mounts corrupt incremental builds.

| Command | Purpose |
|---------|---------|
| `just live` | Docker + file watcher + Tailwind |
| `just up` | Docker container only |
| `just down` | Stop container |

## Harness Commands

```bash
cd harness
just generate    # sqlc generate + templ generate + tailwind build
just build       # go build -o harness .
just dev         # go run .
just lint        # golangci-lint run ./...
```

## Hot Reload

`air` watches Go/templ files and auto-rebuilds. Runs under systemd on VPS via `scripts/harness-run.sh`.

## Project-Root Commands

```bash
just check    # verify Go + Rust + WASM all compile
just fmt      # format all code
```

## WASM Build Constraint

Each `wasm-bindgen` invocation uses ~5 GB RAM. VPS has 10 GB — only one template build at a time. Two simultaneous builds will OOM.

## Deployment Topology

| Server | Runs | Access |
|--------|------|--------|
| EC2 (Ubuntu) | Marketing site (`site/`) | Public: `creative-mode.ai` → Caddy:443 → :3000 |
| VPS (Nix) | Harness (`harness/`) | Internal: Tailscale `100.x.x.x:8080` |

Connected via Tailscale. Site → harness webhooks use `CM_HOOK_SECRET`.
