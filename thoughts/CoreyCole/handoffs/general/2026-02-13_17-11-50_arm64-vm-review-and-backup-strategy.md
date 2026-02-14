---
date: 2026-02-13T17:11:50-08:00
researcher: CoreyCole
git_commit: b1b213c49880a817a9f7cd508f65afb7abf9592a
branch: main
repository: creative-mode
topic: "VPS Deployment Plan — ARM64 VM Compatibility Review & Backup Strategy"
tags: [deployment, arm64, utm, qemu, backup, borgbackup, qcow2, tailscale, docker]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: ARM64 VM Compatibility Review & Backup Strategy for VPS Deployment Plan

## Task(s)

**Status: Complete (review phase) — Implementation not yet started**

Performed a thorough ARM64 Ubuntu 24.04 VM compatibility review of the VPS deployment automation plan, and added a comprehensive VM backup & migration strategy. This was the second review cycle — the first review (v2) identified critical issues (postMessage origin, hardcoded eth0, body limits) which were already addressed. This review focused specifically on ARM64/VM concerns and backup strategies.

The plan has 7 phases. **No code has been implemented yet.** The plan document is now fully reviewed and ready for implementation, starting with Phase 3 → 5 → 2 → 4 → 6 → 1 → 7 (local-testable changes first, VM/VPS-specific last).

## Critical References

1. **The plan document (primary, updated in this session)**: `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md`
2. **The prior review**: `thoughts/CoreyCole/reviews/2026-02-13_14-02-41_vps-deployment-automation-v2_review.md`
3. **Previous handoff (created the review task)**: `thoughts/CoreyCole/handoffs/general/2026-02-13_15-23-08_vps-deployment-plan-arm-vm-review.md`

## Recent changes

No code changes — all work was plan document editing:
- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — Added two major new sections and updated multiple existing sections (see Artifacts)

## Learnings

### ARM64 compatibility — all clear
- Every Dockerfile layer verified: `golang:1.24-bookworm` has native ARM64 images, Rust toolchain auto-detects `aarch64-unknown-linux-gnu`, all `cargo install` tools compile from source on ARM64, Claude Code CLI has prebuilt `linux-arm64` binary
- The only CGO dependency is `mattn/go-sqlite3` — well-tested on ARM64
- WASM cross-compilation is architecture-independent — identical output from ARM64 or x86_64 host
- Bevy WASM builds peak at 4-8 GB RAM on ARM64, same as x86_64

### UTM/QEMU networking validated
- Default network interface in UTM ARM64 Ubuntu: **`enp0s1`** (predictable naming: `en` ethernet + `p0` PCI bus 0 + `s1` slot 1)
- Tailscale works correctly inside QEMU NAT — direct WireGuard connections expected (QEMU user-mode NAT is a standard cone NAT)
- Tailscale SSH and Tailscale Serve both operate inside `tailscaled` userspace daemon, independent of VM network stack
- Docker Engine (not Desktop) runs natively on Linux kernel in VM — no nested virtualization needed
- QEMU adds ~29 microseconds of virtio latency; Tailscale adds ~1-3ms for direct connections

### VM backup strategy
- UTM has **no built-in snapshot UI** (feature request #5484)
- `qemu-img snapshot -c` creates internal snapshots inside qcow2 — instant create/restore, VM must be stopped
- borgbackup with content-defined chunking is ideal for incremental qcow2 backups (60-80% dedup savings)
- Time Machine must EXCLUDE UTM directory — any VM write causes full re-copy of entire qcow2 file
- qcow2 files grow monotonically; monthly `fstrim -av` + `qemu-img convert` reclaims space

### Cloud migration paths
- Oracle Cloud: free ARM64 tier (4 OCPU / 24 GB RAM), accepts qcow2 natively, requires `cloud-init` + UEFI boot (UTM already uses UEFI)
- Hetzner CAX: needs `qemu-img convert -O raw` + `hcloud-upload-image` tool

### Claude Code CLI on ARM64
- Prebuilt `linux-arm64` binary (~220 MB) in official manifest
- Install script auto-detects `aarch64` via `uname -m`
- Known crash issue (#12160 "double free") only affects non-standard envs (Android/Termux proot) — standard Ubuntu 24.04 is fine

## Artifacts

All artifacts are the updated plan document — no code changes:

- **Plan document (updated)**: `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md`
  - **New section**: "ARM64 Compatibility Notes" — Dockerfile layers table, Go deps table, Bevy WASM notes, Tailscale+UTM networking, Docker in VM, Claude Code CLI
  - **New section**: "VM Backup & Migration" — 5-layer backup strategy (SQLite cron, qemu-img snapshots, borgbackup, Time Machine exclusion, qcow2 maintenance) + Oracle Cloud and Hetzner migration paths + backup frequency table
  - **Updated**: Option A (Local VM) — ARM64 compatibility summary, `enp0s1` interface, Docker Engine clarification
  - **Updated**: Option B (Cloud VPS) — Oracle Cloud free tier + Hetzner CAX providers, migration cross-reference
  - **Updated**: Architecture section — interface names in DOCKER-USER explanation
  - **Updated**: Phase 1 steps 5 and 7 — Docker Engine specifics, interface name documentation
  - **Updated**: Phase 2 — Dockerfile EXPOSE fix note
  - **Updated**: Deployment Workflow — steps 0, 2, 11, 12 for snapshot/backup setup
  - **Updated**: Comparison table — Backups and Network interface rows
  - **Updated**: Resolved Questions — added #10 (ARM64) and #11 (VM backups)
  - **Updated**: Future Work — cloud-init pre-installation, automated VM backup script

## Action Items & Next Steps

1. **Begin implementation** per the plan's implementation order:
   - **Phase 3** (`.env.example`) — trivial file creation
   - **Phase 5** (application hardening) — security headers, body limits, server timeouts, cookie fixes, chat/prompt length limits, postMessage origin validation, rate limiting
   - **Phase 2** (docker-compose updates) — `restart: on-failure`, healthcheck, Dockerfile EXPOSE fix
   - **Phase 4** (justfile helpers) — `redeploy` and `status` targets
   - **Phase 6** (reverse proxies) — trunk serve HTTP+WS proxy, game server WS proxy, postMessage simplification. **Note**: Phase 5f (postMessage) and Phase 6a (trunk proxy) should be implemented together to avoid breakage
   - **Phase 1** (bootstrap script) — full `vps-bootstrap.sh` with idempotency
   - **Phase 7** (verification script) — `vps-verify.sh`

2. **Pre-existing build error**: `listGeneratedAssets` undefined in `imagegen.go:172` — unrelated to this work, should be investigated separately

3. **Dockerfile `mold` addition**: The plan recommends adding `mold` linker to the Dockerfile for faster Bevy builds — implement during Phase 2 alongside other Dockerfile changes

## Other Notes

### Plan structure (7 phases)
1. Phase 1: `scripts/vps-bootstrap.sh` — Ubuntu hardening (UFW, DOCKER-USER, Tailscale, sshd, systemd, SQLite backup cron)
2. Phase 2: Docker Compose updates (`restart: on-failure`, healthcheck, Dockerfile EXPOSE fix)
3. Phase 3: `.env.example` template
4. Phase 4: Justfile helpers (`redeploy`, `status`)
5. Phase 5: Application hardening (security headers, body limits, timeouts, cookie fixes, postMessage, rate limiting)
6. Phase 6: Reverse proxies — 6a: trunk HTTP+WS, 6b: game server WS, 6c: postMessage strict origin, 6d: CLAUDE.md updates
7. Phase 7: `scripts/vps-verify.sh` — post-bootstrap verification

### Key codebase locations
- Iframe src logic: `harness/views/world/world.templ:10-38`
- Port pools: `harness/internal/world/ports.go:8-14`
- Game server management: `harness/internal/world/game_server.go`
- Build pipeline: `harness/internal/build/builder.go`
- Auth/cookies: `harness/internal/auth/auth.go`
- postMessage handler: `harness/static/game-loader.js:5-27`
- Docker compose: `harness/docker-compose.yml`
- Dockerfile: `harness/Dockerfile`
- Server routes: `harness/internal/server/server.go`

### Review history
- v1 review → identified critical issues (Compose ports merge, missing WS proxy, dev/prod contradiction)
- v2 review → `thoughts/CoreyCole/reviews/2026-02-13_14-02-41_vps-deployment-automation-v2_review.md` — all issues addressed in plan
- ARM64/VM review (this session) → all clear, plan updated with compatibility notes and backup strategy
