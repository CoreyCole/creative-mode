# Server Setup Guide

This guide walks you through setting up Creative Mode's two-server deployment. You don't need to be a developer to follow it — every step is explained in plain English.

## Architecture overview

Creative Mode runs on two servers connected by a Tailscale private network (tailnet):

| Server | Role | Software |
|--------|------|----------|
| **EC2 instance** (on tailnet) | Marketing site (`creative-mode.ai`) | Docker |
| **Harness VPS** (on tailnet) | Game server (`https://<hostname>.ts.net`) | Nix, direnv, Docker |

### How traffic flows

**Marketing site** (`creative-mode.ai`):
```
Browser → Route 53 → API Gateway (TLS) → EC2 port 80 → Docker site container
```

**Game app** (harness):
```
Browser → Tailscale Serve (HTTPS) → localhost:8080 (harness)
```

The harness is only accessible to tailnet members via its Tailscale Serve URL (`https://<hostname>.<tailnet>.ts.net`). No public DNS, no port opening, no reverse proxy — Tailscale handles TLS and access control.

### Onboarding flow

1. User visits `creative-mode.ai` (marketing site on EC2)
2. Clicks "Meet the Mayor" — Discord OAuth login
3. Enters an invite code
4. Chats with the mayor (Claude) to design their world
5. World hatches — user is redirected to the harness Tailscale Serve URL

## EC2 setup (marketing site)

### Step 1: Launch EC2 instance

Launch an Ubuntu 24.04 instance. The bootstrap script handles the rest.

### Step 2: Clone the repo and run the bootstrap script

```bash
sudo apt update && sudo apt install -y git
git clone https://github.com/CoreyCole/creative-mode.git /home/ubuntu/creative-mode
cd /home/ubuntu/creative-mode
sudo bash scripts/marketing-site-bootstrap.sh
```

The script installs and configures:
- Tailscale (private networking)
- Docker Engine (runs the marketing site container on port 80)
- UFW firewall (deny all incoming except port 80 + tailnet)
- DOCKER-USER iptables rules (prevents Docker from bypassing UFW)
- Fail2Ban (blocks brute-force attempts)
- SSH lockdown (Tailscale-only, port 2222, no passwords)
- systemd service (auto-starts Docker site on boot)

Preview what the script does without changing anything:

```bash
sudo bash scripts/marketing-site-bootstrap.sh --check
```

### Step 3: Start the marketing site

```bash
sudo systemctl start creative-mode-site
```

Verify:

```bash
systemctl status creative-mode-site
curl http://localhost
```

## Harness VPS setup (game server)

### Step 1: Create the virtual machine

**If using a Mac with UTM:**

1. Download UTM from [mac.getutm.app](https://mac.getutm.app)
2. Download the [Ubuntu 24.04 ARM64 server image](https://ubuntu.com/download/server)
3. Create a new VM: Virtualize, 16 GB RAM, 4-8 CPU cores, 80 GB disk
4. Install Ubuntu Server (minimized)

**If using a cloud VPS**, skip to Step 2.

### Step 2: Run the VPS bootstrap script

```bash
sudo apt update && sudo apt install -y curl && curl -fsSL https://raw.githubusercontent.com/CoreyCole/creative-mode/main/scripts/vps-bootstrap.sh | sudo bash
```

This secures the VM: Tailscale, firewall (blocks all public traffic), Docker, SSH lockdown, Fail2Ban.

### Step 3: Switch to the deploy user

```bash
su - deploy
```

### Step 4: Install Nix

```bash
sh <(curl -L https://nixos.org/nix/install) --daemon
```

Log out and back in, then verify:

```bash
exit
su - deploy
nix --version
```

### Step 5: Enable Flakes

```bash
echo "experimental-features = nix-command flakes" | sudo tee -a /etc/nix/nix.conf
sudo systemctl restart nix-daemon
```

### Step 6: Install direnv

```bash
nix profile install nixpkgs#direnv
echo 'eval "$(direnv hook bash)"' >> ~/.bashrc
source ~/.bashrc
```

### Step 7: Activate the dev environment

```bash
cd ~/creative-mode
direnv allow
```

Verify:

```bash
which just && which jq && which sqlite3
```

### Step 8: Create the .env file

```bash
cp harness/.env.example harness/.env
nano harness/.env
```

Fill in:

- **`DISCORD_CLIENT_ID`** and **`DISCORD_CLIENT_SECRET`** — from your Discord application (Discord Developer Portal > Applications > OAuth2)
- **`ANTHROPIC_API_KEY`** — from [console.anthropic.com](https://console.anthropic.com)
- **`HARNESS_URL`** — your Tailscale Serve URL, e.g., `https://your-machine.tailnet-name.ts.net`
- **`CM_HOOK_SECRET`** — generate with: `openssl rand -hex 32`

Set your Discord OAuth2 redirect URL to:

```
https://your-machine.tailnet-name.ts.net/auth/discord/callback
```

Replace `your-machine.tailnet-name.ts.net` with your actual Tailscale machine URL. Find it with `tailscale status`.

### Step 9: Start the harness

```bash
cd ~/creative-mode/harness
just up
```

Verify:

```bash
docker compose ps
```

### Step 10: Set up Tailscale Serve for HTTPS

Tailscale Serve gives the harness a real HTTPS URL with a valid certificate — no configuration needed:

```bash
sudo tailscale serve --bg 8080
```

This tells Tailscale to forward HTTPS traffic to the harness on port 8080. The URL will be:

```
https://your-machine.tailnet-name.ts.net
```

Only people on your tailnet can reach it. TLS certificates are managed automatically by Tailscale. This setting persists across reboots — you only need to run it once.

### Step 11: Verify everything works

Open the Tailscale URL in your browser (on a device that's on your Tailscale network). You should see the Creative Mode login page.

Sign in with Discord. If the login works, everything is configured correctly.

## How the security layers work

### Tailscale (private network)

Tailscale creates an encrypted WireGuard tunnel between your devices. Only people you invite to your tailnet can reach the harness. The free plan supports up to 3 users and 100 devices.

### Tailscale Serve (HTTPS)

Tailscale Serve provides automatic HTTPS with valid TLS certificates for your harness. No Let's Encrypt, no cert management, no port opening — it just works for anyone on the tailnet.

### Firewall (UFW)

Blocks all incoming traffic by default. On the EC2, port 80 is open for the marketing site. On the harness VPS, nothing is open to the public internet — all traffic comes through Tailscale.

### Docker isolation

The game runs inside Docker containers — sandboxed from the host system. DOCKER-USER iptables rules prevent Docker from bypassing UFW.

### SSH lockdown

SSH is moved to port 2222, bound to the Tailscale IP only, and requires keys (no passwords). Unreachable from the public internet.

### Fail2Ban

Watches for repeated failed login attempts and auto-blocks offending IPs.

## DNS setup (Route 53)

| Record | Type | Value | Notes |
|--------|------|-------|-------|
| `creative-mode.ai` | A | API Gateway IP | Public — routes through API GW for TLS |

The harness doesn't need a public DNS record. Tailscale Serve provides its own `*.ts.net` hostname with automatic TLS.

## Updating

### EC2 (marketing site)

```bash
cd ~/creative-mode && git pull
sudo systemctl restart creative-mode-site
```

### Harness VPS

```bash
cd ~/creative-mode/harness
just redeploy
```

## Backups

- **Database:** SQLite is backed up daily by a cron job. Backups in `~/backups/`, rotated after 7 days.
- **Code:** Everything is in git.

### VM snapshots (UTM users)

```bash
brew install qemu
DISK=~/Library/Containers/com.utmapp.UTM/Data/Documents/MyVM.utm/Data/DiskImage.qcow2
qemu-img snapshot -c "before-update" "$DISK"   # create
qemu-img snapshot -l "$DISK"                    # list
qemu-img snapshot -a "before-update" "$DISK"    # restore
```

Exclude the UTM directory from Time Machine:
```
System Settings > General > Time Machine > Options > Exclude:
  ~/Library/Containers/com.utmapp.UTM/Data/Documents/
```

## Troubleshooting

### "Permission denied" when running Docker commands

Log out and back in after being added to the docker group:

```bash
exit
su - deploy
```

### Marketing site not loading

Check the Docker service on EC2:

```bash
systemctl status creative-mode-site
curl http://localhost
```

### Harness not loading

Check Tailscale Serve and Docker on the harness VPS:

```bash
tailscale serve status
docker compose ps
```

### Discord login fails

Make sure your Discord OAuth2 redirect URL matches your Tailscale Serve URL exactly:

```
https://your-machine.tailnet-name.ts.net/auth/discord/callback
```

Also check that `HARNESS_URL` in `harness/.env` matches the same URL (without the callback path).

### "Cannot detect public network interface"

The bootstrap script needs a default network route:

```bash
ip route show default
```

If empty, your network isn't configured. On a UTM VM, make sure the network adapter is set to NAT.

### Build takes too long or runs out of memory

The first build compiles the entire Rust/Go toolchain inside Docker. This can take 10-30 minutes. Make sure the VM has at least 16 GB of RAM.

### Server doesn't start after reboot

```bash
sudo systemctl status creative-mode      # harness VPS
sudo systemctl status creative-mode-site  # EC2
sudo journalctl -u creative-mode -n 50
```

### Can't SSH into the server

After bootstrap, SSH is Tailscale-only. Use Tailscale SSH:

```bash
ssh deploy@your-machine  # harness VPS
ssh ubuntu@your-machine  # EC2
```

---

## Disclaimer

Creative Mode is experimental software. The mayor is learning on the job and there is always a chance it borks your machine.

**Do not run this on your personal computer.** Use a dedicated virtual machine or cloud instance. If something goes wrong, you can roll back a VM snapshot or spin up a fresh server.

**Make backups.** Take VM snapshots before major changes. Keep your `.env` file and database backups somewhere safe.
