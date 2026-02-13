---
date: 2026-02-13T12:03:18-08:00
researcher: CoreyCole
git_commit: beaada374615be99a3bfac9a65429622848834d0
branch: main
repository: creative-mode
topic: "VPS Deployment Security Hardening with Tailscale"
tags: [implementation, strategy, security, deployment, tailscale, docker, vps]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VPS Deployment Security Hardening with Tailscale

## Task(s)

1. **Security audit of creative-mode harness** — **Completed.** Full audit of Docker config, auth/session management, network exposure, secrets handling, and application-level vulnerabilities.
2. **VPS hardening plan with Tailscale** — **Completed.** Comprehensive plan for deploying to a shared VPS using Tailscale as the exclusive access method (no public internet exposure).
3. **Architectural decisions** — **Completed.** Three key decisions were made:
   - Tailscale-only access (no public ports ever; separate public server for deployed game later)
   - WebSocket reverse proxy through harness (game server connections proxied via `/world/{id}/ws`, eliminating exposed port ranges)
   - Tailscale Serve for TLS (no Caddy, no purchased domain — automatic HTTPS at `*.ts.net`)
4. **Implementation of hardening changes** — **Planned/not started.** The research document contains a full deployment checklist and application hardening TODO list.

## Critical References

- `thoughts/CoreyCole/research/2026-02-13_10-31-25_vps-deployment-security-hardening.md` — The primary research document containing the full security audit, architecture decisions, VPS setup steps, and deployment checklist. **Read this first.**
- `harness/CLAUDE.md` — Harness architecture and conventions
- `CLAUDE.md` — Project-level instructions (Docker-only running, skills, build commands)

## Recent changes

- `thoughts/CoreyCole/research/2026-02-13_10-31-25_vps-deployment-security-hardening.md` — Created and updated with full security audit + deployment plan

No code changes were made to the harness or templates. This was a research-only session.

## Learnings

### Security Audit Key Findings
- **Container runs as root** with no `USER` directive in `harness/Dockerfile`
- **Claude Code runs with `--dangerously-skip-permissions` as root** at `harness/internal/tmux/session.go:66-69` — single biggest attack surface
- **`DEV_MODE=true`** (hardcoded in docker-compose) enables unauthenticated `/dev/rebuild` and `/dev/auth/login` endpoints at `harness/internal/server/server.go:111-120`
- **`CM_HOOK_SECRET` is unset by default**, making `/api/claude-event` fully open at `harness/internal/server/server.go:548-560`
- **201 Docker ports exposed** (8080 + 8081-8180 + 9001-9100) at `harness/docker-compose.yml:7-9`
- **No TLS, no rate limiting, no security headers, no body size limits, no server timeouts**

### What's Already Solid
- sqlc parameterized queries (no SQL injection risk)
- templ auto-escaping (no XSS risk)
- Session tokens: 32 bytes `crypto/rand`
- Cookie flags: `HttpOnly`, `SameSite: Lax`, conditional `Secure` based on `HARNESS_URL`

### Tailscale Architecture Decision
- **Zero public ports** — UFW denies all incoming on public interfaces, only `tailscale0` accepts traffic
- **Docker bypasses UFW** — requires DOCKER-USER iptables rules in `/etc/ufw/after.rules`
- **Tailscale Serve** replaces Caddy entirely — `tailscale serve https / http://localhost:8080` provides automatic HTTPS with valid Let's Encrypt certs on `*.ts.net`
- **WebSocket proxy through harness** means only port 8080 needs to leave the container; game servers stay internal

## Artifacts

- `thoughts/CoreyCole/research/2026-02-13_10-31-25_vps-deployment-security-hardening.md` — Full research document with:
  - Part 1: Current application security audit (16 findings table + what's good)
  - Part 2: VPS hardening plan (7 steps: OS, Tailscale, SSH, UFW, Fail2Ban, ACLs, Docker daemon)
  - Part 3: Application-level changes (production docker-compose, Tailscale Serve, WebSocket proxy design, 11-item hardening TODO)
  - Part 4: Tailscale-specific recommendations
  - Deployment checklist (13 VPS items + 14 application items)
  - Architecture diagram showing full Tailscale-only stack

## Action Items & Next Steps

### Infrastructure (VPS Setup)
1. Choose a VPS provider and provision a server
2. Follow the VPS Setup checklist in the research document (steps 1-7: user creation, Tailscale install, SSH lockdown, UFW, Fail2Ban, ACLs, Docker daemon)
3. Configure Tailscale Serve for TLS termination
4. Set up GitHub OAuth app with `*.ts.net` callback URL

### Application Hardening (Code Changes)
Priority order from the research document:

1. **Implement WebSocket reverse proxy** — New handler at `GET /world/:worldID/ws` that proxies to `localhost:{gamePort}`. The harness already tracks `worldID → port` in `GameServerManager`. Also update the WASM client (`templates/3d/client/src/main.rs`) to connect to relative URL `/world/{id}/ws` instead of `ws://localhost:{port}`.
2. **Create production docker-compose** — `DEV_MODE=false`, single port `127.0.0.1:8080`, set `CM_HOOK_SECRET`, `HARNESS_URL=https://{machine}.{tailnet}.ts.net`, reduced volume mounts.
3. **Add Dockerfile `USER` directive** — Run as non-root.
4. **Add security headers middleware** — HSTS, X-Content-Type-Options, X-Frame-Options, Referrer-Policy (as Echo middleware since Tailscale Serve doesn't inject headers).
5. **Add rate limiting, body limits, timeouts** — Echo middleware: `middleware.RateLimiter`, `middleware.BodyLimit("1M")`, and `ReadTimeout`/`WriteTimeout`/`IdleTimeout` on the HTTP server.
6. **Fix logout cookie flags** — `harness/internal/auth/auth.go:252-257` — match creation flags.
7. **Add chat/prompt length limits and postMessage origin validation**.
8. **Create `.env.example`** — Document required environment variables.

## Other Notes

- The harness is **never** intended to be publicly accessible. All users must be on the tailnet. A separate public server will host deployed games later.
- The reference article used for VPS hardening: https://dev.to/tomas223/securing-a-vps-essential-hardening-steps-for-web-developers-part-1-242j
- Tailscale free tier supports up to 100 devices and 3 users — sufficient for the team.
- Lightyear null credentials (`templates/3d/shared/src/protocol.rs:14-15`) are less critical now since game servers stay internal to the container, but should still be hardened for defense in depth.
- The `--dangerously-skip-permissions` flag on Claude Code is an open question — it's inherent to the product design but is the single biggest attack surface. Consider whether container-level isolation is sufficient mitigation.
