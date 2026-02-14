# Server Setup Guide

This guide walks you through setting up a private Creative Mode server that only invited people can access. You don't need to be a developer to follow it — every step is explained in plain English.

## What this guide covers

We're going to turn a blank Ubuntu computer (either a virtual machine on your Mac or a rented server in the cloud) into a secure game server. When we're done:

- The server will be invisible to the public internet
- Only people you personally invite can connect
- The game runs in an isolated sandbox
- Everything restarts automatically if something crashes
- The database is backed up daily

## How the server is secured

Before we start, here's what each security layer does and why it matters.

### Tailscale (private network)

Think of Tailscale as a secret tunnel between your devices. When you install Tailscale on your computer and on the server, they can talk to each other through an encrypted tunnel — but nobody else on the internet can see or reach the server. It's like having a private phone line that only rings for people you've given the number to.

You invite friends by having them install the Tailscale app and join your network (called a "tailnet"). The free plan supports up to 3 users and 100 devices.

### Firewall (UFW)

A firewall is like a bouncer at a club. It stands at the door and checks every incoming connection. Our firewall is set to "deny all" by default — it blocks everything from the public internet. The only exception is the Tailscale tunnel, which gets a permanent spot on the guest list.

### Docker isolation

The game runs inside something called a Docker container. Think of it as a sandbox — the game has its own little world with its own files and settings, completely separate from the rest of the server. Even if something goes wrong inside the game, it can't affect the server itself.

There's a catch though: Docker normally creates its own door to the outside, bypassing the firewall. We fix this with special rules (called DOCKER-USER rules) that force Docker to respect the firewall. No sneaking past the bouncer.

### SSH lockdown

SSH is the way administrators remotely control the server — think of it as the "admin door." We lock it down by:

- **Moving it to a non-standard location** (port 2222 instead of the usual 22). Most automated attackers only try the default door.
- **Making it Tailscale-only** — the admin door only exists on the private network. It's literally unreachable from the public internet.
- **Requiring keys, not passwords** — instead of typing a password (which can be guessed), you need a digital key file that's virtually impossible to forge.

### Fail2Ban

Even with all the above, someone on the private network could theoretically try to guess passwords. Fail2Ban watches for repeated failed login attempts and automatically blocks the source. It's like a bouncer who kicks out anyone who keeps showing the wrong ID.

### Automatic backups

The game's database (where all the world data lives) is automatically backed up every day. Old backups are cleaned up after 7 days so they don't fill up the disk. The backup uses SQLite's built-in snapshot feature, which means it creates a perfect copy even while the game is running.

### Auto-restart

If the game server crashes, Docker automatically restarts it. When the whole machine reboots (after a power outage or update), a system service starts the game server automatically. You don't need to manually intervene.

## What you need

- **A computer to run the server on** — either:
  - A local Ubuntu virtual machine, OR
  - A cloud VPS (rented server) from any provider.
- **Ubuntu 24.04 Server Edition** (the operating system the server runs)
  - On M-series macOS, install the \[ARM 64-bit Ubuntu 24.04 Server Edition(https://ubuntu.com/download/server/arm)
  - On intel machines, install the [x86-64 version](https://ubuntu.com/download/server)
- **An internet connection**
- **A Tailscale account** (free at [tailscale.com](https://tailscale.com))
- **A GitHub account** (for login authentication)

## Step-by-step setup

### Step 1: Create the virtual machine

**If you're using a cloud VPS**, skip to Step 2 — your provider gives you a ready-to-use Ubuntu machine.

**If you're using a Mac with UTM:**

1. Download UTM from [mac.getutm.app](https://mac.getutm.app) (free from GitHub releases)
1. Download the Ubuntu 24.04 ARM64 server image from [ubuntu.com/download/server](https://ubuntu.com/download/server)
1. In UTM, create a new virtual machine:
   - Choose "Virtualize"
   - Select the Ubuntu ISO you downloaded
   - Give it 16 GB of RAM and 4-8 CPU cores
   - Set the disk size to 80 GB (it only uses space as needed)
   - Network: leave as default (NAT / "Emulated VLAN")
1. Start the VM and follow the Ubuntu installer
   - Choose "Ubuntu Server (minimized)" for a lean install
   - Create your initial user account and password
1. When installation finishes, restart the VM

> **What just happened?** You created a virtual computer running Ubuntu inside your rig. It has its own operating system, its own network connection, and its own disk — completely separate from macOS. Tailscale will make this virtual machine accessible to your friends as if it were a real server on the internet.

> **Note:** The UTM console window does not support copy/paste — you'll need to type the next command manually. Once the bootstrap script sets up Tailscale SSH, you can SSH in from your Mac terminal and get normal clipboard support for the remaining steps.

### Step 2: Run the bootstrap script

This single command sets up everything — prerequisites, security, and the game server code. You can pipe it directly on a fresh instance:

```bash
sudo apt update && sudo apt install -y curl && curl -fsSL https://raw.githubusercontent.com/CoreyCole/creative-mode/main/scripts/vps-bootstrap.sh | sudo bash
```

The script will:

1. **Install prerequisites** (git, curl, sqlite3)
1. **Create a 'deploy' user** and clone the repository to `~deploy/creative-mode`
1. **Install and connect Tailscale** — it will print a URL. Open that URL in your browser and sign in to authorize this machine on your Tailscale network
1. **Install Docker, the firewall, Fail2Ban**, and configure everything else automatically

You can preview what the script will do without actually changing anything:

```bash
curl -fsSL https://raw.githubusercontent.com/CoreyCole/creative-mode/main/scripts/vps-bootstrap.sh | sudo bash -s -- --check
```

If you've already cloned the repo, you can run the script directly:

```bash
sudo bash scripts/vps-bootstrap.sh
```

When the script finishes, it prints a summary and next steps.

> **What just happened?** The server is now secured. The firewall blocks all public traffic, Docker is sandboxed, SSH is locked to Tailscale only, and the machine is connected to your private Tailscale network. The only way to reach this server is through Tailscale.

### Step 3: Switch to the deploy user

```bash
su - deploy
```

From now on, everything runs as the `deploy` user (not root).

### Step 4: Install Nix

Nix is a package manager — it installs the development tools you need (like `just`, `jq`, `sqlite`) in an isolated way that doesn't interfere with the rest of the system. Think of it as a clean toolbox that you can pick up and put down without leaving a mess.

```bash
sh <(curl -L https://nixos.org/nix/install) --daemon
```

After it finishes, **log out and log back in** so the `nix` command becomes available:

```bash
exit
su - deploy
```

Verify Nix is installed:

```bash
nix --version
```

You should see something like `nix (Nix) 2.x.x`.

### Step 5: Enable Flakes

Flakes are a Nix feature that lets projects define exactly which tools they need. The Creative Mode repo includes a `flake.nix` file that specifies everything.

Add this line to the Nix config:

```bash
echo "experimental-features = nix-command flakes" | sudo tee -a /etc/nix/nix.conf
```

Then restart the Nix service:

```bash
sudo systemctl restart nix-daemon
```

### Step 6: Install direnv

direnv automatically activates the right tools when you enter the project directory. Without it, you'd have to manually run `nix develop` every time.

```bash
nix profile install nixpkgs#direnv
```

Hook direnv into your shell by adding this to the end of your `~/.bashrc`:

```bash
echo 'eval "$(direnv hook bash)"' >> ~/.bashrc
```

Then reload your shell:

```bash
source ~/.bashrc
```

### Step 7: Activate the dev environment

Navigate to the project and allow direnv to set up the tools:

```bash
cd ~/creative-mode
direnv allow
```

The first time, Nix will download all the tools specified in `flake.nix` (zsh, fzf, just, git, curl, jq, sqlite, docker CLI, docker-compose). This may take a few minutes. After that, activating is near-instant.

Verify the tools are available:

```bash
which just && which jq && which sqlite3
```

Each should show a path starting with `/nix/store/`.

> **What just happened?** You now have a reproducible development environment. Every time you enter the `~/creative-mode` directory, direnv automatically loads the exact same set of tools. This means everyone working on the project has an identical setup.

### Step 8: Create the .env file

The `.env` file holds your secret keys and configuration. It's never committed to git.

```bash
cp harness/.env.example harness/.env
```

Edit the file with your actual values:

```bash
nano harness/.env
```

You need to fill in:

- **`GITHUB_CLIENT_ID`** and **`GITHUB_CLIENT_SECRET`** — from your GitHub OAuth App settings (create one at GitHub > Settings > Developer settings > OAuth Apps)
- **`ANTHROPIC_API_KEY`** — your API key from [console.anthropic.com](https://console.anthropic.com)
- **`HARNESS_URL`** — your Tailscale HTTPS URL, e.g., `https://your-machine.tailnet-name.ts.net`
- **`CM_HOOK_SECRET`** — generate with: `openssl rand -hex 32`

**Important:** Set your GitHub OAuth App's callback URL to:

```
https://your-machine.tailnet-name.ts.net/auth/github/callback
```

Replace `your-machine.tailnet-name.ts.net` with your actual Tailscale machine URL. You can find it by running `tailscale status`.

### Step 9: Start the server

```bash
cd ~/creative-mode/harness
just up
```

This starts the game server inside Docker. The first build takes a while (it compiles Go and Rust code). Subsequent starts are much faster.

Verify it's running:

```bash
docker compose ps
```

You should see a container in the "running" state.

### Step 10: Set up Tailscale Serve for HTTPS

Tailscale Serve gives your server a real HTTPS URL with a valid certificate — no configuration needed:

```bash
sudo tailscale serve https / http://localhost:8080
```

This tells Tailscale to forward HTTPS traffic to the game server running on port 8080. The URL will be something like:

```
https://your-machine.tailnet-name.ts.net
```

This setting persists across reboots — you only need to run it once.

### Step 11: Verify everything works

Open the Tailscale URL in your browser (on a device that's on your Tailscale network). You should see the Creative Mode login page.

Sign in with GitHub. If the login works, everything is configured correctly.

You can also check the server's security configuration:

```bash
# Check that the firewall is active
sudo ufw status

# Check that Tailscale is connected
tailscale status

# Check that Docker is running
docker compose ps
```

> **What just happened?** Your server is fully set up and secured. It's running the game inside Docker, accessible only through your Tailscale private network, with automatic daily backups and crash recovery.

## How friends connect

### Joining your Tailscale network

1. Your friend installs the Tailscale app:

   - **Mac/Windows/Linux:** [tailscale.com/download](https://tailscale.com/download)
   - **iPhone/iPad:** App Store, search "Tailscale"
   - **Android:** Google Play Store, search "Tailscale"

1. You invite them to your tailnet from the [Tailscale admin console](https://login.tailscale.com/admin/users)

1. They sign in and join your network

1. They open the URL in their browser:

   ```
   https://your-machine.tailnet-name.ts.net
   ```

That's it. No port forwarding, no firewall rules, no VPN configuration. Tailscale handles everything.

### For casual testers (no Tailscale needed)

If you want someone to try the game without joining your Tailscale network, use Tailscale Funnel. This makes the server temporarily accessible to anyone with the URL:

```bash
# Make server publicly accessible (anyone with the link)
sudo tailscale funnel https / http://localhost:8080

# Switch back to Tailscale-only (private)
sudo tailscale serve https / http://localhost:8080
```

Funnel uses the same `*.ts.net` domain with valid HTTPS. Traffic routes through Tailscale's relay servers (slightly slower, but fine for testing).

### Mobile devices

Tailscale has native apps for iOS and Android. Once your friend installs the app and joins your tailnet, they can open the game URL in their phone's browser — the same URL works on all devices.

## Updating the server

When there are new changes to pull:

```bash
cd ~/creative-mode/harness
just redeploy
```

This runs `git pull` (downloads latest code) and `just up` (rebuilds and restarts).

## Backups

### What's automatically backed up

- **Database:** SQLite database is backed up daily by a cron job. Backups are stored in `~/backups/` and old ones are deleted after 7 days.
- **Code:** Everything is in git. The code itself doesn't need separate backups.

### VM snapshots (UTM users)

If you're running a UTM virtual machine, you can take snapshots of the entire VM — like a save point in a video game. This is useful before making big changes:

```bash
# On your Mac (VM must be stopped first):
brew install qemu

DISK=~/Library/Containers/com.utmapp.UTM/Data/Documents/MyVM.utm/Data/DiskImage.qcow2

# Create a snapshot
qemu-img snapshot -c "before-update" "$DISK"

# List snapshots
qemu-img snapshot -l "$DISK"

# Restore a snapshot (rolls back everything)
qemu-img snapshot -a "before-update" "$DISK"
```

### Excluding from Time Machine

If you're using Time Machine on your Mac, exclude the UTM directory to avoid huge backups (the VM disk image changes constantly):

```
System Settings > General > Time Machine > Options > Exclude:
  ~/Library/Containers/com.utmapp.UTM/Data/Documents/
```

Use the VM snapshot method above for VM backups instead.

## Troubleshooting

### "Permission denied" when running Docker commands

The `deploy` user needs to log out and back in after being added to the docker group:

```bash
exit
su - deploy
```

### Tailscale URL not working

Check that Tailscale is connected and Serve is configured:

```bash
tailscale status
tailscale serve status
```

If Serve isn't set up:

```bash
sudo tailscale serve https / http://localhost:8080
```

### GitHub login fails

Make sure your GitHub OAuth App's callback URL matches your Tailscale URL exactly:

```
https://your-machine.tailnet-name.ts.net/auth/github/callback
```

Also check that `HARNESS_URL` in `harness/.env` matches the same URL (without the `/auth/github/callback` part).

### "Cannot detect public network interface"

The bootstrap script needs a default network route. Check with:

```bash
ip route show default
```

If this shows nothing, your network isn't configured. On a UTM VM, make sure the network adapter is set to NAT ("Emulated VLAN").

### Build takes too long or runs out of memory

The first build compiles the entire Rust/Go toolchain inside Docker. This is normal and can take 10-30 minutes depending on your hardware. Make sure the VM has at least 16 GB of RAM.

### Server doesn't start after reboot

Check the systemd service:

```bash
sudo systemctl status creative-mode
```

If it failed, check the logs:

```bash
sudo journalctl -u creative-mode -n 50
```

### Can't SSH into the server

After the bootstrap, SSH is only available through Tailscale on port 2222. Use Tailscale SSH instead:

```bash
# From any device on your tailnet:
ssh deploy@your-machine
```

Tailscale SSH doesn't need a port — it handles routing automatically.

______________________________________________________________________

## Disclaimer

Creative Mode is experimental software built on top of other experimental software. The mayor is learning on the job and there is always a chance it borks your machine.

**Do not run this on your personal computer.** Use a dedicated virtual machine (UTM, VirtualBox, etc.) or a cloud VPS. If something goes wrong, you can roll back a VM snapshot or spin up a fresh server — your daily driver should NOT be in the blast radius.

**Make backups.** Take VM snapshots before major changes. Keep your `.env` file and database backups somewhere safe and not included in your mayor's default context. The bootstrap script sets up daily SQLite backups, but that only covers the database — your VM itself is your responsibility.
