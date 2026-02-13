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
---

# Research: VPS Deployment Security Hardening with Tailscale

**Date**: 2026-02-13T10:31:25-08:00
**Researcher**: CoreyCole
**Git Commit**: 4bfe1cc
**Branch**: main
**Repository**: creative-mode

## Research Question

We want to deploy the creative-mode harness to a shared VPS with sensitive API keys. What security hardening is needed — both at the VPS/infrastructure level and within the application itself? We plan to use Tailscale for SSH access over a private network.

## Summary

The creative-mode harness is currently a **development-only setup** with no production hardening. There are significant security gaps at every level: the Docker container runs as root, there's no TLS, no rate limiting on HTTP endpoints, no security headers, dev-mode endpoints have no auth, and the Claude Code integration runs with `--dangerously-skip-permissions`. Deploying to a shared VPS requires hardening at three layers: (1) VPS/OS level (Tailscale, firewall, SSH), (2) Docker/container level (non-root user, port restrictions, secrets management), and (3) application level (TLS termination, security headers, rate limiting, input validation).

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
Internet ──> [VPS:80/443] ──> Caddy (TLS termination) ──> Docker:8080 (harness)
                 |
            (all other ports blocked from internet)
                 |
Tailscale ──> [VPS:tailscale0] ──> SSH, admin, game ports, monitoring
```

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

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 80/tcp      # HTTP (for HTTPS redirect)
sudo ufw allow 443/tcp     # HTTPS
sudo ufw allow in on tailscale0  # All Tailscale traffic
sudo ufw enable
# Do NOT allow port 22 from anywhere
```

**Docker bypasses UFW** via iptables `DOCKER` chain. Add to `/etc/ufw/after.rules`:

```
# BEGIN DOCKER-USER RULES
*filter
:DOCKER-USER - [0:0]
-A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
-A DOCKER-USER -i tailscale0 -j RETURN
-A DOCKER-USER -p tcp --dport 80 -j RETURN
-A DOCKER-USER -p tcp --dport 443 -j RETURN
-A DOCKER-USER -i eth0 -j DROP
COMMIT
# END DOCKER-USER RULES
```

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
      # Only expose port 8080 on localhost — Caddy will proxy to it
      - "127.0.0.1:8080:8080"
      # Game server ports on Tailscale only (for direct client connections)
      - "100.x.y.z:9001-9100:9001-9100"
      # Do NOT expose trunk ports (8081-8180) in production
    environment:
      - CGO_ENABLED=1
      - DEV_MODE=false          # CRITICAL: disable dev endpoints
      - HARNESS_URL=https://your-domain.com
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
- `HARNESS_URL=https://...` — enables `Secure` flag on cookies
- `CM_HOOK_SECRET` set — protects `/api/claude-event`
- Port 8080 bound to `127.0.0.1` — only accessible via reverse proxy
- Trunk ports not exposed — no WASM hot-reload in production
- Game ports on Tailscale IP — only accessible to authenticated users
- Reduced volume mounts — no full project tree

### TLS Termination (Caddy)

Caddy provides automatic HTTPS with Let's Encrypt:

```
# /etc/caddy/Caddyfile
your-domain.com {
    reverse_proxy localhost:8080

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "SAMEORIGIN"
        Referrer-Policy "strict-origin-when-cross-origin"
        Content-Security-Policy "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; connect-src 'self' wss://your-domain.com"
    }
}
```

### Application Code Hardening TODO

These are changes needed in the Go harness code:

1. **Add Dockerfile `USER` directive** — run as non-root user
2. **Add HTTP rate limiting middleware** — Echo has `middleware.RateLimiter`
3. **Add request body size limits** — `e.Use(middleware.BodyLimit("1M"))`
4. **Add server timeouts** — configure `ReadTimeout`, `WriteTimeout`, `IdleTimeout`
5. **Add security headers middleware** — or rely on Caddy (above)
6. **Fix logout cookie flags** — match creation flags (`auth.go:252-257`)
7. **Add chat/prompt length limits** — prevent resource exhaustion
8. **Add `postMessage` origin validation** — `game-loader.js:5-6`
9. **Set `CM_HOOK_SECRET`** — and update hook scripts to send it
10. **Generate real Lightyear protocol credentials** — `shared/src/protocol.rs:14-15`
11. **Create `.env.example`** — document required environment variables

### Game Server Port Strategy (Production)

For production, game clients connect to WebSocket servers directly. Two options:

**Option A: Proxy through Caddy** — Add WebSocket proxy rules for game server ports. Clients connect to `wss://your-domain.com/game/{port}`. Keeps everything behind TLS.

**Option B: Direct Tailscale access** — Game ports exposed only on Tailscale IP. Users must be on the tailnet. More secure but limits who can play.

**Option C: Direct public ports with WASM** — Since game clients are WASM running in the browser, the browser needs to reach the game server port. This likely requires public exposure of game ports, but with proper Lightyear authentication (non-null keys).

---

## Part 4: Tailscale-Specific Recommendations

### Use Tailscale SSH Over Traditional SSH
- Zero SSH key management
- Identity-based access via SSO/IdP
- Centralized ACLs (add/remove access without touching servers)
- `check` mode forces re-authentication for root access
- Session recording available for compliance

### Docker + Tailscale: Host-Level (Simplest)
Install Tailscale on the VPS host. Bind admin ports to Tailscale IP in docker-compose. No sidecar containers needed.

### Emergency Access
- Keep VPS provider console access enabled (DigitalOcean Droplet Console, etc.)
- Optionally keep fallback OpenSSH on port 2222, bound to Tailscale IP only
- Tailscale connections survive brief control plane outages (WireGuard tunnels persist)

### Tailscale Account Security
- Enable 2FA on Tailscale account
- Consider tailnet lock for cryptographic node verification
- Use OAuth clients (not auth keys) for long-lived server registrations

---

## Deployment Checklist

### VPS Setup
- [ ] Create non-root user `deploy`
- [ ] Install Tailscale, register with `tag:server,tag:production`
- [ ] Enable Tailscale SSH (`tailscale set --ssh`)
- [ ] Disable key expiry in admin console
- [ ] Configure Tailscale ACLs
- [ ] Lock down sshd to Tailscale IP only
- [ ] Configure UFW (80, 443, tailscale0 only)
- [ ] Add DOCKER-USER iptables rules for Docker/UFW compatibility
- [ ] Install and configure Fail2Ban
- [ ] Configure Docker daemon security (`/etc/docker/daemon.json`)
- [ ] Install Caddy for TLS termination
- [ ] Verify: SSH via public IP fails, SSH via Tailscale works, HTTPS works

### Application Hardening
- [ ] Create production docker-compose variant (`DEV_MODE=false`)
- [ ] Set `HARNESS_URL` to production HTTPS URL
- [ ] Set `CM_HOOK_SECRET` environment variable
- [ ] Create `.env.example` for documentation
- [ ] Add `USER` directive to Dockerfile (non-root)
- [ ] Add HTTP rate limiting middleware
- [ ] Add request body size limits
- [ ] Add server read/write timeouts
- [ ] Fix logout cookie flags
- [ ] Add chat/prompt length limits
- [ ] Add `postMessage` origin validation in game-loader.js
- [ ] Decide game server port strategy (proxy vs direct)

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

## Open Questions

1. **Game server port strategy**: How will browser WASM clients reach game WebSocket servers? Through Caddy proxy, or direct? This affects whether game ports need public exposure.
2. **Claude Code sandboxing**: Is `--dangerously-skip-permissions` required, or can we restrict it? This is the single biggest attack surface.
3. **Multi-user Tailscale access**: Who else needs VPS access? This determines ACL complexity.
4. **Domain name**: What domain will the production harness use? Needed for Caddy config and OAuth callback URL.
5. **VPS provider**: Which provider? Affects console access, provider-level firewall options, and Docker storage driver.
