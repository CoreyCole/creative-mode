---
date: 2026-03-02T15:19:18-08:00
researcher: CoreyCole
git_commit: b8e3b99e88a95431a4fed4a6ab628141de3cb202
branch: feature/agent-swarm
repository: creative-mode
topic: "VM Performance Audit & Fixes"
tags: [infrastructure, performance, tailscale, systemd, ufw, utm, ssh]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VM Performance Audit & Fixes

## Important Context: This is a LOCAL UTM VM, not a remote VPS
The "VPS" is actually a local Ubuntu 24.04 VM running in UTM on the same Mac. It uses UTM Shared Network (NAT) mode, giving it IP `192.168.66.2` on the `enp0s1` interface. Tailscale is also installed for remote access but should NOT be used for local SSH due to DERP relay latency.

## Task(s)

### Completed: Full performance audit of harness server
Deep research across 5 parallel agents covering: goroutine lifecycle, Temporal setup, systemd services, main.go initialization, EventBus/SSE patterns. All findings documented below.

### Completed: Live VM diagnostics
SSHed in and confirmed system is idle (0% CPU, 28 GB of 32 GB free). The "lag" was network (Tailscale DERP relay), not server load.

### Completed: Fix SSH typing lag — direct local SSH
Configured direct SSH bypassing Tailscale. `ssh cm` now works via `192.168.66.2:2222` with sub-millisecond latency.

### Work In Progress: Remaining fixes
Code changes and systemd fixes still needed (see Action Items).

## Critical References
- `harness/main.go` — server initialization, all background goroutines, shutdown sequence
- `harness/internal/world/game_server.go` — game server dev vs prod mode logic
- `scripts/vps-bootstrap.sh` — systemd service definitions, UFW rules, SSH lockdown

## Recent changes (on the VM, not in the repo)
- `~/.ssh/authorized_keys` — added Mac's ed25519 public key for direct SSH auth
- `/etc/ssh/sshd_config.d/*.conf` — added `ListenAddress 192.168.66.2` alongside Tailscale IP
- `/etc/netplan/99-static.yaml` — static IP `192.168.66.2/24` for UTM shared network
- UFW rule added: `allow from 192.168.66.0/24 to any port 2222 proto tcp` (Local VM SSH)
- UFW rule added: `allow 41641/udp` (Tailscale direct connections — didn't fix DERP for local VM due to NAT, but useful for remote)
- `~/.ssh/config` on Mac — added `Host cm` pointing to `192.168.66.2:2222`

## Learnings

### Root Cause of SSH Typing Lag: Tailscale DERP Relay + Local UTM NAT
The VM runs locally in UTM with Shared Network (NAT) mode. Tailscale cannot establish a direct WireGuard tunnel because the VM is behind UTM's NAT — both the Mac and VM Tailscale nodes are on the same physical machine but Tailscale sees them as behind different NATs, forcing traffic through the Seattle DERP relay at 40-250ms per round trip.

**Fix applied**: Bypass Tailscale entirely for local access. Configured sshd to also listen on the local NAT IP (`192.168.66.2:2222`), added the Mac's SSH key to `authorized_keys`, opened UFW for the local subnet, and added `Host cm` to `~/.ssh/config`. Result: `ssh cm` connects in <1ms.

**Tailscale SSH remains available** via `ssh deploy@claude-2.tailcdc985.ts.net` for remote access when away from the Mac (with DERP relay latency as a tradeoff).

### UTM Networking Modes
- **Shared (NAT)**: Current mode. Works everywhere (home, office). VM gets `192.168.66.x`. Tailscale can't do direct connections due to double NAT.
- **Bridged**: Would fix Tailscale direct connections but breaks on office networks with MAC filtering/802.1X.
- Recommendation: Keep Shared mode, use direct SSH locally.

### Template Worlds Hardcoded to Dev Mode
`harness/internal/world/manager.go:581` always calls `ConnectDev` → `game_server.go:106` hardcodes `GameServerModeDev`. On VPS, this means `cargo watch -w shared -w server -x 'run -p server'` runs in a tmux session. It fails silently (cargo watch exits, leaving a dead zsh prompt). No release binary exists at `templates/3d/target/release/server`. The `DEV_MODE` env var only controls auth, not game server mode.

### Process Tree on Fresh Boot (22 min uptime)
```
systemd
├── temporal-dev.service     (163 MB RSS, PID 808)
├── openclaw-gateway.service (465 MB RSS, peaked 2.2 GB, PID 1086)
├── creative-mode.service    (harness 46 MB + air 10 MB)
│   └── tmux: cm-server-5d189ed4-e9d848f0 (dead — just zsh + cat logger)
├── docker.service           (81 MB, NOT NEEDED on VPS)
├── containerd.service       (44 MB, NOT NEEDED on VPS)
├── tailscaled, fail2ban, sshd, nix-daemon
```

### Temporal Service Name Mismatch
The service is named `temporal-dev.service` (not `temporal.service`). The harness's systemd unit does NOT declare `After=temporal-dev.service`, so on slow boots the harness can crash-loop if Temporal isn't ready.

### Goroutine Audit Summary (from research)
- **watchSession goroutines** (`harness/internal/swarmorch/manager.go:486-562`): No `ctx.Done()` in the main loop. If Claude hangs but tmux stays alive, the goroutine runs forever. `DetectStalls` alerts but does NOT kill stalled sessions.
- **Mayor dashboard SSE** (`harness/internal/server/mayor_dashboard.go:83-116`): Missing heartbeat ticker, unlike all other SSE endpoints. Dead connections linger.
- **All other goroutines are properly bounded** with `ctx.Done()`, `defer ticker.Stop()`, or timeouts.

### Swarm tool_use Events Fan Out Wastefully
Every Claude Code tool call publishes a global SSE event (`harness/internal/server/swarm_hooks.go:147`) that fans to ALL open browser tabs. World/lobby pages immediately discard these. CPU waste proportional to `tool_calls × open_tabs`.

## Artifacts
- This handoff document

## Action Items & Next Steps

### 1. ~~Fix SSH typing lag~~ — DONE
Direct SSH configured: `ssh cm` → `192.168.66.2:2222` with <1ms latency.

### 2. Disable Docker on VM
```bash
sudo systemctl disable --now docker docker.socket containerd
```
Saves ~150 MB RAM. Docker is only used for local macOS dev.

### 3. Fix corrupt zsh history
```bash
mv ~/.zsh_history ~/.zsh_history.bak
strings ~/.zsh_history.bak > ~/.zsh_history
```

### 4. Add systemd dependency ordering
Edit `/etc/systemd/system/creative-mode.service` to add:
```ini
After=network.target temporal-dev.service
Requires=temporal-dev.service
```
Then: `sudo systemctl daemon-reload`

### 5. (Code change) Template worlds should detect prod and use prod mode
The fix belongs in `harness/internal/world/manager.go` — `startTemplateDevServers` (line 576) should check an env var (e.g., absence of `DEV_MODE`) and call `Connect` (prod) instead of `ConnectDev`. This requires building the release binary first: `cd templates/3d && cargo build --release -p server`.

### 6. (Code change) Add heartbeat to mayor dashboard SSE
Add a `time.Ticker` heartbeat to `harness/internal/server/mayor_dashboard.go:83-116`, matching the pattern in `events.go:69-70`.

### 7. (Code change) Add `ctx.Done()` to watchSession
Add shutdown context to `harness/internal/swarmorch/manager.go:523-562` main loop so swarm watcher goroutines exit on graceful shutdown.

### 8. Update `scripts/vps-bootstrap.sh` to include local SSH setup
The bootstrap script should be updated to:
- Add UFW rule for local subnet SSH: `ufw allow from 192.168.66.0/24 to any port 2222 proto tcp`
- Add `ListenAddress 192.168.66.2` to sshd config
- Set static IP via netplan
So future re-provisions don't lose local SSH access.

## Other Notes

### VM Access
- **Local (fast)**: `ssh cm` → `192.168.66.2:2222` via direct connection (<1ms)
- **Remote (fallback)**: `ssh deploy@claude-2.tailcdc985.ts.net` via Tailscale DERP (40-250ms)
- Tailscale IP: `100.89.95.41`
- Harness: `http://192.168.66.2:8080` (direct) or via Tailscale Serve HTTPS
- Temporal UI: `http://192.168.66.2:8233`

### Key Service Commands
```bash
sudo systemctl status creative-mode temporal-dev openclaw-gateway
sudo journalctl -u creative-mode -f          # stream harness logs
tmux list-sessions                            # see all tmux sessions
tmux attach -t cm-server-5d189ed4-e9d848f0    # attach to game server session
```

### VM Specs (UTM on macOS)
- 32 GB RAM, 8 vCPUs
- Ubuntu 24.04
- UTM Shared Network (NAT) — `192.168.66.2` on `enp0s1`
- Nix for toolchain (Go, Rust, tmux, temporal-cli, etc.)
- 4 GB swap (unused)
- Systemd services: `creative-mode`, `temporal-dev`, `openclaw-gateway`
- Docker installed but NOT NEEDED — should be disabled
