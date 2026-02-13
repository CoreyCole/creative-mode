---
date: 2026-02-13T12:15:44-08:00
researcher: CoreyCole
git_commit: b9ff0ff
branch: main
repository: creative-mode
topic: "VPS Deployment Automation Plan"
tags: [plan, deployment, tailscale, docker, vps, security]
status: draft
last_updated: 2026-02-13
last_updated_by: CoreyCole
type: implementation_plan
---

# Plan: VPS Deployment Automation

## Goal

From a fresh Ubuntu VPS, one bootstrap script secures the machine and installs dependencies. Then `git clone` + `create .env` + `just up` starts the harness — identical to local dev, but network-hardened via Tailscale with no public ports. The VPS is a **shared development server** for building the game and the harness around it. A separate release/deployment pipeline for serving production builds to end users will come later.

## Decisions

- **Same environment everywhere** — VPS runs the exact same Docker image and `docker-compose.yml` as local dev (`DEV_MODE=true`, air, cargo-watch, full Rust toolchain). The only difference is the `.env` file contents and the OS-level network hardening.
- **Git clone + pull** — Repo lives on VPS. `git pull && just up` to update.
- **Tailscale-only** — No public ports. Tailscale Serve for HTTPS. All users must be on the tailnet. DOCKER-USER iptables rules ensure Docker can't bypass UFW.
- **No compose override** — Network security comes from the OS (UFW + DOCKER-USER), not from Docker port binding. Same ports, same config, different `.env`.

## Architecture

```
Fresh Ubuntu VPS
    │
    ├── scripts/vps-bootstrap.sh (run once, from cloned repo)
    │     ├── Create 'deploy' user
    │     ├── Install Tailscale + enable Tailscale SSH
    │     ├── Install Docker
    │     ├── Configure UFW (deny all, allow tailscale0)
    │     ├── Configure DOCKER-USER iptables rules
    │     ├── Install Fail2Ban
    │     ├── Configure Docker daemon (/etc/docker/daemon.json)
    │     └── Lock down sshd to Tailscale IP
    │
    ├── git clone → /home/deploy/creative-mode/
    │
    ├── harness/.env (created from .env.example — all secrets here)
    │
    ├── just up (from harness/) — same command as local dev
    │
    ├── sudo tailscale serve https / http://localhost:8080
    │
    └── https://{machine}.{tailnet}.ts.net → harness
```

**How network security works without a compose override:**

Docker bypasses UFW by default via its own iptables chains. The DOCKER-USER rules in `/etc/ufw/after.rules` fix this — they DROP all traffic from the public interface (`eth0`) to Docker containers, and only RETURN (allow) traffic from the Tailscale interface (`tailscale0`). This means all Docker-exposed ports (8080, 8081-8180, 9001-9100) are reachable only from tailnet members, without changing anything in `docker-compose.yml`.

On local dev, there's no UFW — all ports are reachable on localhost as usual.

## Files to Create/Modify

### Phase 1: VPS Bootstrap Script

**New file: `scripts/vps-bootstrap.sh`**

Interactive script run from the cloned repo (NOT piped from the internet). Must be run as root.

1. Creates `deploy` user with sudo (prompts for password)
2. Installs Tailscale (`curl -fsSL https://tailscale.com/install.sh | sh`)
3. Runs `tailscale up` (interactive — opens auth URL)
4. Enables Tailscale SSH (`tailscale set --ssh`)
5. Installs Docker Engine (official Docker apt repo)
6. Configures UFW:
   ```bash
   ufw default deny incoming
   ufw default allow outgoing
   ufw allow in on tailscale0
   ufw enable
   ```
7. Adds DOCKER-USER rules to `/etc/ufw/after.rules`:
   ```
   *filter
   :DOCKER-USER - [0:0]
   -A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
   -A DOCKER-USER -i tailscale0 -j RETURN
   -A DOCKER-USER -i eth0 -j DROP
   COMMIT
   ```
8. Creates `/etc/docker/daemon.json`:
   ```json
   {
     "live-restore": true,
     "userland-proxy": false,
     "no-new-privileges": true,
     "log-driver": "json-file",
     "log-opts": { "max-size": "10m", "max-file": "3" }
   }
   ```
9. Installs Fail2Ban with sane defaults
10. Locks down sshd: `ListenAddress {tailscale_ip}`, `Port 2222`, `PermitRootLogin no`, `PasswordAuthentication no`
11. Adds `deploy` user to `docker` group
12. Sets up daily SQLite backup cron:
    ```bash
    # /etc/cron.daily/backup-creative-mode
    sqlite3 /home/deploy/creative-mode/data/creative-mode.db \
      ".backup /home/deploy/backups/creative-mode-$(date +%Y%m%d).db"
    # Keep last 7 days
    find /home/deploy/backups -name '*.db' -mtime +7 -delete
    ```
13. Prints summary of what was done + next steps (including GitHub OAuth callback URL reminder)

The script should be idempotent — safe to re-run. Each step checks if already done before modifying.

### Phase 2: Base Docker Compose Updates

**Modified file: `harness/docker-compose.yml`**

Add `restart: unless-stopped` to the existing service (harmless for local dev — just means `docker compose up` auto-restarts on crash; `docker compose down` still stops cleanly):

```yaml
services:
  harness:
    build:
      context: .
      dockerfile: Dockerfile
    restart: unless-stopped    # NEW: survive VPS reboots
    ports:
      - "8080:8080"
      - "8081-8180:8081-8180"
      - "9001-9100:9001-9100"
    volumes:
      - ..:/app:cached
      - go-mod-cache:/go/pkg/mod
      - go-build-cache:/root/.cache/go-build
      - cargo-registry:/usr/local/cargo/registry
      - cargo-git:/usr/local/cargo/git
    env_file: .env
    environment:
      - CGO_ENABLED=1
      - DEV_MODE=true
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
```

No port changes, no `DEV_MODE` override. Network security comes from the OS level, not Docker.

### Phase 3: Environment Template

**New file: `harness/.env.example`**

```bash
# GitHub OAuth (required for authentication)
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=

# Anthropic API key (required for Claude Code sessions)
ANTHROPIC_API_KEY=

# --- VPS only (not needed for local dev) ---

# Harness URL — set to your Tailscale Serve URL on the VPS.
# Enables Secure cookies and HSTS. Leave unset for local dev (defaults to http://localhost:8080).
# HARNESS_URL=https://your-machine.tailnet-name.ts.net

# Hook secret — protects /api/claude-event endpoint.
# Generate with: openssl rand -hex 32
# CM_HOOK_SECRET=
```

All secrets live in `.env` (already gitignored). No secrets in `.bashrc` or host environment.

### Phase 4: Justfile Deployment Helpers

**Modified file: `harness/justfile`** — add VPS convenience targets:

```just
# --- VPS Helpers ---

# Redeploy (pull latest + rebuild)
redeploy:
    git pull
    just up

# Status check (container + Tailscale)
status:
    @docker compose ps
    @echo ""
    @tailscale status --peers=false 2>/dev/null || echo "Tailscale: not installed (local dev)"
```

No separate `deploy` / `deploy-down` commands — the VPS uses the same `just up` / `just down` as local dev. The `redeploy` target is just a convenience for `git pull && just up`.

### Phase 5: Application Hardening (Go code + JS)

Changes to the harness code. Applied everywhere (local + VPS). These are security improvements independent of deployment.

**5a. Security headers middleware** — `harness/internal/server/server.go`

Add middleware in `RegisterRoutes` (early, before all routes):
```go
e.Use(securityHeadersMiddleware(baseURL))
```

The middleware sets headers only when `HARNESS_URL` is not localhost (same conditional as cookie Secure flag):
- `Strict-Transport-Security: max-age=31536000; includeSubDomains`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: SAMEORIGIN`
- `Referrer-Policy: strict-origin-when-cross-origin`

**5b. Request body size limit** — `harness/internal/server/server.go`

Add Echo middleware:
```go
e.Use(middleware.BodyLimit("1M"))
```

**5c. Server timeouts** — `harness/main.go`

Configure the underlying `http.Server`:
```go
e.Server.ReadTimeout = 30 * time.Second
e.Server.WriteTimeout = 0  // must be 0 for SSE (long-lived connections)
e.Server.IdleTimeout = 120 * time.Second
```

Note: `WriteTimeout` must be 0 (disabled) because SSE connections are long-lived. Read and idle timeouts protect against slowloris attacks.

**5d. Fix logout cookie flags** — `harness/internal/auth/auth.go:252-257`

Add missing flags to match the creation cookie:
```go
c.SetCookie(&http.Cookie{
    Name:     "session",
    Value:    "",
    Path:     "/",
    MaxAge:   -1,
    HttpOnly: true,
    Secure:   !isLocalhost(h.config.BaseURL),
    SameSite: http.SameSiteLaxMode,
})
```

Also fix the state-clearing cookie at lines 100-105 with the same flags.

**5e. Chat/prompt length limits** — `harness/internal/server/server.go`

In the chat handler, add after the empty check:
```go
if len(input.ChatText) > 4000 {
    return echo.NewHTTPError(http.StatusBadRequest, "message too long")
}
```

In the prompt handler, add after the empty check:
```go
if len(promptText) > 10000 {
    return echo.NewHTTPError(http.StatusBadRequest, "prompt too long")
}
```

**5f. postMessage origin validation** — `harness/static/game-loader.js`

Add origin check at the top of the message handler:
```js
window.addEventListener('message', function(event) {
    if (event.origin !== window.location.origin) return;
    if (!event.data || !event.data.type) return;
    // ... rest of handler
});
```

**5g. HTTP rate limiting** — `harness/internal/server/server.go`

Add Echo rate limiter middleware for sensitive endpoints:
```go
rateLimiter := middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(
    rate.Limit(20), // 20 requests/second
))
```

Apply to chat and prompt routes. The existing application-level prompt rate limiter (30s cooldown per user) stays — this is an additional HTTP-level guard against abuse.

### Phase 6: WebSocket Reverse Proxy

**Why this is needed:** On the VPS, the browser is remote. The WASM game client currently connects to `ws://localhost:{port}` which resolves to the user's machine, not the VPS. Even if we fix the URL, the page is served over HTTPS (via Tailscale Serve) so the browser blocks mixed-content `ws://` connections. The only way to reach game servers from a remote browser is to proxy WebSocket connections through the harness, which already has HTTPS via Tailscale Serve.

**New handler in `harness/internal/server/server.go`:**

Add `GET /world/:worldID/ws` route in `registerWorldRoutes`:
```go
w.GET("/:worldID/ws", s.handleGameWebSocket)
```

The handler:
1. Looks up the user's current checkpoint via `GetUserPosition`
2. Gets the game server port from `GameServerManager.GetServer`
3. Upgrades to WebSocket and proxies to `ws://localhost:{port}`
4. Uses `net/http/httputil.ReverseProxy` or a lightweight WS proxy library

**Update WASM client** (`templates/3d/client/src/main.rs`):
- Currently reads `server_port` from URL query param and connects to `ws://localhost:{port}`
- Change to connect to `wss://{window.location.host}/world/{worldID}/ws` (relative URL)
- The harness already passes `worldID` context to the iframe

**Note:** This phase is only needed for 3D games (Lightyear WebSocket protocol). 2D games are client-side only (no game server) and work without this.

## Implementation Order

1. **Phase 3**: `.env.example` — trivial, document what's needed
2. **Phase 5**: Application hardening (Go code + JS) — security fixes, testable locally
3. **Phase 2**: Add `restart: unless-stopped` to base compose
4. **Phase 4**: Justfile helpers
5. **Phase 6**: WebSocket proxy — enables 3D games on VPS
6. **Phase 1**: `scripts/vps-bootstrap.sh` — VPS setup script (last because it's only run on the VPS)

Phases 1-5 are independent. Phase 6 is only needed for 3D game support on the VPS; 2D games work without it. The order lets us validate everything locally before touching the VPS.

## Deployment Workflow (End State)

### First-time setup (on fresh VPS):
```bash
# 1. SSH in as root (or via VPS console)
# 2. Install git and clone the repo
apt update && apt install -y git
git clone https://github.com/{user}/creative-mode.git /opt/creative-mode

# 3. Run the bootstrap script from the cloned repo
sudo bash /opt/creative-mode/scripts/vps-bootstrap.sh

# 4. Switch to deploy user, move repo to home
su - deploy
mv /opt/creative-mode ~/creative-mode
cd ~/creative-mode/harness

# 5. Create .env from template
cp .env.example .env
# Edit .env: add GitHub OAuth creds, ANTHROPIC_API_KEY,
# HARNESS_URL=https://{machine}.{tailnet}.ts.net,
# CM_HOOK_SECRET=$(openssl rand -hex 32)

# 6. Start the harness (same command as local dev)
just up

# 7. Set up Tailscale Serve (one-time, persists across reboots)
sudo tailscale serve https / http://localhost:8080
```

### Updating:
```bash
cd ~/creative-mode/harness
just redeploy   # git pull + docker compose up --build
```

### What's different from local dev:
| | Local | VPS |
|---|---|---|
| Start command | `just up` | `just up` |
| Docker image | Same | Same |
| `DEV_MODE` | `true` | `true` |
| Network security | None (localhost) | Tailscale + UFW + DOCKER-USER |
| HTTPS | No | Tailscale Serve (`*.ts.net`) |
| `.env` contents | GitHub OAuth + API key | + `HARNESS_URL` + `CM_HOOK_SECRET` |
| Auto-restart | On crash only | On crash + VPS reboot |

## Resolved Questions

1. **GitHub OAuth callback URL** — Update in GitHub OAuth App settings to `https://{machine}.{tailnet}.ts.net/auth/github/callback`. Bootstrap script prints a reminder.
2. **ANTHROPIC_API_KEY management** — Goes in `.env` (already gitignored). Simpler than host env, and `.env` is the single source for all secrets.
3. **Tailscale Serve on boot** — Persists across reboots by default. Verified.
4. **Docker auto-restart** — Yes: `restart: unless-stopped` added to base compose. Harmless for local dev.

## Future Work (Not In This Plan)

- **Release deployment pipeline** — Separate server for serving production builds to end users. Different domain, Caddy/nginx, public ports, `DEV_MODE=false`, stripped Docker image. Entirely separate concern from the dev VPS.
- **Dockerfile `USER` directive** — Run container as non-root. Important for defense in depth but requires adjusting file permissions for `data/`, tmux sessions, and Cargo/Go caches. Tracked separately to avoid scope creep in this plan.
- **Tailscale ACLs** — Group/tag-based access control for multi-user tailnet. Not needed until more people join the tailnet.
- **Disk space monitoring** — Rust builds and WASM artifacts can fill a small VPS. Add alerts before it becomes a problem.
