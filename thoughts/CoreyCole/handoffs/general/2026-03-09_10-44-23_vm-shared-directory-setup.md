---
date: 2026-03-09T10:44:23-07:00
researcher: CoreyCole
git_commit: 194bbc3
branch: feat/agent-primitives
repository: creative-mode
topic: "VM Shared Directory & rsync Sync Setup"
tags: [infrastructure, vm, utm, rsync, air, systemd]
status: complete
last_updated: 2026-03-09
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VM Shared Directory & Air/Systemd Configuration

## Task(s)

### Completed
- **Diagnosed UTM shared directory issues**: The VM uses QEMU backend (not Apple Virtualization), so `virtiofs` does not work. VirtFS (9p) is the correct protocol but has critical limitations: symlinks don't resolve properly ("Too many levels of symbolic links"), inotify doesn't work (so Air can't detect changes), and file permissions show UID mismatch (501/dialout vs coreycole/staff).
- **Established rsync-based sync strategy**: Instead of relying on 9p shared directory, use `rsync -avz -L` (with `--copy-links` to dereference symlinks) from the Mac host to the VM over SSH. This solves symlink issues, works with native ext4 + inotify on the VM, and keeps git on the Mac.
- **Added rsync Stop hook**: Added to `.claude/settings.local.json` a Stop hook that rsyncs the project to `claude@192.168.69.2:~/creative-mode/` after Claude Code finishes.

### Work In Progress / Next Steps
- **VM Air/systemd reconfiguration**: The VM currently has Air watching `~/creative-mode` (a separate git clone). This needs to be updated — either:
  - **Option A (rsync approach, recommended)**: Keep Air watching `~/creative-mode` on the VM. The rsync Stop hook already pushes to that path. Air uses native inotify on ext4, so it will detect changes automatically. **This may already work as-is** — just verify Air is running and test the rsync.
  - **Option B (9p shared mount)**: Mount the UTM shared directory at `/mnt/shared`, point Air at it, and enable polling mode (`poll = true` in `.air.toml`) since inotify doesn't work over 9p. This avoids rsync but has the symlink and performance downsides.

## Critical References
- `.claude/settings.local.json` — contains the rsync Stop hook (local-only, not committed)
- `.claude/settings.json:19-42` — existing Stop hook runs `./scripts/check.sh` before rsync
- `scripts/check.sh` — full lint/build check pipeline (Go + Rust + WASM)

## Recent changes
- `.claude/settings.local.json` — added `hooks.Stop` with rsync command: `rsync -avz -L --delete --exclude='.git' --exclude='node_modules' --exclude='target' --exclude='.playwright-cli' "$(git rev-parse --show-toplevel)/" claude@192.168.69.2:~/creative-mode/`

## Learnings

### UTM QEMU vs Apple Virtualization
- The VM shows "QEMU 7.2 ARM Virtual Machine" — this means it's QEMU backend, NOT Apple Virtualization
- `mount -t virtiofs` only works with Apple Virtualization backend
- QEMU backend uses VirtFS (9p): `mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/shared`
- Directory share mode in UTM settings must be set to **VirtFS** (not SPICE WebDAV) for 9p to work

### 9p Limitations (why rsync is preferred)
- **Symlinks**: Directory symlinks with relative paths cause "Too many levels of symbolic links". The project has symlinks like `skills/linear-api -> ../.agents/skills/linear-api` (pointing to directories containing SKILL.md)
- **inotify**: Does not fire over 9p — file watchers like Air won't detect host-side changes
- **Permissions**: macOS UID 501 maps to no user on the VM (shows as `501:dialout`), causing git to see phantom permission changes
- **File locking**: Limited support, causes `error: could not lock config file .git/config: Permission denied`
- `git config core.fileMode false` does not fix the symlink issue

### VM Details
- Ubuntu 24.04 LTS, ARM64 (aarch64), Linux 6.8.0-101-generic
- IP: `192.168.69.2` (SSH user: `claude`)
- 32 GB RAM, 79.21 GB disk
- Air (Go live reload) runs under systemd for hot-reload builds

### Project Symlink Structure
- `skills/linear-api -> ../.agents/skills/linear-api` (directory containing SKILL.md)
- `.claude/skills/linear-api -> ../../.agents/skills/linear-api` (same target)
- All `linear-*` skills follow this pattern (~38 symlinks each in `skills/` and `.claude/skills/`)

## Artifacts
- `.claude/settings.local.json` — updated with rsync Stop hook

## Action Items & Next Steps

1. **Verify rsync works manually from Mac**:
   ```bash
   cd ~/cdev/creative-mode && rsync -avz -L --dry-run --delete --exclude='.git' --exclude='node_modules' --exclude='target' --exclude='.playwright-cli' ./ claude@192.168.69.2:~/creative-mode/
   ```

2. **On the VM**: Check Air configuration and systemd service — ensure Air is watching `~/creative-mode` and will pick up changes after rsync lands files there. Key files to check:
   - Air config: likely `.air.toml` or `air.toml` in `~/creative-mode/harness/` on the VM
   - Systemd service: check with `systemctl status` for the creative-mode service
   - The existing setup uses `scripts/harness-run.sh` which runs `exec air`

3. **On the VM**: If switching to 9p shared mount instead of rsync:
   - Load kernel modules: `sudo modprobe 9p && sudo modprobe 9pnet_virtio`
   - Mount: `sudo mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/shared`
   - Add to `/etc/fstab`: `share /mnt/shared 9p trans=virtio,version=9p2000.L,rw,_netdev,nofail,auto 0 0`
   - Update Air config to enable polling: `poll = true`, `poll_interval = 500`
   - Update systemd service working directory to `/mnt/shared/harness/`

4. **Decision needed**: Whether to use rsync (Option A) or 9p mount (Option B). Rsync is simpler and already partially configured. 9p avoids the sync step but has the symlink/inotify limitations.

## Other Notes

- The host project path is `~/cdev/creative-mode` (NOT `~/creative-mode` as mentioned in some earlier discussion)
- The VM project path is `~/creative-mode`
- CLAUDE.md warns: on macOS, never run `cargo build`/`go build` directly on host — use Docker. But `just check` uses isolated `CARGO_TARGET_DIR="/tmp/cm-check-target"` to avoid conflicts.
- The rsync `--delete` flag will remove files on the VM that don't exist on the host — this is intentional to keep them in sync, but means the VM's `~/creative-mode/.git` would be deleted. The `--exclude='.git'` flag prevents this, preserving the VM's separate git state.
- SSH key-based auth to `claude@192.168.69.2` must be set up for the rsync hook to work without password prompts.
