---
date: 2026-02-14T00:21:18-08:00
researcher: CoreyCole
git_commit: 3992800
branch: main
repository: creative-mode
topic: "Nix Flake Ubuntu VM Installation"
tags: [nix, flake, ubuntu, vm, devshell, direnv]
status: complete
last_updated: 2026-02-14
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: Install Nix Flake Dev Environment in Ubuntu VM

## Task(s)

1. **Create Nix flake for server environment** — **Completed**. Created `flake.nix`, `.envrc`, and updated `.gitignore` with Nix-related entries. The flake provides a devShell with zsh, oh-my-zsh, fzf, just, git, curl, jq, sqlite, docker CLI, and docker-compose. Targets `aarch64-linux` and `x86_64-linux` only.

2. **Install the Nix flake in the Ubuntu VM** — **Planned/Not Started**. The user wants help getting Nix + direnv + the flake working on their Ubuntu 24.04 ARM64 VM (UTM). This is the next step.

## Critical References

- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — Full VPS deployment plan. The Nix flake is part of the host VM shell environment setup (not Docker toolchain).
- `flake.nix` — The Nix flake devShell definition (just created).
- `.envrc` — direnv integration file (`use flake`).

## Recent changes

- `flake.nix:1-55` — New file. Nix flake with devShell providing zsh, oh-my-zsh, fzf, just, git, curl, jq, sqlite, docker, docker-compose. shellHook sources oh-my-zsh and fzf keybindings/completion.
- `.envrc:1` — New file. Single line `use flake` for direnv auto-activation.
- `.gitignore:20-23` — Added `.direnv/` and `result` under `# Nix` section.

## Learnings

- The Nix flake is for the **host VM shell environment only** — not the build toolchain. Docker handles Go, Rust, trunk, etc. inside the container. The flake provides the shell UX (oh-my-zsh, fzf) and host utilities (just, jq, sqlite) for the deploy user.
- The flake targets Linux only (`aarch64-linux` / `x86_64-linux`) — not macOS. The user's dev Mac doesn't need this; it's for the VM/VPS.
- `oh-my-zsh` in Nix provides the package at `${pkgs.oh-my-zsh}/share/oh-my-zsh` — the shellHook sets `$ZSH` to this path rather than using the standard `~/.oh-my-zsh` install.
- fzf key-bindings and completion scripts are at `${pkgs.fzf}/share/fzf/key-bindings.zsh` and `completion.zsh`.

## Artifacts

- `/Users/coreycole/cdev/creative-mode/flake.nix` — Nix flake devShell definition
- `/Users/coreycole/cdev/creative-mode/.envrc` — direnv integration
- `/Users/coreycole/cdev/creative-mode/.gitignore` — Updated with Nix entries (lines 20-23)

## Action Items & Next Steps

The next agent should help the user install and configure Nix + direnv in their Ubuntu VM. Steps to execute on the VM:

1. **Install Nix** (multi-user daemon mode):
   ```bash
   sh <(curl -L https://nixos.org/nix/install) --daemon
   ```
   Then source the Nix profile or re-login for `nix` to be on PATH.

2. **Enable flakes** — append to `/etc/nix/nix.conf`:
   ```
   experimental-features = nix-command flakes
   ```
   Then restart the Nix daemon: `sudo systemctl restart nix-daemon`

3. **Install direnv** via Nix:
   ```bash
   nix profile install nixpkgs#direnv
   ```

4. **Hook direnv into the shell** — add to the deploy user's `.bashrc` (or `.zshrc` if zsh is already default):
   ```bash
   eval "$(direnv hook bash)"
   ```

5. **Set zsh as default shell** for the deploy user:
   ```bash
   sudo chsh -s $(which zsh) deploy
   ```
   Note: System zsh for login shell, Nix zsh used inside the devShell.

6. **Clone/pull the repo** with the new `flake.nix`, then:
   ```bash
   cd ~/creative-mode
   direnv allow .
   ```

7. **Verify**:
   - `cd ~/creative-mode` should trigger direnv activation
   - `which zsh && which fzf && which just && which jq && which sqlite3` — all should resolve to Nix store paths
   - `echo $ZSH` — should point to Nix store oh-my-zsh path
   - `Ctrl+R` — should show fzf fuzzy history search

## Other Notes

- The user has an Ubuntu 24.04 ARM64 VM running in UTM (macOS host). The VM may or may not have the full VPS bootstrap (`scripts/vps-bootstrap.sh`) run yet — the Nix installation is a subset of the broader VPS setup.
- The `flake.lock` file will be generated on first `nix develop` or `direnv allow` — it should be committed to the repo for reproducibility.
- If `nix develop` is slow on first run, it's downloading all packages. Subsequent activations use the Nix store cache and are near-instant.
- The deploy user needs to be in the `nix-users` group (the multi-user Nix installer usually handles this, but worth verifying).
