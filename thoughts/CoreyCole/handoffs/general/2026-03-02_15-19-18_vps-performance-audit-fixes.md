---
date: 2026-03-02T15:19:18-08:00
researcher: CoreyCole
git_commit: b8e3b99e88a95431a4fed4a6ab628141de3cb202
branch: feature/agent-swarm
repository: creative-mode
topic: "VPS Performance Audit & Fixes"
tags: [infrastructure, performance, tailscale, systemd, ufw, vps]
status: complete
last_updated: 2026-03-02
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VPS Performance Audit & Fixes

## Task(s)

### Completed: Full performance audit of VPS harness server
Deep research across 5 parallel agents covering: goroutine lifecycle, Temporal setup, systemd services, main.go initialization, EventBus/SSE patterns. All findings documented below.

### Completed: Live VPS diagnostics
SSHed into `deploy@claude-2.tailcdc985.ts.net` and confirmed current state. System is idle (0% CPU, 28 GB of 32 GB free). The "lag" is network, not server load.

### Work In Progress: Fix issues identified
The following issues need to be fixed on the VPS directly.

## Critical References
- `harness/main.go` — server initialization, all background goroutines, shutdown sequence
- `harness/internal/world/game_server.go` — game server dev vs prod mode logic
- `scripts/vps-bootstrap.sh` — systemd service definitions, UFW rules, SSH lockdown

## Recent changes
No code changes were made — this was a diagnostic/research session.

## Learnings

### Root Cause of SSH Typing Lag: Tailscale DERP Relay
The SSH typing lag is caused by Tailscale routing traffic through the Seattle DERP relay instead of a direct WireGuard tunnel:
```
pong from claude-2 (100.89.95.41) via DERP(sea) in 250ms
```
**250ms round-trip per keystroke.** The VPS itself is completely idle. The fix is to open UDP port 41641 for Tailscale direct connections. The VPS bootstrap script (`scripts/vps-bootstrap.sh:255-269`) configures UFW to deny all incoming except on the `tailscale0` interface, but Tailscale needs UDP 41641 open on the **physical interface** to establish direct WireGuard tunnels. Without it, all traffic bounces through the DERP relay.

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

### 1. Fix Tailscale Direct Connection (UFW port)
**This is the #1 priority — fixes the SSH typing lag.**

On the VPS, open UDP port 41641 for Tailscale WireGuard direct connections:
```bash
sudo ufw allow 41641/udp comment "Tailscale direct connections"
sudo ufw reload
```

Then verify from the Mac:
```bash
tailscale ping claude-2.tailcdc985.ts.net
# Should show "via [direct IP]" instead of "via DERP(sea)"
# Latency should drop from ~250ms to ~10-30ms
```

The bootstrap script at `scripts/vps-bootstrap.sh:255-269` should also be updated to include this rule so future bootstraps don't regress:
```bash
# After line 267 (ufw allow in on tailscale0)
ufw allow 41641/udp comment "Tailscale direct connections"
```

### 2. Disable Docker on VPS
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

### 5. (Code change, not VPS fix) Template worlds should detect VPS and use prod mode
The fix belongs in `harness/internal/world/manager.go` — `startTemplateDevServers` (line 576) should check an env var (e.g., `VPS_MODE=true` or absence of `DEV_MODE`) and call `Connect` (prod) instead of `ConnectDev`. This requires building the release binary first: `cd templates/3d && cargo build --release -p server`.

### 6. (Code change) Add heartbeat to mayor dashboard SSE
Add a `time.Ticker` heartbeat to `harness/internal/server/mayor_dashboard.go:83-116`, matching the pattern in `events.go:69-70`.

### 7. (Code change) Add `ctx.Done()` to watchSession
Add shutdown context to `harness/internal/swarmorch/manager.go:523-562` main loop so swarm watcher goroutines exit on graceful shutdown.

## Other Notes

### VPS Access
- SSH: `ssh deploy@claude-2.tailcdc985.ts.net`
- Tailscale IP: `100.89.95.41`
- Harness: `http://localhost:8080` (or via Tailscale Serve HTTPS)
- Temporal UI: `http://localhost:8233`

### Key Service Commands
```bash
sudo systemctl status creative-mode temporal-dev openclaw-gateway
sudo journalctl -u creative-mode -f          # stream harness logs
tmux list-sessions                            # see all tmux sessions
tmux attach -t cm-server-5d189ed4-e9d848f0    # attach to game server session
```

### VPS Specs
- 32 GB RAM, 8 vCPUs (from `top` output)
- Ubuntu 24.04
- Nix for toolchain (Go, Rust, tmux, temporal-cli, etc.)
- 4 GB swap (unused)
