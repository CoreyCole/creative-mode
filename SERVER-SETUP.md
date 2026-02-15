# Server Setup Guide

This guide walks you through setting up the Creative Mode harness on a VPS. You don't need to be a developer to follow it — every step is explained in plain English.

## Architecture overview

The harness runs as a native Go binary under systemd, with live-reload via air. Game servers run in tmux sessions managed by the harness. All traffic is routed through Tailscale — nothing is exposed to the public internet.

```
Browser → Tailscale Serve (HTTPS) → localhost:8080 (harness)
```

The harness is only accessible to tailnet members via its Tailscale Serve URL (`https://<hostname>.<tailnet>.ts.net`). No public DNS, no port opening, no reverse proxy — Tailscale handles TLS and access control.

## VPS setup

### Step 1: Create the virtual machine

**If using a Mac with UTM:**

1. Download UTM from [mac.getutm.app](https://mac.getutm.app)
2. Download the [Ubuntu 24.04 ARM64 server image](https://ubuntu.com/download/server)
3. Create a new VM: Virtualize, 16 GB RAM, 4-8 CPU cores, 80 GB disk
4. Install Ubuntu Server (minimized)

**If using a cloud VPS**, skip to Step 2.

### Step 2: Run the bootstrap script

```bash
sudo apt update && sudo apt install -y curl && curl -fsSL https://raw.githubusercontent.com/CoreyCole/creative-mode/main/scripts/vps-bootstrap.sh | sudo bash
```

The script is interactive (Tailscale auth, .env secrets) and idempotent (safe to re-run). It installs and configures:

- deploy user with passwordless sudo
- Tailscale (private networking + SSH)
- Docker Engine (for macOS local dev compatibility)
- UFW firewall (blocks all public traffic, allows tailnet)
- DOCKER-USER iptables rules (prevents Docker from bypassing UFW)
- Fail2Ban (blocks brute-force attempts)
- SSH lockdown (Tailscale-only, port 2222, no passwords)
- Nix + direnv (dev environment: Go, gcc, tmux, just, etc.)
- Rust toolchain + cargo tools (trunk, cargo-watch, wasm-bindgen-cli)
- Go tools (templ, air)
- Tailwind CSS standalone binary
- Claude Code CLI
- systemd service (auto-starts harness on boot with live-reload)
- Tailscale Serve (HTTPS → localhost:8080)
- SQLite backup cron (daily, 7-day rotation)

Preview what the script does without changing anything:

```bash
sudo bash scripts/vps-bootstrap.sh --check
```

### Step 3: Build and start the harness

```bash
su - deploy
cd ~/creative-mode/harness
just vps-build
sudo systemctl start creative-mode
```

Verify:

```bash
just vps-status
curl http://localhost:8080
```

### Step 4: Verify HTTPS access

Open `https://<hostname>.<tailnet>.ts.net` in your browser (on a device that's on your Tailscale network). You should see the Creative Mode login page.

Sign in with GitHub. If the login works, everything is configured correctly.

## Day-to-day operations

### Updating

```bash
cd ~/creative-mode/harness
just vps-deploy  # git pull + build + restart
```

Or equivalently: `just redeploy`

### Logs

```bash
just vps-logs  # journalctl -u creative-mode -f
```

### Live-reload

The harness runs under air — editing `.go`, `.templ`, or `.css` files triggers an automatic rebuild and restart. No manual steps needed during development.

### Status

```bash
just vps-status  # systemd service status
just status      # service + Tailscale status
```

## How the security layers work

### Tailscale (private network)

Tailscale creates an encrypted WireGuard tunnel between your devices. Only people you invite to your tailnet can reach the harness. The free plan supports up to 3 users and 100 devices.

### Tailscale Serve (HTTPS)

Tailscale Serve provides automatic HTTPS with valid TLS certificates for the harness. No Let's Encrypt, no cert management, no port opening — it just works for anyone on the tailnet.

### Firewall (UFW)

Blocks all incoming traffic by default. Nothing is open to the public internet — all traffic comes through Tailscale.

### SSH lockdown

SSH is moved to port 2222, bound to the Tailscale IP only, and requires keys (no passwords). Unreachable from the public internet.

### Fail2Ban

Watches for repeated failed login attempts and auto-blocks offending IPs.

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

### Harness not loading

```bash
just vps-status
just vps-logs
tailscale serve status
```

### GitHub login fails

Make sure your GitHub OAuth App callback URL matches your Tailscale Serve URL exactly:

```
https://your-machine.tailnet-name.ts.net/auth/github/callback
```

Also check that `HARNESS_URL` in `harness/.env` matches the same URL (without the callback path).

### "Cannot detect public network interface"

The bootstrap script needs a default network route:

```bash
ip route show default
```

If empty, your network isn't configured. On a UTM VM, make sure the network adapter is set to NAT.

### Build takes too long or runs out of memory

The first build compiles Rust toolchains and Go dependencies. Make sure the VM has at least 16 GB of RAM.

### Server doesn't start after reboot

```bash
sudo systemctl status creative-mode
sudo journalctl -u creative-mode -n 50
```

### Can't SSH into the server

After bootstrap, SSH is Tailscale-only. Use Tailscale SSH:

```bash
ssh deploy@your-machine
```

---

## Disclaimer

Creative Mode is experimental software. The mayor is learning on the job and there is always a chance it borks your machine.

**Do not run this on your personal computer.** Use a dedicated virtual machine or cloud instance. If something goes wrong, you can roll back a VM snapshot or spin up a fresh server.

**Make backups.** Take VM snapshots before major changes. Keep your `.env` file and database backups somewhere safe.
