---
date: 2026-02-15T20:35:20+00:00
researcher: CoreyCole
git_commit: fe78b9ad7ca342210903f79ecc50a47db8e7b63a
branch: main
repository: creative-mode
topic: "WASM Build Memory Optimization - System Design for Live Reload on Constrained VPS"
tags: [wasm, trunk, wasm-bindgen, memory, vps, live-reload, system-design]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: WASM Build Memory Optimization on VPS

## Task(s)

**Status: Research / Planning phase — no code changes made yet.**

The VPS (10 GB RAM, 8 cores) is being crushed by `wasm-bindgen` processes spawned by `trunk serve`. We need to redesign how WASM live-reload works so it doesn't OOM the machine.

### Problem Summary

- Each world gets its own `trunk serve` instance in a tmux session (managed by the harness)
- `trunk serve` watches for file changes and rebuilds WASM via `wasm-bindgen`
- **Each `wasm-bindgen` invocation consumes ~5 GB RAM** for the 3D template and ~4 GB for the 2D template
- With 2+ worlds running, two simultaneous rebuilds eat ~9 GB — exhausting all RAM + swap
- `kswapd0` (kernel swap daemon) pegs a CPU core at 83% from thrashing
- The processes complete and die, but trunk keeps respawning them on file changes, creating a recurring OOM cycle
- **19 game server processes** were also running at time of investigation

### Key Observations from System Investigation

1. **Memory at time of discovery**: 9.7 GB total, only 84 MB free, 2.6 GB swap used
2. **Load average**: 23.6 on 8 cores (3x overloaded)
3. **Two trunk serve instances**: ports 8081 and 8082, spawned in tmux sessions `cm-trunk-*`
4. **wasm-bindgen commands observed**:
   - `/usr/local/cargo/bin/wasm-bindgen --target=web --out-dir=.../templates/3d/target/wasm-bindgen/debug --out-name=client .../templates/3d/target/wasm32-unknown-unknown/debug/client.wasm --no-typescript`
   - `/usr/local/cargo/bin/wasm-bindgen --target=web --out-dir=.../templates/2d/target/wasm-bindgen/debug --out-name=room-world .../templates/2d/target/wasm32-unknown-unknown/debug/room-world.wasm --no-typescript`

## Critical References

- `harness/internal/build/builder.go` — where trunk serve / game server tmux sessions are spawned
- `templates/3d/Trunk.toml` and `templates/2d/Trunk.toml` — trunk configuration

## Recent changes

No code changes were made. This was purely investigation/diagnosis.

## Learnings

- **`wasm-bindgen` is the memory hog, not cargo/rustc.** The WASM binary is already compiled when wasm-bindgen runs; it's the bindgen post-processing step that eats 4-5 GB per template.
- **Trunk serve spawns per-world via tmux.** The harness creates `cm-trunk-{worldID}` tmux sessions. Each world of the same template type gets its own independent trunk serve + rebuild cycle.
- **Killing trunk is pointless** — the harness will respawn it, and trunk itself will just re-trigger wasm-bindgen on any file change.
- **The fundamental design issue**: multiple worlds using the same template each run independent trunk builds of identical source code. There's no sharing of build artifacts across worlds of the same template type.

## Artifacts

None produced — this was investigation only.

## Action Items & Next Steps

### Research Needed (do this on a non-OOM machine)

1. **Explore the build/server spawning code** in detail:
   - `harness/internal/build/builder.go` — how trunk serve tmux sessions are created
   - How ports are assigned per world
   - How the harness proxies/routes to trunk-served WASM
   - Whether worlds of the same template type could share a single trunk instance

2. **Research `wasm-bindgen` memory usage**:
   - Why does it consume 5 GB? Is this a known issue?
   - Are there CLI flags or configurations to reduce memory?
   - Can `wasm-opt` settings help?

3. **Research alternative architectures**:
   - **Shared trunk instance**: One `trunk serve` per template type (not per world), with all worlds of that type pointing to the same build output
   - **Build once, serve statically**: `trunk build --watch` for rebuilding + a lightweight static server (nginx/caddy) for serving, decoupling build from serve
   - **Pre-built WASM**: Build WASM once at deploy time, skip live-reload in production, only live-reload in dev
   - **Separate build machine**: Offload wasm-bindgen to a machine with more RAM, rsync artifacts back
   - **`cargo-leptos` or other tools**: Alternative WASM build pipelines that may be more memory-efficient

4. **Design a solution** that preserves fast iteration (live reload) while not OOMing a 10 GB VPS with multiple worlds running.

## Other Notes

- The VPS is running: harness (Go), multiple game servers (Rust debug binaries), trunk serve instances, tailscaled, 3 claude sessions, and nvim instances
- Tmux sessions observed: `cm-trunk-06296adf-*`, `cm-trunk-5d189ed4-*`, `cm-server-5d189ed4-*`
- Rust is installed system-wide via rustup at `/usr/local/cargo/bin/`
- The harness runs as a systemd service in production; game servers and trunk are managed via tmux
- On macOS (local dev), everything runs in Docker — this problem is VPS-specific since Docker would have its own memory limits
