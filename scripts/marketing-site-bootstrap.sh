#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Creative Mode — Marketing Site Bootstrap Script
# =============================================================================
#
# Secures and configures an EC2 instance that hosts public-facing sites.
# Adapted from vps-bootstrap.sh for servers that need specific ports open to
# the internet (e.g. web servers, API endpoints).
#
# What this script does:
#   1.  Installs prerequisites (git, curl, unzip)
#   2.  Installs Tailscale (private networking)
#   3.  Connects to your Tailscale network (interactive)
#   4.  Enables Tailscale SSH
#   5.  Installs Docker Engine
#   6.  Configures UFW firewall (port 80 + optional extra ports)
#   7.  Adds DOCKER-USER iptables rules (prevents Docker from bypassing UFW)
#   8.  Creates Docker daemon config (security + logging)
#   9.  Installs Fail2Ban
#   10. Locks down SSH (Tailscale-only, port 2222, no passwords)
#   11. Adds ubuntu user to docker group
#   12. Installs systemd service (auto-start on boot)
#   13. Installs Go
#   14. Installs Node.js + pnpm (for Tailwind CSS builds)
#   15. Installs templ + just
#   16. Builds site binary
#   17. Creates env file
#   18. Prints summary and next steps
#
# Usage:
#   sudo bash scripts/marketing-site-bootstrap.sh                          # just port 80
#   sudo bash scripts/marketing-site-bootstrap.sh --port 3000 --port 4242  # + extra ports
#   sudo bash scripts/marketing-site-bootstrap.sh --check                  # dry run
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
DRY_RUN=false
EXTRA_PORTS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --check|--dry-run)
            DRY_RUN=true
            shift
            ;;
        --port)
            if [[ -z "${2:-}" ]]; then
                echo "Error: --port requires a value"
                exit 1
            fi
            if ! [[ "$2" =~ ^[0-9]+$ ]] || [ "$2" -lt 1 ] || [ "$2" -gt 65535 ]; then
                echo "Error: --port value must be a number between 1 and 65535"
                exit 1
            fi
            EXTRA_PORTS+=("$2")
            shift 2
            ;;
        *)
            echo "Unknown argument: $1"
            echo "Usage: sudo bash scripts/marketing-site-bootstrap.sh [--check] [--port PORT]..."
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
    echo "WARNING: This script is designed for Ubuntu. Proceed with caution."
fi

info "Open ports: 80/tcp${EXTRA_PORTS[*]:+ ${EXTRA_PORTS[*]}}"

# ============================================================================
# Step 1: Install prerequisites
# ============================================================================
section "Step 1: Install prerequisites"

if command -v git &>/dev/null && command -v curl &>/dev/null && command -v unzip &>/dev/null && command -v jq &>/dev/null; then
    skip "Prerequisites already installed (git, curl, unzip, jq)"
else
    if $DRY_RUN; then
        info "Would apt-get update and install git, curl, unzip, jq"
    else
        apt-get update
        apt-get install -y git curl unzip jq
        ok "Installed prerequisites (git, curl, unzip, jq)"
    fi
fi

# ============================================================================
# Step 2: Install Tailscale
# ============================================================================
# Tailscale creates an encrypted WireGuard tunnel between your devices.
# Only people you invite to your tailnet can reach the management interface.
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
# Interactive — prints a URL you need to open in your browser to authorize
# this machine on your Tailscale account.
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
# without managing SSH keys. After Step 10 locks down SSH, this is the
# only way to get shell access.
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
# Docker runs containers for the marketing site. Install from the official
# Docker apt repository with architecture auto-detection.
# ============================================================================
section "Step 5: Install Docker Engine"

if command -v docker &>/dev/null; then
    skip "Docker is already installed"
else
    if $DRY_RUN; then
        info "Would install Docker Engine from official Docker apt repository"
    else
        apt-get update
        apt-get install -y ca-certificates curl

        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc

        ARCH=$(dpkg --print-architecture)
        CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
        echo "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $CODENAME stable" \
            > /etc/apt/sources.list.d/docker.list

        apt-get update
        apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

        systemctl enable --now docker

        ok "Installed Docker Engine ($ARCH)"
    fi
fi

# ============================================================================
# Step 6: Configure UFW firewall
# ============================================================================
# Unlike the VPS script (which blocks ALL incoming), this server needs
# specific ports open to the public internet:
#   - Port 80: Marketing site + webhook (creative-mode.ai)
#   - Port 22: Kept open temporarily until Tailscale SSH is verified
#   - Extra --port args: co-hosted services (e.g. 3000, 4242)
#   - tailscale0: all tailnet traffic
# ============================================================================
section "Step 6: Configure UFW firewall"

if ufw status | grep -q "Status: active"; then
    skip "UFW is already active"
else
    if $DRY_RUN; then
        info "Would configure UFW:"
        info "  default deny incoming"
        info "  default allow outgoing"
        info "  allow in on tailscale0"
        info "  allow 80/tcp"
        info "  allow 22/tcp (temporary — until Tailscale SSH verified)"
        for port in "${EXTRA_PORTS[@]}"; do
            info "  allow ${port}/tcp"
        done
    else
        ufw default deny incoming
        ufw default allow outgoing
        ufw allow in on tailscale0
        ufw allow 80/tcp
        ufw allow 22/tcp
        for port in "${EXTRA_PORTS[@]}"; do
            ufw allow "${port}/tcp"
        done
        ufw --force enable
        ok "UFW enabled — ports: 80, 22${EXTRA_PORTS[*]:+, ${EXTRA_PORTS[*]}} + tailscale0"
    fi
fi

# ============================================================================
# Step 7: Add DOCKER-USER iptables rules
# ============================================================================
# Docker bypasses UFW by default. The DOCKER-USER chain runs before Docker's
# own rules so we can enforce our firewall policy.
#
# Rules allow:
#   - Established connections (so responses work)
#   - All Tailscale traffic
#   - Port 80 from the public interface
#   - Each --port arg from the public interface
#   - DROP everything else from the public interface
# ============================================================================
section "Step 7: DOCKER-USER iptables rules"

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
        info "  - Allow port 80 from $PUBLIC_IF"
        for port in "${EXTRA_PORTS[@]}"; do
            info "  - Allow port $port from $PUBLIC_IF"
        done
        info "  - Drop all other traffic from $PUBLIC_IF"
    else
        # Build the port-allow rules
        PORT_RULES="-A DOCKER-USER -i $PUBLIC_IF -p tcp --dport 80 -j RETURN"
        for port in "${EXTRA_PORTS[@]}"; do
            PORT_RULES="$PORT_RULES
-A DOCKER-USER -i $PUBLIC_IF -p tcp --dport $port -j RETURN"
        done

        cat >> /etc/ufw/after.rules << EOF

# Creative Mode Marketing Site: Prevent Docker from bypassing UFW.
# Allow specific ports through, drop everything else from the public interface.
*filter
:DOCKER-USER - [0:0]
-A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
-A DOCKER-USER -i tailscale0 -j RETURN
$PORT_RULES
-A DOCKER-USER -i $PUBLIC_IF -j DROP
COMMIT
EOF
        ok "Added DOCKER-USER rules (allow 80${EXTRA_PORTS[*]:+/${EXTRA_PORTS[*]}} from $PUBLIC_IF, drop rest)"
    fi
fi

# Reload UFW to apply the new rules
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
# Security and reliability settings:
#   live-restore: true       — Containers survive daemon restarts
#   userland-proxy: false    — Use iptables instead (faster, no extra processes)
#   no-new-privileges: true  — Prevent privilege escalation in containers
#   log-driver + log-opts    — 10 MB log files, keep last 3
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

        systemctl restart docker

        ok "Created /etc/docker/daemon.json"
    fi
fi

# ============================================================================
# Step 9: Install Fail2Ban
# ============================================================================
# Watches for repeated failed login attempts and blocks offending IPs.
# Extra defense layer on top of SSH lockdown.
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
# SSH hardening:
#   ListenAddress {tailscale_ip} — SSH only on Tailscale, unreachable from internet
#   Port 2222                    — Non-standard port stops automated scanners
#   PermitRootLogin no           — Must use regular user + sudo
#   PasswordAuthentication no    — Keys or Tailscale SSH only
#
# WARNING: Verify Tailscale SSH works before disconnecting your current session!
# ============================================================================
section "Step 10: Lock down SSH"

SSHD_CONFIG="/etc/ssh/sshd_config"

if grep -q "^Port 2222" "$SSHD_CONFIG" 2>/dev/null; then
    skip "SSH config already locked down (Port 2222 in sshd_config)"
else
    if $DRY_RUN; then
        TS_IP=$(tailscale ip -4 2>/dev/null || echo "<tailscale-ip>")
        info "Would lock down sshd:"
        info "  ListenAddress $TS_IP"
        info "  Port 2222"
        info "  PermitRootLogin no"
        info "  PasswordAuthentication no"
    else
        TS_IP=$(tailscale ip -4)
        if [ -z "$TS_IP" ]; then
            fail "Cannot get Tailscale IP — is Tailscale connected?"
            exit 1
        fi

        cp "$SSHD_CONFIG" "${SSHD_CONFIG}.bak.$(date +%Y%m%d)"

        cat >> "$SSHD_CONFIG" << EOF

# Creative Mode: SSH lockdown — only accessible via Tailscale
ListenAddress $TS_IP
Port 2222
PermitRootLogin no
PasswordAuthentication no
EOF
        ok "SSH config written"

        echo ""
        echo -e "  ${RED}${BOLD}WARNING: Verify Tailscale SSH works before disconnecting!${NC}"
        echo -e "  ${YELLOW}Open a NEW terminal and test:${NC}"
        echo -e "    ssh ubuntu@$(tailscale status --json 2>/dev/null | jq -r '.Self.DNSName' | sed 's/\.$//' || echo '<tailscale-hostname>')"
        echo -e "  ${YELLOW}Only after confirming access, restart SSH:${NC}"
        echo -e "    sudo systemctl restart ssh"
        echo ""
    fi
fi

# Do NOT auto-restart SSH — user must verify Tailscale access first
if ! $DRY_RUN && grep -q "^Port 2222" "$SSHD_CONFIG" 2>/dev/null; then
    if ss -tlnp | grep -q ':2222'; then
        ok "SSH already listening on port 2222"
    else
        info "SSH config updated but NOT restarted — verify Tailscale SSH first, then run:"
        info "  sudo systemctl restart ssh"
    fi
fi

# ============================================================================
# Step 11: Add ubuntu user to docker group
# ============================================================================
section "Step 11: Add ubuntu to docker group"

if id -nG ubuntu 2>/dev/null | grep -qw docker; then
    skip "User 'ubuntu' is already in the docker group"
else
    if $DRY_RUN; then
        info "Would add 'ubuntu' to the docker group"
    else
        usermod -aG docker ubuntu
        ok "Added 'ubuntu' to docker group"
    fi
fi

# ============================================================================
# Step 12: Install systemd service
# ============================================================================
# Creates a systemd service that auto-starts the marketing site on boot.
# The service runs the site binary directly (no Docker in production).
# ============================================================================
section "Step 12: Install systemd service"

SERVICE_SRC="/home/ubuntu/creative-mode/site/creative-mode-site.service"
SERVICE_DST="/etc/systemd/system/creative-mode-site.service"

if [ -f "$SERVICE_DST" ]; then
    # Always update in case the service file changed
    if $DRY_RUN; then
        info "Would update creative-mode-site.service"
    else
        cp "$SERVICE_SRC" "$SERVICE_DST"
        systemctl daemon-reload
        ok "Updated creative-mode-site.service"
    fi
else
    if $DRY_RUN; then
        info "Would copy creative-mode-site.service to /etc/systemd/system/"
        info "Would run systemctl daemon-reload"
        info "Would run systemctl enable creative-mode-site.service"
    else
        if [ ! -f "$SERVICE_SRC" ]; then
            fail "Service file not found at $SERVICE_SRC"
            info "Make sure the repo is cloned to /home/ubuntu/creative-mode"
            exit 1
        fi
        cp "$SERVICE_SRC" "$SERVICE_DST"
        systemctl daemon-reload
        systemctl enable creative-mode-site.service
        ok "Installed and enabled creative-mode-site.service"
    fi
fi

# ============================================================================
# Step 13: Install Go
# ============================================================================
# The site binary is built natively (no Docker in production).
# Download Go from go.dev and install to /usr/local/go.
# ============================================================================
section "Step 13: Install Go"

GO_VERSION="1.24.3"

if command -v go &>/dev/null && go version | grep -q "go${GO_VERSION}"; then
    skip "Go ${GO_VERSION} is already installed"
else
    if $DRY_RUN; then
        info "Would download and install Go ${GO_VERSION} from go.dev"
    else
        ARCH=$(dpkg --print-architecture)
        GO_ARCH="$ARCH"
        if [ "$ARCH" = "amd64" ]; then GO_ARCH="amd64"; fi
        if [ "$ARCH" = "arm64" ]; then GO_ARCH="arm64"; fi

        curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tar.gz
        rm -rf /usr/local/go
        tar -C /usr/local -xzf /tmp/go.tar.gz
        rm /tmp/go.tar.gz

        # Ensure Go is on PATH for all users
        if [ ! -f /etc/profile.d/go.sh ]; then
            cat > /etc/profile.d/go.sh << 'GOEOF'
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
GOEOF
        fi
        export PATH=$PATH:/usr/local/go/bin

        ok "Installed Go ${GO_VERSION} ($GO_ARCH)"
    fi
fi

# Ensure Go is on PATH for remaining steps
export PATH=$PATH:/usr/local/go/bin:/root/go/bin:/home/ubuntu/go/bin

# ============================================================================
# Step 14: Install Node.js + pnpm
# ============================================================================
# Node.js and pnpm are needed for Tailwind CSS builds during self-rebuild.
# ============================================================================
section "Step 14: Install Node.js + pnpm"

if command -v node &>/dev/null && command -v pnpm &>/dev/null; then
    skip "Node.js and pnpm are already installed"
else
    if $DRY_RUN; then
        info "Would install Node.js LTS via NodeSource and pnpm via corepack"
    else
        # Install Node.js LTS
        if ! command -v node &>/dev/null; then
            curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
            apt-get install -y nodejs
            ok "Installed Node.js $(node --version)"
        fi

        # Enable corepack and install pnpm
        if ! command -v pnpm &>/dev/null; then
            corepack enable
            corepack prepare pnpm@latest --activate
            ok "Installed pnpm $(pnpm --version)"
        fi
    fi
fi

# ============================================================================
# Step 15: Install templ + just
# ============================================================================
# templ generates Go code from .templ files.
# just is a command runner used for build-tailwind.
# ============================================================================
section "Step 15: Install templ + just"

if command -v templ &>/dev/null; then
    skip "templ is already installed"
else
    if $DRY_RUN; then
        info "Would install templ via 'go install'"
    else
        GOBIN=/usr/local/bin go install github.com/a-h/templ/cmd/templ@latest
        ok "Installed templ"
    fi
fi

if command -v just &>/dev/null; then
    skip "just is already installed"
else
    if $DRY_RUN; then
        info "Would install just"
    else
        curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin
        ok "Installed just"
    fi
fi

# ============================================================================
# Step 16: Build site binary
# ============================================================================
# Build the site binary and place it at /tmp/creative-mode-site,
# matching the path in the systemd service file.
# ============================================================================
section "Step 16: Build site binary"

SITE_DIR="/home/ubuntu/creative-mode/site"

if [ -f /tmp/creative-mode-site ]; then
    skip "Site binary already exists at /tmp/creative-mode-site"
else
    if $DRY_RUN; then
        info "Would build site binary:"
        info "  cd $SITE_DIR"
        info "  pnpm install"
        info "  templ generate"
        info "  just build-tailwind"
        info "  go build -o /tmp/creative-mode-site ."
    else
        cd "$SITE_DIR"
        pnpm install
        templ generate
        just build-tailwind
        go build -o /tmp/creative-mode-site .
        ok "Built site binary at /tmp/creative-mode-site"
    fi
fi

# ============================================================================
# Step 17: Create env file
# ============================================================================
# The env file holds all secrets and configuration.
# ============================================================================
section "Step 17: Create env file"

ENV_DIR="/home/ubuntu/.config/creative-mode"
ENV_FILE="$ENV_DIR/site.env"

if [ -f "$ENV_FILE" ]; then
    skip "Env file already exists at $ENV_FILE"
else
    if $DRY_RUN; then
        info "Would create $ENV_FILE with placeholder values"
    else
        mkdir -p "$ENV_DIR"
        cat > "$ENV_FILE" << 'ENVEOF'
# Creative Mode Site — Environment Variables
# Generate WEBHOOK_SECRET with: openssl rand -hex 32
WEBHOOK_SECRET=CHANGE_ME
DISCORD_CLIENT_ID=
DISCORD_CLIENT_SECRET=
DISCORD_REDIRECT_URI=
DISCORD_BOT_TOKEN=
DISCORD_GUILD_ID=
DISCORD_WORLDS_CATEGORY_ID=
ANTHROPIC_API_KEY=
INVITE_CODES=
ENVEOF
        chown ubuntu:ubuntu "$ENV_DIR" "$ENV_FILE"
        chmod 600 "$ENV_FILE"
        ok "Created $ENV_FILE (chmod 600)"
    fi
fi

# ============================================================================
# Step 18: Summary
# ============================================================================
section "Bootstrap Complete"

# Build ports display string
PORTS_DISPLAY="80"
for port in "${EXTRA_PORTS[@]}"; do
    PORTS_DISPLAY="$PORTS_DISPLAY, $port"
done

echo ""
echo -e "${GREEN}${BOLD}What was done:${NC}"
echo "  - prerequisites: git, curl, unzip, jq"
echo "  - Tailscale: installed and connected"
echo "  - Tailscale SSH: enabled"
echo "  - Docker Engine: installed"
echo "  - UFW firewall: deny incoming, allow ports $PORTS_DISPLAY + tailscale0"
echo "  - DOCKER-USER rules: allow ports $PORTS_DISPLAY from ${PUBLIC_IF}, drop rest"
echo "  - Docker daemon: live-restore, no-new-privileges, log rotation"
echo "  - Fail2Ban: installed and running"
echo "  - SSH: Tailscale-only ($(tailscale ip -4 2>/dev/null || echo 'N/A')), port 2222, no passwords"
echo "  - ubuntu user: added to docker group"
echo "  - systemd service: creative-mode-site.service enabled"
echo "  - Go ${GO_VERSION}: installed"
echo "  - Node.js + pnpm: installed"
echo "  - templ + just: installed"
echo "  - site binary: built at /tmp/creative-mode-site"
echo "  - env file: ~/.config/creative-mode/site.env"
echo ""
echo -e "${YELLOW}${BOLD}Detected public interface:${NC} $PUBLIC_IF"
echo -e "${YELLOW}${BOLD}Public ports:${NC} $PORTS_DISPLAY"
echo -e "${YELLOW}${BOLD}Tailscale IP:${NC} $(tailscale ip -4 2>/dev/null || echo 'N/A')"
echo ""
echo -e "${BOLD}Firewall status:${NC}"
if ! $DRY_RUN; then
    ufw status numbered
fi
echo ""
echo -e "${BOLD}Next steps:${NC}"
echo "  1. Open a NEW terminal and verify Tailscale SSH:"
echo "       ssh ubuntu@$(tailscale status --json 2>/dev/null | jq -r '.Self.DNSName' | sed 's/\.$//' || echo '<tailscale-hostname>')"
echo ""
echo "  2. Once Tailscale SSH is confirmed, restart SSH to apply lockdown:"
echo "       sudo systemctl restart ssh"
echo ""
echo "  3. After SSH restart, remove the temporary port 22 rule:"
echo "       sudo ufw delete allow 22/tcp"
echo ""
echo "  4. Edit the env file with your secrets:"
echo "       nano ~/.config/creative-mode/site.env"
echo "       # Set WEBHOOK_SECRET to: openssl rand -hex 32"
echo ""
echo "  5. Start the marketing site:"
echo "       sudo systemctl start creative-mode-site"
echo ""
echo "  6. Add GitHub webhook (repo Settings > Webhooks):"
echo "       URL: http://<public-ip>/webhook/github"
echo "       Content type: application/json"
echo "       Secret: (same value from site.env)"
echo "       Events: Just the push event"
echo ""
echo -e "  ${YELLOW}IMPORTANT:${NC} Do NOT close this terminal until you've verified"
echo "  Tailscale SSH access in a separate session."
echo ""
