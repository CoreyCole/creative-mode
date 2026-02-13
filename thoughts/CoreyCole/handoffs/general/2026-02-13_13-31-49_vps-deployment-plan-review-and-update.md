---
date: 2026-02-13T13:31:49-08:00
researcher: CoreyCole
git_commit: 6b2b699abf66bb093010833bbe8046c44fd6a610
branch: main
repository: creative-mode
topic: "VPS Deployment Plan — Review and Update"
tags: [implementation, strategy, security, deployment, tailscale, docker, vps, review]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VPS Deployment Plan — Critical Review and Update

## Task(s)

1. **Critical review of VPS deployment plan** — **Completed.** Staff-eng-level review of the original deployment automation plan. Found 3 critical issues, 8 concerns, and 5 open questions. Review saved to `thoughts/CoreyCole/reviews/2026-02-13_12-26-04_vps-deployment-automation_review.md`.

2. **Update plan based on review findings + user direction** — **Completed.** The user clarified that the VPS is a shared development server (not production). Local dev and VPS should be identical — the only differences are `.env` contents and OS-level network hardening. A separate "prod" release server will come later.

The plan was substantially rewritten to reflect this. All critical issues from the review have been addressed.

## Critical References

- **Updated plan**: `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — The canonical plan, fully rewritten. **Read this first.**
- **Review document**: `thoughts/CoreyCole/reviews/2026-02-13_12-26-04_vps-deployment-automation_review.md` — Original review findings. Many items were addressed in the plan update; some were moved to Future Work.
- **Security research**: `thoughts/CoreyCole/research/2026-02-13_10-31-25_vps-deployment-security-hardening.md` — Full 16-finding security audit of the codebase + VPS hardening architecture. The plan is based on this.

## Recent changes

No code changes were made. Only thought documents were created/updated:
- `thoughts/CoreyCole/reviews/2026-02-13_12-26-04_vps-deployment-automation_review.md` — New: critical review
- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — Rewritten: updated plan

## Learnings

### Docker Compose ports merge behavior
The original plan assumed Docker Compose overrides **replace** the `ports` list. This is wrong — Compose **appends** lists (including `ports`). A compose override with only `127.0.0.1:8080:8080` would result in BOTH the original ports AND the new one, causing a port conflict. The fix was to remove the compose override entirely and rely on OS-level DOCKER-USER iptables rules for network security.

### Key architectural decision: dev ≡ VPS
The user clarified that:
- The VPS is a **shared dev server**, not production
- `DEV_MODE=true` everywhere — same Docker image, same `just up` command
- Network security comes from Tailscale + UFW + DOCKER-USER iptables, not Docker config
- A separate "prod" server with `DEV_MODE=false`, stripped image, Caddy/nginx, and public domain is future work

### WebSocket proxy is required for remote game access
On the VPS, the browser is remote. The WASM client connects to `ws://localhost:{port}` which resolves to the user's machine, not the VPS. Even fixing the URL doesn't work — Tailscale Serve provides HTTPS, and browsers block mixed-content `ws://` from `https://` pages. The only solution is a WebSocket reverse proxy through the harness (`/world/:worldID/ws → localhost:{gamePort}`). This only affects 3D games (Lightyear protocol); 2D games are client-side only.

### Verified security findings in the codebase
- Logout cookie at `harness/internal/auth/auth.go:252-257` is missing `HttpOnly`, `Secure`, `SameSite` flags (confirmed by reading the code)
- State-clearing cookie at `harness/internal/auth/auth.go:100-105` has the same issue
- `harness/static/game-loader.js:5` has no `event.origin` check on the postMessage listener
- No `middleware.BodyLimit`, `middleware.RateLimiter`, or server timeouts configured anywhere in `server.go` or `main.go`
- `handleAssetUpload` at `harness/internal/server/assets.go:23` has no file size limit

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — **The updated plan** (6 phases: bootstrap script, compose updates, env template, justfile helpers, application hardening, WebSocket proxy)
- `thoughts/CoreyCole/reviews/2026-02-13_12-26-04_vps-deployment-automation_review.md` — Critical review of the original plan (3 critical issues, 8 concerns, 5 questions)
- `thoughts/CoreyCole/research/2026-02-13_10-31-25_vps-deployment-security-hardening.md` — Security research (created in prior session, referenced here)
- `thoughts/CoreyCole/handoffs/general/2026-02-13_12-03-18_vps-deployment-security-hardening.md` — Prior handoff from the security research session

## Action Items & Next Steps

**The user wants another critical review of the updated plan.** The next session should:

1. **Run the `review_plan` skill** against `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md`
2. Focus on verifying:
   - DOCKER-USER iptables rules actually prevent Docker from exposing ports on eth0 (the entire security model depends on this)
   - The WebSocket proxy design (Phase 6) is feasible with Echo/Go's stdlib
   - The `restart: unless-stopped` addition doesn't cause problems for local dev (e.g., containers auto-starting unexpectedly)
   - Whether `postMessage` origin validation (Phase 5f) breaks trunk serve dev mode (cross-origin iframes)
   - The bootstrap script's idempotency claims are realistic
3. Compare the updated plan against the review document to verify all critical issues were addressed
4. Check if any new issues were introduced by the rewrite

## Other Notes

### Key source files for the reviewer
- `harness/docker-compose.yml` — Current compose (no override exists)
- `harness/internal/server/server.go` — Routes, middleware, handlers (all security hardening goes here)
- `harness/main.go` — Server startup, env var handling, timeout config
- `harness/internal/auth/auth.go` — Cookie handling, OAuth flow
- `harness/Dockerfile` — Container image (runs as root, no USER directive)
- `harness/static/game-loader.js` — postMessage bridge
- `harness/internal/world/game_server.go` — Game server lifecycle, port allocation, tmux sessions
- `harness/justfile` — Build/run targets

### Items explicitly deferred to Future Work
These were identified in the review but intentionally excluded from the plan to avoid scope creep:
- Dockerfile `USER` directive (non-root container) — needs investigation for tmux/cargo/go cache permissions
- Tailscale ACLs — not needed until more people join the tailnet
- Disk space monitoring — VPS-specific concern
- Release deployment pipeline — entirely separate server/domain/config
