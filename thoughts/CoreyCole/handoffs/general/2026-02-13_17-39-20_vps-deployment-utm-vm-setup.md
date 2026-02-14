---
date: 2026-02-13T17:39:20-08:00
researcher: CoreyCole
git_commit: aa8ffa626e5055487dd0be2529a5b29534df7957
branch: main
repository: creative-mode
topic: "VPS Deployment — UTM VM Setup & Implementation"
tags: [deployment, arm64, utm, qemu, ubuntu, tailscale, docker, vps]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VPS Deployment — UTM VM Setup & Begin Implementation

## Task(s)

**Status: Plan complete, implementation not started**

Resuming from a prior handoff that completed the ARM64 compatibility review and backup strategy for the VPS deployment plan. In this session we updated the plan's implementation order to a VM-first approach: set up the UTM VM before making any code changes, so everything can be tested on the actual deployment target.

**Implementation order (updated in this session):**
1. Phase 1 — Bootstrap script (`vps-bootstrap.sh`) + UTM VM setup
2. Phase 3 — `.env.example`
3. Phase 7 — Verification script (`vps-verify.sh`)
4. Phase 2 — Docker compose updates
5. Phase 5 — Application hardening
6. Phase 4 — Justfile helpers
7. Phase 6 — Reverse proxies (5f + 6a together)

No code has been written yet. The next session should begin with Phase 1: setting up a UTM VM with Ubuntu 24.04 ARM64 and writing the bootstrap script.

## Critical References

1. **The plan document (primary)**: `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md`
2. **Prior review (all issues addressed in plan)**: `thoughts/CoreyCole/reviews/2026-02-13_14-02-41_vps-deployment-automation-v2_review.md`
3. **Previous handoff (ARM64 review)**: `thoughts/CoreyCole/handoffs/general/2026-02-13_17-11-50_arm64-vm-review-and-backup-strategy.md`

## Recent changes

- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md:736-745` — Updated implementation order to VM-first approach (Phase 1 → 3 → 7 → 2 → 5 → 4 → 6)

## Learnings

- **VM-first approach**: The user wants all changes tested on the VM, not on the local macOS host. The VM must be set up before any code changes are implemented.
- **ARM64 compatibility is fully verified** — all Dockerfile layers, Go deps, Rust/WASM toolchain, Tailscale, Docker confirmed working on ARM64 Ubuntu 24.04 (see prior handoff for details).
- **UTM VM specifics**: Network interface will be `enp0s1` (predictable naming), NAT mode works with Tailscale, Docker Engine (not Desktop) runs natively in the Linux VM.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — The complete 7-phase plan (updated implementation order)

## Action Items & Next Steps

1. **Guide the user through UTM VM setup on macOS**:
   - Download UTM from https://mac.getutm.app
   - Create Ubuntu 24.04 ARM64 VM: 16 GB RAM, 4-8 CPU cores, 80 GB disk (qcow2 sparse)
   - Network: NAT (default "Emulated VLAN")
   - Install Ubuntu Server (minimal)
   - The plan's "First-time setup — Local VM (UTM)" section at line 748 has the full workflow

2. **Write `scripts/vps-bootstrap.sh`** (Phase 1) — The plan has detailed specs for all 14 steps (lines 312-408): create deploy user, install Tailscale, install Docker Engine, configure UFW, DOCKER-USER iptables rules (auto-detect interface), Docker daemon config, Fail2Ban, sshd lockdown, systemd unit, SQLite backup cron. Must be idempotent with `--check`/`--dry-run` flag.

3. **Write `harness/.env.example`** (Phase 3) — Template at plan lines 454-471.

4. **Write `scripts/vps-verify.sh`** (Phase 7) — Verification script at plan lines 696-731.

5. **Pre-existing build error**: `listGeneratedAssets` undefined in `imagegen.go:172` — unrelated, investigate separately.

## Other Notes

### UTM VM setup details (from plan)
- macOS host has 64 GB RAM → allocate 16 GB to VM, leaves ~48 GB for macOS
- qcow2 disk is sparse (only uses actual space consumed)
- Create a clean qemu-img snapshot after fresh Ubuntu install, before bootstrap
- Install `brew install qemu borgbackup` on macOS host for snapshot/backup tools
- Time Machine must EXCLUDE the UTM directory to avoid full re-copy on every VM write

### Key codebase locations for implementation
- Server routes: `harness/internal/server/server.go`
- Auth/cookies: `harness/internal/auth/auth.go`
- Port pools: `harness/internal/world/ports.go:8-14`
- Iframe src: `harness/views/world/world.templ:10-38`
- postMessage handler: `harness/static/game-loader.js:5-27`
- Docker compose: `harness/docker-compose.yml`
- Dockerfile: `harness/Dockerfile`
- Game server management: `harness/internal/world/game_server.go`

### Phase 5f + 6a coupling
postMessage origin validation (5f) and trunk reverse proxy (6a) MUST be implemented together. Deploying 5f alone breaks trunk serve postMessage (different ports). The plan notes this at lines 585 and 657-667.
