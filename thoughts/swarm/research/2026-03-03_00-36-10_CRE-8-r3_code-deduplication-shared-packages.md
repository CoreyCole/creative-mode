---
ticket: CRE-8-r3
workflow: 5a3b4fb6
session: 002adab5
timestamp: 2026-03-03T00:36:10Z
---

# Research: Code Deduplication — Shared Package Design

## Questions

1. Where are duplicated implementations across the codebase (Discord auth, SSE helpers, BridgePlugin)?
2. What are the exact differences between duplicated implementations?
3. How should shared abstractions be designed to consolidate them?

## Findings

### Finding 1: Discord OAuth Flow (harness vs. site) — HIGH severity

The harness (`harness/internal/auth/auth.go`) and site (`site/internal/auth/auth.go`) both implement the full Discord OAuth2 code-grant flow independently. This is the most significant duplication in the codebase (~200 lines extractable).

**Identical functions (copy-paste):**

| Function | Harness Location | Site Location |
|----------|-----------------|---------------|
| `randomHex()` | `auth.go:421-428` | `auth.go:439-445` |
| `devDiscordID()` | `auth.go:514-519` | `auth.go:316-320` |
| `avatarURL()` / `AvatarURL()` | `auth.go:326-332` | `auth.go:46-51` |

**Near-identical functions (~95% same, config field names differ):**

| Function | Harness Location | Site Location | Key Difference |
|----------|-----------------|---------------|----------------|
| `HandleLogin()` | `auth.go:60-84` | `auth.go:144-168` | Cookie Secure flag logic |
| `exchangeCode()` | `auth.go:335-382` | `auth.go:323-370` | HTTP client (Default vs timeout), context passing |
| `fetchDiscordUser()` | `auth.go:385-418` | `auth.go:373-402` | Return struct (3 vs 5 fields) |
| `HandleLogout()` | `auth.go:242-260` | `auth.go:258-272` | DB call (typed sqlc vs raw SQL) |
| `HandleDevLogin()` | `auth.go:432-510` | `auth.go:276-313` | Role model vs verification flags |

**Identical constants (both files):**
```go
oauthStateBytes  = 16
oauthStateTTLSec = 300
sessionBytes     = 32
sessionTTLDays   = 7
sessionMaxAgeSec = sessionTTLDays * 24 * 3600
```

**Key divergences (why full unification is hard):**

| Aspect | Harness | Site |
|--------|---------|------|
| DB layer | `*db.DB` + sqlc typed queries | Raw `*sql.DB` with inline SQL |
| Session model | Session → User ID (separate user table) | Self-contained (Discord data in session row) |
| Auth model | Role-based (admin/user/pending) with approval workflow | Verification-based (guild membership + invite code) |
| Callback behavior | HTTP 307 redirect | 200 HTML + meta-refresh (Discord popup workaround) |
| HTTP client | `http.DefaultClient` | `*http.Client{Timeout: 10s}` |
| Cookie Secure | `isLocalhost()` parses URL hostname | `isSecure()` checks `https://` prefix |
| Config fields | `DiscordClientID`, `DiscordClientSecret`, `BaseURL` | `ClientID`, `ClientSecret`, `RedirectURI`, `BotToken`, `GuildID` |

**Extractable to `pkg/discordauth` (~200 lines):**
- `ExchangeCode(ctx, httpClient, config)` — Discord token exchange
- `FetchUser(ctx, httpClient, accessToken)` — Discord user fetch
- `AvatarURL(userID, avatar)` — CDN avatar URL construction
- `RandomHex(n)` — crypto random hex string
- `DevDiscordID(username)` — deterministic dev ID via FNV hash
- `AuthorizeURL(clientID, redirectURI, state)` — OAuth authorize URL builder
- `SetStateCookie(c, state, secure)` — CSRF state cookie
- `ValidateStateCookie(c)` — state cookie validation + clear
- `SetSessionCookie(c, sessionID, secure, maxAge)` — session cookie
- `ClearSessionCookie(c)` — cookie deletion
- Constants: `oauthStateBytes`, `oauthStateTTLSec`, `sessionBytes`, etc.
- Types: `DiscordUser` (5 fields, exported), `TokenResponse`

**NOT extracted (intentionally divergent):**
- `HandleCallback()` — different storage models, redirect strategies
- `HandleLogin()` — different config shapes (could extract OAuth URL logic)
- `SessionMiddleware()` — completely different auth models
- `resolveUser()` / `GetSession()` — harness-only / site-only
- Cookie Secure detection — different approaches (`isLocalhost` vs `isSecure`)

### Finding 2: Datastar Expression & Signal Helpers — MEDIUM severity

**DatastarExpression builder — identical:**

`harness/views/dsutil/expressions.go` and `site/internal/ui/utils/expressions.go` share the same 7 core functions: `NewExpression()`, `Statement()`, `SetSignal()`, `Conditional()`, `Build()`, `BuildConditional()`, and the `DatastarExpression` struct. The site adds a `FocusCapture` builder (lines 66-100) not present in the harness.

**SignalManager — fundamentally divergent:**

| Aspect | Harness (`dsutil/signals.go`) | Site (`utils/signals.go`) |
|--------|-------------------------------|---------------------------|
| Constructor | `Signals(struct)` | `Signals(id, struct)` |
| Signal ref | `$property` (flat) | `$id.property` (namespaced) |
| JSON output | `{"open":false}` | `{"my_comp":{"open":false}}` |
| Extra methods | None | `ConditionalMultiAction`, `MultiStateConditional`, `TernaryClass`, `TernaryStyle` |

The divergence is intentional: the harness has one component per page (no name collisions), while the site has multiple components (needs namespacing). Unification would require making namespacing optional.

**SSE helper (`dsutil/sse.go`):** Harness-only, single function `GetSSENoCancel()`. No site equivalent — site uses `datastar.GetSSE()` directly.

### Finding 3: Bevy BridgePlugin (2D vs. boardgame) — HIGH severity

`templates/2d/src/bridge.rs` (72 lines) and `templates/boardgame/src/bridge.rs` (70 lines) are **byte-for-byte identical** except:
- Boardgame adds `#[allow(dead_code)]` on `BridgeAction`
- Doc comment is slightly shorter

Shared code: `BridgePlugin`, `BridgeAction` enum (`NavigateWorld`, `NavigateCheckpoint`, `OpenEmbed`), `PendingBridgeAction` resource, `send_bridge_actions` system, `post_message()` with WASM/non-WASM variants.

The 3D template has **no BridgePlugin**. Instead it has an inline `post_message_to_parent(msg_type: &str)` in `main.rs:299-316` that sends only `type` (no `data`), used exclusively for cursor lock signaling (`"cursor-locked"`, `"cursor-unlocked"`).

### Finding 4: Bevy Debug Query Infrastructure — MEDIUM severity

All three templates duplicate the same JS bridge polling pattern in `debug.rs`:
1. Read `window.__debugRequest` via `js_sys::Reflect::get`
2. Clear it immediately
3. Parse JSON into `DebugQuery`
4. Execute query, write result to `window.__debugResponse`

Line ranges: 2D (44-85), boardgame (31-68), 3D (29-62).

**But the query engines are completely different:**
- 2D: domain-specific (rooms, hotspots, dialog, click actions), regular system with 7 params
- Boardgame: domain-specific (board, moves, reset), regular system with 3 params
- 3D: generic ECS reflection via `QueryBuilder<FilteredEntityRef>`, exclusive system taking `&mut World`

Only the I/O scaffolding (~30 lines) is truly extractable. The query execution is template-specific by design.

### Finding 5: index.html JS Bridge — MEDIUM severity

All three `index.html` files duplicate:
- Keyboard shortcut blocking (F-keys, Ctrl combos) — identical in all three
- Debug query relay via `postMessage` — identical pattern, ~20 lines each
- Backtick forwarding — diverges for 3D (pointer lock check vs input focus check)

The 3D template has extra pointer lock / Keyboard Lock API code. The 2D template has a `reload-room` handler.

### Finding 6: Claude Streaming Chat Handler — MEDIUM severity (deferred)

`site/internal/mayor/handler.go:64-351` and `harness/internal/server/create.go:134-393` implement the same ~260 line SSE streaming algorithm. Tightly coupled to template rendering (templ components). `pkg/mayorchat/` already handles conversation business logic, but the SSE coordination layer is reimplemented.

**Recommendation**: Defer extraction until priorities 1-2 are stable — requires interface design for the rendering layer.

### Finding 7: Minor Duplications (not worth extracting)

| What | Locations | Severity | Reason to Skip |
|------|-----------|----------|----------------|
| `Trunk.toml` config | All 3 templates | Low | Identical files, trivial |
| `Cargo.toml` deps | 2D vs boardgame | Low | Same dep blocks, diverge for template-specific deps |
| Bevy `DefaultPlugins` setup | All 3 templates | Low | ~15 lines, template-specific window config |
| Camera spawn+fit | 2D vs boardgame | Low | Boardgame is strict subset |
| SQLite init (WAL) | harness vs site | Low | Different drivers |
| Session cleanup ticker | harness vs site | Low | Trivial code |

## Architecture Notes

### Existing shared package pattern
Both `harness/go.mod` and `site/go.mod` use `replace` directives for packages in `pkg/`:
- `pkg/worldchannel` — Discord channel management
- `pkg/mayorchat` — Chat conversation logic
- `pkg/imagegen` — Image generation client
- `pkg/markdown` — Markdown renderer

A new `pkg/discordauth` would follow this exact pattern — its own `go.mod`, with `replace` directives in both consumers.

### Rust template isolation
Templates are standalone crates (not in a Cargo workspace). Cross-template sharing requires a shared crate referenced via `path` dependency. **Critical constraint**: templates are copied into world directories during builds, so the build pipeline must also copy the shared crate alongside the template.

### WASM build constraint
Each WASM build uses ~5 GB RAM. Shared crates add no memory overhead (compiled into same binary), but changing the shared crate requires rebuilding all templates.

## Risks and Considerations

1. **Go module complexity**: Each new `pkg/` module needs its own `go.mod` and `replace` directives in both consumers. Already the pattern, but adds maintenance burden.

2. **Rust shared crate path resolution**: Templates forked into `data/worlds/{worldID}/{checkpointID}/` would need the build pipeline to copy the shared crate alongside the template. Relative paths like `path = "../shared"` break when templates are copied.

3. **SignalManager divergence**: Flat (`$property`) vs namespaced (`$id.property`) are genuinely different strategies. Unification requires either optional namespacing or migrating all harness templates to namespaced signals.

4. **Chat handler coupling**: Claude streaming SSE logic is tightly coupled to template rendering. Extracting requires callback/interface design for the rendering layer.

5. **Breaking changes risk**: Extracting shared packages from working code introduces regression risk. Each extraction should be accompanied by passing tests.

6. **Color constants in Bevy**: `Color::srgba()` may not be usable in `const` contexts in Bevy 0.15. May need `lazy_static` or `const fn` wrappers.

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

Eliminates ~200 lines of duplication. Both servers retain their own `HandleCallback()` for divergent storage/authorization logic. Low risk because the extracted functions have no side effects beyond HTTP calls.

### Priority 2: `pkg/dsutil` (medium impact, low risk)

Extract the Datastar expression builder only:
```
pkg/dsutil/
├── go.mod
└── expressions.go  // DatastarExpression (identical in both)
```

**Do NOT unify SignalManager** — the flat vs namespaced divergence is intentional and the cost of abstraction exceeds the benefit. Each consumer keeps its own `signals.go`. The `sse.go` helper is harness-only and stays in `dsutil/`.

### Priority 3: Shared Bevy crate for BridgePlugin (medium impact, medium risk)

Create `templates/shared/` with `BridgePlugin`, `BridgeAction`, `PendingBridgeAction`, `post_message()`:
```
templates/shared/
├── Cargo.toml
└── src/
    ├── lib.rs
    └── bridge.rs    // BridgePlugin + BridgeAction + post_message
```

Each template depends via `path`: `shared = { path = "../shared" }`. The build pipeline must copy this crate alongside templates.

**Do NOT extract debug query I/O** — only ~30 lines are shared, and the query engines are entirely template-specific. The abstraction overhead (generic over `DebugQuery` type) isn't worth it.

### Priority 4: Chat streaming extraction (defer)

Defer until `pkg/discordauth` and `pkg/dsutil` are stable. Requires designing a rendering interface that both servers implement. High coupling to templ components makes this the riskiest extraction.

### Not recommended for extraction

- SQLite init — different drivers, marginal benefit
- Session cleanup — trivial code
- Bevy DefaultPlugins — template-specific, ~15 lines
- Camera code — boardgame is strict subset
- Debug query I/O — only ~30 shared lines, high abstraction cost
- index.html JS — diverges for 3D pointer lock, small duplication
