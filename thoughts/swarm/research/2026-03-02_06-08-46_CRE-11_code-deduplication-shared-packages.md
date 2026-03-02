---
ticket: CRE-11
workflow: dce2410b
session: 126df666
timestamp: 2026-03-02T06:08:46Z
---

# Research: Code Deduplication — Shared Package Design

## Questions

1. Where are duplicated implementations across the codebase (Discord auth, SSE helpers, BridgePlugin)?
2. What are the exact differences between duplicated implementations?
3. How should shared abstractions be designed to consolidate them?

## Findings

### Finding 1: Discord OAuth Flow (harness vs. site) — HIGH severity

The harness (`harness/internal/auth/auth.go`) and site (`site/internal/auth/auth.go`) both implement the full Discord OAuth2 code-grant flow independently. This is the most significant duplication in the codebase.

**Identical functions (copy-paste):**

| Function | Harness Location | Site Location |
|----------|-----------------|---------------|
| `randomHex()` | `auth.go:421-428` | `auth.go:439-445` |
| `devDiscordID()` | `auth.go:514-519` | `auth.go:316-320` |
| `avatarURL()` / `AvatarURL()` | `auth.go:326-332` | `auth.go:46-51` |

**Near-identical functions (same logic, config field names differ):**

| Function | Harness Location | Site Location |
|----------|-----------------|---------------|
| `HandleLogin()` | `auth.go:60-84` | `auth.go:144-168` |
| `exchangeCode()` | `auth.go:335-382` | `auth.go:323-370` |
| `fetchDiscordUser()` | `auth.go:385-418` | `auth.go:373-402` |
| `HandleCallback()` (top half) | `auth.go:87-132` | `auth.go:171-206` |
| `HandleLogout()` | `auth.go:242-260` | `auth.go:258-272` |
| `HandleDevLogin()` | `auth.go:432-510` | `auth.go:276-313` |
| `SessionMiddleware()` | `middleware.go:17-55` | `middleware.go:11-28` |

**Identical constants:**
```go
oauthStateBytes  = 16
oauthStateTTLSec = 300
sessionBytes     = 32
sessionTTLDays   = 7
sessionMaxAgeSec = sessionTTLDays * 24 * 3600
```

**Key divergences (why full unification is hard):**

1. **Storage model**: Harness has normalized `users` + `sessions` tables with sqlc; site stores Discord info directly in `sessions` with raw SQL.
2. **Authorization model**: Harness uses role-based access (`admin`/`user`/`pending`); site uses guild membership + invite code verification.
3. **Callback behavior**: Harness does HTTP 307 redirect; site returns 200 HTML with `<meta refresh>` for Discord popup flow.
4. **HTTP client**: Harness uses `http.DefaultClient`; site uses explicit `*http.Client` with 10s timeout.
5. **Secure cookie logic**: Harness checks `isLocalhost(baseURL)`; site checks `isSecure(redirectURI)` (HTTPS prefix).

**Extractable to shared package (~200 lines):**
- Discord API interactions: `ExchangeCode()`, `FetchUser()`, `AvatarURL()`
- Crypto helpers: `RandomHex()`, `DevDiscordID()`
- OAuth URL building: `AuthorizeURL()`
- CSRF state cookie management: `SetStateCookie()`, `ValidateStateCookie()`
- Session cookie management: `SetSessionCookie()`, `ClearSessionCookie()`
- Constants: all the `oauth*` and `session*` constants

### Finding 2: Datastar Expression & Signal Helpers (harness vs. site) — MEDIUM severity

**DatastarExpression builder — exact copy:**
- Harness: `harness/views/dsutil/expressions.go:1-66`
- Site: `site/internal/ui/utils/expressions.go:1-63`

Same `DatastarExpression` struct with `NewExpression()`, `Statement()`, `SetSignal()`, `Conditional()`, `Build()`, `BuildConditional()`. The site version adds `FocusCapture` (not in harness).

**SignalManager — diverged copy:**
- Harness: `harness/views/dsutil/signals.go:1-93` — flat signals (`$property`)
- Site: `site/internal/ui/utils/signals.go:1-152` — namespaced signals (`$id.property`)

Core methods are the same (`Toggle`, `Set`, `SetString`, `Equals`, `NotEquals`, `Conditional`, `ConditionalAction`, `DataClass`), but the signal reference format differs. Site has extra methods: `ConditionalMultiAction`, `MultiStateConditional`, `TernaryClass`, `TernaryStyle`.

**SSE helper (`dsutil/sse.go`):** Harness-only, one function `GetSSENoCancel()`. No site equivalent.

### Finding 3: Bevy BridgePlugin (2D vs. boardgame) — HIGH severity

`templates/2d/src/bridge.rs` (72 lines) and `templates/boardgame/src/bridge.rs` (70 lines) are **byte-for-byte identical** except:
- Boardgame adds `#[allow(dead_code)]` on `BridgeAction`
- Doc comment is slightly shorter

All shared: `BridgePlugin` struct, `BridgeAction` enum (`NavigateWorld`, `NavigateCheckpoint`, `OpenEmbed`), `PendingBridgeAction` resource, `send_bridge_actions` system, `post_message()` function (WASM + non-WASM variants).

The 3D template has a simpler inline `post_message_to_parent()` in `main.rs:298-316` that sends only `type` (no `data`), used exclusively for cursor lock signaling.

### Finding 4: Bevy Debug Query Infrastructure — MEDIUM severity

All three templates duplicate the JS bridge polling pattern in `debug.rs`:

| Template | File | Lines |
|----------|------|-------|
| 2D | `templates/2d/src/debug.rs:35-86` | ~50 lines |
| Boardgame | `templates/boardgame/src/debug.rs:26-69` | ~43 lines |
| 3D | `templates/3d/client/src/debug.rs:28-63` | ~35 lines |

The I/O scaffolding is identical in all three:
1. Read `window.__debugRequest` via `js_sys::Reflect::get`
2. Clear it immediately
3. Parse JSON into `DebugQuery`
4. Execute query, write result to `window.__debugResponse`

Only the `DebugQuery` enum and query execution differ per template (rooms/hotspots for 2D, board/moves for boardgame, generic ECS reflection for 3D).

### Finding 5: index.html JS Debug Bridge — MEDIUM severity

All three `index.html` files contain identical JS blocks for:
- Debug query relay via `postMessage` (~20 lines each)
- Keyboard shortcut blocking (F-keys, Ctrl combos) (~12 lines each)
- Backtick forwarding to parent (minor variation in 3D for pointer lock)

Locations:
- `templates/2d/index.html:38-67`
- `templates/boardgame/index.html:38-61`
- `templates/3d/client/index.html:55-76`

### Finding 6: Claude Streaming Chat Handler — MEDIUM severity

The Claude chat SSE streaming logic is structurally duplicated:
- Site: `site/internal/mayor/handler.go:64-351` (`HandleChat`)
- Harness: `harness/internal/server/create.go:134-393` (`handleCreateChat`)

Both implement the same ~260 line algorithm: rate limit check, add user message, build Anthropic messages, create SSE, stream tokens, track code blocks, render incremental markdown, handle billing errors, parse WORLD_READY. The `scrollChatJS` constant is identical in both.

Related duplication: scripted fallback handlers (`create.go:396-591` vs `scripted.go:17-164`) and cover art generation (`create.go:594-705` vs `handler.go:356-501`).

Note: `pkg/mayorchat/` already exists as a shared package for conversation/message business logic, but the SSE rendering/streaming coordination layer is reimplemented in each server.

### Finding 7: Minor Duplications

| What | Locations | Severity |
|------|-----------|----------|
| `Trunk.toml` config | All 3 templates | Low — identical files |
| `Cargo.toml` deps | 2D vs boardgame | Low — identical dep blocks |
| Bevy `DefaultPlugins` setup | All 3 templates | Low — same window/asset config |
| Camera spawn+fit | 2D `camera.rs` vs boardgame `camera.rs` | Low — boardgame is subset |
| SQLite init (WAL, busy_timeout) | `harness/db/db.go` vs `site/db/db.go` | Low — different drivers |
| Session cleanup ticker | `harness/main.go:113-127` vs `site/auth.go:431-437` | Low — trivial |

## Architecture Notes

### Existing shared package pattern
Both `harness/go.mod` and `site/go.mod` already use `replace` directives to reference shared packages in `pkg/`:
- `pkg/worldchannel` — Discord channel management
- `pkg/mayorchat` — Chat conversation logic
- `pkg/imagegen` — Image generation client
- `pkg/markdown` — Markdown renderer

This is the established pattern for Go code sharing. A new `pkg/discordauth` would follow the same pattern.

### Rust template isolation
The 2D and boardgame templates are standalone crates (not in a Cargo workspace). The 3D template uses a workspace (`shared`, `server`, `client`). Cross-template sharing would require either:
1. A new shared crate referenced via `path` dependency from each template
2. A Cargo workspace encompassing all templates (likely too invasive — templates are forked per-world)

Given that templates are copied into world directories during builds, option 1 is the only viable approach. The shared crate would need to be copied alongside the template, or templates would need to reference it at a stable path.

### WASM build constraint
Each WASM build uses ~5 GB RAM. Shared crates add no memory overhead (compiled into the same binary), but changing the shared crate would require rebuilding all templates.

## Risks and Considerations

1. **Go module complexity**: Each new `pkg/` module needs its own `go.mod`, and both harness and site need `replace` directives. This is already the pattern, but adds maintenance burden.

2. **Rust shared crate path resolution**: Templates are forked into `data/worlds/{worldID}/{checkpointID}/`. A shared crate at `templates/shared-bevy/` would need either:
   - Relative paths that break when templates are copied (bad)
   - The build pipeline to also copy the shared crate (adds complexity)
   - Embedding shared code via a build-time codegen step (overkill)

3. **SignalManager divergence**: The harness (flat `$property`) and site (namespaced `$id.property`) have genuinely different signal strategies. Unifying them requires either making namespacing optional or migrating all harness templates to namespaced signals.

4. **Chat handler coupling**: The Claude streaming chat logic is tightly coupled to template rendering (templ components) and server-specific SSE patterns. Extracting it requires a callback/interface design for the rendering layer.

5. **Breaking changes risk**: Extracting shared packages from working code introduces regression risk. Each extraction should be accompanied by the existing test suites passing.

## Recommendations

### Priority 1: `pkg/discordauth` (highest impact, lowest risk)

Extract Discord API interactions into a new shared Go package:
```
pkg/discordauth/
├── go.mod
├── client.go      // ExchangeCode(), FetchUser(), AvatarURL()
├── crypto.go      // RandomHex(), DevDiscordID()
├── oauth.go       // AuthorizeURL(), Config, constants
├── cookie.go      // SetStateCookie(), ValidateStateCookie(), SetSessionCookie(), ClearSessionCookie()
└── types.go       // DiscordUser, TokenResponse
```

This eliminates ~200 lines of duplication. Both servers would use the shared client for Discord API calls, but retain their own `HandleCallback()` implementations for the divergent storage/authorization logic.

### Priority 2: `pkg/dsutil` (medium impact, low risk)

Extract the Datastar expression builder:
```
pkg/dsutil/
├── go.mod
├── expressions.go  // DatastarExpression (identical in both)
├── signals.go      // SignalManager with optional namespace support
└── sse.go          // GetSSENoCancel() and other shared helpers
```

The `SignalManager` should accept an optional ID parameter: `Signals(signalsStruct)` for flat (harness) and `NamespacedSignals(id, signalsStruct)` for namespaced (site).

### Priority 3: Shared Bevy crate for bridge + debug (medium impact, medium risk)

Create `templates/shared/` as a library crate with:
```
templates/shared/
├── Cargo.toml
├── src/
│   ├── lib.rs
│   ├── bridge.rs    // BridgePlugin, BridgeAction, PendingBridgeAction
│   └── debug.rs     // Debug query I/O scaffolding (generic over DebugQuery type)
```

Each template would depend on it: `shared = { path = "../shared" }` (for 2D/boardgame) or `cm-shared = { path = "../../shared" }` (for 3D client).

**Caveat**: The world-fork build pipeline must copy this crate alongside the template. This needs a build pipeline change.

### Priority 4: Chat streaming extraction (lower priority, higher risk)

Extract the Claude streaming SSE coordination into `pkg/mayorchat/` (which already handles conversation logic). This requires designing a rendering interface that both servers can implement. Defer until priorities 1-2 are stable.

### Not recommended for extraction

- SQLite init — different drivers (`mattn/go-sqlite3` vs `modernc.org/sqlite`), marginal benefit
- Session cleanup — trivial code, not worth a shared abstraction
- Bevy DefaultPlugins setup — template-specific, only ~15 lines
- Camera code — boardgame is a strict subset, not worth the coupling
