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
last_updated_by: Claude (ARM64 VM compatibility review + VM backup strategy)
type: implementation_plan
---

# Plan: VPS Deployment Automation

## Goal

From a fresh Ubuntu instance (cloud VPS or local VM), one bootstrap script secures the machine and installs dependencies. Then `git clone` + `create .env` + `just up` starts the harness — identical to local dev, but network-hardened via Tailscale with no public ports. The server is a **shared development server** for building the game and the harness around it. A separate release/deployment pipeline for serving production builds to end users will come later.

## Hosting Options

### Option A: Local Ubuntu VM (recommended to start)

Run an Ubuntu 24.04 ARM64 VM on macOS using UTM (QEMU + Apple Hypervisor.framework). Tailscale inside the VM makes it accessible to friends on the tailnet, exactly like a cloud VPS.

**ARM64 compatibility**: Verified. All Dockerfile layers (Go, Rust, trunk, cargo-watch, wasm-bindgen-cli, Claude Code CLI) build and run natively on ARM64 Linux. The WASM cross-compilation target (`wasm32-unknown-unknown`) is architecture-independent. Docker Engine runs natively on the Linux kernel inside the VM using namespaces/cgroups — no nested virtualization. See [ARM64 Compatibility Notes](#arm64-compatibility-notes) for details.

**Why start here:**
- No monthly cost
- Full control over resources
- Docker Engine runs natively in the Linux VM (not Docker Desktop — no nested VM)
- 64 GB host RAM → 16 GB for VM, leaves ~48 GB for macOS
- Tailscale makes it indistinguishable from a cloud VPS to other tailnet members

**VM setup (UTM):**
1. Download UTM from https://mac.getutm.app (free from GitHub)
2. Create new VM: Ubuntu 24.04 ARM64, 16 GB RAM, 4-8 CPU cores, 80 GB disk (qcow2 sparse — only uses actual space)
3. Network: NAT (default, "Emulated VLAN" in UTM) — Tailscale tunnels through it, no port forwarding needed
4. Install Ubuntu Server (minimal), then run `scripts/vps-bootstrap.sh`

**How friends connect:**
```
Friend's browser → Tailscale tunnel → VM's tailscale0 → localhost:8080 → Docker harness
```
The VM appears as a regular node on the tailnet. QEMU's NAT doesn't matter — Tailscale bypasses it. QEMU's user-mode NAT behaves as a standard cone NAT that Tailscale traverses easily; direct WireGuard connections are expected (not DERP relay fallback).

**Expected network interface:** `enp0s1` (UTM/QEMU ARM64 Ubuntu uses predictable names: `en` + `p0` bus + `s1` slot). The bootstrap script auto-detects this via `ip route show default`.

**Tradeoffs:**
- Server is only up when the laptop is on and VM is running
- Uses laptop CPU/RAM/battery while running
- Network speed depends on laptop's internet connection
- No guaranteed uptime — fine for a dev server, not for production

### Option B: Cloud VPS

For always-on hosting, use any Ubuntu 24.04 VPS. The bootstrap script works identically.

**Recommended specs:** 16 GB RAM, 80 GB disk, 4 vCPUs. Providers: Hetzner CAX (ARM64 Ampere, best price/performance), Oracle Cloud A1 (free ARM64 tier — up to 4 OCPU / 24 GB RAM), DigitalOcean, Linode.

**Cloud migration from local VM:** The qcow2 disk image is portable. Oracle Cloud accepts qcow2 natively (install `cloud-init` in the guest first). Hetzner requires `qemu-img convert -O raw` + upload via `hcloud-upload-image`. See [VM Backup & Migration](#vm-backup--migration) for details.

**When to move to cloud:** When uptime matters or the laptop can't stay on.

## Decisions

- **Same environment everywhere** — The server runs the exact same Docker image and `docker-compose.yml` as local dev (`DEV_MODE=true`, air, cargo-watch, full Rust toolchain). The only difference is the `.env` file contents and the OS-level network hardening.
- **Git clone + pull** — Repo lives on server. `git pull && just up` to update.
- **Tailscale-only** — No public ports. Tailscale Serve for HTTPS. All users must be on the tailnet. DOCKER-USER iptables rules ensure Docker can't bypass UFW.
- **No compose override** — Network security comes from the OS (UFW + DOCKER-USER), not from Docker port binding. Same ports, same config, different `.env`.
- **VM or VPS — same bootstrap** — The bootstrap script works on any Ubuntu 24.04 instance. On a local VM, Fail2Ban and sshd lockdown are optional (no public SSH to brute-force) but harmless to include.

## Server Sizing

**Recommended: 16 GB RAM, 80 GB disk** (single developer, 1-2 concurrent builds, 3-5 worlds)

### Memory Budget

| Component | RAM |
|-----------|-----|
| Idle baseline (Go server + 1 game server + 2 trunk serves + tmux) | ~500 MB |
| Bevy release build (741 crates, 3D template) | +4-8 GB peak |
| Bevy release build (510 crates, 2D template) | +2-4 GB peak |
| Per additional 3D game server | +50-150 MB |
| Per Claude Code session | +200-500 MB |

Builds are the dominant consumer. The rate limiter enforces 1 active build per user but there is no global concurrency cap — multiple users can trigger simultaneous Bevy compiles. Consider adding a global semaphore (future work).

### Disk Budget

| Component | Size | Growth |
|-----------|------|--------|
| Docker image (Go + Rust toolchain + trunk + cargo tools) | 3-5 GB | Static |
| Cargo registry + git caches | 2-4 GB | Slow |
| Rust `target/` per build tree (hardlinked via `cp -al`) | 2-5 GB | Diverges per build |
| WASM output per checkpoint | 20-40 MB | Per checkpoint |
| Go mod + build caches | ~700 MB | Slow |
| SQLite + logs + shared assets | < 1 GB | Slow |

Rust `target/` cache divergence is the main growth factor. `ForkCheckpoint` uses `cp -al` (hardlinks on Linux), so initial forks are free, but each incremental build creates new object files. Old checkpoints are never cleaned up — future work should add pruning.

## ARM64 Compatibility Notes

All components have been verified for ARM64 (aarch64) Ubuntu 24.04 in a UTM/QEMU VM.

### Dockerfile Layers

| Layer | ARM64 Status | Notes |
|-------|-------------|-------|
| `golang:1.24-bookworm` | Native multi-arch | Official image includes `arm64v8`. Also installs `binutils-gold` on ARM64 specifically for Go. |
| `apt-get install gcc libc6-dev ...` | Available | All packages in ARM64 Debian repos. gcc/libc6-dev are redundant with base image but harmless. |
| `rustup` + `wasm32-unknown-unknown` | Fully supported | rustup auto-detects `aarch64-unknown-linux-gnu`. WASM target is Tier 2, cross-compilable from any host. |
| `cargo install trunk` | Works | Historical ARM64 issue (trunk #212, #903) only affected pre-built binary downloads, not `cargo install` from source. |
| `cargo install cargo-watch` | Works | Pure Rust. Uses `inotify` on Linux (kernel feature, not arch-dependent). |
| `cargo install wasm-bindgen-cli` | Works | Compiles from source. Requires Rust >= 1.81 (stable satisfies). |
| `Claude Code CLI install` | Supported | Prebuilt `linux-arm64` binary (~220 MB) in official manifest. Install script auto-detects `aarch64`. |

**Dockerfile improvement**: Add `mold` linker to reduce Bevy release build memory and link time on ARM64:
```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc6-dev tmux curl jq ca-certificates pkg-config mold \
    && rm -rf /var/lib/apt/lists/*
```

**Dockerfile EXPOSE fix**: Current Dockerfile only documents `EXPOSE 8081` (single port). Should match the full ranges:
```dockerfile
EXPOSE 8080
EXPOSE 8081-8180
EXPOSE 9001-9100
```
(`EXPOSE` is documentation only — not functional — but should be accurate.)

### Go Dependencies

| Dependency | Type | ARM64 Status |
|-----------|------|-------------|
| `mattn/go-sqlite3` | CGO (C library) | Well-tested on ARM64 Linux (standard for ARM servers) |
| `gorilla/websocket` | Pure Go | No arch concerns |
| `labstack/echo/v4` | Pure Go | No arch concerns |
| `google.golang.org/genai` (Gemini) | Pure Go (gRPC) | No arch concerns |
| `starfederation/datastar-go` | Pure Go | No arch concerns |

No blockers. The only CGO dependency (`go-sqlite3`) is well-tested on ARM64.

### Bevy WASM Builds from ARM64

- WASM code generation is architecture-independent — `rustc` produces identical WASM whether the host is ARM64 or x86_64
- `naga` (Bevy's shader translator) is pure Rust — no native `shaderc` dependency for WASM builds
- Release builds peak at 4-8 GB RAM — the 16 GB VM allocation handles this with headroom
- `mold` linker (recommended above) reduces both link time and memory during release builds
- No reported ARM64-specific Bevy WASM compilation failures

### Tailscale + UTM Networking

- **NAT mode**: QEMU user-mode NAT (UTM "Emulated VLAN") behaves as a standard cone NAT. Tailscale traverses it easily. Direct WireGuard connections expected.
- **Tailscale SSH**: Runs entirely inside `tailscaled` userspace daemon — independent of VM network stack, iptables, and UFW. No VM-specific concerns.
- **Tailscale Serve**: Reverse proxy runs inside `tailscaled`, forwards to `localhost:8080`. NAT layer is uninvolved.
- **Performance overhead**: QEMU adds ~29 microseconds of virtio latency. Tailscale adds ~1-3ms for direct connections. Negligible for a web application.

### Docker in the VM

Docker Engine (not Docker Desktop) runs natively on the Linux kernel inside the VM:
```
macOS Hypervisor.framework → QEMU (ARM64 Linux) → Linux kernel → Docker (namespaces + cgroups)
```
No nested virtualization. Docker Engine on ARM64 Ubuntu is a production-grade configuration (same setup as AWS Graviton instances). Install via the official Docker apt repo — `dpkg --print-architecture` auto-detects `arm64`.

### Claude Code CLI

The native installer (`curl -fsSL https://claude.ai/install.sh | bash`) downloads a prebuilt `linux-arm64` binary. The install script auto-detects `aarch64` via `uname -m`. Known issues are limited to non-standard environments (Android/Termux proot) — standard Ubuntu 24.04 is fully supported.

## VM Backup & Migration

UTM has **no built-in snapshot UI** (tracked as future feature, UTM issue #5484). Use a multi-layered backup strategy.

### Layer 1: Application-Level Backups (highest priority)

Already covered by Phase 1 step 12 (daily SQLite `.backup` cron). The `sqlite3 .backup` command produces a consistent snapshot even while the database is in use. Git version control covers all code and configuration.

### Layer 2: qemu-img Snapshots (fast rollback)

Internal snapshots stored inside the qcow2 file. Instant to create, instant to restore. Use before risky changes (OS upgrades, Docker upgrades, bootstrap re-runs).

```bash
# Install qemu tools on macOS host
brew install qemu

DISK=~/Library/Containers/com.utmapp.UTM/Data/Documents/MyVM.utm/Data/DiskImage.qcow2

# VM MUST be stopped for all snapshot commands
qemu-img snapshot -c "before-bootstrap" "$DISK"    # Create snapshot
qemu-img snapshot -l "$DISK"                        # List snapshots
qemu-img snapshot -a "before-bootstrap" "$DISK"     # Restore (instant rollback)
qemu-img snapshot -d "before-bootstrap" "$DISK"     # Delete snapshot
```

### Layer 3: borgbackup for Incremental Full-VM Backups (daily)

borgbackup uses content-defined chunking — only changed chunks of the qcow2 are stored in subsequent backups. Typical dedup ratio: 60-80% savings (a 50 GB disk image may only transfer 1-5 GB of deltas per day).

```bash
# Install on macOS host
brew install borgbackup

# Initialize repo (external drive, NAS, etc.)
borg init --encryption=repokey /Volumes/Backup/utm-borg-repo

# Daily backup — VM must be stopped
borg create --progress --stats --compression zstd,3 \
  /Volumes/Backup/utm-borg-repo::{now:%Y-%m-%d} \
  ~/Library/Containers/com.utmapp.UTM/Data/Documents/MyVM.utm

# Prune old backups (keep 7 daily, 4 weekly, 6 monthly)
borg prune --keep-daily=7 --keep-weekly=4 --keep-monthly=6 \
  /Volumes/Backup/utm-borg-repo

# Restore a specific backup
borg extract /Volumes/Backup/utm-borg-repo::2026-02-13
```

Automate with `utmctl stop` + `borg create` + `utmctl start` in a launchd plist or cron job.

### Layer 4: Exclude from Time Machine

Time Machine re-copies the entire qcow2 file on every backup because any VM write marks the whole file as changed. **Exclude the UTM directory:**
```
System Settings > General > Time Machine > Options > Exclude:
  ~/Library/Containers/com.utmapp.UTM/Data/Documents/
```

### Layer 5: qcow2 Maintenance (monthly)

qcow2 files grow monotonically — deleted guest data doesn't reclaim host space. Compact monthly:

```bash
# Inside the guest VM — zero out free space
sudo fstrim -av

# On the macOS host (VM must be stopped)
DISK=~/Library/Containers/com.utmapp.UTM/Data/Documents/MyVM.utm/Data/DiskImage.qcow2
qemu-img convert -O qcow2 "$DISK" "${DISK}.compacted"
mv "${DISK}.compacted" "$DISK"
```

### Cloud Migration

The qcow2 image is portable to ARM64 cloud providers:

**Oracle Cloud (free ARM64 tier — up to 4 OCPU / 24 GB RAM):**
1. Install `cloud-init` inside the guest (`sudo apt install cloud-init`)
2. Ensure UEFI boot (already the case for UTM ARM64 VMs)
3. Run `fstrim -av` and compact the qcow2
4. Upload to Object Storage: `oci os object put -bn my-bucket --file DiskImage.qcow2`
5. Import as custom image (set type to QCOW2, firmware to UEFI_64)
6. Launch with `VM.Standard.A1.Flex` shape

**Hetzner Cloud (CAX ARM64 Ampere):**
1. Convert: `qemu-img convert -f qcow2 -O raw DiskImage.qcow2 DiskImage.raw`
2. Compress: `bzip2 DiskImage.raw`
3. Upload via `hcloud-upload-image` tool with `--architecture arm`

**Preparation checklist (both providers):** Install `cloud-init`, ensure `openssh-server` is installed, remove hardcoded network configs (let cloud-init handle DHCP), ensure kernel supports virtio devices (standard Ubuntu 24.04 does).

### Recommended Backup Frequency

| What | Frequency | Method |
|------|-----------|--------|
| SQLite database | Every 4 hours | Cron inside VM (`.backup` command) — Phase 1 step 12 |
| qemu-img snapshot | Before risky changes | Manual from macOS host |
| Full VM (borgbackup) | Daily | Automated script, VM stopped during backup |
| Compact qcow2 | Monthly | `fstrim -av` inside guest + `qemu-img convert` on host |
| Code / config | On change | Git (already version controlled) |

## Architecture

```
Fresh Ubuntu (VM or VPS)
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

Docker bypasses UFW by default via its own iptables chains. The DOCKER-USER rules in `/etc/ufw/after.rules` fix this — they DROP all traffic from the public interface (auto-detected via `ip route show default`) to Docker containers, and only RETURN (allow) traffic from the Tailscale interface (`tailscale0`). Expected interface names: `enp0s1` (UTM/QEMU ARM64), `eth0` or `ens3` (cloud VPS). This means all Docker-exposed ports (8080, 9001-9100) are reachable only from tailnet members, without changing anything in `docker-compose.yml`.

On a local VM, these rules are defense-in-depth — the VM's NAT already isolates it from the host network. On a cloud VPS, they're critical. Either way, the bootstrap applies them uniformly.

On local dev (macOS host, no VM), there's no UFW — all ports are reachable on localhost as usual.

## Files to Create/Modify

### Phase 1: VPS Bootstrap Script

**New file: `scripts/vps-bootstrap.sh`**

Interactive script run from the cloned repo (NOT piped from the internet). Must be run as root.

1. Creates `deploy` user with sudo (prompts for password)
2. Installs Tailscale (`curl -fsSL https://tailscale.com/install.sh | sh`)
3. Runs `tailscale up` (interactive — opens auth URL)
4. Enables Tailscale SSH (`tailscale set --ssh`)
5. Installs Docker Engine — not Docker Desktop — via official Docker apt repo (`dpkg --print-architecture` auto-detects `arm64`)
6. Configures UFW:
   ```bash
   ufw default deny incoming
   ufw default allow outgoing
   ufw allow in on tailscale0
   ufw enable
   ```
7. Auto-detects the public network interface and adds DOCKER-USER rules to `/etc/ufw/after.rules`.
   Expected values: `enp0s1` (UTM/QEMU ARM64 — predictable naming: `en` ethernet + `p0` PCI bus 0 + `s1` slot 1), `eth0` or `ens3` (cloud VPS):
   ```bash
   # Detect the interface with the default route (public-facing)
   PUBLIC_IF=$(ip route show default | awk '{print $5}' | head -1)
   if [ -z "$PUBLIC_IF" ]; then
       echo "ERROR: Cannot detect public network interface."
       exit 1
   fi
   echo "Detected public interface: $PUBLIC_IF"
   ```
   Then writes to `/etc/ufw/after.rules` (with idempotent check — see step note below):
   ```
   *filter
   :DOCKER-USER - [0:0]
   -A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
   -A DOCKER-USER -i tailscale0 -j RETURN
   -A DOCKER-USER -i $PUBLIC_IF -j DROP
   COMMIT
   ```
   Post-install verification:
   ```bash
   iptables -L DOCKER-USER -v -n | grep DROP | grep "$PUBLIC_IF" || echo "WARNING: DOCKER-USER rules not active!"
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
12. Sets up daily SQLite backup cron. The bootstrap script writes the actual repo path to `/etc/creative-mode.conf` so the cron script discovers it dynamically:
    ```bash
    # Written by bootstrap: /etc/creative-mode.conf
    CREATIVE_MODE_DIR=/home/deploy/creative-mode

    # /etc/cron.daily/backup-creative-mode
    . /etc/creative-mode.conf
    DB_PATH="${CREATIVE_MODE_DIR}/data/creative-mode.db"
    BACKUP_DIR="${CREATIVE_MODE_DIR%/*}/backups"
    mkdir -p "$BACKUP_DIR"
    sqlite3 "$DB_PATH" ".backup ${BACKUP_DIR}/creative-mode-$(date +%Y%m%d).db"
    # Keep last 7 days
    find "$BACKUP_DIR" -name '*.db' -mtime +7 -delete
    ```
13. Creates systemd unit for auto-start after reboot (since we use `restart: on-failure` instead of `unless-stopped`):
    ```ini
    # /etc/systemd/system/creative-mode.service
    [Unit]
    Description=Creative Mode Harness
    After=docker.service
    Requires=docker.service

    [Service]
    Type=oneshot
    RemainAfterExit=yes
    User=deploy
    WorkingDirectory=/home/deploy/creative-mode/harness
    ExecStart=/usr/bin/docker compose up -d
    ExecStop=/usr/bin/docker compose down

    [Install]
    WantedBy=multi-user.target
    ```
    Then `systemctl enable creative-mode.service`.
14. Prints summary of what was done + next steps (including GitHub OAuth callback URL reminder, detected interface name)

The script should be idempotent — safe to re-run. Each step checks if already done before modifying:
- `id -u deploy 2>/dev/null || adduser deploy` (skip if user exists)
- `grep -q DOCKER-USER /etc/ufw/after.rules || cat >> ...` (skip if rules exist)
- `ufw --force enable` (non-interactive)
- `tailscale up` is a no-op if already connected
- Add a `--check` / `--dry-run` flag that reports what would be changed without modifying anything

### Phase 2: Base Docker Compose Updates

**Modified file: `harness/docker-compose.yml`**

Add `restart: on-failure` to the existing service. This only restarts on non-zero exit codes (crashes), NOT after reboot or explicit stop. This avoids the macOS Docker Desktop annoyance where `unless-stopped` auto-starts the container on every boot. On the VPS, the bootstrap script creates a systemd unit for auto-start after reboot:

```yaml
services:
  harness:
    build:
      context: .
      dockerfile: Dockerfile
    restart: on-failure         # NEW: auto-restart on crashes (VPS reboot handled by systemd)
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
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
```

No port changes, no `DEV_MODE` override. Network security comes from the OS level, not Docker.

**Dockerfile EXPOSE fix:** The current Dockerfile only has `EXPOSE 8081` (single port). Update to document the full ranges to match docker-compose: `EXPOSE 8080`, `EXPOSE 8081-8180`, `EXPOSE 9001-9100`. This is documentation-only but should be accurate.

**Note on port range:** Docker compose exposes `9001-9100` (100 ports), but `ports.go` defines `gameServerMaxPort = 9999`. If >100 game servers run simultaneously, ports 9101-9999 would be allocated but unreachable. This is fine — 100 concurrent game servers is well beyond current needs. Add a comment in `ports.go` documenting this limitation.

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

**5b. Request body size limits** — `harness/internal/server/server.go`

Route-specific limits to avoid blocking asset uploads:
```go
// Global default: 1MB (covers chat, prompts, API calls)
e.Use(middleware.BodyLimit("1M"))

// Override for asset upload: 10MB
approved.POST("/api/assets/upload", s.handleAssetUpload, middleware.BodyLimit("10M"))
```

Also add `io.LimitReader` in `handleAssetUpload` as defense-in-depth against disk exhaustion:
```go
limited := io.LimitReader(file, 10<<20) // 10MB max
if _, copyErr := io.Copy(out, limited); copyErr != nil { ... }
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

Add strict origin check at the top of the message handler. Since Phase 6 proxies trunk serve through the harness (same-origin), a strict check is safe:
```js
window.addEventListener('message', function(event) {
    if (event.origin !== window.location.origin) return;
    if (!event.data || !event.data.type) return;
    // ... rest of handler
});
```

**Note:** Phase 5 is implemented before Phase 6 in the implementation order. If 5f is deployed before 6a (trunk proxy), the strict check will break trunk serve postMessage (different ports). Options: (a) implement 5f and 6a together, or (b) use a localhost-aware check initially and simplify to strict after 6a ships. Recommended: implement together.

**5g. HTTP rate limiting** — `harness/internal/server/server.go`

Add Echo rate limiter middleware for sensitive endpoints:
```go
rateLimiter := middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(
    rate.Limit(20), // 20 requests/second
))
```

Apply to chat and prompt routes. The existing application-level prompt rate limiter (30s cooldown per user) stays — this is an additional HTTP-level guard against abuse.

### Phase 6: Reverse Proxies (Trunk Serve + Game Server WebSocket)

**Why this is needed:** On the VPS (and for consistency, everywhere), the browser cannot reach `localhost:{trunkPort}` or `localhost:{gamePort}` directly — those ports resolve to the user's local machine, not the server. Even with port forwarding, HTTPS pages (via Tailscale Serve) block mixed-content `http://` iframes and `ws://` connections. The solution is to proxy all game traffic through the harness on port 8080.

**Design principle:** Always proxy, everywhere (local + VPS). One code path, everything same-origin. The iframe src changes from `http://localhost:{trunkPort}/` to `/world/{worldID}/trunk/`, eliminating cross-origin complexity entirely.

#### 6a. Trunk Serve Reverse Proxy (HTTP + WebSocket)

**New handler in `harness/internal/server/server.go`:**

Add route in `registerWorldRoutes`:
```go
w.Any("/:worldID/trunk/*", s.handleTrunkProxy)
```

The handler:
1. Looks up the user's current checkpoint via `GetUserPosition`
2. Gets the trunk port from `GameServerManager.GetServer`
3. Uses `httputil.ReverseProxy` to forward HTTP requests to `http://localhost:{trunkPort}`
4. For WebSocket upgrade requests (trunk's hot-reload), detects `Upgrade: websocket` header and uses `gorilla/websocket` bidirectional proxy instead

Note: `httputil.ReverseProxy` handles normal HTTP fine but does NOT handle WebSocket upgrades. The handler must check for the `Upgrade` header and branch:
- Normal HTTP → `httputil.ReverseProxy` (strip `/world/{worldID}/trunk` prefix)
- WebSocket → `gorilla/websocket` Upgrader+Dialer + `io.Copy` in goroutines

**Update `harness/views/world/world.templ`:**

Change iframe src from direct trunk URL to proxied path:
```go
// Before (cross-origin):
src={ fmt.Sprintf("http://localhost:%d/", trunkPort) }

// After (same-origin):
src={ fmt.Sprintf("/world/%s/trunk/", worldID) }
```

For 3D worlds, the `server_port` query param is no longer needed in the iframe URL — the game client connects via the WS proxy route instead (see 6b).

#### 6b. Game Server WebSocket Proxy

Add route in `registerWorldRoutes`:
```go
w.GET("/:worldID/ws", s.handleGameWebSocket)
```

The handler:
1. Looks up the user's current checkpoint via `GetUserPosition`
2. Gets the game server port from `GameServerManager.GetServer`
3. Upgrades to WebSocket using `gorilla/websocket.Upgrader` (accept browser connection)
4. Dials backend game server using `gorilla/websocket.Dialer` → `ws://localhost:{port}`
5. Bidirectional byte-level forwarding via `io.Copy` in two goroutines (~30 lines of Go)

`gorilla/websocket` is already an indirect dependency (`go.mod:30`, `v1.5.3`) — promote to direct dependency. The Lightyear protocol uses binary WebSocket frames; the proxy must be transparent (no frame inspection).

**Update WASM client** (`templates/3d/client/src/main.rs`):
- Currently reads `server_port` from URL query param and connects to `ws://localhost:{port}`
- Change to connect to `wss://{window.location.host}/world/{worldID}/ws` (relative URL, same-origin)
- The harness already passes `worldID` context to the iframe via the proxied URL path

#### 6c. postMessage Origin Check Simplification

With the trunk proxy, the iframe is always same-origin. The Phase 5f localhost-aware origin check can be simplified to a strict check:
```js
window.addEventListener('message', function(event) {
    if (event.origin !== window.location.origin) return;
    if (!event.data || !event.data.type) return;
    // ... rest of handler
});
```

Update all `postMessage` calls in templates to use `window.location.origin` instead of `'*'`:
- `templates/2d/index.html` — `window.parent.postMessage({...}, window.location.origin)`
- `templates/3d/client/index.html` — same
- `templates/2d/src/bridge.rs` — `parent.post_message(&obj, &origin)` (read from `window.location.origin`)
- `harness/internal/server/debug.go` — `frame.contentWindow.postMessage({...}, window.location.origin)`

#### 6d. Update harness CLAUDE.md

The "Iframe + Datastar: Keyboard Events and Focus" section should be updated:
- Remove references to "cross-origin iframe" — the iframe is now same-origin via the trunk proxy
- The postMessage bridge pattern is still needed for keyboard events (iframe focus boundary is about DOM focus, not origin)
- Update the explanation: the iframe steals focus regardless of same/cross-origin — this is a DOM focus issue, not a security boundary issue
- Remove docker-compose trunk port exposure (`8081-8180`) from the exposed ports list — traffic goes through the harness proxy now

#### Impact on docker-compose.yml

With trunk traffic proxied through the harness, the trunk port range (`8081-8180`) no longer needs to be exposed to the host. Only the harness port and game server ports need exposure:
```yaml
ports:
  - "8080:8080"           # harness (serves everything: pages, trunk proxy, WS proxy)
  - "9001-9100:9001-9100" # game server ports (direct access for local dev convenience)
```

Actually, with the game server WS proxy too, game server ports don't need host exposure either. But keeping them exposed is useful for debugging (direct `wscat` to game server). The trunk ports can safely be removed since all trunk access goes through the harness.

**Note on 3D games:** This phase covers both 2D (trunk proxy only) and 3D (trunk proxy + WS proxy). 2D games are client-side only and don't need the WS proxy.

### Phase 7: Post-Bootstrap Verification Script

**New file: `scripts/vps-verify.sh`**

Run after bootstrap and periodically (can be added to cron) to verify all security assumptions hold:

```bash
#!/usr/bin/env bash
set -euo pipefail

PASS=0; FAIL=0
check() { if eval "$2"; then echo "  PASS: $1"; ((PASS++)); else echo "  FAIL: $1"; ((FAIL++)); fi }

echo "=== Creative Mode VPS Verification ==="

# Network security
check "UFW is active" "ufw status | grep -q 'Status: active'"
check "DOCKER-USER rules exist" "iptables -L DOCKER-USER -n 2>/dev/null | grep -q DROP"
check "tailscale0 allowed in DOCKER-USER" "iptables -L DOCKER-USER -n 2>/dev/null | grep -q tailscale0"

# Tailscale
check "Tailscale is running" "tailscale status --peers=false >/dev/null 2>&1"
check "Tailscale SSH enabled" "tailscale debug prefs 2>/dev/null | grep -q '\"RunSSH\":true'"

# Docker
check "Docker is running" "docker info >/dev/null 2>&1"
check "Harness container running" "docker compose -f /home/deploy/creative-mode/harness/docker-compose.yml ps --format json 2>/dev/null | grep -q running"

# sshd
check "sshd not on port 22" "! ss -tlnp | grep ':22 '"
check "Root login disabled" "grep -q 'PermitRootLogin no' /etc/ssh/sshd_config"

# Summary
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "All checks passed." || echo "ACTION REQUIRED: Fix the failing checks above."
exit "$FAIL"
```

## Implementation Order

1. **Phase 1**: `scripts/vps-bootstrap.sh` — VM/VPS setup script (auto-detect interface, systemd unit, idempotency). Set up the UTM VM first so all subsequent changes can be tested on it.
2. **Phase 3**: `.env.example` — needed during VM setup to create `.env`
3. **Phase 7**: `scripts/vps-verify.sh` — validate bootstrap worked before proceeding
4. **Phase 2**: Add `restart: on-failure` + healthcheck to base compose
5. **Phase 5**: Application hardening (Go code + JS) — security headers, body limits, timeouts, cookie fixes, postMessage, rate limiting
6. **Phase 4**: Justfile helpers (`redeploy`, `status`)
7. **Phase 6**: Reverse proxies (trunk serve + game WS) — makes everything same-origin, enables VPS access. **Phase 5f (postMessage) and Phase 6a (trunk proxy) must ship together.**

VM-first approach: set up the UTM VM and verify the bootstrap before implementing code changes. This ensures every change can be tested on the actual deployment target. Phases 2-6 are independent of each other but all depend on the VM being available for testing.

## Deployment Workflow (End State)

### First-time setup — Local VM (UTM):
```bash
# 0. Install qemu tools on macOS host (for VM snapshots)
brew install qemu borgbackup

# 1. Create Ubuntu 24.04 ARM64 VM in UTM
#    - 16 GB RAM, 4-8 CPU cores, 80 GB disk (qcow2 sparse)
#    - Network: NAT (default, "Emulated VLAN")
#    - Install Ubuntu Server (minimal)
#    - Expected network interface: enp0s1

# 2. BEFORE any customization — create a clean snapshot from macOS host:
#    qemu-img snapshot -c "fresh-install" \
#      ~/Library/Containers/com.utmapp.UTM/Data/Documents/MyVM.utm/Data/DiskImage.qcow2

# 3. SSH into VM (UTM gives you a console, or use the VM's IP)
# 4. Install git and clone the repo
sudo apt update && sudo apt install -y git
git clone https://github.com/{user}/creative-mode.git /opt/creative-mode

# 5. Run the bootstrap script from the cloned repo
sudo bash /opt/creative-mode/scripts/vps-bootstrap.sh

# 6. Switch to deploy user, move repo to home
su - deploy
mv /opt/creative-mode ~/creative-mode
cd ~/creative-mode/harness

# 7. Create .env from template
cp .env.example .env
# Edit .env: add GitHub OAuth creds, ANTHROPIC_API_KEY,
# HARNESS_URL=https://{vm-name}.{tailnet}.ts.net,
# CM_HOOK_SECRET=$(openssl rand -hex 32)

# 8. Start the harness (same command as local dev)
just up

# 9. Set up Tailscale Serve (one-time, persists across reboots)
sudo tailscale serve https / http://localhost:8080

# 10. Verify
sudo bash ~/creative-mode/scripts/vps-verify.sh

# 11. Exclude UTM from Time Machine:
#     System Settings > General > Time Machine > Options > Exclude:
#       ~/Library/Containers/com.utmapp.UTM/Data/Documents/

# 12. Set up borgbackup repo for daily incremental backups:
#     borg init --encryption=repokey /Volumes/Backup/utm-borg-repo
```

### First-time setup — Cloud VPS:
Same steps, just SSH in instead of using UTM console.

### Updating:
```bash
cd ~/creative-mode/harness
just redeploy   # git pull + docker compose up --build
```

### What's different from local dev:
| | macOS host (local dev) | VM / VPS (shared dev) |
|---|---|---|
| Start command | `just up` | `just up` |
| Docker image | Same | Same |
| `DEV_MODE` | `true` | `true` |
| Network security | None (localhost) | Tailscale + UFW + DOCKER-USER |
| HTTPS | No | Tailscale Serve (`*.ts.net`) |
| `.env` contents | GitHub OAuth + API key | + `HARNESS_URL` + `CM_HOOK_SECRET` |
| Auto-restart | On crash only | On crash (`on-failure`) + reboot (systemd) |
| Uptime | When running locally | VM: when laptop is on / VPS: always |
| Backups | Git only | SQLite cron + qemu-img snapshots + borgbackup |
| Network interface | N/A (localhost) | `enp0s1` (UTM) or `eth0`/`ens3` (cloud) — auto-detected |

## Resolved Questions

1. **GitHub OAuth callback URL** — Update in GitHub OAuth App settings to `https://{machine}.{tailnet}.ts.net/auth/github/callback`. Bootstrap script prints a reminder.
2. **ANTHROPIC_API_KEY management** — Goes in `.env` (already gitignored). Simpler than host env, and `.env` is the single source for all secrets.
3. **Tailscale Serve on boot** — Persists across reboots by default. Verified.
4. **Docker auto-restart** — `restart: on-failure` for crash recovery (no macOS boot annoyance). VPS auto-start via systemd unit created by bootstrap script.
5. **Body limit vs asset uploads** — Route-specific limits: 1M global default, 10M override on `/api/assets/upload`. Plus `io.LimitReader` in the upload handler as defense-in-depth.
6. **Network interface detection** — Auto-detect via `ip route show default` instead of hardcoding `eth0`. Bootstrap prints detected interface for user verification.
7. **postMessage origin check** — Strict same-origin check. Trunk proxy (Phase 6a) makes the iframe same-origin everywhere, so no localhost-aware hack needed.
8. **Trunk serve on VPS** — Always proxy through harness (Phase 6a), even on local dev. One code path, everything same-origin. Eliminates cross-origin iframe complexity. Docker trunk ports (8081-8180) no longer need host exposure.
9. **Hosting** — Start with a local Ubuntu 24.04 ARM64 VM via UTM on macOS (64 GB host RAM, allocate 16 GB to VM). Tailscale inside the VM makes it accessible to tailnet members. Can migrate to a cloud VPS later for always-on hosting. Bootstrap script works identically on both.
10. **ARM64 compatibility** — All Dockerfile layers, Go dependencies, Rust/WASM toolchain, Tailscale, and Docker verified for ARM64. See [ARM64 Compatibility Notes](#arm64-compatibility-notes). Expected UTM interface name: `enp0s1` (auto-detected by bootstrap).
11. **VM backups** — Multi-layered: SQLite `.backup` cron (Phase 1), qemu-img internal snapshots for rollback, borgbackup for daily incremental full-VM backups, Time Machine exclusion. See [VM Backup & Migration](#vm-backup--migration).

## Future Work (Not In This Plan)

- **Release deployment pipeline** — Separate server for serving production builds to end users. Different domain, Caddy/nginx, public ports, `DEV_MODE=false`, stripped Docker image. Entirely separate concern from the dev VPS.
- **Dockerfile `USER` directive** — Run container as non-root. Important for defense in depth but requires adjusting file permissions for `data/`, tmux sessions, and Cargo/Go caches. Tracked separately to avoid scope creep in this plan.
- **Tailscale ACLs** — Group/tag-based access control for multi-user tailnet. Not needed until more people join the tailnet.
- **Disk space monitoring** — Rust builds and WASM artifacts can fill a small VPS. Add alerts before it becomes a problem.
- **cloud-init pre-installation** — Install `cloud-init` in the VM early to keep the cloud migration path open (Oracle Cloud free ARM64 tier, Hetzner CAX). Not needed until migration, but easier to do proactively.
- **Automated VM backup script** — launchd plist on macOS host that runs `utmctl stop` + `borg create` + `utmctl start` on a daily schedule. Manual for now.
