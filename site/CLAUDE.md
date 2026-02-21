# Site — Marketing & Onboarding

Go server (Echo + templ + Datastar + Tailwind) that serves the marketing pages and the "Meet the Mayor" onboarding flow.

## Architecture

| Directory | Purpose |
|-----------|---------|
| `main.go` | Route definitions, middleware wiring, greeting text |
| `layouts/` | templ root layout (HTML head, nav, footer) |
| `pages/` | templ page components and SSE fragments |
| `internal/auth/` | Discord OAuth, session management, invite codes |
| `internal/db/` | SQLite persistence (WAL mode, schema migrations) |
| `internal/mayor/` | Claude-powered onboarding conversation (uses `claude-sonnet-4-5-20250929`) |
| `pkg/markdown/` | Goldmark markdown renderer (shared package, not site-internal) |
| `internal/webhook/` | GitHub push webhook handler for self-rebuild (`POST /webhook/github`) |
| `internal/ui/` | Shared templ components (tooltip) and utilities (signals, expressions, tailwind merge) |
| `static/` | CSS, images |

## Meet the Mayor — Onboarding Design

The mayor onboarding is a real-time chat interface where a user talks to Claude to design their multiplayer game world. The conversation collects four things: world setting, gameplay, world name, and mayor name.

### Conversation Flow

1. **GET /mayor** — Builds system prompt (with Discord username + taken mayor names), seeds greeting into `ConversationManager`, renders `MayorPage` templ
2. **POST /mayor/chat** — SSE endpoint: reads Datastar signals, streams Claude's response token-by-token, renders markdown incrementally
3. **WORLD_READY marker** — When Claude emits `WORLD_READY|<mayor>|<world>|<summary>`, the handler creates a Discord channel and shows the hatched card

### Personality Rules

The mayor's system prompt is anti-sycophantic by design:
- No filler ("Great question!", "That sounds amazing!")
- Short responses (2-3 sentences typical)
- One question at a time, not a questionnaire
- Adaptive personality — the mayor's tone evolves to match the world's theme (cyberpunk = edgier, cozy village = warmer)
- The mayor has opinions and pushes back on vague ideas

### Scripted Fallback

When the Anthropic API is unavailable (billing error, rate limit, overload), the conversation falls back to `scripted.go` — four pre-written responses that collect the same four data points. The fallback is invisible to the user (same SSE/templ rendering path).

### Conversation Persistence

After world hatching, the full onboarding conversation is pinned as a JSON message in the Discord channel (`PinOnboardingData`). This enables future OpenClaw agent bootstrap — the harness reads the pinned conversation back with `ReadOnboardingData` and uses it to generate the agent's personality files (IDENTITY.md, SOUL.md, etc).

### World Hatching Webhook

After the Discord channel is created, the site fires a webhook to the harness to trigger mayor agent provisioning:

```
POST {HARNESS_URL}/api/world-hatched
Header: X-Hook-Secret: {CM_HOOK_SECRET}
Body: { discord_channel_id, world_name, mayor_name, creator_discord_id, creator_username }
```

This is fire-and-forget (goroutine in `notifyHarnessWorldHatched`). Errors are logged but not surfaced to the user. The harness creates a world record and provisions the OpenClaw agent asynchronously.

**Required env vars** in `site.env`: `CM_HOOK_SECRET` (shared secret) and `HARNESS_URL` (e.g., `http://100.x.x.x:8080` via Tailscale).

### Key Constraints

- **ReadSignals before NewSSE** — `ReadSignals` must be called before `NewSSE` (which flushes headers). Reversing this silently breaks the handler.
- **Greeting dedup** — The greeting is only seeded into `ConversationManager` if the conversation is empty, preventing duplicate greetings on page refresh.
- **Rate limiting** — 2-second cooldown between messages per user.
- **Mayor name uniqueness** — System prompt includes taken names so Claude avoids them. Race condition safety net appends roman numeral suffixes at hatch time.

### Datastar Integration

The chat uses Datastar's SSE protocol:
- `data-signals` for `mayor_input` binding
- `data-bind:mayor_input` on the input field (colon syntax, not dashes)
- `data-on:keydown` / `data-on:click` to POST via `datastar.PostSSE`
- Server patches DOM elements via `sse.PatchElementTempl` and clears input via `sse.MarshalAndPatchSignals`

## Mobile Layout Patterns

### Fixed-Viewport Chat UIs

The mayor chat page uses `ChatLayout` (in `layouts/chat.templ`) — a purpose-built layout for full-screen chat that prevents document-level scrolling.

**Key rules:**
- **Never use `position: fixed` or `overflow: hidden` on `<body>` or `<html>` alone** — Android Chromium ignores them. This is a [documented browser behavior](https://github.com/whatwg/compat/issues/79), not a bug.
- **Use `overflow: clip` instead of `overflow: hidden`** on html/body — `clip` forbids all scrolling (including programmatic), while `hidden` still creates a scroll container.
- **Use `position: fixed; inset: 0` on a wrapper div** — removes all content from document flow so the body has nothing to scroll.
- **Use `touch-action` to control scrolling at the compositor level** — `touch-action: none` on the wrapper, `touch-action: pan-y` on the scrollable area.

**The proven pattern** — layered defense:

```html
<html class="overflow-clip">
<body class="overflow-clip overscroll-none">
  <div class="fixed inset-0 flex flex-col overflow-hidden touch-none">
    <header class="shrink-0">nav</header>
    <div class="flex-1 min-h-0 flex flex-col overflow-hidden">
      <div class="shrink-0">pinned banner</div>
      <div class="flex-1 min-h-0 overflow-y-auto overscroll-y-contain touch-pan-y">scrollable content</div>
      <div class="shrink-0">pinned input</div>
    </div>
  </div>
</body>
```

Why each layer matters:

| Technique | Purpose |
|-----------|---------|
| `overflow-clip` on html + body | Stronger than `hidden` — forbids all document-level scrolling |
| `fixed inset-0` on wrapper | Removes content from document flow — nothing to scroll |
| `overflow-hidden` on wrapper | Clips any overflow within the fixed container |
| `touch-none` on wrapper | Tells compositor: no touch scrolling on wrapper |
| `touch-pan-y` on messages | Allows vertical scroll only in the messages area |
| `overscroll-y-contain` on messages | Prevents scroll chaining to parent when hitting top/bottom |
| `min-h-0` on flex children | Essential — without it, flex children can't shrink below content size |
| `interactive-widget=resizes-content` viewport meta | Layout viewport shrinks when virtual keyboard opens |

**What does NOT work** (tried and failed on Android Brave):
- `position: fixed` or `overflow: hidden` on body/html alone
- `h-[100svh]` wrapper without `position: fixed` (browser still scrolls the document)
- Inline `style` attributes mixed with Tailwind classes

### Tailwind-Only Styling

Use Tailwind classes exclusively for layout — no inline `style` attributes. Mixing `style="position:fixed;..."` with Tailwind classes creates specificity confusion and makes debugging harder. If Tailwind doesn't have a utility for what you need, use arbitrary values like `h-[100svh]`.

## Running

### Local dev (Docker)

```bash
cd site
just up    # Docker compose with hot-reload via air
just down  # stop
just logs  # container logs
```

### EC2 production deployment

The marketing site runs on an EC2 instance as a native Go binary under systemd (not Docker).

**Server**: Ubuntu 24.04, connected to the same Tailscale tailnet as the harness VPS.

**Traffic flow**:
```
Browser → Route 53 → EC2 Elastic IP → Caddy:443 (TLS) → localhost:3000
```

**Setup** (on a fresh EC2 instance):
1. Install Tailscale, UFW, Fail2Ban, SSH lockdown (same pattern as `scripts/vps-bootstrap.sh`)
2. Install Go and build tools
3. Copy `site.env.example` to `~/.config/creative-mode/site.env` and fill in secrets
4. Copy `creative-mode-site.service` to `/etc/systemd/system/`
5. Build and start:
   ```bash
   cd ~/creative-mode/site
   just install && just build
   sudo cp site-linux /usr/local/bin/creative-mode-site
   sudo systemctl enable --now creative-mode-site
   ```

**Auto-deploy**: Pushes to `main` trigger automatic deployment via a GitHub webhook (`POST /webhook/github`). The handler pulls the latest code, rebuilds the binary, copies it to `/usr/local/bin/creative-mode-site`, and sends itself SIGTERM. Systemd restarts the service with the new binary. No manual deploy steps needed.

**Logs**: `just deploy-logs` or `journalctl -u creative-mode-site -f`

**DNS**: `creative-mode.ai` → Route 53 A record → EC2 Elastic IP → Caddy:443 → localhost:3000

**Key files**:
- `creative-mode-site.service` — systemd unit (runs as `ubuntu` user, listens on port 3000 behind Caddy)
- `site.env.example` — env var template (Discord OAuth, bot token, Anthropic key, invite codes)
- `internal/webhook/` — GitHub push webhook handler for self-rebuild
