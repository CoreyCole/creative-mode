# Site — Marketing & Onboarding

Go server (Echo + templ + Datastar + Tailwind) that serves the marketing pages and the "Meet the Mayor" onboarding flow.

## Architecture

| Directory | Purpose |
|-----------|---------|
| `main.go` | Route definitions, middleware wiring, greeting text |
| `layouts/` | templ root layout (HTML head, nav, footer) |
| `pages/` | templ page components and SSE fragments |
| `internal/auth/` | Discord OAuth, session management, invite codes |
| `internal/mayor/` | Claude-powered onboarding conversation |
| `internal/markdown/` | Goldmark markdown renderer |
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

## Running

This site is part of the Docker compose setup. See the root CLAUDE.md for `just live` / `just up` / `just down`. Never run `go run .` directly.
