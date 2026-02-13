---
date: 2026-02-13T14:02:41-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 980f2197b6181ce155d933bfc76abc2f1f2694e0
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-13_12-15-44_vps-deployment-automation.md
status: complete
type: plan_review
---

# Plan Review: VPS Deployment Automation (v2 — Post-Rewrite)

### Summary

The rewritten plan is a significant improvement over the original. All three critical issues from the prior review (Compose ports merge, missing WebSocket proxy, dev/prod contradiction) are resolved. The "dev ≡ VPS" simplification is architecturally sound and eliminates unnecessary complexity. However, there are two issues that would cause real breakage if implemented as-written: the postMessage origin check will kill trunk serve dev mode, and the DOCKER-USER iptables rules hardcode `eth0` which may not match the VPS interface name. Both are straightforward to fix.

### Critical Issues (Must Address Before Implementation)

1. **postMessage origin validation breaks trunk serve dev mode (Phase 5f)**
   - Problem: The plan proposes `if (event.origin !== window.location.origin) return;` in `game-loader.js`. But when trunk serve is running (dev mode for both local and VPS), the game iframe is loaded from `http://localhost:{trunkPort}` (e.g., `:8081`) while the parent harness is at `http://localhost:8080`. The iframe's `postMessage` carries origin `http://localhost:8081`, which doesn't match `window.location.origin` (`http://localhost:8080`). This silently breaks all cursor lock/unlock, overlay toggle, and world navigation messages from the game.
   - Verified: `harness/views/world/world.templ:13-16` confirms trunk serve uses a different port: `src={ fmt.Sprintf("http://localhost:%d/", trunkPort) }`. The trunk port pool is `8081-8180` (`harness/internal/world/ports.go:12-13`).
   - Risk: Every 2D and 3D game in dev mode loses postMessage communication with the harness overlay. Cursor lock breaks, overlay toggle breaks, room navigation breaks. Only WASM-build iframes (served from `/wasm/...`, same origin) would work.
   - Suggestion: Use a localhost-aware check instead of strict origin matching:
     ```js
     window.addEventListener('message', function(event) {
         var loc = window.location;
         if (event.origin !== loc.origin) {
             // Allow localhost on any port (trunk serve, dev servers)
             try {
                 var src = new URL(event.origin);
                 if (src.hostname !== loc.hostname) return;
             } catch (e) { return; }
         }
         if (!event.data || !event.data.type) return;
         // ... rest of handler
     });
     ```
     This allows `localhost:{any_port}` → `localhost:8080` postMessage while blocking cross-origin messages from other hosts. On the VPS behind Tailscale Serve, `window.location.hostname` would be `machine.tailnet.ts.net`, and trunk serve runs on `localhost` inside the container — so the VPS case also needs the iframe to communicate via the harness host, which would require the trunk proxy pattern described in the next review cycle.

2. **DOCKER-USER iptables rules hardcode `eth0` — may not match VPS interface (Phase 1, step 7)**
   - Problem: The plan adds `-A DOCKER-USER -i eth0 -j DROP` to `/etc/ufw/after.rules`. Many VPS providers (DigitalOcean, Linode, Hetzner, AWS) use predictable network interface names like `ens3`, `enp0s3`, `ens160`, `eth0`, etc. If the VPS interface is NOT `eth0`, the DROP rule never matches traffic and **all Docker-exposed ports (8080 + 200 game/trunk ports) are open to the public internet**.
   - Risk: The entire network security model depends on this single rule. A wrong interface name means zero protection — equivalent to running with no firewall.
   - Suggestion: Auto-detect the primary public interface in the bootstrap script:
     ```bash
     # Find the interface with the default route (public-facing)
     PUBLIC_IF=$(ip route show default | awk '{print $5}' | head -1)
     if [ -z "$PUBLIC_IF" ]; then
         echo "ERROR: Cannot detect public network interface."
         exit 1
     fi
     echo "Detected public interface: $PUBLIC_IF"
     ```
     Then use `$PUBLIC_IF` instead of hardcoded `eth0` in the DOCKER-USER rules. Print the detected interface in the summary so the user can verify. Also add a post-install verification step:
     ```bash
     # Verify DOCKER-USER rules are active
     iptables -L DOCKER-USER -v -n | grep DROP | grep "$PUBLIC_IF" || echo "WARNING: DOCKER-USER rules not active!"
     ```

### Concerns (Should Address)

1. **Global `BodyLimit("1M")` will break asset uploads (Phase 5b vs existing `handleAssetUpload`)**
   - Observation: Phase 5b adds `e.Use(middleware.BodyLimit("1M"))` globally. But `handleAssetUpload` at `harness/internal/server/assets.go:23` accepts multipart image file uploads (PNG, JPEG, WebP, GIF). A 1MB limit is too small for many images — a single 1080p screenshot can be 2-5MB.
   - Verified: The upload handler at `assets.go` has no file size limit of its own (just MIME type validation).
   - Suggestion: Either (a) apply the 1M limit only to specific routes (chat, prompt) and use a larger limit (e.g., 10M) for the upload endpoint, or (b) set the global limit higher (10M) and add specific route-level limits for chat/prompt. Echo supports route-specific middleware:
     ```go
     approved.POST("/api/assets/upload", s.handleAssetUpload, middleware.BodyLimit("10M"))
     ```

2. **WebSocket proxy design underspecifies the Go implementation (Phase 6)**
   - Observation: The plan says "Uses `net/http/httputil.ReverseProxy` or a lightweight WS proxy library." `httputil.ReverseProxy` does **not** handle WebSocket upgrades — it's HTTP-only. You need explicit WebSocket handling.
   - Verified: `gorilla/websocket` is already an indirect dependency in `go.mod:30` (`v1.5.3`). The Lightyear netcode protocol wraps custom frames inside WebSocket binary messages, so the proxy must be transparent (byte-level forwarding, no frame inspection).
   - Suggestion: The handler should use `gorilla/websocket.Upgrader` to accept the browser connection and `gorilla/websocket.Dialer` to connect to the backend game server, then `io.Copy` both directions in goroutines. This is ~30 lines of Go. Consider promoting `gorilla/websocket` to a direct dependency.
   - Good news: Lightyear 0.26's `WebSocketTarget::Url(String)` variant (verified via docs.rs) supports full URL paths, so the client change (`wss://{host}/world/{worldID}/ws`) is feasible without fighting the Lightyear API.

3. **Bootstrap script idempotency requires careful conditional logic (Phase 1)**
   - Observation: The plan states "The script should be idempotent — safe to re-run. Each step checks if already done before modifying." This is the right design, but several steps have non-obvious idempotency pitfalls:
     - `adduser deploy` fails if user exists → needs `id -u deploy 2>/dev/null || adduser deploy`
     - Appending DOCKER-USER rules to `/etc/ufw/after.rules` would create duplicates on re-run → needs `grep -q DOCKER-USER /etc/ufw/after.rules || cat >> ...`
     - `ufw enable` prompts for confirmation → needs `ufw --force enable` or `echo y | ufw enable`
     - `tailscale up` is interactive (opens auth URL) → on re-run it's a no-op if already connected, which is correct
   - Suggestion: For each step, use the pattern `check_if_done && skip || do_it`. Add a `--check` / `--dry-run` flag that reports what would be changed without modifying anything. This makes the script safe to audit before running.

4. **Port range mismatch: allocator can assign unreachable ports (pre-existing)**
   - Observation: `docker-compose.yml` exposes ports `9001-9100` (100 ports), but `ports.go:9` defines `gameServerMaxPort = 9999` (999 ports). If more than 100 game servers run simultaneously, ports 9101-9999 are allocated but unreachable from outside the Docker container. With the WebSocket proxy (Phase 6), this is fine for VPS (traffic proxied through :8080), but on local dev without the proxy, games on high ports silently fail.
   - Suggestion: Either align the Docker compose range to `9001-9999`, or reduce `gameServerMaxPort` to `9100`, or document this as a known limitation. 100 concurrent game servers is unlikely to be a problem now, but it's a silent failure mode that would be confusing to debug.

5. **`restart: unless-stopped` auto-starts on macOS Docker Desktop (Phase 2)**
   - Observation: On macOS with Docker Desktop set to start on login, `unless-stopped` means the harness container auto-starts on every boot (even if you stopped it days ago with `Ctrl+C` on `just up`). This is because `docker compose up` without `-d` runs foreground — if the process is killed (Ctrl+C), Docker considers it a crash, not an explicit stop.
   - Verified: Only `docker compose down` or `docker stop` count as "explicitly stopped." A Ctrl+C on foreground `docker compose up` does NOT.
   - Risk: Minor annoyance for local dev — container using CPU/RAM on boot. Not a blocker.
   - Suggestion: Document this behavior. Alternatively, use `restart: on-failure` which only restarts on non-zero exit codes and does NOT restart after reboot. The VPS would then need a systemd unit or cron `@reboot` for auto-start, but this keeps local dev clean.

6. **SQLite backup cron path assumption (Phase 1, step 12)**
   - Observation: The cron job hardcodes `/home/deploy/creative-mode/data/creative-mode.db`. But the bootstrap workflow clones to `/opt/creative-mode` first (line 323), then moves it (line 330). If the user skips the move step or clones to a different path, the backup silently targets a nonexistent file.
   - Suggestion: Make the backup script use a variable or discover the path:
     ```bash
     DB_PATH="${CREATIVE_MODE_DIR:-/home/deploy/creative-mode}/data/creative-mode.db"
     ```
     Or better: have the bootstrap script write the actual repo path to a config file that the cron script reads.

### Questions (Need Clarification)

1. What VPS provider and instance type is planned? This determines the network interface name (critical for DOCKER-USER rules) and available disk space (relevant for Rust build caches).

2. Should the postMessage origin check be relaxed to all-localhost or restricted to a specific list of allowed ports? The plan's strict check breaks dev mode; a localhost-wildcard check is simpler but less secure than a port whitelist.

3. What is the expected maximum image upload size for `handleAssetUpload`? This determines whether the global 1M body limit needs exceptions.

4. Is there a preference between `restart: unless-stopped` (auto-starts after reboot) vs `restart: on-failure` + systemd unit (explicit startup control)? The former is simpler; the latter is more predictable.

### Suggestions (Nice to Have)

1. **Add a post-bootstrap verification script** — A separate `scripts/vps-verify.sh` that checks all security assumptions are met: DOCKER-USER rules active, UFW enabled, Tailscale running, Docker listening only on expected ports, sshd on correct port. Run this after bootstrap and periodically via cron.

2. **Add `healthcheck` to docker-compose.yml** — Docker health checks enable `restart: unless-stopped` to restart on application-level failures (not just process death):
   ```yaml
   healthcheck:
     test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
     interval: 30s
     timeout: 5s
     retries: 3
   ```
   The `/health` endpoint already exists (`server.go:125`).

3. **Document the port range mismatch** — Whether you fix it or not, add a comment in `ports.go` explaining why `gameServerMaxPort` is higher than the Docker compose range and what the implications are.

4. **Consider `io.LimitReader` in `handleAssetUpload`** — Even with a route-specific body limit, the handler itself should cap the bytes read to prevent a single upload from filling the disk:
   ```go
   limited := io.LimitReader(file, 10<<20) // 10MB max
   if _, copyErr := io.Copy(out, limited); copyErr != nil { ... }
   ```

### What's Good

- **All prior critical issues resolved** — The Compose ports merge (eliminated override entirely), missing WebSocket proxy (added as Phase 6), and dev/prod contradiction (clarified as dev-only) are all addressed. This was a substantial rewrite, not just a patch.
- **"dev ≡ VPS" is an excellent architectural simplification** — Eliminates entire categories of config drift, environment-specific bugs, and deployment complexity. The principle that the only differences are `.env` contents and OS-level hardening is clean and testable.
- **DOCKER-USER approach is correct** (modulo interface name) — This is the canonical Docker + UFW integration pattern. The rules in the plan (`ESTABLISHED,RELATED → RETURN`, `tailscale0 → RETURN`, `eth0 → DROP`) are correctly ordered and complete.
- **`WriteTimeout = 0` is correctly justified** — The plan explicitly notes SSE connections require this. Slow-write attacks are mitigated by Tailscale-only access.
- **Cookie flag fixes are verified** — Logout cookie at `auth.go:252-257` and state cookie at `auth.go:100-105` are both confirmed missing `HttpOnly`, `Secure`, and `SameSite`. The plan's fix correctly mirrors the creation cookie at `auth.go:161-169`.
- **WebSocket proxy is feasible** — Lightyear 0.26 has `WebSocketTarget::Url(String)` for URL-path-based connections. `gorilla/websocket` is already an indirect dependency. The proxy is ~30 lines of straightforward Go.
- **Implementation order is well-reasoned** — Security fixes first (Phase 5, testable locally), then infrastructure (Phases 2-4), then VPS-specific (Phases 1, 6).

### Recommended Next Steps

1. **Fix the two critical issues** — postMessage origin check (Phase 5f) and interface name detection (Phase 1 step 7). Both are small changes to the plan text.
2. **Decide on body limit strategy** — Global 1M with route-specific override for uploads, or route-specific limits only.
3. **Decide on restart policy** — `unless-stopped` (simpler, but auto-starts on macOS) vs `on-failure` + systemd (more control).
4. **Clarify VPS provider** — Determines interface name and informs the bootstrap script.
5. **Begin implementation with Phase 5** (application hardening) — All changes are testable locally with no VPS needed. Start with the cookie fixes and body limits, as they're the lowest-risk highest-value changes.
