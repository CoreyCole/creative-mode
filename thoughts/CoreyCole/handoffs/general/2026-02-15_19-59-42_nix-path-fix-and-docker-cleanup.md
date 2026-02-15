---
date: 2026-02-15T19:59:42+00:00
researcher: CoreyCole
git_commit: b8403be9dff42450f0295ff04078f0dfaef0265d
branch: main
repository: creative-mode
topic: "Nix PATH Fix & Docker Cleanup"
tags: [nix, direnv, bootstrap, vps, docker-removal, path]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Fix Nix PATH system-wide + Remove Docker from VPS

## Task(s)

### Completed: Fix Nix PATH for all contexts
Tools from `flake.nix` were only available when direnv activated in an interactive shell. Claude Code, `just check`, `just vps-build`, and systemd couldn't find them. **Solution**: removed direnv dependency entirely.

- Added `packages.default` to `flake.nix` (a `buildEnv` that bundles all dev tools)
- Ran `nix profile install /home/deploy/creative-mode` to put tools in `~/.nix-profile/bin/`
- Created `~/.zshenv` so non-interactive zsh sessions (Claude Code) get all tools on PATH
- Simplified `~/.zshrc` to interactive-only config (oh-my-zsh, aliases)
- Removed `eval "$(direnv export bash)"` from `scripts/harness-run.sh`
- Deleted `.envrc`
- Updated `scripts/vps-bootstrap.sh` to use `nix profile install` instead of direnv

### Completed: Install missing Rust toolchain
Rust was not installed on this VPS. Ran the bootstrap steps manually:
- Installed Rust to `/usr/local/rustup` and `/usr/local/cargo` via rustup (system-wide)
- Added `wasm32-unknown-unknown` target
- Installed trunk 0.21.14, cargo-watch 8.5.3, wasm-bindgen-cli 0.2.108

### Completed: Remove Docker from VPS bootstrap
Docker is no longer used on the VPS (harness runs as native binary under systemd). Removed:
- Step 5 (Install Docker Engine), Step 7 (DOCKER-USER iptables), Step 8 (Docker daemon config), Step 11 (docker group)
- Docker/docker-compose from `flake.nix` devShells and oh-my-zsh plugins
- Renumbered all bootstrap steps sequentially (0-17)

### Next: Set up OpenClaw on this VM
OpenClaw (agent framework for world mayors) needs to be configured. `OPENCLAW_HOME` is already set in `harness-run.sh` pointing to `/home/deploy/creative-mode/data/openclaw`, but the actual setup needs to be verified/completed.

## Critical References
- `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md` - Mayor/President agent architecture
- `harness/scripts/setup-openclaw.sh` - OpenClaw setup script (untracked, new file)
- `harness/internal/mayor/` - Mayor implementation code (untracked, new)

## Recent changes

- `flake.nix`: Added `packages.default` buildEnv output, removed docker/docker-compose from devShells
- `~/.zshenv`: Created - sources nix-daemon.sh, sets Go/Rust/Claude/npm paths for all zsh sessions
- `~/.zshrc`: Simplified - oh-my-zsh + aliases only, removed nix-daemon.sh/direnv/auto-cd/docker plugins
- `scripts/harness-run.sh:15`: Removed `eval "$(direnv export bash 2>/dev/null)"`
- `scripts/vps-bootstrap.sh`: Replaced direnv with nix profile install, removed 4 Docker steps, renumbered all steps, removed docker from zsh plugins
- `.envrc`: Deleted

## Learnings

- **nix-daemon.sh is the key**: Sourcing `/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh` adds `~/.nix-profile/bin` to PATH. This is what makes `nix profile install` tools available.
- **`.zshenv` vs `.zshrc`**: `.zshenv` is sourced for ALL zsh sessions (including non-interactive like Claude Code). `.zshrc` is only for interactive sessions. PATH setup belongs in `.zshenv`.
- **sudo doesn't preserve PATH**: When running cargo via sudo, must use full path like `sudo /usr/local/cargo/bin/cargo install ...` rather than `sudo cargo install`.
- **Rust is installed system-wide via rustup** to `/usr/local/rustup` and `/usr/local/cargo`, NOT via the Nix flake. This is because rustup manages toolchain targets (wasm32) which is hard to replicate in pure Nix.
- **Docker is still used for macOS local dev** (Dockerfile, docker-compose.yml in `harness/`). Only the VPS no longer needs Docker.
- **`nix profile install` warning**: The command shows `'install' is a deprecated alias for 'add'` but still works.

## Artifacts

- `flake.nix` - packages.default output added
- `scripts/vps-bootstrap.sh` - Major rewrite (direnv removed, Docker removed, renumbered)
- `scripts/harness-run.sh` - Minor edit (direnv line removed)
- `~/.zshenv` - New file
- `~/.zshrc` - Simplified
- `.envrc` - Deleted

## Action Items & Next Steps

1. **Set up OpenClaw on this VM** - Check `harness/scripts/setup-openclaw.sh` for what needs to be done. The `OPENCLAW_HOME` env var is already set in `harness-run.sh` pointing to `/home/deploy/creative-mode/data/openclaw`.
2. **Consider adding golangci-lint** - `just check` fails because `golangci-lint` isn't installed. It's not in the flake or bootstrap script. Could add to `packages.default` in `flake.nix` or install separately.
3. **Commit these changes** - All changes are unstaged. The nix PATH fix, Docker removal, and bootstrap cleanup should be committed.
4. **Restart the creative-mode service** after committing to pick up the harness-run.sh change (direnv removal).

## Other Notes

- The VPS still has Docker installed (it was installed before). The bootstrap script no longer installs it on fresh VPSes, but Docker Engine remains on this machine. It's harmless but could be uninstalled if desired.
- The `DOCKER-USER` iptables rules were never added to this VPS (confirmed missing during audit). Not needed since Docker containers aren't used.
- The `marketing-site-bootstrap.sh` in `scripts/` still has Docker steps - that's a separate concern for the marketing site EC2 instance.
- All nix profile tools resolve through the nix store (e.g., `/nix/store/.../bin/go`), symlinked via `~/.nix-profile/bin/`.
