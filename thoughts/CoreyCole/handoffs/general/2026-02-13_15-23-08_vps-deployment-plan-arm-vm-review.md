---
date: 2026-02-13T15:23:08-08:00
researcher: CoreyCole
git_commit: 1c35fda02f1d3571bcd40e4e023b559d3beca475
branch: main
repository: creative-mode
topic: "VPS Deployment Automation — ARM VM Compatibility Review"
tags: [deployment, tailscale, docker, vm, arm64, ubuntu, plan-review]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Review VPS Deployment Plan for ARM Ubuntu 24.04 VM Compatibility

## Task(s)

**Status: Ready for review**

We have a comprehensive VPS deployment automation plan that has gone through one full review cycle and multiple rounds of refinement. The plan now targets a **local Ubuntu 24.04 ARM64 VM** (via UTM/QEMU on macOS) as the primary hosting option, with cloud VPS as a secondary option. The plan needs a focused review to ensure all components work correctly on ARM64 Ubuntu in a VM context.

Key areas needing ARM64/VM validation:
- Docker ARM64 image builds (Rust toolchain, trunk, cargo tools, Go)
- Bevy/WASM cross-compilation from ARM64 host (WASM target is architecture-independent, but build tools must run on ARM64)
- QEMU/UTM networking with Tailscale (NAT + tailscale0 interface coexistence)
- DOCKER-USER iptables rules with QEMU's virtual NIC naming
- `gorilla/websocket` and `httputil.ReverseProxy` — no architecture concerns, but verify Go dependencies compile on ARM64
- systemd unit for auto-start after VM reboot

## Critical References

1. **The plan document**: `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — This is the primary artifact. ~550 lines, 7 phases.
2. **The review that prompted all refinements**: `thoughts/CoreyCole/reviews/2026-02-13_14-02-41_vps-deployment-automation-v2_review.md` — All critical issues and concerns from this review have been addressed in the plan.
3. **Harness CLAUDE.md**: `harness/CLAUDE.md` — Documents the cross-origin iframe architecture that Phase 6 will change to same-origin.

## Recent changes

No code changes were made — all work was plan/document editing:
- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — Extensively updated (see Artifacts)

## Learnings

### Cross-origin iframe discovery (important for Phase 6)
- Trunk serve runs on ports 8081-8180, creating cross-origin iframes from harness on port 8080
- On a remote VM/VPS, `http://localhost:{trunkPort}` in the iframe src resolves to the user's local machine, NOT the server — trunk serve iframes are completely unreachable, not just cross-origin
- Solution: Phase 6a adds a trunk serve reverse proxy (`/world/{worldID}/trunk/*` → `localhost:{trunkPort}`), making everything same-origin
- This was a gap the original plan review didn't catch — it only identified the WebSocket proxy need for game servers
- Trunk serve also uses WebSocket for hot-reload, so the trunk proxy needs WS upgrade handling too

### Resource analysis
- Bevy 3D template has 741 crate dependencies, peaks at 4-8 GB RAM during release builds
- No global build concurrency limit — multiple users can trigger simultaneous builds
- `target/` cache divergence is the main disk growth factor (hardlinks via `cp -al` diverge with each build)
- 16 GB RAM, 80 GB disk recommended for single developer

### Key decisions made during this session
- **Always proxy trunk serve** (not VPS-only): one code path, everything same-origin everywhere
- **`restart: on-failure`** instead of `unless-stopped`: avoids macOS Docker Desktop auto-start annoyance, VPS uses systemd for reboot persistence
- **Route-specific body limits**: 1M global, 10M for `/api/assets/upload`
- **Auto-detect network interface** via `ip route show default` instead of hardcoding `eth0`
- **Local VM via UTM** as primary hosting option (64 GB host RAM, allocate 16 GB to VM)

## Artifacts

All artifacts are documents (no code changes):

- **Plan document (primary)**: `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md`
  - Added: Hosting Options section (VM vs VPS), Server Sizing section, expanded Phase 6 (trunk + WS proxies), Phase 7 (verification script), systemd unit, idempotency patterns, Docker healthcheck, port range documentation, resolved questions 4-9
- **Review document (reference)**: `thoughts/CoreyCole/reviews/2026-02-13_14-02-41_vps-deployment-automation-v2_review.md`

## Action Items & Next Steps

1. **Review the plan for ARM64 Ubuntu 24.04 VM compatibility** — This is the primary ask. Read the full plan at `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` and validate that all 7 phases work on ARM64. Key concerns:
   - Does the Dockerfile build correctly on ARM64? (Rust ARM64 toolchain + wasm32 target, trunk, cargo-watch)
   - Any Go dependencies with native code that might not have ARM64 builds?
   - QEMU virtual NIC names — will `ip route show default` correctly detect the interface?
   - UTM/QEMU + Tailscale networking — any known issues with NAT mode?

2. **After review approval, begin implementation** starting with Phase 3 (`.env.example`), then Phase 5 (application hardening), per the implementation order in the plan.

3. **Note**: There is a pre-existing build error (`listGeneratedAssets` undefined in `imagegen.go:172`) that is unrelated to this work. It should be investigated separately but does not block plan review.

## Other Notes

### Plan structure (7 phases)
1. **Phase 1**: `scripts/vps-bootstrap.sh` — Ubuntu hardening (UFW, DOCKER-USER, Tailscale, sshd, systemd unit, SQLite backup cron)
2. **Phase 2**: Docker Compose updates (`restart: on-failure`, healthcheck)
3. **Phase 3**: `.env.example` template
4. **Phase 4**: Justfile helpers (`redeploy`, `status`)
5. **Phase 5**: Application hardening (security headers, body limits, server timeouts, cookie fixes, postMessage origin check, rate limiting, chat/prompt length limits)
6. **Phase 6**: Reverse proxies — 6a: trunk serve HTTP+WS proxy, 6b: game server WS proxy, 6c: postMessage strict origin, 6d: CLAUDE.md updates
7. **Phase 7**: `scripts/vps-verify.sh` — post-bootstrap security verification

### Implementation order (not phase order)
Phase 3 → 5 → 2 → 4 → 6 → 1 → 7 (local-testable changes first, VM/VPS-specific last)

### Relevant codebase locations
- Iframe src logic: `harness/views/world/world.templ:10-38`
- Port pools: `harness/internal/world/ports.go:8-14`
- Game server management: `harness/internal/world/game_server.go`
- Build pipeline: `harness/internal/build/builder.go`
- Auth/cookies: `harness/internal/auth/auth.go`
- postMessage handler: `harness/static/game-loader.js:5-27`
- Docker compose: `harness/docker-compose.yml`
- Dockerfile: `harness/Dockerfile`
