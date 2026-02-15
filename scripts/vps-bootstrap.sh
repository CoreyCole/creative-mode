#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Creative Mode — VPS Bootstrap Script
# =============================================================================
#
# Secures and configures a fresh Ubuntu 24.04 instance (VM or cloud VPS) for
# running the Creative Mode harness. Pipable from curl on a fresh instance.
#
# What this script does:
#   0.  Installs prerequisites (git, curl, sqlite3, jq, openssl, zsh, fzf)
#   1.  Creates a 'deploy' user with sudo access
#   1b. Clones the repository to ~deploy/creative-mode
#   2.  Installs Tailscale (private networking)
#   3.  Connects to your Tailscale network (interactive)
#   4.  Enables Tailscale SSH (so you can SSH over Tailscale)
#   5.  Installs Docker Engine (container runtime)
#   6.  Configures UFW firewall (blocks all public traffic)
#   7.  Adds DOCKER-USER iptables rules (prevents Docker from bypassing the firewall)
#   8.  Creates Docker daemon config (security + logging)
#   9.  Installs Fail2Ban (blocks brute-force login attempts)
#   10. Locks down SSH (Tailscale-only, non-standard port, no passwords)
#   11. Adds deploy user to docker group
#   12. Sets up daily SQLite backup cron job
#   13. Creates systemd service for auto-start on reboot (native binary)
#   14. Installs Nix (daemon mode)
#   15. Enables Nix flakes
#   16. Installs direnv
#   16b. Installs Rust toolchain (system-wide)
#   16c. Installs cargo tools (trunk, cargo-watch, wasm-bindgen-cli)
#   16d. Installs Go tools (templ)
#   17. Installs oh-my-zsh + configures zsh as login shell
#   18. Activates dev environment (direnv allow)
#   19. Creates .env file (interactive prompts for secrets)
#   20. Sets up Tailscale Serve (HTTPS)
#   20b. Installs Claude Code CLI
#   21. Starts the server via systemd
#   22. Prints summary
#
# Usage:
#   curl -fsSL <raw-url>/scripts/vps-bootstrap.sh | sudo bash
#   sudo bash scripts/vps-bootstrap.sh              # from cloned repo
#   sudo bash scripts/vps-bootstrap.sh --check      # dry run
#
# Idempotent: safe to re-run. Each step checks if already done before modifying.
# =============================================================================

# ---------------------------------------------------------------------------
# Color output helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

section() { echo -e "\n${BLUE}${BOLD}=== $1 ===${NC}"; }
ok()      { echo -e "  ${GREEN}DONE${NC}  $1"; }
skip()    { echo -e "  ${YELLOW}SKIP${NC}  $1 (already configured)"; }
fail()    { echo -e "  ${RED}FAIL${NC}  $1"; }
info()    { echo -e "  ${BOLD}INFO${NC}  $1"; }

# ---------------------------------------------------------------------------
# Parse flags
# ---------------------------------------------------------------------------
REPO_URL="https://github.com/CoreyCole/creative-mode.git"
DRY_RUN=false
for arg in "$@"; do
    case "$arg" in
        --check|--dry-run)
            DRY_RUN=true
            ;;
        https://*|git@*)
            REPO_URL="$arg"
            ;;
        *)
            echo "Unknown argument: $arg"
            echo "Usage: curl -fsSL <url> | sudo bash [--check]"
            exit 1
            ;;
    esac
done

if $DRY_RUN; then
    echo -e "${YELLOW}${BOLD}DRY RUN — showing what would change without modifying anything${NC}"
fi

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
    echo "This script must be run as root."
    exit 1
fi

if [ ! -f /etc/os-release ] || ! grep -q 'Ubuntu' /etc/os-release; then
    echo "WARNING: This script is designed for Ubuntu 24.04. Proceed with caution."
fi

DEPLOY_HOME="/home/deploy"
CREATIVE_MODE_DIR="$DEPLOY_HOME/creative-mode"
info "Repository URL: $REPO_URL"
info "Install path: $CREATIVE_MODE_DIR"

# ============================================================================
# Step 0: Install prerequisites
# ============================================================================
# A fresh Ubuntu 24.04 minimal image may not have git or curl. Install them
# now so the rest of the script can clone the repo and fetch install scripts.
# sqlite3 is needed for the daily backup cron job (Step 12).
# jq is needed for parsing Tailscale DNS name (Step 19).
# openssl is needed for generating CM_HOOK_SECRET (Step 19).
# ============================================================================
section "Step 0: Install prerequisites"

if command -v git &>/dev/null && command -v curl &>/dev/null && command -v sqlite3 &>/dev/null && command -v jq &>/dev/null && command -v openssl &>/dev/null && command -v zsh &>/dev/null && command -v fzf &>/dev/null; then
    skip "Prerequisites already installed (git, curl, sqlite3, jq, openssl, zsh, fzf)"
else
    if $DRY_RUN; then
        info "Would apt-get update and install git, curl, sqlite3, jq, openssl, zsh, fzf"
    else
        apt-get update
        apt-get install -y git curl sqlite3 jq openssl zsh fzf
        ok "Installed prerequisites (git, curl, sqlite3, jq, openssl, zsh, fzf)"
    fi
fi

# ============================================================================
# Step 1: Create 'deploy' user
# ============================================================================
# The harness runs as this user, not root. Principle of least privilege —
# if the application is compromised, the attacker gets limited permissions.
# ============================================================================
section "Step 1: Create 'deploy' user"

if id -u deploy &>/dev/null; then
    skip "User 'deploy' already exists"
else
    if $DRY_RUN; then
        info "Would create user 'deploy' and add to sudo group"
    else
        adduser --disabled-password --gecos "" deploy
        usermod -aG sudo deploy
        ok "Created user 'deploy'"
    fi
fi

# Passwordless sudo — deploy has no password set (--disabled-password)
# so normal sudo would be unusable without this. Idempotent check
# ensures this works even if the user was created in a previous run.
if [ -f /etc/sudoers.d/deploy ]; then
    skip "Passwordless sudo already configured"
else
    if $DRY_RUN; then
        info "Would configure passwordless sudo for deploy"
    else
        echo "deploy ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/deploy
        chmod 440 /etc/sudoers.d/deploy
        ok "Configured passwordless sudo for deploy"
    fi
fi

# ============================================================================
# Step 1b: Clone repository
# ============================================================================
section "Clone repository"

if [ -d "$CREATIVE_MODE_DIR" ]; then
    skip "Repository already exists at $CREATIVE_MODE_DIR"
else
    if $DRY_RUN; then
        info "Would clone $REPO_URL to $CREATIVE_MODE_DIR"
    else
        sudo -u deploy git clone "$REPO_URL" "$CREATIVE_MODE_DIR"
        ok "Cloned to $CREATIVE_MODE_DIR"
    fi
fi

# ============================================================================
# Step 2: Install Tailscale
# ============================================================================
# Tailscale creates an encrypted WireGuard tunnel between your devices.
# Only people you invite to your Tailscale network (tailnet) can reach this
# server. The public internet cannot see it at all.
# ============================================================================
section "Step 2: Install Tailscale"

if command -v tailscale &>/dev/null; then
    skip "Tailscale is already installed"
else
    if $DRY_RUN; then
        info "Would install Tailscale via official install script"
    else
        curl -fsSL https://tailscale.com/install.sh | sh
        ok "Installed Tailscale"
    fi
fi

# ============================================================================
# Step 3: Connect to Tailscale network
# ============================================================================
# This is interactive — it will print a URL you need to open in your browser
# to authorize this machine on your Tailscale account. After authorization,
# this machine gets a private Tailscale IP (100.x.x.x) and can talk to your
# other Tailscale devices.
# ============================================================================
section "Step 3: Connect to Tailscale"

if tailscale status --peers=false &>/dev/null; then
    skip "Tailscale is already connected"
else
    if $DRY_RUN; then
        info "Would run 'tailscale up' (interactive — opens auth URL)"
    else
        info "This step is interactive. Follow the URL to authorize this machine."
        tailscale up
        ok "Connected to Tailscale"
    fi
fi

# ============================================================================
# Step 4: Enable Tailscale SSH
# ============================================================================
# Tailscale SSH lets you SSH into this machine over the Tailscale tunnel
# without managing SSH keys yourself. Tailscale handles authentication using
# your Tailscale account identity. This is simpler and more secure than
# traditional SSH key management.
# ============================================================================
section "Step 4: Enable Tailscale SSH"

if tailscale debug prefs 2>/dev/null | grep -q '"RunSSH":true'; then
    skip "Tailscale SSH is already enabled"
else
    if $DRY_RUN; then
        info "Would enable Tailscale SSH"
    else
        tailscale set --ssh
        ok "Enabled Tailscale SSH"
    fi
fi

# ============================================================================
# Step 5: Install Docker Engine
# ============================================================================
# Docker runs the game server inside an isolated container. This means the
# game has its own filesystem, network, and processes — separate from the
# rest of the server. If something goes wrong inside Docker, it can't affect
# the host system.
#
# We install Docker Engine (the server daemon), NOT Docker Desktop (the GUI
# app). Docker Engine runs natively on Linux with near-zero overhead.
#
# The install auto-detects your CPU architecture (arm64 for UTM/QEMU VMs,
# amd64 for most cloud VPS) via dpkg --print-architecture.
# ============================================================================
section "Step 5: Install Docker Engine"

if command -v docker &>/dev/null; then
    skip "Docker is already installed"
else
    if $DRY_RUN; then
        info "Would install Docker Engine from official Docker apt repository"
    else
        # Install prerequisites for adding apt repositories over HTTPS
        apt-get update
        apt-get install -y ca-certificates curl

        # Add Docker's official GPG key (verifies packages are from Docker)
        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc

        # Add the Docker apt repository for our architecture
        ARCH=$(dpkg --print-architecture)
        CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
        echo "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $CODENAME stable" \
            > /etc/apt/sources.list.d/docker.list

        # Install Docker Engine + CLI + plugins
        apt-get update
        apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

        # Start and enable Docker so it runs on boot
        systemctl enable --now docker

        ok "Installed Docker Engine ($ARCH)"
    fi
fi

# ============================================================================
# Step 6: Configure UFW firewall
# ============================================================================
# UFW (Uncomplicated Firewall) is like a bouncer at the door. It blocks all
# uninvited network traffic by default and only lets through connections from
# Tailscale (your private network).
#
# Rules:
#   - Deny all incoming traffic (nothing from the public internet gets in)
#   - Allow all outgoing traffic (the server can still reach the internet)
#   - Allow traffic on the tailscale0 interface (your private tunnel)
# ============================================================================
section "Step 6: Configure UFW firewall"

if ufw status | grep -q "Status: active"; then
    skip "UFW is already active"
else
    if $DRY_RUN; then
        info "Would configure UFW: deny incoming, allow outgoing, allow tailscale0"
    else
        ufw default deny incoming
        ufw default allow outgoing
        ufw allow in on tailscale0
        ufw --force enable
        ok "UFW enabled — only Tailscale traffic allowed"
    fi
fi

# ============================================================================
# Step 7: Add DOCKER-USER iptables rules
# ============================================================================
# Here's a gotcha: Docker bypasses UFW by default. Docker manages its own
# iptables rules, which means even with UFW blocking everything, Docker
# containers could still be reachable from the public internet.
#
# The fix: Docker provides a special chain called DOCKER-USER that runs
# BEFORE Docker's own rules. We add rules here that:
#   1. Allow established connections to continue (so responses work)
#   2. Allow traffic from Tailscale (your private network)
#   3. DROP everything else from the public network interface
#
# We auto-detect the public interface (the one with the default route).
# Common values: enp0s1 (UTM/QEMU ARM64), eth0 or ens3 (cloud VPS).
# ============================================================================
section "Step 7: DOCKER-USER iptables rules"

# Detect the public-facing network interface (the one with the default route)
PUBLIC_IF=$(ip route show default | awk '{print $5}' | head -1)
if [ -z "$PUBLIC_IF" ]; then
    fail "Cannot detect public network interface — no default route found"
    exit 1
fi
info "Detected public interface: $PUBLIC_IF"

if grep -q "DOCKER-USER" /etc/ufw/after.rules 2>/dev/null; then
    skip "DOCKER-USER rules already in /etc/ufw/after.rules"
else
    if $DRY_RUN; then
        info "Would append DOCKER-USER rules to /etc/ufw/after.rules"
        info "  - Allow established connections"
        info "  - Allow tailscale0"
        info "  - Drop traffic from $PUBLIC_IF"
    else
        # Append the DOCKER-USER rules to the end of after.rules
        cat >> /etc/ufw/after.rules << EOF

# Creative Mode: Prevent Docker from bypassing UFW.
# Without these rules, Docker containers are reachable from the public internet
# even though UFW blocks incoming traffic. The DOCKER-USER chain runs before
# Docker's own rules, so we can enforce our firewall policy here.
*filter
:DOCKER-USER - [0:0]
-A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
-A DOCKER-USER -i tailscale0 -j RETURN
-A DOCKER-USER -i $PUBLIC_IF -j DROP
COMMIT
EOF
        ok "Added DOCKER-USER rules (drop $PUBLIC_IF, allow tailscale0)"
    fi
fi

# Ensure rules are loaded (ufw reload is idempotent)
if ! $DRY_RUN && grep -q "DOCKER-USER" /etc/ufw/after.rules 2>/dev/null; then
    ufw reload
    if iptables -L DOCKER-USER -v -n 2>/dev/null | grep -q DROP; then
        ok "Verified: DOCKER-USER DROP rule is active"
    else
        info "DOCKER-USER chain not yet active (normal if Docker hasn't started a container yet)"
        info "Rules will take effect after the first 'docker compose up'"
    fi
fi

# ============================================================================
# Step 8: Create Docker daemon configuration
# ============================================================================
# These settings improve Docker's security and reliability:
#
#   live-restore: true     — Containers keep running even if the Docker daemon
#                            restarts (e.g., during a Docker update). Without
#                            this, updating Docker would kill all containers.
#
#   userland-proxy: false  — Disables Docker's userland proxy for port mapping.
#                            Uses iptables instead, which is faster and doesn't
#                            create extra processes.
#
#   no-new-privileges: true — Prevents processes inside containers from gaining
#                             additional Linux privileges (like setuid). Defense
#                             in depth against container escapes.
#
#   log-driver + log-opts  — Limits container log files to 10 MB each, keeps
#                            the last 3 files. Without this, logs grow forever
#                            and can fill the disk.
# ============================================================================
section "Step 8: Docker daemon configuration"

if [ -f /etc/docker/daemon.json ]; then
    skip "/etc/docker/daemon.json already exists"
else
    if $DRY_RUN; then
        info "Would create /etc/docker/daemon.json with security + logging settings"
    else
        mkdir -p /etc/docker
        cat > /etc/docker/daemon.json << 'EOF'
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
EOF

        # Restart Docker to pick up the new config
        systemctl restart docker

        ok "Created /etc/docker/daemon.json"
    fi
fi

# ============================================================================
# Step 9: Install Fail2Ban
# ============================================================================
# Fail2Ban watches log files for repeated failed login attempts (like someone
# trying to guess your password) and automatically blocks their IP address.
# It's an extra layer of defense — even though we lock down SSH in the next
# step, Fail2Ban catches brute-force attempts before they can do any damage.
# ============================================================================
section "Step 9: Install Fail2Ban"

if command -v fail2ban-client &>/dev/null; then
    skip "Fail2Ban is already installed"
else
    if $DRY_RUN; then
        info "Would install Fail2Ban via apt"
    else
        apt-get install -y fail2ban
        systemctl enable --now fail2ban
        ok "Installed and started Fail2Ban"
    fi
fi

# ============================================================================
# Step 10: Lock down SSH
# ============================================================================
# We make SSH much harder to attack by:
#
#   ListenAddress {tailscale_ip} — SSH only listens on the Tailscale IP, so
#     it's literally unreachable from the public internet.
#
#   Port 2222 — Move SSH off the default port 22. This alone stops most
#     automated scanners (they only try port 22).
#
#   PermitRootLogin no — Can't log in as root via SSH. Must log in as a
#     regular user and use sudo.
#
#   PasswordAuthentication no — Only SSH keys allowed (or Tailscale SSH).
#     No one can brute-force a password because passwords aren't accepted.
#
# IMPORTANT: Make sure Tailscale SSH (step 4) is working before this step,
# because this locks you out of regular SSH on the public interface.
# ============================================================================
section "Step 10: Lock down SSH"

SSHD_CONFIG="/etc/ssh/sshd_config"
SSHD_DROP_IN="/etc/ssh/sshd_config.d/99-creative-mode.conf"

# Get this machine's Tailscale IPv4 address
TS_IP=$(tailscale ip -4 2>/dev/null || true)
if [ -z "$TS_IP" ]; then
    fail "Cannot get Tailscale IP — is Tailscale connected?"
    exit 1
fi

# Migration: remove old append-style config from sshd_config if present
# (Previous versions of this script appended directly to sshd_config, which
# caused duplicate/conflicting directives on re-run.)
if grep -q "# Creative Mode: SSH lockdown" "$SSHD_CONFIG" 2>/dev/null; then
    if $DRY_RUN; then
        info "Would remove old SSH lockdown block from $SSHD_CONFIG"
    else
        cp "$SSHD_CONFIG" "${SSHD_CONFIG}.bak.$(date +%Y%m%d)"
        sed -i '/^# Creative Mode: SSH lockdown/,/^PasswordAuthentication no$/d' "$SSHD_CONFIG"
        ok "Removed old SSH lockdown block from $SSHD_CONFIG"
    fi
fi

# Write the drop-in config (always overwrite to pick up Tailscale IP changes)
if [ -f "$SSHD_DROP_IN" ] && grep -q "ListenAddress $TS_IP" "$SSHD_DROP_IN" 2>/dev/null; then
    skip "SSH drop-in already correct (ListenAddress $TS_IP, Port 2222)"
else
    if $DRY_RUN; then
        info "Would write $SSHD_DROP_IN:"
        info "  ListenAddress $TS_IP"
        info "  Port 2222"
        info "  PermitRootLogin no"
        info "  PasswordAuthentication no"
    else
        mkdir -p /etc/ssh/sshd_config.d
        cat > "$SSHD_DROP_IN" << EOF
# Creative Mode: SSH lockdown — only accessible via Tailscale
ListenAddress $TS_IP
Port 2222
PermitRootLogin no
PasswordAuthentication no
EOF
        ok "Wrote SSH drop-in config ($SSHD_DROP_IN)"
    fi
fi

# Validate config and restart SSH if needed
if ! $DRY_RUN && [ -f "$SSHD_DROP_IN" ]; then
    mkdir -p /run/sshd
    if ! sshd -t 2>&1; then
        fail "SSH config validation failed (see above). Fix and run: systemctl restart ssh"
    elif ss -tlnp | grep -q ':2222'; then
        ok "SSH already listening on port 2222"
    else
        systemctl restart ssh
        ok "Restarted SSH — now listening on port 2222"
    fi
fi

# ============================================================================
# Step 11: Add deploy user to docker group
# ============================================================================
# This lets the 'deploy' user run Docker commands without sudo. The deploy
# user needs this to start and manage the game server containers.
# ============================================================================
section "Step 11: Add deploy to docker group"

if id -nG deploy 2>/dev/null | grep -qw docker; then
    skip "User 'deploy' is already in the docker group"
else
    if $DRY_RUN; then
        info "Would add 'deploy' to the docker group"
    else
        usermod -aG docker deploy
        ok "Added 'deploy' to docker group"
    fi
fi

# ============================================================================
# Step 12: Set up SQLite backup cron job
# ============================================================================
# The game's database is a single SQLite file. This cron job backs it up
# every day using SQLite's built-in .backup command, which creates a
# consistent snapshot even while the database is in use (no corruption risk).
#
# Backups are stored alongside the repo and old ones are automatically
# deleted after 7 days to prevent filling the disk.
#
# The repo path is written to /etc/creative-mode.conf so the cron script
# can find it regardless of where the repo lives.
# ============================================================================
section "Step 12: SQLite backup cron"

if [ -f /etc/cron.daily/backup-creative-mode ]; then
    skip "Backup cron job already exists"
else
    if $DRY_RUN; then
        info "Would create /etc/creative-mode.conf with repo path"
        info "Would create /etc/cron.daily/backup-creative-mode"
    else
        # Write the config file so the cron script knows where to find the repo
        cat > /etc/creative-mode.conf << EOF
# Path to the Creative Mode repository (written by vps-bootstrap.sh)
CREATIVE_MODE_DIR=$CREATIVE_MODE_DIR
EOF

        # Create the daily backup script
        cat > /etc/cron.daily/backup-creative-mode << 'CRONEOF'
#!/usr/bin/env bash
set -euo pipefail

# Load the repo path from config
. /etc/creative-mode.conf

DB_PATH="${CREATIVE_MODE_DIR}/data/creative-mode.db"
BACKUP_DIR="${CREATIVE_MODE_DIR%/*}/backups"

# Skip if the database doesn't exist yet (server hasn't run)
if [ ! -f "$DB_PATH" ]; then
    exit 0
fi

# Create backup directory if needed
mkdir -p "$BACKUP_DIR"

# Use SQLite's .backup command for a consistent snapshot
sqlite3 "$DB_PATH" ".backup ${BACKUP_DIR}/creative-mode-$(date +%Y%m%d).db"

# Remove backups older than 7 days
find "$BACKUP_DIR" -name '*.db' -mtime +7 -delete
CRONEOF

        chmod +x /etc/cron.daily/backup-creative-mode

        ok "Created daily backup cron job"
    fi
fi

# ============================================================================
# Step 13: Create systemd service
# ============================================================================
# systemd is Linux's service manager. This service definition tells Linux to:
#   - Start the game server automatically when the machine boots
#   - Run it as the 'deploy' user (not root)
#   - Restart on failure with a 5-second delay
#   - Allow stopping the server cleanly with 'systemctl stop creative-mode'
#
# The harness runs as a native binary (not Docker). The harness-run.sh
# wrapper sets up PATH (Nix, Cargo, Go tools, Claude CLI) and starts tmux.
# ============================================================================
section "Step 13: systemd service"

SERVICE_FILE="/etc/systemd/system/creative-mode.service"

# Always overwrite — the service definition may have changed (e.g., Docker → native)
if $DRY_RUN; then
    info "Would create $SERVICE_FILE"
    info "Would enable creative-mode.service"
else
    cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Creative Mode Harness
After=network.target

[Service]
Type=simple
User=deploy
WorkingDirectory=$CREATIVE_MODE_DIR/harness
ExecStart=$CREATIVE_MODE_DIR/scripts/harness-run.sh
Restart=on-failure
RestartSec=5
EnvironmentFile=$CREATIVE_MODE_DIR/harness/.env

[Install]
WantedBy=multi-user.target
EOF
    ok "Created $SERVICE_FILE"
fi

# Ensure service is registered and enabled (both commands are idempotent)
if ! $DRY_RUN && [ -f "$SERVICE_FILE" ]; then
    systemctl daemon-reload
    systemctl enable creative-mode.service 2>/dev/null
    ok "creative-mode.service enabled"
fi

# ============================================================================
# Step 14: Install Nix
# ============================================================================
# Nix is a package manager that provides reproducible dev environments.
# We install in daemon mode so it's available system-wide.
# The --yes flag skips interactive confirmation.
# ============================================================================
section "Step 14: Install Nix"

if [ -d /nix ]; then
    skip "Nix already installed"
else
    if $DRY_RUN; then
        info "Would install Nix in daemon mode"
    else
        sh <(curl -L https://nixos.org/nix/install) --daemon --yes
        ok "Installed Nix (daemon mode)"
    fi
fi

# ============================================================================
# Step 15: Enable Nix flakes
# ============================================================================
# Flakes are Nix's modern project management feature. The creative-mode repo
# uses a flake.nix for its dev environment. This is still behind an
# experimental feature flag.
# ============================================================================
section "Step 15: Enable Nix flakes"

NIX_CONF="/etc/nix/nix.conf"
if grep -q "experimental-features.*flakes" "$NIX_CONF" 2>/dev/null; then
    skip "Nix flakes already enabled in $NIX_CONF"
else
    if $DRY_RUN; then
        info "Would add 'experimental-features = nix-command flakes' to $NIX_CONF"
    else
        mkdir -p /etc/nix
        echo "experimental-features = nix-command flakes" >> "$NIX_CONF"
        systemctl restart nix-daemon
        ok "Enabled Nix flakes and restarted nix-daemon"
    fi
fi

# ============================================================================
# Step 16: Install direnv
# ============================================================================
# direnv automatically activates the Nix dev environment when you cd into
# the project directory. We install it via Nix. The shell hook is added in
# the .zshrc created in Step 17.
# ============================================================================
section "Step 16: Install direnv"

if sudo -u deploy bash -lc 'command -v direnv' &>/dev/null; then
    skip "direnv already installed for deploy user"
else
    if $DRY_RUN; then
        info "Would install direnv via nix profile for deploy user"
    else
        sudo -u deploy bash -lc 'nix profile install nixpkgs#direnv'
        ok "Installed direnv for deploy user"
    fi
fi

# ============================================================================
# Step 16b: Install Rust toolchain
# ============================================================================
# Rust is needed for building Bevy game servers and WASM clients. We install
# system-wide to /usr/local so tmux sessions (spawned by the harness for game
# servers and Claude Code) inherit Rust on PATH automatically.
# ============================================================================
section "Step 16b: Install Rust toolchain"

if [ -f /usr/local/cargo/bin/rustup ]; then
    skip "Rust toolchain already installed"
else
    if $DRY_RUN; then
        info "Would install Rust toolchain to /usr/local/cargo"
    else
        RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo \
            curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | \
            sh -s -- -y --default-toolchain stable --profile minimal
        ok "Installed Rust toolchain"
    fi
fi

# Add wasm32 target for Bevy client builds
if /usr/local/cargo/bin/rustup target list --installed 2>/dev/null | grep -q wasm32-unknown-unknown; then
    skip "wasm32-unknown-unknown target already installed"
else
    if $DRY_RUN; then
        info "Would add wasm32-unknown-unknown target"
    else
        RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo \
            /usr/local/cargo/bin/rustup target add wasm32-unknown-unknown
        ok "Added wasm32-unknown-unknown target"
    fi
fi

# ============================================================================
# Step 16c: Install cargo tools
# ============================================================================
# trunk: WASM bundler for Bevy client
# cargo-watch: auto-rebuild during Claude dev sessions
# wasm-bindgen-cli: WASM bindings (pinned version must match Cargo.lock)
# ============================================================================
section "Step 16c: Install cargo tools"

export RUSTUP_HOME=/usr/local/rustup
export CARGO_HOME=/usr/local/cargo
export PATH="/usr/local/cargo/bin:$PATH"

if command -v trunk &>/dev/null && command -v cargo-watch &>/dev/null; then
    skip "trunk and cargo-watch already installed"
else
    if $DRY_RUN; then
        info "Would install trunk, cargo-watch, wasm-bindgen-cli"
    else
        cargo install trunk cargo-watch
        cargo install wasm-bindgen-cli --version 0.2.108
        ok "Installed trunk, cargo-watch, wasm-bindgen-cli"
    fi
fi

# ============================================================================
# Step 16d: Install Go tools (templ)
# ============================================================================
# templ is a Go HTML templating engine. It compiles .templ files to Go code.
# Needs Go on PATH from Nix first.
# ============================================================================
section "Step 16d: Install Go tools (templ)"

# Source Nix + direnv to get Go on PATH
source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh 2>/dev/null || true
eval "$(sudo -u deploy bash -c 'cd '"$CREATIVE_MODE_DIR"' && direnv export bash 2>/dev/null')" 2>/dev/null || true

if command -v templ &>/dev/null; then
    skip "templ already installed"
else
    if $DRY_RUN; then
        info "Would install templ via go install"
    else
        sudo -u deploy bash -lc 'cd '"$CREATIVE_MODE_DIR"' && eval "$(direnv export bash 2>/dev/null)" && go install github.com/a-h/templ/cmd/templ@v0.3.977'
        ok "Installed templ"
    fi
fi

# ============================================================================
# Step 17: Install oh-my-zsh + configure zsh as login shell
# ============================================================================
# Sets zsh as deploy's login shell, installs oh-my-zsh for a nice prompt and
# plugin ecosystem, and writes a .zshrc that hooks direnv (so the Nix flake
# dev environment activates automatically on cd into the project).
#
# Note: The flake.nix shellHook sources oh-my-zsh, but direnv only captures
# environment variable diffs — it can't source scripts in the parent shell.
# So oh-my-zsh must be installed independently for the login shell.
# ============================================================================
section "Step 17: Install oh-my-zsh + configure zsh"

# Set zsh as deploy's login shell
if getent passwd deploy | grep -q '/bin/zsh'; then
    skip "zsh already set as deploy's login shell"
else
    if $DRY_RUN; then
        info "Would set zsh as deploy's login shell"
    else
        chsh -s /usr/bin/zsh deploy
        ok "Set zsh as deploy's login shell"
    fi
fi

# Create .zshrc for deploy user (BEFORE oh-my-zsh install so the installer
# sees an existing .zshrc and KEEP_ZSHRC=yes prevents overwriting it)
DEPLOY_ZSHRC="$DEPLOY_HOME/.zshrc"
if grep -q 'cd ~/creative-mode' "$DEPLOY_ZSHRC" 2>/dev/null; then
    skip ".zshrc already configured"
else
    if $DRY_RUN; then
        info "Would create $DEPLOY_ZSHRC with oh-my-zsh + direnv hook"
    else
        cat > "$DEPLOY_ZSHRC" << 'ZSHRCEOF'
# Nix daemon environment (puts ~/.nix-profile/bin in PATH)
if [ -e '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' ]; then
  . '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh'
fi

# oh-my-zsh configuration
export ZSH="$HOME/.oh-my-zsh"
ZSH_THEME="robbyrussell"
plugins=(git fzf docker docker-compose)
source $ZSH/oh-my-zsh.sh

# direnv hook — activates Nix flake dev environment on cd
eval "$(direnv hook zsh)"

# Auto-cd to project directory (triggers direnv)
cd ~/creative-mode
ZSHRCEOF
        chown deploy:deploy "$DEPLOY_ZSHRC"
        ok "Created $DEPLOY_ZSHRC with oh-my-zsh + direnv hook"
    fi
fi

# Install oh-my-zsh for deploy user
if [ -d "$DEPLOY_HOME/.oh-my-zsh" ]; then
    skip "oh-my-zsh already installed"
else
    if $DRY_RUN; then
        info "Would install oh-my-zsh for deploy user"
    else
        sudo -u deploy sh -c 'curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh | RUNZSH=no KEEP_ZSHRC=yes sh'
        ok "Installed oh-my-zsh for deploy user"
    fi
fi

# ============================================================================
# Step 18: Activate dev environment
# ============================================================================
# direnv allow tells direnv to trust the .envrc in the project directory.
# This triggers the Nix flake to build the dev environment on first use.
# ============================================================================
section "Step 18: Activate dev environment"

if [ -f "$CREATIVE_MODE_DIR/.direnv/flake-profile/bin/just" ]; then
    skip "Dev environment already activated"
else
    if $DRY_RUN; then
        info "Would run 'direnv allow' in $CREATIVE_MODE_DIR"
    else
        sudo -u deploy bash -lc "cd $CREATIVE_MODE_DIR && direnv allow"
        ok "Activated dev environment (direnv allow)"
        info "First activation may take a while as Nix builds the environment"
    fi
fi

# ============================================================================
# Step 19: Create .env file
# ============================================================================
# The harness needs secrets for GitHub OAuth, AI APIs, etc. This step
# prompts interactively for each secret and auto-computes what it can
# (HARNESS_URL from Tailscale DNS, CM_HOOK_SECRET via openssl).
# ============================================================================
section "Step 19: Create .env file"

ENV_FILE="$CREATIVE_MODE_DIR/harness/.env"
if [ -f "$ENV_FILE" ]; then
    skip ".env file already exists at $ENV_FILE"
else
    if $DRY_RUN; then
        info "Would prompt for secrets and create $ENV_FILE"
    else
        echo ""
        echo -e "${BOLD}Enter secrets for the .env file (leave blank to skip):${NC}"
        echo ""

        read -rp "  GitHub OAuth Client ID: " GITHUB_CLIENT_ID
        read -rp "  GitHub OAuth Client Secret: " GITHUB_CLIENT_SECRET
        read -rp "  Gemini API Key: " GEMINI_API_KEY
        read -rp "  Anthropic API Key: " ANTHROPIC_API_KEY

        # Auto-compute HARNESS_URL from Tailscale DNS name
        TS_DNS=$(tailscale status --json 2>/dev/null | jq -r '.Self.DNSName' | sed 's/\.$//' || true)
        if [ -n "$TS_DNS" ]; then
            HARNESS_URL="https://$TS_DNS"
            info "Auto-detected HARNESS_URL: $HARNESS_URL"
        else
            HARNESS_URL=""
            info "Could not detect Tailscale DNS name — HARNESS_URL left blank"
        fi

        # Auto-generate hook secret
        HOOK_SECRET=$(openssl rand -hex 32)
        info "Auto-generated CM_HOOK_SECRET"

        cat > "$ENV_FILE" << EOF
# GitHub OAuth (required for authentication)
GITHUB_CLIENT_ID=$GITHUB_CLIENT_ID
GITHUB_CLIENT_SECRET=$GITHUB_CLIENT_SECRET

# Gemini API key (required for AI features)
GEMINI_API_KEY=$GEMINI_API_KEY

# Anthropic API key (required for Claude Code sessions)
ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY

# --- VPS only ---

# Harness URL — Tailscale Serve HTTPS URL
HARNESS_URL=$HARNESS_URL

# Hook secret — protects /api/claude-event endpoint
CM_HOOK_SECRET=$HOOK_SECRET
EOF

        chown deploy:deploy "$ENV_FILE"
        chmod 600 "$ENV_FILE"
        ok "Created $ENV_FILE (owner: deploy, mode: 600)"
    fi
fi

# ============================================================================
# Step 20: Set up Tailscale Serve
# ============================================================================
# Tailscale Serve provides HTTPS with automatic TLS certificates, proxying
# traffic from https://{machine}.{tailnet}.ts.net to localhost:8080.
# This is how the harness is accessed over the Tailscale network.
# ============================================================================
section "Step 20: Tailscale Serve"

if tailscale serve status 2>/dev/null | grep -q 'https'; then
    skip "Tailscale Serve already configured"
else
    if $DRY_RUN; then
        info "Would configure Tailscale Serve: https / -> http://localhost:8080"
    else
        tailscale serve https / http://localhost:8080
        ok "Configured Tailscale Serve (https -> localhost:8080)"
    fi
fi

# ============================================================================
# Step 20b: Install Claude Code CLI
# ============================================================================
# Claude Code CLI is used by the harness to run Claude sessions in tmux.
# Installed as the deploy user to ~/.local/bin.
# ============================================================================
section "Step 20b: Install Claude Code CLI"

if sudo -u deploy bash -lc 'command -v claude' &>/dev/null; then
    skip "Claude Code CLI already installed"
else
    if $DRY_RUN; then
        info "Would install Claude Code CLI for deploy user"
    else
        sudo -u deploy bash -lc 'curl -fsSL https://claude.ai/install.sh | bash'
        ok "Installed Claude Code CLI"
    fi
fi

# ============================================================================
# Step 21: Start the server
# ============================================================================
# Start the harness via the systemd service created in Step 13. The harness
# runs as a native binary via harness-run.sh. The first build must be done
# manually with 'just vps-build' before starting.
# ============================================================================
section "Step 21: Start the server"

if systemctl is-active --quiet creative-mode; then
    skip "creative-mode service is already running"
else
    if $DRY_RUN; then
        info "Would start creative-mode service"
    else
        systemctl start creative-mode
        ok "Started creative-mode service"
        info "Make sure 'just vps-build' was run first (harness binary must exist)"
        info "Check progress: journalctl -u creative-mode -f"
    fi
fi

# ============================================================================
# Step 22: Summary
# ============================================================================
section "Bootstrap Complete"

TS_DNS_NAME=$(tailscale status --json 2>/dev/null | jq -r '.Self.DNSName' | sed 's/\.$//' || echo '{machine}.{tailnet}.ts.net')

echo ""
echo -e "${GREEN}${BOLD}Everything is set up and running!${NC}"
echo ""
echo -e "${BOLD}What to do next:${NC}"
echo ""
echo "  1. Check server logs:"
echo "       journalctl -u creative-mode -f"
echo ""
echo -e "  2. ${YELLOW}IMPORTANT:${NC} Update your GitHub OAuth App callback URL to:"
echo "       https://$TS_DNS_NAME/auth/github/callback"
echo "     (This must be done manually in the GitHub web UI)"
echo ""
echo "  3. SSH in via Tailscale going forward:"
echo "       ssh deploy@$TS_DNS_NAME"
echo ""
echo "  4. Visit your server:"
echo "       https://$TS_DNS_NAME"
echo ""
