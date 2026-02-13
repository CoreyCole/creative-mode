---
date: 2026-02-13T12:26:04-08:00
reviewer: Claude (Staff Eng Review)
git_commit: b9ff0ff0181873170bde73830f83dd23a92a0c47
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md
status: complete
type: plan_review
---

# Plan Review: VPS Deployment Automation

### Summary

Solid plan with good security focus and simple architecture (Tailscale-only, no public ports). However, it has one factual error that would cause the deployment to fail (Docker Compose ports merge behavior), and silently drops critical items from the research it's based on (WebSocket proxy, Dockerfile USER, rate limiting). The "dev on VPS" intent also contradicts the `DEV_MODE=false` production override.

### Critical Issues (Must Address Before Implementation)

1. **Docker Compose `ports` merge behavior is wrong — game ports will NOT be removed**
   - Problem: Plan line 118 states "ports in the override replaces the base ports list entirely (Docker Compose merge behavior)". This is **incorrect**. Docker Compose **merges/appends** lists (including `ports`). With the proposed override, the result would be ALL base ports PLUS `127.0.0.1:8080:8080` — you'd get a port conflict on 8080 and game/trunk ports would still be exposed.
   - Risk: Production deployment exposes all 201 ports to the network, completely undermining the single-port security model.
   - Suggestion: Three options:
     1. **Split into base + dev overlay** (recommended): Move port ranges to a new `docker-compose.dev.yml` that's used by `just up`. The base has no ports; prod override adds only `127.0.0.1:8080:8080`. Dev adds all ports.
     2. Use Docker Compose v2.24+ `!reset` syntax: `ports: !reset` followed by the single port. Requires recent Docker.
     3. Use a completely separate `docker-compose.prod.yml` (not an override) that redefines the entire service. Duplicates config but is unambiguous.

2. **WebSocket proxy missing — game servers unreachable with single-port expose**
   - Problem: The prod compose only exposes port 8080. The research document identified implementing the WebSocket reverse proxy (`GET /world/:worldID/ws` → `localhost:{gamePort}`) as its **#1 priority** application change. The plan drops this entirely. Without it, game servers inside the container cannot receive WebSocket connections from browsers.
   - Risk: No games work in the deployed environment. Users can log in, create worlds, submit prompts, but never play them.
   - Suggestion: Either add a Phase for WebSocket proxy implementation, or expose game ports in the prod compose (which undermines the single-port model). The WS proxy is architecturally cleaner and was already designed in the research.

3. **"Dev setup on VPS" contradicts `DEV_MODE=false`**
   - Problem: Plan Decision (line 23) says "Same Docker image (air, cargo-watch, full Rust toolchain). Users can iterate on the shared server." But the prod compose sets `DEV_MODE=false`, which disables dev endpoints (`/dev/rebuild`, `/dev/auth/login`, `/dev/sse`). Additionally, `air` (the entrypoint) watches files and hot-reloads — does this make sense in production?
   - Risk: Ambiguous intent leads to either a broken dev experience (no dev endpoints) or a production deployment running with air hot-reload and full dev tooling.
   - Suggestion: Clarify the intent. If this is a shared dev server, consider `DEV_MODE=true` with Tailscale ACLs providing the access control. If it's production, the dev tooling (air, cargo-watch, trunk serve) is unnecessary overhead and should be stripped.

### Concerns (Should Address)

1. **Container runs as root — not addressed**
   - Observation: The research (Finding #1, High severity) identified that the container has no `USER` directive. The research's production compose even has a `# TODO: Add USER directive to Dockerfile for non-root` comment. The plan's Phase 5 omits this.
   - Suggestion: Add a Phase 5g (or Phase 2.5) for creating a non-root user in the Dockerfile. This is especially important since Claude Code runs with `--dangerously-skip-permissions` as root — any user prompt can run arbitrary commands as root inside the container. Note: this may require adjusting file permissions for `data/`, tmux sessions, and the Cargo/Go caches.

2. **`curl | sudo bash` for bootstrap is risky**
   - Observation: The deployment workflow (line 271) pipes a script from raw GitHub directly into `sudo bash`. A compromised GitHub account, branch, or MITM could inject arbitrary commands running as root.
   - Suggestion: Clone the repo first, then run the bootstrap from the local clone: `git clone ... && sudo bash creative-mode/scripts/vps-bootstrap.sh`. This is the safer pattern.

3. **ANTHROPIC_API_KEY in `.bashrc` is insecure**
   - Observation: Deployment workflow (line 285) puts `export ANTHROPIC_API_KEY=sk-ant-...` in `.bashrc`. This means: the key appears in shell history, is readable by any process running as the `deploy` user, shows up in `ps` output of child processes, and is visible to anyone who can read `.bashrc`.
   - Suggestion: Put it in a dedicated `.env` file with `chmod 600`, or use a tool like `sops` or `age` for encrypted secrets. At minimum, use a separate `.secrets` file sourced by `.bashrc` that isn't in any standard readable location.

4. **No `restart: unless-stopped` — VPS reboot kills harness permanently**
   - Observation: Open Question #4 in the plan, but this is actually critical for VPS deployment. Without restart policy, any VPS reboot (scheduled maintenance, kernel update, OOM kill) leaves the service down until manual intervention.
   - Suggestion: Add `restart: unless-stopped` to the prod compose. Combine with Docker's `live-restore: true` (already in daemon.json). Verify Tailscale Serve persists across reboots (it does by default).

5. **No backup strategy for SQLite database**
   - Observation: `data/creative-mode.db` is a single-file SQLite database containing all users, sessions, worlds, checkpoints, and messages. A disk failure, accidental deletion, or corrupted write would lose everything.
   - Suggestion: Add a simple cron job for periodic SQLite backups (`sqlite3 creative-mode.db ".backup /path/to/backup.db"`). Even a daily backup to a different directory is better than nothing.

6. **`redeploy` kills all game servers without warning**
   - Observation: `just redeploy` runs `docker compose up --build -d`, which recreates the container. All tmux sessions (game servers + Claude sessions) inside the container are destroyed. There's no graceful shutdown notification to connected users.
   - Suggestion: Document this behavior. Consider a pre-deploy step that warns active users via the EventBus SSE channel, or stops game servers gracefully before rebuild.

7. **Volume mount gives container RW access to entire project tree**
   - Observation: The plan (line 124) explicitly keeps the `..:/app:cached` bind mount for the "dev on VPS" model. This means the container (running as root) can modify any file in the repo, including `.env`, `scripts/`, and `.git/`.
   - Suggestion: If keeping the full mount, at least mount non-essential directories as `:ro` in the prod override. Or accept this risk given the Tailscale-only access model, but document it.

8. **HTTP rate limiting dropped from plan**
   - Observation: The research (Finding #6, Medium severity) identified no HTTP-level rate limiting. The research's hardening TODO included `middleware.RateLimiter`. The plan's Phase 5 omits this.
   - Suggestion: Add rate limiting at least for `POST /api/chat`, `POST /world/:worldID/prompt`, and `POST /auth/github/login`. Echo's built-in `middleware.RateLimiter` is easy to add.

### Questions (Need Clarification)

1. Is this a **dev server** (users iterate, air hot-reloads, cargo-watch runs) or a **production server** (`DEV_MODE=false`, no dev endpoints)? The plan says both — the decision and volume mount say dev, the compose override says prod. This needs a clear answer because it affects every other decision.

2. How will users connect to game servers without the WebSocket proxy? Is the proxy intended as a prerequisite that's tracked elsewhere, or was it intentionally dropped?

3. Has the Docker Compose ports merge behavior been tested? This is the plan's central mechanism for network hardening and it's based on an incorrect assumption.

4. What's the intended user workflow on the VPS? Can users create worlds, submit prompts, and play games? Or is this primarily a demo/staging environment?

5. The base `docker-compose.yml` declares named volumes for Go/Cargo caches (`go-mod-cache`, `go-build-cache`, `cargo-registry`, `cargo-git`). These survive `docker compose down` but not `docker compose down -v`. Does `deploy-down` need to preserve these for fast rebuilds?

### Suggestions (Nice to Have)

1. **Add a `deploy-status` justfile target** that shows container health, port binding, Tailscale Serve status, and last deploy time. Useful for debugging remote issues.

2. **Add disk space monitoring** — Rust compilation and WASM builds generate large artifacts. A 40GB VPS could fill up quickly with multiple worlds.

3. **Consider Tailscale Funnel for webhook callbacks** — If GitHub OAuth callback needs a publicly accessible URL, Tailscale Funnel can expose a single path publicly without opening the rest of the server. This avoids the awkwardness of requiring all users to be on the tailnet just to complete OAuth.

4. **Add `--ssh-agent-forwarding` to the Tailscale SSH config** so developers can `git pull` on the VPS using their local SSH keys without storing deploy keys on the server.

### What's Good

- **Tailscale-only architecture** is a clean, simple security model that eliminates entire categories of problems (TLS config, domain management, public firewall rules, DDoS).
- **Idempotent bootstrap script** design is the right approach — safe to re-run, checks before modifying.
- **Security headers middleware** (Phase 5a) correctly conditionalizes HSTS on non-localhost to avoid breaking local dev.
- **Logout cookie fix** (Phase 5d) catches a real bug — verified that `auth.go:252-257` indeed omits `HttpOnly`, `Secure`, and `SameSite` flags.
- **postMessage origin validation** (Phase 5f) addresses a real gap — verified `game-loader.js:5` has no origin check.
- **Implementation order** is well-reasoned — security fixes first (testable locally), then infrastructure.
- **The research document** this plan is based on is exceptionally thorough. The 16-finding security audit with code references is high quality.

### Recommended Next Steps

1. **Fix the Docker Compose ports issue** — test the merge behavior locally with `docker compose -f docker-compose.yml -f docker-compose.prod.yml config` to see the merged output. This will immediately show the problem.
2. **Decide dev vs prod** — resolve the fundamental identity question before proceeding. This changes Phases 2, 3, and 5.
3. **Add the WebSocket proxy** as a Phase (or link to a separate plan) — this is a prerequisite for the single-port model.
4. **Add `USER` directive** to the Dockerfile as a plan phase.
5. **Add `restart: unless-stopped`** — this is table stakes for VPS deployment.
6. Re-read the plan after fixes, verifying each phase's assumptions match the resolved decisions.
