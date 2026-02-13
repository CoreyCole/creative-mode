---
date: 2026-02-13T10:31:25-08:00
researcher: CoreyCole
git_commit: 4bfe1cc095dec1a0f439348f3c4e11d961054778
branch: main
repository: creative-mode
topic: "VPS Deployment Security Hardening with Tailscale"
tags: [research, codebase, security, deployment, tailscale, docker, vps, hardening]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
last_updated_note: "Updated with architectural decisions: Tailscale-only access, WebSocket proxy through harness, Tailscale Serve for TLS, no public domain needed"
---

# Research: VPS Deployment Security Hardening with Tailscale

**Date**: 2026-02-13T10:31:25-08:00
**Researcher**: CoreyCole
**Git Commit**: 4bfe1cc
**Branch**: main
**Repository**: creative-mode

## Research Question

We want to deploy the creative-mode harness to a shared VPS with sensitive API keys. What security hardening is needed — both at the VPS/infrastructure level and within the application itself? We plan to use Tailscale as the exclusive access method (no public internet exposure).

## Key Architectural Decisions

1. **Tailscale-only access** — The harness will never be publicly accessible. All users must be on the tailnet. A separate public server will handle deployed games later.
2. **WebSocket reverse proxy through harness** — Game server WebSocket connections are proxied through the harness (`/world/{id}/ws` → `localhost:{port}`), eliminating the need to expose game port ranges. Only one port (8080) leaves the container.
3. **Tailscale Serve for TLS** — No Caddy, no purchased domain. Tailscale Serve provides automatic HTTPS at `https://{machine}.{tailnet}.ts.net` with valid Let's Encrypt certs.
4. **No public ports at all** — UFW denies all incoming on public interfaces. Only `tailscale0` interface accepts traffic.

## Summary

The creative-mode harness is currently a **development-only setup** with no production hardening. There are significant security gaps at every level: the Docker container runs as root, there's no TLS, no rate limiting on HTTP endpoints, no security headers, dev-mode endpoints have no auth, and the Claude Code integration runs with `--dangerously-skip-permissions`. Deploying to a shared VPS requires hardening at three layers: (1) VPS/OS level (Tailscale, firewall, SSH), (2) Docker/container level (non-root user, single-port exposure, secrets management), and (3) application level (WebSocket proxy, security headers, rate limiting, input validation). Tailscale Serve handles TLS termination — no reverse proxy or domain purchase needed.

---

## Part 1: Current Application Security Audit

### Critical Findings

| # | Finding | Location | Severity |
|---|---------|----------|----------|
| 1 | Container runs as root (no `USER` directive) | `harness/Dockerfile` | **High** |
| 2 | Claude Code runs with `--dangerously-skip-permissions` as root | `harness/internal/tmux/session.go:66-69` | **High** |
| 3 | No TLS/HTTPS anywhere — all traffic is plaintext | `harness/main.go:225` | **High** |
| 4 | `DEV_MODE=true` enables unauthenticated `/dev/rebuild` and `/dev/auth/login` endpoints | `harness/internal/server/server.go:111-120` | **High** |
| 5 | `/api/claude-event` is fully open when `CM_HOOK_SECRET` is unset | `harness/internal/server/server.go:548-560` | **Medium** |
| 6 | No HTTP-level rate limiting on login, chat, SSE, or any endpoint | No middleware configured | **Medium** |
| 7 | No request body size limits — unbounded JSON/form parsing | `server.go:566`, `debug.go:34` | **Medium** |
| 8 | No server read/write timeouts — connections can hang indefinitely | `main.go:225` (Echo defaults) | **Medium** |
| 9 | No security headers (CSP, HSTS, X-Frame-Options, etc.) | None configured | **Medium** |
| 10 | `postMessage` in game-loader.js has no origin validation | `harness/static/game-loader.js:5-6` | **Medium** |
| 11 | Lightyear netcode uses null authentication (protocol_id=0, key=all zeros) | `templates/3d/shared/src/protocol.rs:14-15` | **Medium** |
| 12 | 201 Docker ports exposed to host (8080, 8081-8180, 9001-9100) | `harness/docker-compose.yml:7-9` | **Medium** |
| 13 | Entire project tree mounted read-write in container | `harness/docker-compose.yml:11` | **Medium** |
| 14 | No chat message or prompt text length limits | `server.go:604`, `server.go:348` | **Low** |
| 15 | Logout cookie clearing omits HttpOnly/Secure/SameSite flags | `harness/internal/auth/auth.go:252-257` | **Low** |
| 16 | `postMessage` debug proxy uses wildcard target origin `'*'` | `harness/internal/server/debug.go:73-77` | **Low** |

### What's Already Good

- **SQL injection prevention**: All queries use sqlc-generated parameterized queries — no raw string concatenation
- **XSS prevention**: templ auto-escapes all template expressions; no `templ.Raw` usage
- **CSRF protection**: OAuth state parameter uses `crypto/rand`; cookies are `SameSite: Lax`
- **Session tokens**: 32 bytes of `crypto/rand`, hex-encoded (64 chars) — cryptographically secure
- **Path traversal protection**: `filepath.Clean` + `strings.HasPrefix` on all file-serving endpoints
- **Cookie flags**: `HttpOnly: true`, `SameSite: Lax`, `Secure` conditional on non-localhost
- **First-user admin**: Atomic transaction prevents race condition
- **Session cleanup**: Expired sessions deleted hourly + on startup
- **World name sanitization**: Filesystem-safe names via `sanitizeName()` (`harness/internal/world/manager.go:559-578`)

### Port Exposure Map

| Port(s) | Service | Bind Address | Docker-Exposed | Auth |
|---------|---------|-------------|----------------|------|
| 8080 | Go harness (HTTP + SSE) | 0.0.0.0 | Yes | Session cookies + middleware |
| 8081-8180 | Trunk serve (WASM dev) | 0.0.0.0 | Yes | **None** |
| 9001-9100 | Bevy game server (WebSocket) | 0.0.0.0 | Yes | Lightyear netcode (null key) |
| 9101-9999 | Bevy game server (allocatable) | 0.0.0.0 | **No** (Docker mismatch) | N/A |
| 10001-10100 | BRP debug (HTTP JSON-RPC) | 127.0.0.1 | **No** | None + CORS `*` |

### Secrets Inventory

| Secret | Source | Storage |
|--------|--------|---------|
| `GITHUB_CLIENT_ID` | `harness/.env` | Plaintext file (gitignored) |
| `GITHUB_CLIENT_SECRET` | `harness/.env` | Plaintext file (gitignored) |
| `ANTHROPIC_API_KEY` | Host environment | Passed via docker-compose |
| `CM_HOOK_SECRET` | Environment (optional) | **Not set by default** |
| Session tokens | `crypto/rand` | SQLite DB |

---

## Part 2: VPS Hardening Plan (with Tailscale)

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│ VPS                                                         │
│                                                             │
│  Public interface (eth0): ALL PORTS BLOCKED                 │
│                                                             │
│  Tailscale interface (tailscale0):                          │
│    ┌──────────────────────────────────────────────────┐     │
│    │ tailscale serve (TLS termination)                │     │
│    │ https://{machine}.{tailnet}.ts.net                │     │
│    │         │                                        │     │
│    │         ▼                                        │     │
│    │   localhost:8080 (harness Docker container)      │     │
│    │         │                                        │     │
│    │    ┌────┴─────────────────┐                      │     │
│    │    │ /world/{id}/ws       │  (WebSocket proxy)   │     │
│    │    │      │               │                      │     │
│    │    │      ▼               │                      │     │
│    │    │ localhost:900x       │  (game servers)      │     │
│    │    │ (inside container)   │                      │     │
│    │    └──────────────────────┘                      │     │
│    └──────────────────────────────────────────────────┘     │
│                                                             │
│  SSH: Tailscale SSH only (no public sshd)                   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Zero public ports.** The VPS is invisible to the internet. All access is through Tailscale.

### Step 1: Base OS Hardening

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Create non-root admin user
sudo adduser deploy
sudo usermod -aG sudo deploy

# SSH hardening (/etc/ssh/sshd_config)
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
MaxAuthTries 3
AllowUsers deploy
```

### Step 2: Install and Configure Tailscale

```bash
# Install Tailscale
curl -fsSL https://tailscale.com/install.sh | sh

# Start with tags for ACL targeting
sudo tailscale up --advertise-tags=tag:server,tag:production

# Enable Tailscale SSH (eliminates SSH key management)
sudo tailscale set --ssh

# Disable key expiry in admin console: https://login.tailscale.com/admin/machines
```

**Tailscale SSH vs Traditional SSH**: Tailscale SSH provides identity-based access via SSO, centralized ACLs, optional session recording, and zero SSH key management. Recommended over traditional SSH for production.

### Step 3: Lock Down SSH to Tailscale Only

```bash
# Bind sshd to Tailscale IP as fallback
# /etc/ssh/sshd_config:
ListenAddress 100.x.y.z    # Tailscale IP
Port 2222                   # Non-standard port
sudo systemctl restart sshd
```

### Step 4: Firewall (UFW)

Since this is Tailscale-only (no public web ports), the firewall is simple:

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow in on tailscale0  # All Tailscale traffic — the ONLY ingress rule
sudo ufw enable
# No port 22, no port 80, no port 443 on public interface
```

**Docker bypasses UFW** via iptables `DOCKER` chain. Add to `/etc/ufw/after.rules`:

```
# BEGIN DOCKER-USER RULES
*filter
:DOCKER-USER - [0:0]
-A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
-A DOCKER-USER -i tailscale0 -j RETURN
# Drop ALL traffic from public interface to Docker containers
-A DOCKER-USER -i eth0 -j DROP
COMMIT
# END DOCKER-USER RULES
```

This is even simpler than a public-facing setup — no HTTP/HTTPS exceptions needed.

### Step 5: Fail2Ban

```bash
sudo apt install fail2ban
# /etc/fail2ban/jail.local:
[DEFAULT]
bantime = 3600
findtime = 600
maxretry = 3
backend = systemd
```

### Step 6: Tailscale ACLs

Configure at https://login.tailscale.com/admin/acls:

```jsonc
{
  "groups": {
    "group:admin": ["coreycole@github"]
  },
  "tagOwners": {
    "tag:server":     ["group:admin"],
    "tag:production": ["group:admin"]
  },
  "acls": [
    {
      "action": "accept",
      "src":    ["group:admin"],
      "dst":    ["tag:server:*"]
    }
  ],
  "ssh": [
    {
      "action":      "check",
      "src":         ["group:admin"],
      "dst":         ["tag:production"],
      "users":       ["root", "deploy"],
      "checkPeriod": "12h"
    }
  ]
}
```

### Step 7: Docker Daemon Security

Create `/etc/docker/daemon.json`:

```json
{
  "live-restore": true,
  "userland-proxy": false,
  "no-new-privileges": true,
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
```

---

## Part 3: Application-Level Changes Required

### Production docker-compose.yml

The current `docker-compose.yml` needs a production variant:

```yaml
services:
  harness:
    build: .
    ports:
      # Single port — Tailscale Serve proxies to this
      - "127.0.0.1:8080:8080"
      # No game server ports exposed — harness proxies WebSocket internally
      # No trunk ports — no WASM hot-reload in production
    environment:
      - CGO_ENABLED=1
      - DEV_MODE=false          # CRITICAL: disable dev endpoints
      - HARNESS_URL=https://{machine}.{tailnet}.ts.net
      - CM_HOOK_SECRET=${CM_HOOK_SECRET}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
    env_file: .env
    volumes:
      # Mount only what's needed, not the entire project
      - ./data:/app/data
      - ./templates:/app/templates:ro
    security_opt:
      - no-new-privileges:true
    # TODO: Add USER directive to Dockerfile for non-root
```

Key changes from dev:
- `DEV_MODE=false` — disables `/dev/rebuild`, `/dev/auth/login`, `/dev/sse`
- `HARNESS_URL=https://{machine}.{tailnet}.ts.net` — enables `Secure` flag on cookies
- `CM_HOOK_SECRET` set — protects `/api/claude-event`
- **Single port** (8080) bound to `127.0.0.1` — Tailscale Serve proxies to it
- **No game server ports exposed** — harness reverse-proxies WebSocket connections internally
- No trunk ports — no WASM hot-reload in production
- Reduced volume mounts — no full project tree

### TLS Termination (Tailscale Serve)

No Caddy or domain purchase needed. Tailscale Serve provides automatic HTTPS with valid Let's Encrypt certs on `*.ts.net`:

```bash
# On the VPS: proxy HTTPS → harness container
tailscale serve https / http://localhost:8080
```

This gives you `https://{machine}.{tailnet}.ts.net` with:
- Valid TLS certificate (auto-renewed)
- Only accessible to tailnet members
- HTTPS → HTTP proxy to the container

For security headers, add them as Echo middleware in the harness (since Tailscale Serve doesn't have a header injection feature like Caddy):

```go
e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        h := c.Response().Header()
        h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
        h.Set("X-Content-Type-Options", "nosniff")
        h.Set("X-Frame-Options", "SAMEORIGIN")
        h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
        return next(c)
    }
})
```

### Game Server WebSocket Proxy (Decided)

**Decision: Reverse-proxy game WebSocket connections through the harness.**

Instead of exposing game server ports (9001-9100) outside the container, the harness proxies WebSocket connections:

```
Browser WASM client
    → wss://{machine}.{tailnet}.ts.net/world/{worldID}/ws
    → Tailscale Serve (TLS termination)
    → localhost:8080 (harness)
    → localhost:{gamePort} (game server inside container)
```

**Why this works:**
- The harness already tracks `worldID → gamePort` via `GameServerManager`
- Only one port (8080) needs to leave the container
- WebSocket connections inherit session auth from the `/world/*` middleware chain
- No dynamic firewall/port management when worlds are created/forked
- Game servers don't need their own TLS or authentication

**Implementation needed in harness:**
1. Add a WebSocket reverse proxy handler at `GET /world/:worldID/ws`
2. Look up game server port from `GameServerManager`
3. Proxy the WebSocket connection to `ws://localhost:{port}`
4. Use `net/http/httputil.ReverseProxy` or a lightweight WebSocket proxy
5. Update WASM client to connect to relative URL (`/world/{id}/ws`) instead of `ws://localhost:{port}`

**What changes in the client** (`templates/3d/client/src/main.rs`):
- Currently reads `server_port` from URL query param and connects to `ws://localhost:{port}`
- Change to connect to `ws://{window.location.host}/world/{worldID}/ws`
- The harness already passes `worldID` context; just needs to pass it to the iframe

### Application Code Hardening TODO

Changes needed in the Go harness code:

1. **Add WebSocket reverse proxy** — `GET /world/:worldID/ws` → `localhost:{gamePort}` (eliminates exposed game ports)
2. **Add Dockerfile `USER` directive** — run as non-root user
3. **Add security headers middleware** — HSTS, X-Content-Type-Options, X-Frame-Options, Referrer-Policy
4. **Add HTTP rate limiting middleware** — Echo has `middleware.RateLimiter`
5. **Add request body size limits** — `e.Use(middleware.BodyLimit("1M"))`
6. **Add server timeouts** — configure `ReadTimeout`, `WriteTimeout`, `IdleTimeout`
7. **Fix logout cookie flags** — match creation flags (`auth.go:252-257`)
8. **Add chat/prompt length limits** — prevent resource exhaustion
9. **Add `postMessage` origin validation** — `game-loader.js:5-6`
10. **Set `CM_HOOK_SECRET`** — and update hook scripts to send it
11. **Create `.env.example`** — document required environment variables

Note: Lightyear protocol credentials (`templates/3d/shared/src/protocol.rs:14-15`) are less critical now since game servers are only reachable inside the container, but should still be generated properly for defense in depth.

---

## Part 4: Tailscale-Specific Recommendations

### Use Tailscale SSH Over Traditional SSH
- Zero SSH key management
- Identity-based access via SSO/IdP
- Centralized ACLs (add/remove access without touching servers)
- `check` mode forces re-authentication for root access
- Session recording available for compliance

### Docker + Tailscale: Host-Level (Simplest)
Install Tailscale on the VPS host. Container binds to `127.0.0.1:8080`. Tailscale Serve proxies to it. No sidecar containers needed — the simplest possible setup.

### Tailscale Serve: No Caddy, No Domain
Tailscale Serve acts as the TLS-terminating reverse proxy:
- Automatic HTTPS certs for `*.ts.net` domains
- Only accessible to tailnet members
- One command: `tailscale serve https / http://localhost:8080`
- The resulting URL (e.g., `https://creative-mode.tail12345.ts.net`) becomes `HARNESS_URL`
- GitHub OAuth callback URL also uses this `*.ts.net` domain

### Emergency Access
- Keep VPS provider console access enabled (DigitalOcean Droplet Console, etc.)
- Optionally keep fallback OpenSSH on port 2222, bound to Tailscale IP only
- Tailscale connections survive brief control plane outages (WireGuard tunnels persist)

### Tailscale Account Security
- Enable 2FA on Tailscale account
- Consider tailnet lock for cryptographic node verification
- Use OAuth clients (not auth keys) for long-lived server registrations

### Future: Public Game Server (Separate)
The deployed game will eventually be served from a separate public-facing server. That server will have its own domain, Caddy/nginx, and public ports. The harness VPS remains Tailscale-only — it is the development environment, never the production game host.

---

## Deployment Checklist

### VPS Setup
- [ ] Create non-root user `deploy`
- [ ] Install Tailscale, register with `tag:server,tag:production`
- [ ] Enable Tailscale SSH (`tailscale set --ssh`)
- [ ] Disable key expiry in admin console
- [ ] Configure Tailscale ACLs (groups, tags, SSH rules)
- [ ] Lock down sshd to Tailscale IP only (fallback on port 2222)
- [ ] Configure UFW (deny all incoming, allow only `tailscale0`)
- [ ] Add DOCKER-USER iptables rules for Docker/UFW compatibility
- [ ] Install and configure Fail2Ban
- [ ] Configure Docker daemon security (`/etc/docker/daemon.json`)
- [ ] Configure Tailscale Serve (`tailscale serve https / http://localhost:8080`)
- [ ] Set up GitHub OAuth app with `*.ts.net` callback URL
- [ ] Verify: SSH via public IP fails, SSH via Tailscale works, HTTPS via `*.ts.net` works

### Application Hardening
- [ ] Implement WebSocket reverse proxy (`/world/:worldID/ws` → game server)
- [ ] Update WASM client to use relative WebSocket URL instead of `ws://localhost:{port}`
- [ ] Create production docker-compose (`DEV_MODE=false`, single port `127.0.0.1:8080`)
- [ ] Set `HARNESS_URL` to `https://{machine}.{tailnet}.ts.net`
- [ ] Set `CM_HOOK_SECRET` environment variable
- [ ] Create `.env.example` for documentation
- [ ] Add `USER` directive to Dockerfile (non-root)
- [ ] Add security headers middleware (HSTS, X-Content-Type-Options, etc.)
- [ ] Add HTTP rate limiting middleware
- [ ] Add request body size limits
- [ ] Add server read/write timeouts
- [ ] Fix logout cookie flags
- [ ] Add chat/prompt length limits
- [ ] Add `postMessage` origin validation in game-loader.js

---

## Code References

- `harness/Dockerfile` — Container image, runs as root, installs Claude Code CLI
- `harness/docker-compose.yml:7-9` — 201 ports exposed
- `harness/docker-compose.yml:11` — Entire project tree mounted RW
- `harness/internal/auth/auth.go:66-74` — Session cookie flags
- `harness/internal/auth/auth.go:147-169` — Session token generation (crypto/rand)
- `harness/internal/auth/auth.go:252-257` — Logout cookie (missing flags)
- `harness/internal/auth/auth.go:423-501` — Dev login endpoint
- `harness/internal/auth/middleware.go:17-55` — Session validation middleware
- `harness/internal/server/server.go:111-120` — Dev endpoints (no auth)
- `harness/internal/server/server.go:548-560` — Hook secret middleware (optional)
- `harness/internal/tmux/session.go:66-69` — Claude Code `--dangerously-skip-permissions`
- `harness/internal/world/ports.go:9-13` — Port range definitions
- `harness/internal/world/game_server.go:689-691` — Trunk binds 0.0.0.0
- `harness/internal/world/rate_limit.go` — Only rate limiter (prompt submissions)
- `harness/static/game-loader.js:5-6` — postMessage without origin validation
- `harness/internal/server/debug.go:73-77` — postMessage with wildcard target
- `harness/main.go:225` — Server binds :8080, no timeouts
- `templates/3d/shared/src/protocol.rs:14-15` — Null Lightyear credentials

## Architecture Insights

1. **Dev-only design**: The entire system is built for local development. There is no production mode, no CI/CD, no TLS, no reverse proxy config. Everything needs to be created from scratch.
2. **Claude Code is the biggest risk**: Running with `--dangerously-skip-permissions` as root in the container means user-submitted prompts have full access to all secrets and can execute arbitrary commands. This is inherent to the product design but needs container-level isolation.
3. **Docker bypasses UFW**: This is a well-known issue. The DOCKER-USER chain rules in `/etc/ufw/after.rules` are essential.
4. **Cookie security is well-designed**: The conditional `Secure` flag based on `HARNESS_URL` means setting it to an HTTPS URL automatically enables secure cookies.
5. **sqlc prevents SQL injection**: All database access uses generated parameterized queries — this is solid.
6. **Tailscale-only simplifies everything**: No public ports means no Caddy, no domain, simpler firewall rules, and the WebSocket proxy through the harness means game ports stay internal to the container.
7. **Single-port container**: With the WebSocket proxy, the entire harness (HTTP, SSE, WebSocket game connections) runs through one port. This is the ideal Docker setup — minimal surface area.

## Resolved Questions

1. ~~**Game server port strategy**~~: **Decided — WebSocket reverse proxy through harness.** Browser connects to `/world/{id}/ws`, harness proxies to `localhost:{port}` inside the container. No game ports exposed.
2. ~~**Domain name**~~: **Not needed.** Tailscale Serve provides `https://{machine}.{tailnet}.ts.net` with valid TLS certs. GitHub OAuth callback URL uses this domain.
3. ~~**Public access**~~: **Never.** Harness is Tailscale-only. A separate public server will host deployed games later.

## Open Questions

1. **Claude Code sandboxing**: Is `--dangerously-skip-permissions` required, or can we restrict it? This is the single biggest attack surface.
2. **Multi-user Tailscale access**: Who else needs VPS access? This determines ACL complexity.
3. **VPS provider**: Which provider? Affects console access, provider-level firewall options, and Docker storage driver.
