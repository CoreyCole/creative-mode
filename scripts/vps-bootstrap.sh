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
#   5.  Configures UFW firewall (blocks all public traffic)
#   6.  Installs Fail2Ban (blocks brute-force login attempts)
#   7.  Locks down SSH (Tailscale-only, non-standard port, no passwords)
#   8.  Sets up daily SQLite backup cron job
#   9.  Creates systemd service for auto-start on reboot (native binary)
#   10. Installs Nix (daemon mode)
#   11. Enables Nix flakes
#   12. Installs flake tools to nix profile
#   12b. Installs Rust toolchain (system-wide)
#   12c. Installs cargo tools (trunk, cargo-watch, wasm-bindgen-cli)
#   12d. Installs Go tools (templ, air)
#   12e. Installs Tailwind CSS standalone binary
#   13. Installs oh-my-zsh + configures zsh as login shell
#   14. Creates .env file (interactive prompts for secrets)
#   15. Sets up Tailscale Serve (HTTPS)
#   15b. Installs Claude Code CLI
#   15c. Installs uv (Python package runner for Claude Code hooks)
#   15d. Installs playwright-cli (autonomous browser testing)
#   15e. Installs and configures OpenClaw gateway (agent framework for mayors)
#   16. Starts the server via systemd
#   17. Prints summary
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
# Step 4b: Detect local network interfaces
# ============================================================================
# Find non-Tailscale, non-loopback private IPs so we can allow fast local SSH.
# This handles UTM NAT (192.168.66.x), bridged networking, cloud private
# subnets, etc. — whatever local network the machine is on.
# ============================================================================
section "Step 4b: Detect local network interfaces"

TS_IP=$(tailscale ip -4 2>/dev/null || true)
LOCAL_IPS=()
LOCAL_SUBNETS=()

if [ -n "$TS_IP" ]; then
    # Collect all IPv4 addresses that are NOT Tailscale and NOT loopback
    while IFS= read -r line; do
        ip_addr=$(echo "$line" | awk '{print $1}')
        iface=$(echo "$line" | awk '{print $2}')
        # Skip Tailscale interface and loopback
        [[ "$iface" == "tailscale0" ]] && continue
        [[ "$iface" == "lo" ]] && continue
        [[ "$ip_addr" == "$TS_IP" ]] && continue
        # Only keep private IPs (RFC 1918)
        case "$ip_addr" in
            10.*|172.1[6-9].*|172.2[0-9].*|172.3[0-1].*|192.168.*)
                LOCAL_IPS+=("$ip_addr")
                # Derive /24 subnet for UFW rules
                subnet=$(echo "$ip_addr" | sed 's/\.[0-9]*$/.0\/24/')
                LOCAL_SUBNETS+=("$subnet")
                ok "Found local interface: $ip_addr on $iface (subnet $subnet)"
                ;;
            *)
                info "Skipping public IP: $ip_addr on $iface"
                ;;
        esac
    done < <(ip -4 addr show | awk '/inet / {gsub(/\/.*/, "", $2); print $2, $NF}')
fi

if [ ${#LOCAL_IPS[@]} -eq 0 ]; then
    info "No local private IPs detected (Tailscale-only access)"
else
    ok "Detected ${#LOCAL_IPS[@]} local interface(s) for fast SSH"
fi

# ============================================================================
# Step 5: Configure UFW firewall
# ============================================================================
# UFW (Uncomplicated Firewall) blocks all uninvited network traffic by default
# and only lets through connections from Tailscale and detected local networks.
#
# Rules:
#   - Deny all incoming traffic (nothing from the public internet gets in)
#   - Allow all outgoing traffic (the server can still reach the internet)
#   - Allow traffic on the tailscale0 interface (your private tunnel)
#   - Allow SSH (port 2222) from detected local subnets (fast local access)
# ============================================================================
section "Step 5: Configure UFW firewall"

if ufw status | grep -q "Status: active"; then
    # UFW already active — just ensure local subnet rules exist
    for subnet in "${LOCAL_SUBNETS[@]}"; do
        if ufw status | grep -q "$subnet.*2222"; then
            skip "UFW rule already exists: $subnet → port 2222"
        else
            if $DRY_RUN; then
                info "Would add UFW rule: allow from $subnet to port 2222"
            else
                ufw allow from "$subnet" to any port 2222 proto tcp comment "Local SSH ($subnet)"
                ok "Added UFW rule: $subnet → port 2222"
            fi
        fi
    done
else
    if $DRY_RUN; then
        info "Would configure UFW: deny incoming, allow outgoing, allow tailscale0"
        for subnet in "${LOCAL_SUBNETS[@]}"; do
            info "Would add UFW rule: allow from $subnet to port 2222"
        done
    else
        ufw default deny incoming
        ufw default allow outgoing
        ufw allow in on tailscale0
        ufw allow 41641/udp comment "Tailscale direct connections"
        for subnet in "${LOCAL_SUBNETS[@]}"; do
            ufw allow from "$subnet" to any port 2222 proto tcp comment "Local SSH ($subnet)"
        done
        ufw --force enable
        ok "UFW enabled — Tailscale + local subnet traffic allowed"
    fi
fi

# ============================================================================
# Step 6: Install Fail2Ban
# ============================================================================
# Fail2Ban watches log files for repeated failed login attempts (like someone
# trying to guess your password) and automatically blocks their IP address.
# It's an extra layer of defense — even though we lock down SSH in the next
# step, Fail2Ban catches brute-force attempts before they can do any damage.
# ============================================================================
section "Step 6: Install Fail2Ban"

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
# Step 7: Lock down SSH
# ============================================================================
# We make SSH much harder to attack by:
#
#   ListenAddress — SSH listens on the Tailscale IP (remote access) plus any
#     detected local private IPs (fast local access). Not reachable from the
#     public internet thanks to both ListenAddress and UFW.
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
section "Step 7: Lock down SSH"

SSHD_CONFIG="/etc/ssh/sshd_config"
SSHD_DROP_IN="/etc/ssh/sshd_config.d/99-creative-mode.conf"

# TS_IP was already set in Step 4b
if [ -z "$TS_IP" ]; then
    TS_IP=$(tailscale ip -4 2>/dev/null || true)
fi
if [ -z "$TS_IP" ]; then
    fail "Cannot get Tailscale IP — is Tailscale connected?"
    exit 1
fi

# Build the list of ListenAddress lines
LISTEN_ADDRS="ListenAddress $TS_IP"
for local_ip in "${LOCAL_IPS[@]}"; do
    LISTEN_ADDRS="$LISTEN_ADDRS
ListenAddress $local_ip"
done

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

# Build expected config content for comparison
EXPECTED_CONFIG="# Creative Mode: SSH lockdown — Tailscale + local interfaces
$LISTEN_ADDRS
Port 2222
PermitRootLogin no
PasswordAuthentication no"

# Write the drop-in config (overwrite if IPs changed)
if [ -f "$SSHD_DROP_IN" ] && [ "$(cat "$SSHD_DROP_IN")" = "$EXPECTED_CONFIG" ]; then
    skip "SSH drop-in already correct ($(echo "$LISTEN_ADDRS" | wc -l) address(es), Port 2222)"
else
    if $DRY_RUN; then
        info "Would write $SSHD_DROP_IN:"
        info "  $LISTEN_ADDRS"
        info "  Port 2222"
        info "  PermitRootLogin no"
        info "  PasswordAuthentication no"
    else
        mkdir -p /etc/ssh/sshd_config.d
        cat > "$SSHD_DROP_IN" << EOF
$EXPECTED_CONFIG
EOF
        ok "Wrote SSH drop-in config ($SSHD_DROP_IN)"
        for local_ip in "${LOCAL_IPS[@]}"; do
            ok "  SSH will listen on $local_ip (local)"
        done
        ok "  SSH will listen on $TS_IP (Tailscale)"
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
# Step 8: Set up SQLite backup cron job
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
section "Step 8: SQLite backup cron"

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
# Step 9: Create systemd service
# ============================================================================
# systemd is Linux's service manager. This service definition tells Linux to:
#   - Start the game server automatically when the machine boots
#   - Run it as the 'deploy' user (not root)
#   - Restart on failure with a 5-second delay
#   - Allow stopping the server cleanly with 'systemctl stop creative-mode'
#
# The harness runs as a native binary. The harness-run.sh wrapper sets up
# PATH (Nix, Cargo, Go tools, Claude CLI) and starts tmux.
# ============================================================================
section "Step 9: systemd service"

SERVICE_FILE="/etc/systemd/system/creative-mode.service"

# Always overwrite — the service definition may have changed
if $DRY_RUN; then
    info "Would create $SERVICE_FILE"
    info "Would enable creative-mode.service"
else
    cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Creative Mode Harness
After=network.target temporal-dev.service
Requires=temporal-dev.service

[Service]
Type=simple
User=deploy
KillMode=process
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
# Step 10: Install Nix
# ============================================================================
# Nix is a package manager that provides reproducible dev environments.
# We install in daemon mode so it's available system-wide.
# The --yes flag skips interactive confirmation.
# ============================================================================
section "Step 10: Install Nix"

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
# Step 11: Enable Nix flakes
# ============================================================================
# Flakes are Nix's modern project management feature. The creative-mode repo
# uses a flake.nix for its dev environment. This is still behind an
# experimental feature flag.
# ============================================================================
section "Step 11: Enable Nix flakes"

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
# Step 12: Install flake tools to nix profile
# ============================================================================
# Installs all dev tools from flake.nix (go, just, tmux, sqlc, etc.) into
# ~/.nix-profile/bin/ via nix profile. This makes tools available system-wide
# — in interactive shells, Claude Code, tmux sessions, and systemd — without
# requiring direnv.
# ============================================================================
section "Step 12: Install flake tools to nix profile"

if sudo -u deploy bash -lc 'source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh && command -v just' &>/dev/null; then
    skip "Flake tools already installed to nix profile"
else
    if $DRY_RUN; then
        info "Would install flake tools via nix profile install"
    else
        sudo -u deploy bash -lc "source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh && nix profile install $CREATIVE_MODE_DIR"
        ok "Installed flake tools to nix profile"
    fi
fi

# ============================================================================
# Step 12b: Install Rust toolchain
# ============================================================================
# Rust is needed for building Bevy game servers and WASM clients. We install
# system-wide to /usr/local so tmux sessions (spawned by the harness for game
# servers and Claude Code) inherit Rust on PATH automatically.
# ============================================================================
section "Step 12b: Install Rust toolchain"

if [ -f /usr/local/cargo/bin/rustup ]; then
    skip "Rust toolchain already installed"
else
    if $DRY_RUN; then
        info "Would install Rust toolchain to /usr/local/cargo"
    else
        RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo \
            curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | \
            sh -s -- -y --default-toolchain stable --profile default
        ok "Installed Rust toolchain (includes rustfmt + clippy)"
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

# Ensure cargo registry is writable by deploy (rustup installs as root)
chown -R deploy:deploy /usr/local/cargo/registry/ 2>/dev/null || true

# ============================================================================
# Step 12c: Install cargo tools
# ============================================================================
# trunk: WASM bundler for Bevy client
# cargo-watch: auto-rebuild during Claude dev sessions
# wasm-bindgen-cli: WASM bindings (pinned version must match Cargo.lock)
# ============================================================================
section "Step 12c: Install cargo tools"

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
# Step 12d: Install Go tools (templ, air)
# ============================================================================
# templ: Go HTML templating engine — compiles .templ files to Go code.
# air: live-reload — watches files and rebuilds/restarts the harness.
# Needs Go on PATH from Nix first.
# ============================================================================
section "Step 12d: Install Go tools (templ, air)"

# Source Nix to get Go on PATH
source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh 2>/dev/null || true

if command -v templ &>/dev/null && command -v air &>/dev/null; then
    skip "templ and air already installed"
else
    if $DRY_RUN; then
        info "Would install templ and air via go install"
    else
        sudo -u deploy bash -lc 'source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh && go install github.com/a-h/templ/cmd/templ@v0.3.977 && go install github.com/air-verse/air@latest'
        ok "Installed templ and air"
    fi
fi

# ============================================================================
# Step 12e: Install Tailwind CSS standalone binary
# ============================================================================
# Standalone binary — no Node.js/npm/pnpm required at runtime.
# Detects architecture (arm64 or x64) automatically.
# ============================================================================
section "Step 12e: Install Tailwind CSS"

if command -v tailwindcss &>/dev/null; then
    skip "tailwindcss already installed"
else
    if $DRY_RUN; then
        info "Would install tailwindcss standalone binary to /usr/local/bin"
    else
        ARCH=$(uname -m)
        case "$ARCH" in
            aarch64|arm64) TW_ARCH="linux-arm64" ;;
            x86_64)        TW_ARCH="linux-x64" ;;
            *)             fail "Unsupported architecture: $ARCH"; exit 1 ;;
        esac
        curl -sLo /usr/local/bin/tailwindcss \
            "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-${TW_ARCH}"
        chmod +x /usr/local/bin/tailwindcss
        ok "Installed tailwindcss standalone ($TW_ARCH)"
    fi
fi

# ============================================================================
# Step 13: Install oh-my-zsh + configure zsh as login shell
# ============================================================================
# Sets zsh as deploy's login shell, installs oh-my-zsh for a nice prompt and
# plugin ecosystem, and writes .zshenv (PATH for all sessions) and .zshrc
# (interactive config only).
# ============================================================================
section "Step 13: Install oh-my-zsh + configure zsh"

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

# Create .zshenv — sourced for ALL zsh sessions (interactive, non-interactive,
# login, non-login). This ensures Claude Code and other non-interactive
# contexts get all tools on PATH.
DEPLOY_ZSHENV="$DEPLOY_HOME/.zshenv"
if [ -f "$DEPLOY_ZSHENV" ] && grep -q 'nix-daemon.sh' "$DEPLOY_ZSHENV" 2>/dev/null; then
    skip ".zshenv already configured"
else
    if $DRY_RUN; then
        info "Would create $DEPLOY_ZSHENV with PATH for all sessions"
    else
        cat > "$DEPLOY_ZSHENV" << 'ZSHENVEOF'
# Nix profile PATH (tools from flake.nix)
if [ -e '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' ]; then
  . '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh'
fi

# Go-installed tools (templ, air)
export PATH="$HOME/go/bin:$PATH"

# Rust/Cargo
export RUSTUP_HOME=/usr/local/rustup
export CARGO_HOME=/usr/local/cargo
export PATH="/usr/local/cargo/bin:$PATH"

# Claude Code CLI
export PATH="$HOME/.local/bin:$PATH"

# npm global packages (playwright-cli)
export PATH="$HOME/.npm-global/bin:$PATH"
ZSHENVEOF
        chown deploy:deploy "$DEPLOY_ZSHENV"
        ok "Created $DEPLOY_ZSHENV"
    fi
fi

# Create .zshrc for deploy user (BEFORE oh-my-zsh install so the installer
# sees an existing .zshrc and KEEP_ZSHRC=yes prevents overwriting it)
DEPLOY_ZSHRC="$DEPLOY_HOME/.zshrc"
if [ -f "$DEPLOY_ZSHRC" ] && ! grep -q 'direnv' "$DEPLOY_ZSHRC" 2>/dev/null; then
    skip ".zshrc already configured (no direnv)"
else
    if $DRY_RUN; then
        info "Would create $DEPLOY_ZSHRC with oh-my-zsh config"
    else
        cat > "$DEPLOY_ZSHRC" << 'ZSHRCEOF'
# oh-my-zsh configuration
export ZSH="$HOME/.oh-my-zsh"
ZSH_THEME="robbyrussell"
plugins=(git fzf)
source $ZSH/oh-my-zsh.sh

alias cld='claude --dangerously-skip-permissions'

export PATH="$HOME/.npm-global/bin:$PATH"
export PATH="$HOME/.nvim/bin:$PATH"
ZSHRCEOF
        chown deploy:deploy "$DEPLOY_ZSHRC"
        ok "Created $DEPLOY_ZSHRC"
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
# Step 14: Create .env file
# ============================================================================
# The harness needs secrets for GitHub OAuth, AI APIs, etc. This step
# prompts interactively for each secret and auto-computes what it can
# (HARNESS_URL from Tailscale DNS, CM_HOOK_SECRET via openssl).
# ============================================================================
section "Step 14: Create .env file"

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

        read -rp "  Discord OAuth Client ID: " DISCORD_CLIENT_ID
        read -rp "  Discord OAuth Client Secret: " DISCORD_CLIENT_SECRET
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
# Discord OAuth (required for authentication)
DISCORD_CLIENT_ID=$DISCORD_CLIENT_ID
DISCORD_CLIENT_SECRET=$DISCORD_CLIENT_SECRET

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
# Step 15:Set up Tailscale Serve
# ============================================================================
# Tailscale Serve provides HTTPS with automatic TLS certificates, proxying
# traffic from https://{machine}.{tailnet}.ts.net to localhost:8080.
# This is how the harness is accessed over the Tailscale network.
# ============================================================================
section "Step 15: Tailscale Serve"

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
# Step 15b: Install Claude Code CLI
# ============================================================================
# Claude Code CLI is used by the harness to run Claude sessions in tmux.
# Installed as the deploy user to ~/.local/bin.
# ============================================================================
section "Step 15b: Install Claude Code CLI"

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
# Step 15c: Install uv (Python package runner)
# ============================================================================
# uv is an extremely fast Python package installer/runner. Claude Code hooks
# use `uv run` with PEP 723 inline script metadata to run Python scripts
# with auto-managed dependencies. Installed system-wide so hooks work
# outside the Nix dev shell.
# ============================================================================
section "Step 15c: Install uv"

if command -v uv &>/dev/null; then
    skip "uv already installed"
else
    if $DRY_RUN; then
        info "Would install uv via official install script"
    else
        curl -LsSf https://astral.sh/uv/install.sh | sudo -u deploy sh
        ok "Installed uv"
    fi
fi

# ============================================================================
# Step 15d: Install playwright-cli
# ============================================================================
# playwright-cli enables autonomous browser testing for world mayors.
# Installed via npm to ~/.npm-global (Nix store is read-only).
# Also downloads Chromium browser for headless testing.
# ============================================================================
section "Step 15d: Install playwright-cli"

# Configure npm global prefix to a writable location (Nix node store is read-only)
if [ ! -f "$DEPLOY_HOME/.npmrc" ] || ! grep -q 'prefix' "$DEPLOY_HOME/.npmrc" 2>/dev/null; then
    if $DRY_RUN; then
        info "Would configure npm global prefix to ~/.npm-global"
    else
        sudo -u deploy mkdir -p "$DEPLOY_HOME/.npm-global"
        sudo -u deploy bash -lc 'npm config set prefix ~/.npm-global'
        ok "Configured npm global prefix to ~/.npm-global"
    fi
fi

if sudo -u deploy bash -lc 'export PATH="$HOME/.npm-global/bin:$PATH" && command -v playwright-cli' &>/dev/null; then
    skip "playwright-cli already installed"
else
    if $DRY_RUN; then
        info "Would install playwright-cli and Chromium browser"
    else
        sudo -u deploy bash -lc 'source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh && npm install -g @playwright/cli@latest'
        sudo -u deploy bash -lc 'source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh && export PATH="$HOME/.npm-global/bin:$PATH" && playwright-cli install'
        ok "Installed playwright-cli + Chromium"
    fi
fi

# ============================================================================
# Step 15e: Install and configure OpenClaw gateway
# ============================================================================
# OpenClaw is the agent framework for world mayors. The gateway runs as a
# separate systemd service on port 18789, providing the /v1/chat/completions
# API that the harness uses for mayor chat and orchestration.
#
# Sub-steps:
#   1. Copy source from context/openclaw/ to /opt/openclaw/ (if missing)
#   2. Install dependencies with pnpm
#   3. Generate OPENCLAW_GATEWAY_TOKEN (if not in .env)
#   4. Run setup-openclaw.sh for config
#   5. Create and start openclaw-gateway.service
# ============================================================================
section "Step 15e: Install and configure OpenClaw gateway"

OPENCLAW_SRC="$CREATIVE_MODE_DIR/context/openclaw"
OPENCLAW_INSTALL="/opt/openclaw"
OPENCLAW_BIN="$OPENCLAW_INSTALL/openclaw.mjs"
OPENCLAW_DATA="$CREATIVE_MODE_DIR/data/openclaw"

# Sub-step 1: Install OpenClaw source
if [ -f "$OPENCLAW_BIN" ]; then
    skip "OpenClaw already installed at $OPENCLAW_INSTALL"
else
    if [ ! -d "$OPENCLAW_SRC" ]; then
        warn "OpenClaw source not found at $OPENCLAW_SRC — skipping gateway setup"
        warn "Clone or copy OpenClaw source to context/openclaw/ first"
    else
        if $DRY_RUN; then
            info "Would copy OpenClaw source to $OPENCLAW_INSTALL and install dependencies"
        else
            cp -r "$OPENCLAW_SRC" "$OPENCLAW_INSTALL"
            chown -R deploy:deploy "$OPENCLAW_INSTALL"
            ok "Copied OpenClaw source to $OPENCLAW_INSTALL"
        fi
    fi
fi

# Sub-step 2: Install dependencies and build (skip if dist/ exists)
if [ -f "$OPENCLAW_BIN" ]; then
    if [ -d "$OPENCLAW_INSTALL/dist" ]; then
        skip "OpenClaw already built"
    else
        if $DRY_RUN; then
            info "Would install OpenClaw dependencies and build"
        else
            sudo -u deploy bash -lc "
                source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh
                export PATH=\"\$HOME/.npm-global/bin:\$PATH\"
                cd $OPENCLAW_INSTALL && pnpm install --frozen-lockfile && pnpm build
            "
            ok "Installed OpenClaw dependencies and built"
        fi
    fi
fi

# Sub-step 3: Generate gateway token
if [ -f "$OPENCLAW_BIN" ]; then
    if grep -q 'OPENCLAW_GATEWAY_TOKEN' "$ENV_FILE" 2>/dev/null; then
        skip "OPENCLAW_GATEWAY_TOKEN already in .env"
    else
        if $DRY_RUN; then
            info "Would generate OPENCLAW_GATEWAY_TOKEN and append to .env"
        else
            OC_TOKEN=$(openssl rand -hex 32)
            {
                echo ""
                echo "# OpenClaw gateway auth token"
                echo "OPENCLAW_GATEWAY_TOKEN=$OC_TOKEN"
            } >> "$ENV_FILE"
            ok "Generated OPENCLAW_GATEWAY_TOKEN"
        fi
    fi
fi

# Sub-step 4: Run setup-openclaw.sh for config
if [ -f "$OPENCLAW_BIN" ]; then
    if [ -f "$OPENCLAW_DATA/.openclaw/openclaw.json" ]; then
        skip "OpenClaw config already exists"
    else
        if $DRY_RUN; then
            info "Would run setup-openclaw.sh to configure OpenClaw"
        else
            sudo -u deploy bash -lc "
                source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh
                set -a && source $ENV_FILE && set +a
                export OPENCLAW_HOME=$OPENCLAW_DATA
                export OPENCLAW_BIN=$OPENCLAW_BIN
                bash $CREATIVE_MODE_DIR/harness/scripts/setup-openclaw.sh
            "
            ok "OpenClaw configured"
        fi
    fi
fi

# Sub-step 5: Create systemd service
if [ -f "$OPENCLAW_BIN" ]; then
    if [ -f /etc/systemd/system/openclaw-gateway.service ]; then
        skip "openclaw-gateway.service already exists"
    else
        if $DRY_RUN; then
            info "Would create openclaw-gateway.service"
        else
            NODE_BIN=$(sudo -u deploy bash -lc 'source /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh && which node')
            cat > /etc/systemd/system/openclaw-gateway.service << SVCEOF
[Unit]
Description=OpenClaw Gateway
After=network-online.target
Wants=network-online.target
Before=creative-mode.service

[Service]
Type=simple
User=deploy
WorkingDirectory=$OPENCLAW_INSTALL
ExecStart=$NODE_BIN $OPENCLAW_BIN gateway run
Restart=always
RestartSec=5
KillMode=process
Environment=HOME=/home/deploy
Environment=OPENCLAW_HOME=$OPENCLAW_DATA
Environment=NODE_ENV=production
EnvironmentFile=$ENV_FILE

[Install]
WantedBy=multi-user.target
SVCEOF
            systemctl daemon-reload
            systemctl enable openclaw-gateway
            ok "Created and enabled openclaw-gateway.service"
        fi
    fi

    # Start the gateway
    if systemctl is-active --quiet openclaw-gateway; then
        skip "openclaw-gateway is already running"
    else
        if $DRY_RUN; then
            info "Would start openclaw-gateway"
        else
            systemctl start openclaw-gateway
            sleep 2
            # Health returns 503 when Control UI assets aren't built (expected),
            # but the gateway is still functional. Check for any HTTP response.
            HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' http://localhost:18789/health 2>/dev/null || echo "000")
            if [ "$HTTP_CODE" != "000" ]; then
                ok "OpenClaw gateway running (HTTP $HTTP_CODE on /health)"
            else
                warn "OpenClaw gateway started but not responding on port 18789"
                warn "Check logs: journalctl -u openclaw-gateway -n 20"
            fi
        fi
    fi
fi

# ============================================================================
# Step 16: Start the server
# ============================================================================
# Start the harness via the systemd service created in Step 13. The harness
# runs as a native binary via harness-run.sh. The first build must be done
# manually with 'just vps-build' before starting.
# ============================================================================
section "Step 16: Start the server"

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
# Step 17: Summary
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
echo -e "  2. ${YELLOW}IMPORTANT:${NC} Update your Discord OAuth App redirect URI to:"
echo "       https://$TS_DNS_NAME/auth/discord/callback"
echo "     (Discord Developer Portal > Your App > OAuth2 > Redirects)"
echo ""
echo "  3. SSH access:"
echo ""
echo "     Remote (via Tailscale, works from anywhere):"
echo "       ssh deploy@$TS_DNS_NAME"
echo ""
if [ ${#LOCAL_IPS[@]} -gt 0 ]; then
    echo "     Local (fast, bypasses Tailscale relay):"
    for local_ip in "${LOCAL_IPS[@]}"; do
        echo "       ssh -p 2222 deploy@$local_ip"
    done
    echo ""
    echo "     Recommended: add to your ~/.ssh/config for convenience:"
    echo ""
    echo "       Host cm"
    echo "         HostName ${LOCAL_IPS[0]}"
    echo "         Port 2222"
    echo "         User deploy"
    echo ""
fi
echo "  4. Visit your server:"
echo "       https://$TS_DNS_NAME"
echo ""
