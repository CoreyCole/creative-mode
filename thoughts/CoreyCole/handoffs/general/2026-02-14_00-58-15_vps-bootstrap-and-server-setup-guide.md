---
date: 2026-02-14T00:58:15-08:00
researcher: CoreyCole
git_commit: e970536
branch: main
repository: creative-mode
topic: "VPS Bootstrap Script & Server Setup Guide"
tags: [implementation, vps, bootstrap, documentation, security, tailscale, docker]
status: complete
last_updated: 2026-02-14
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: VPS Bootstrap Script & Server Setup Guide

## Task(s)

1. **Create `scripts/vps-bootstrap.sh`** — **Completed**. Wrote a heavily-commented, idempotent bootstrap script implementing all 14 steps from Phase 1 of the VPS deployment plan. Includes `--check`/`--dry-run` flag, color output, and idempotency checks at every step.

2. **Create `SERVER-SETUP.md`** — **Completed**. Wrote a beginner-friendly guide explaining server security layers in plain English and providing a full step-by-step setup walkthrough (bootstrap + Nix + direnv + .env + start + Tailscale Serve).

3. **Review both files** — **Not started**. A thorough review of both files is needed before committing.

4. **Add disclaimer to README** — **Not started**. Need to add a disclaimer at the bottom of `SERVER-SETUP.md` (the setup guide, not the main README) warning that this is experimental software built on experimental software and should not be run on a personal computer.

## Critical References

- `thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md` — VPS deployment plan. Phase 1 (all 14 steps) is the source for the bootstrap script.
- `thoughts/CoreyCole/handoffs/general/2026-02-14_00-21-18_nix-flake-ubuntu-vm-install.md` — Nix flake installation steps (source for SERVER-SETUP.md steps 5-8).
- `flake.nix` — Nix flake devShell definition (tools: zsh, oh-my-zsh, fzf, just, git, curl, jq, sqlite, docker, docker-compose).

## Recent changes

- `scripts/vps-bootstrap.sh:1-414` — New file. 14-step bootstrap script: deploy user, Tailscale, Docker, UFW, DOCKER-USER iptables, daemon.json, Fail2Ban, SSH lockdown, docker group, SQLite backup cron, systemd service, summary.
- `SERVER-SETUP.md:1-286` — New file. Non-technical guide: security explanations, 12-step setup walkthrough, how friends connect, updating, backups, troubleshooting.

## Learnings

- The bootstrap script's `--check`/`--dry-run` mode uses the same idempotency checks as the real run — if something is already configured, it reports "SKIP" in both modes, and only shows "Would do X" for unconfigured steps.
- DOCKER-USER rules in `/etc/ufw/after.rules` use a `grep -q` idempotency check since the rules are appended to an existing file (not written to a new one).
- SSH lockdown appends to `/etc/ssh/sshd_config` rather than rewriting — later directives override earlier ones in sshd. A backup is created before modifying.
- The systemd service uses `Type=oneshot` + `RemainAfterExit=yes` because `docker compose up -d` exits immediately after starting containers.
- The deploy user's home directory is resolved with `eval echo ~deploy` for portability.

## Artifacts

- `/Users/coreycole/cdev/creative-mode/scripts/vps-bootstrap.sh` — Bootstrap script (executable)
- `/Users/coreycole/cdev/creative-mode/SERVER-SETUP.md` — Server setup guide

## Action Items & Next Steps

1. **Thorough review of `scripts/vps-bootstrap.sh`** — Verify:
   - All 14 steps from the VPS deployment plan are present and correct
   - Idempotency checks work correctly (re-run safety)
   - Commands match the deployment plan exactly (interface detection, UFW rules, sshd config, systemd unit, cron script)
   - `--check`/`--dry-run` mode covers every step

2. **Thorough review of `SERVER-SETUP.md`** — Verify:
   - Security section explains every layer in plain English without jargon
   - All setup steps are covered (bootstrap + Nix + direnv + .env + start + Tailscale Serve)
   - Commands match `flake.nix`, the Nix handoff, and the VPS plan
   - Step ordering makes sense (e.g., deploy user before Nix install)

3. **Add disclaimer to bottom of SERVER-SETUP.md** — Experimental software warning: this is experimental software built on top of other experimental software. Do not run this on your personal computer. Use a dedicated VM or VPS.

4. **Run `just check`** to verify no regressions after any changes from the review.

## Other Notes

- The bootstrap script was verified with `just check` — no build regressions.
- The `.env.example` file referenced in SERVER-SETUP.md step 9 (`cp harness/.env.example harness/.env`) does not exist yet. It's planned as Phase 3 of the VPS deployment plan but was not in scope for this task. The guide's step 9 will work once `.env.example` is created.
- The `just redeploy` command referenced in SERVER-SETUP.md is planned as Phase 4 of the VPS deployment plan but also not yet implemented. The guide mentions it in the "Updating the server" section.
- Existing `scripts/setup.sh` is a placeholder — the bootstrap script is separate (VPS-specific, not general setup).
