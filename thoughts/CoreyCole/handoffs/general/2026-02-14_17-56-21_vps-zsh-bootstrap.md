---
date: 2026-02-14T17:56:21-08:00
researcher: CoreyCole
git_commit: 239c3925fea2b0441c5ecd6932ca300c9ff5e9ce
branch: main
repository: creative-mode
topic: "VPS Bootstrap: zsh + oh-my-zsh + flake shellHook cleanup"
tags: [vps, bootstrap, zsh, oh-my-zsh, direnv, nix, flake]
status: complete
last_updated: 2026-02-14
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VPS Bootstrap zsh + oh-my-zsh Setup

## Task(s)

1. **Add zsh + oh-my-zsh to `scripts/vps-bootstrap.sh`** — COMPLETED
   - Added `zsh` and `fzf` to apt prerequisites (Step 0)
   - New Step 17: sets zsh as deploy's login shell, installs oh-my-zsh, writes `.zshrc` with direnv hook + auto-cd to `~/creative-mode`
   - Removed direnv bash hook from Step 16 (now in `.zshrc`)
   - Renumbered Steps 17→18 through 21→22, updated TOC

2. **Remove oh-my-zsh sourcing from `flake.nix` shellHook** — COMPLETED
   - direnv evaluates shellHook inside bash, causing "Oh My Zsh can't be loaded from: bash" errors
   - Set `shellHook = "";` since oh-my-zsh is now in the login shell `.zshrc`

3. **Fix container build failure: `error obtaining VCS status: exit status 128`** — NOT STARTED
   - The harness container fails to `go build` due to git safe directory issue on the bind-mounted repo
   - `.air.toml:5` has `cmd = "templ generate && go build -o /tmp/harness ."`
   - The go toolchain tries to stamp VCS info but git rejects the bind-mounted directory (different owner inside container vs host)
   - User wants to handle this inside the container

## Critical References
- `scripts/vps-bootstrap.sh` — the bootstrap script with all changes
- `flake.nix` — shellHook was cleared to empty string
- `harness/Dockerfile` — no git safe.directory config exists yet (needed for fix)

## Recent changes

- `scripts/vps-bootstrap.sh:12` — TOC updated with new step numbering and zsh/fzf in prerequisites
- `scripts/vps-bootstrap.sh:114` — Step 0: added `zsh` and `fzf` to apt install and command checks
- `scripts/vps-bootstrap.sh:705-722` — Step 16: removed direnv bash hook, now direnv-only install
- `scripts/vps-bootstrap.sh:725-788` — Step 17 (NEW): chsh to zsh, write .zshrc (with nix-daemon.sh sourcing, oh-my-zsh config, direnv hook, auto-cd), install oh-my-zsh
- `scripts/vps-bootstrap.sh:521` — Added `mkdir -p /run/sshd` before `sshd -t` validation
- `flake.nix:33` — shellHook set to empty string with explanatory comment

## Learnings

1. **Ubuntu's `/bin/sh` is `dash`**, which doesn't support `<(...)` process substitution. Use pipes instead: `curl ... | sh` not `sh <(curl ...)`
2. **oh-my-zsh installer creates a default `.zshrc`** with `source $ZSH/oh-my-zsh.sh`. If you write `.zshrc` AFTER installing oh-my-zsh, your idempotency check on that pattern will match the installer's file and skip your custom config. Solution: write `.zshrc` BEFORE installing oh-my-zsh, and use `KEEP_ZSHRC=yes`.
3. **zsh doesn't source `/etc/profile.d/*.sh`** by default. Nix daemon sets up PATH via `/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh`, which bash gets automatically but zsh needs explicitly in `.zshrc`.
4. **direnv evaluates flake shellHook inside bash**, not zsh. Any shell-specific code (oh-my-zsh sourcing, fzf zsh completions) in shellHook will fail. Keep shellHook empty and configure the login shell separately.
5. **`/run/sshd`** is on tmpfs and may not exist after reboot. `sshd -t` validation fails without it. Create it with `mkdir -p /run/sshd` before validation.
6. **Container git VCS stamping fails** when the bind-mounted repo has a different owner inside the container. Fix options: add `git config --global --add safe.directory /app` to Dockerfile, OR use `-buildvcs=false` in the air build command.

## Artifacts

- `scripts/vps-bootstrap.sh` — modified (5 commits of changes)
- `flake.nix` — modified (shellHook cleared)

## Action Items & Next Steps

1. **Fix container `go build` failure** — The `error obtaining VCS status: exit status 128` needs resolution. Two options:
   - Add `RUN git config --global --add safe.directory /app` to `harness/Dockerfile` (allows VCS stamping)
   - Add `-buildvcs=false` to the go build command in `harness/.air.toml:5` (disables VCS stamping)
   - User indicated they want to handle this inside the container
2. **Clean up VM `.zshrc`** — The VM's current `.zshrc` may be the oh-my-zsh default (missing nix-daemon.sh sourcing, direnv hook, and `cd ~/creative-mode`). On next bootstrap re-run, the idempotency check (`cd ~/creative-mode`) won't match, so it will overwrite with the correct version. Alternatively, manually replace it.

## Other Notes

- The deploy user's `.bashrc` still has the old `eval "$(direnv hook bash)"` from a previous bootstrap run. This is harmless (bash isn't the login shell anymore) but could be cleaned up.
- The flake.nix still lists `zsh`, `oh-my-zsh`, and `fzf` in its packages. These are now somewhat redundant (zsh/fzf installed via apt, oh-my-zsh via its installer), but keeping them doesn't hurt — they just add to the PATH when direnv activates.
- The `warning: Git tree '/home/deploy/creative-mode' is dirty` from direnv is expected when there are uncommitted changes on the VM.
