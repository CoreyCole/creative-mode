---
date: 2026-02-22T11:03:42-08:00
researcher: CoreyCole
git_commit: b5296feb680f1e84dd22c08b55ade8d58ba1d1a5
branch: main
repository: creative-mode
topic: "EC2 Webhook Deploy — Stale Binary Investigation"
tags: [deploy, webhook, ec2, systemd, site]
status: complete
last_updated: 2026-02-22
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: EC2 Webhook Deploy — Stale Binary

## Task(s)

### 1. Investigate why deployed site is serving stale code (IN PROGRESS — needs EC2 verification)

The deployed site at `creative-mode.ai` is serving the old `Root` layout instead of `ChatLayout` for the `/mayor` page. Confirmed by comparing the HTML:

- **Deployed (stale)**: `<body class="min-h-screen bg-background font-sans antialiased">` — uses `Root` layout, `mayor-container` has `h-[calc(100dvh-3.5rem)]`, missing `overscroll-y-contain touch-pan-y` on chat-messages, missing `navigator.maxTouchPoints === 0` Enter key guard.
- **Local (current)**: `<body class="bg-background font-sans antialiased overflow-clip overscroll-none">` — uses `ChatLayout`, fixed viewport pattern with `fixed inset-0`.

### 2. Previous fix: Deploy webhook binary path mismatch (COMPLETED in code, NOT applied on EC2)

The webhook handler was building the binary to `/tmp/creative-mode-site` but systemd ran `/usr/local/bin/creative-mode-site`. Fixed in commit `580cb53` — both now point to `/home/ubuntu/bin/creative-mode-site`. But the running binary on EC2 still has the OLD webhook code, creating a chicken-and-egg problem.

## Critical References
- `site/internal/webhook/handler.go:195-210` — rebuild pipeline: builds to `/tmp/site-next`, renames to `/home/ubuntu/bin/creative-mode-site`, SIGTERMs self
- `site/creative-mode-site.service:12` — `ExecStart=/home/ubuntu/bin/creative-mode-site`
- Previous handoff: `thoughts/CoreyCole/handoffs/general/2026-02-22_10-47-22_responsive-mayor-chat-layout.md`

## Recent changes

No code changes this session — this was a diagnostic session.

## Learnings

### Chicken-and-egg problem with self-updating webhooks
The webhook handler is compiled INTO the binary it replaces. When we changed the binary destination path from `/usr/local/bin/` to `~/bin/`, the fix only exists in the NEW code. The OLD running binary on EC2 still writes to the old path. Even when GitHub fires the webhook, the old binary:
1. Pulls new code (correct)
2. Builds new binary with new code (correct)
3. Renames it to the OLD path (wrong — uses the old handler's hardcoded path)
4. SIGTERMs itself, systemd restarts from old path (stale binary)

### Verifying deploy state from HTML
Quick way to check: the `<body>` class tells you which layout is active. `overflow-clip overscroll-none` = `ChatLayout` (current). `min-h-screen` = `Root` layout (stale).

## Artifacts
- `site/internal/webhook/handler.go` — webhook rebuild pipeline
- `site/creative-mode-site.service` — systemd unit file
- `thoughts/CoreyCole/handoffs/general/2026-02-22_10-47-22_responsive-mayor-chat-layout.md` — previous handoff with the one-time setup steps

## Action Items & Next Steps

1. **SSH into EC2 and verify current state** — The user believes they already ran the one-time setup steps. Check:
   - Does `~/bin/` exist? (`ls -la ~/bin/`)
   - What does `systemctl cat creative-mode-site` show for `ExecStart`? (should be `/home/ubuntu/bin/creative-mode-site`)
   - What binary is actually running? (`readlink /proc/$(pgrep creative-mode)/exe` or check `systemctl status`)
   - Check webhook logs: `journalctl -u creative-mode-site --since "1 hour ago" | grep webhook`

2. **If the one-time setup was NOT applied**, run:
   ```bash
   mkdir -p ~/bin && \
   sudo cp ~/creative-mode/site/creative-mode-site.service /etc/systemd/system/ && \
   sudo systemctl daemon-reload && \
   sudo systemctl restart creative-mode-site
   ```
   Note: no need to copy the old binary if we're about to rebuild anyway. Just ensure the service file points to `~/bin/` and trigger a rebuild.

3. **If the setup WAS applied but it's still stale**, the webhook may be failing silently. Check:
   - Are `templ` and `tailwindcss` CLIs available in the webhook's PATH? (The service `PATH` is hardcoded in the unit file at line 10)
   - Is `go` available? (`which go` as ubuntu user)
   - Try triggering a manual rebuild by pushing a trivial change to a `site/` file

4. **After fixing deploy**: The mayor chat layout already looks good on desktop (per user). No responsive layout changes needed right now.

## Other Notes

- The site runs on EC2 (Ubuntu), NOT Docker. Direct binary under systemd.
- The webhook only triggers on pushes to `main` that touch `site/` or `pkg/` files (`handler.go:117-139`).
- The git hash in the footer is determined at **runtime** (`site/main.go:39-44` via `git rev-parse`), NOT embedded at compile time. So the hash will appear correct even when the binary is stale — it reflects the repo state, not the binary.
- The `tailwindcss` content paths in the webhook (`handler.go:181`) scan `./pages/**/*` and `./layouts/**/*` — make sure these globs match all template files.
