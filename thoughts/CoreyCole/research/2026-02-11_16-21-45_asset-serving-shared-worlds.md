---
date: 2026-02-11T16:21:45-08:00
researcher: CoreyCole
git_commit: 8e75ee486f31d3a088bfdf7cc688a481a33b9d19
branch: main
repository: creative-mode
topic: "How to serve and share assets across multiple Bevy/WASM game worlds from the harness server"
tags: [research, codebase, assets, bevy, wasm, trunk, shared-assets, caching]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
---

# Research: Serving Shared Assets Across Multiple Bevy/WASM Game Worlds

**Date**: 2026-02-11T16:21:45-08:00
**Researcher**: CoreyCole
**Git Commit**: 8e75ee486f31d3a088bfdf7cc688a481a33b9d19
**Branch**: main
**Repository**: creative-mode

## Research Question

How should we serve assets from the harness server so they can be shared across multiple Bevy/WASM game worlds? What are the best practices for Bevy web asset loading, how does Trunk handle assets, and what patterns work for our unique multi-world architecture?

## Summary

The architecture is already partially designed but not yet implemented. The harness has a `/assets` route pointing to `data/shared-assets/` (which doesn't exist yet), and `template/CLAUDE.md` documents the pattern `asset_server.load("http://{harness_host}/assets/...")`. The key findings are:

1. **Bevy WASM uses `HttpWasmAssetReader`** — on web, all asset loads become HTTP GETs relative to the WASM page by default, but we should override this to point at the harness `/assets` endpoint
2. **Trunk should NOT bundle assets** — our `index.html` correctly omits `copy-dir` directives; assets should come from the shared harness endpoint, not be duplicated per-world
3. **Browser HTTP cache is our primary sharing mechanism** — same-origin iframes share a cache partition, so multiple worlds loading the same asset URL only download it once
4. **Three approaches exist** for configuring the base URL, ranging from simple (AssetPlugin.file_path) to flexible (custom AssetReader or full URL loading)

## Detailed Findings

### 1. Current State of Asset Infrastructure

#### Harness Server — Route Already Exists

`harness/internal/server/server.go:100`:
```go
e.Static("/assets", filepath.Join(s.DataDir, "shared-assets"))
```

This serves files from `data/shared-assets/` at the `/assets/*` URL path. The route is **public** (no auth required), registered before the auth middleware chain. The `data/shared-assets/` directory does not yet exist.

#### Template — Pattern Documented, Not Implemented

`template/CLAUDE.md:136-137`:
```
Assets load from HTTP: asset_server.load("http://{harness_host}/assets/...")
Do NOT use copy-dir in client/index.html for assets — they are served separately
```

The current client (`template/client/src/main.rs`) uses **zero external assets** — everything is procedurally generated (ground plane, capsule meshes, lights). No `AssetServer` usage, no `load()` calls, no `assets/` directory exists anywhere in the template.

#### Trunk — Minimal Config, No Asset Bundling

`template/client/Trunk.toml`:
```toml
[build]
target = "index.html"
filehash = true
minify = "on_release"

[tools]
wasm_bindgen = "0.2.108"
```

`template/client/index.html` has no `<link data-trunk rel="copy-dir" ...>` directives. Trunk only outputs the WASM module loader (`index.html`, `client-<hash>.js`, `client-<hash>_bg.wasm`).

### 2. How Bevy Loads Assets on WASM

#### Platform Detection

When compiled to `wasm32`, Bevy's `AssetSource::get_default_reader()` creates an `HttpWasmAssetReader` instead of a `FileAssetReader`. The reader prepends a configurable `root_path` (default `"assets"`) to all asset paths and issues HTTP GET requests via `web_sys::XmlHttpRequest`.

#### Default Behavior

```rust
asset_server.load("models/tree.glb")
// → HTTP GET ./assets/models/tree.glb (relative to the page serving the WASM)
```

For our setup, the WASM is served from `/wasm/{worldID}/{cpID}/index.html`, so the default would resolve to `/wasm/{worldID}/{cpID}/assets/models/tree.glb` — which is **wrong**. We need to redirect asset loading to `/assets/` at the harness root.

#### Critical WASM Configuration: AssetMetaCheck::Never

On WASM, Bevy tries to load `.meta` sidecar files for every asset. These don't exist on HTTP servers, causing 404 spam. **Must** be disabled:

```rust
app.add_plugins(DefaultPlugins.set(AssetPlugin {
    meta_check: AssetMetaCheck::Never,
    ..default()
}));
```

### 3. Approaches for Shared Asset Loading

#### Approach A: Full URL Loading (Recommended for Our Use Case)

Since Bevy 0.17, `bevy_web_asset` was upstreamed into core. You can load assets with full HTTP/HTTPS URLs:

```rust
// In Cargo.toml:
// bevy = { version = "0.18", features = ["https"] }

fn setup(asset_server: Res<AssetServer>) {
    let handle = asset_server.load("http://localhost:8080/assets/models/tree.glb");
}
```

**Pros**: Most explicit, no ambiguity about where assets come from, works with any origin.
**Cons**: Requires knowing the harness host at compile time or reading it from URL params (like we already do for `server_port`). Requires the `https` Bevy feature.

**For our project**: The client already reads `server_port` from URL query params via `web_sys`. We should add a `harness_host` parameter the same way, or derive it from `window.location.origin`.

#### Approach B: Override AssetPlugin.file_path

```rust
app.add_plugins(DefaultPlugins.set(AssetPlugin {
    file_path: "/assets".to_string(),  // Absolute path from origin root
    meta_check: AssetMetaCheck::Never,
    ..default()
}));
```

Then load normally:
```rust
asset_server.load("models/tree.glb")
// → HTTP GET /assets/models/tree.glb (from origin root)
```

**Pros**: Simplest change, all `load()` calls use short paths, works because WASM and assets are same-origin.
**Cons**: Only works for same-origin. Less explicit about where assets come from. Need to verify `HttpWasmAssetReader` handles absolute paths correctly (it may treat `/assets` as relative).

#### Approach C: Custom AssetReader / Named Asset Source

Register a custom or named asset source that points to the harness:

```rust
// Named source: load("harness://models/tree.glb")
app.register_asset_source(
    "harness",
    AssetSourceBuilder::platform_default("http://localhost:8080/assets", None),
);
```

**Pros**: Explicit source prefix, can have multiple sources (e.g., `harness://` for shared, default for world-specific).
**Cons**: More complex, every `load()` call needs the prefix.

### 4. Recommended Architecture: Deriving Harness Host from Window Location

The cleanest approach for our multi-world setup:

```rust
// In client/src/main.rs, alongside get_server_port():

fn get_harness_origin() -> String {
    #[cfg(target_family = "wasm")]
    {
        let window = web_sys::window().unwrap();
        window.location().origin().unwrap()
    }
    #[cfg(not(target_family = "wasm"))]
    {
        "http://localhost:8080".to_string()
    }
}

// Then load assets with:
fn setup(asset_server: Res<AssetServer>) {
    let origin = get_harness_origin(); // or pass as a Resource
    let handle = asset_server.load(format!("{origin}/assets/models/tree.glb"));
}
```

This works because the WASM iframe is served from the same harness origin (`/wasm/{worldID}/{cpID}/index.html`), so `window.location.origin` returns the harness URL.

**Even simpler**: Since we're same-origin, we can use Approach B with `file_path: "/assets"` and skip the full URL construction entirely — the browser resolves `/assets/...` relative to the origin.

### 5. How Trunk Handles Assets (and Why We Skip It)

#### Trunk's Asset Pipeline

Trunk can copy directories into `dist/` using HTML directives:
```html
<link data-trunk rel="copy-dir" href="assets" />
```

This recursively copies `assets/` → `dist/assets/` with no hashing, no transformation. The WASM then loads from `./assets/` relative to `index.html`.

#### Why We Don't Use This

In our architecture, each world gets its own Trunk build output in `data/wasm-builds/{worldID}/{cpID}/`. If Trunk bundled assets:
- **Asset duplication**: Every world's build would contain a full copy of all assets
- **Build time**: Trunk would need to copy (potentially large) asset files for every build
- **No sharing**: Browser wouldn't cache assets across worlds because URLs would differ (`/wasm/world1/cp1/assets/tree.glb` vs `/wasm/world2/cp1/assets/tree.glb`)

By serving assets from a single `/assets` endpoint, we get:
- **Zero duplication**: One copy of each asset on disk
- **Fast builds**: Trunk only outputs the small WASM + JS files
- **Browser cache sharing**: All worlds load from the same URL, so the browser caches once

#### Trunk's `public_url` Setting

While not needed for our approach, Trunk's `public_url` in `Trunk.toml` controls the base path for `.wasm` and `.js` references in the generated HTML:

```toml
[build]
public_url = "/"  # default
# Could be: public_url = "https://cdn.example.com/game/v1/"
```

We don't need this because the harness rewrites the dist output location with `--dist` to serve from `/wasm/{worldID}/{cpID}/`, and the relative paths work correctly.

### 6. Caching Strategy

#### Browser HTTP Cache (Primary Mechanism)

Same-origin iframes share a browser HTTP cache partition. When multiple worlds load the same asset URL:
1. First iframe: HTTP GET `/assets/models/tree.glb` → cache miss → download
2. Second iframe: HTTP GET `/assets/models/tree.glb` → cache hit → no network

#### Recommended Cache Headers for the Harness

Currently, Echo's `e.Static()` uses Go's `http.ServeFile` which supports `If-Modified-Since` / `304 Not Modified` but sets no explicit `Cache-Control`.

**For development** (current state): Fine as-is — no caching means assets always reload when changed.

**For production**: Add middleware on the `/assets` route:

```go
// Content-addressed assets (hash in filename): cache forever
// e.g., /assets/models/tree-a1b2c3d4.glb
Cache-Control: public, max-age=31536000, immutable

// Stable-name assets: cache with revalidation
// e.g., /assets/models/tree.glb
Cache-Control: public, max-age=3600
ETag: "<content-hash>"
```

The `immutable` directive prevents the browser from ever revalidating — only safe when filenames change with content (content-addressed storage).

#### Content-Addressed Asset Storage (Future Optimization)

```
data/shared-assets/
  models/
    tree-a1b2c3d4.glb      # hash in filename
    rock-e5f6a7b8.glb
  textures/
    grass-c9d0e1f2.png
  manifest.json             # logical name → hashed filename
```

With a manifest, the client fetches `manifest.json` first, then uses hashed filenames for everything else. This enables aggressive caching (`immutable`) while still allowing updates.

#### CORS Considerations

Since our WASM and assets are **same-origin** (both served from the harness), CORS headers are NOT needed. If we later move assets to a CDN on a different origin, we'd need:

```
Access-Control-Allow-Origin: https://harness.example.com
```

### 7. WASM Build Artifact Serving (Current)

For reference, the WASM build output is per-world/checkpoint and already has content-hash filenames via Trunk's `filehash = true`:

```
data/wasm-builds/{worldID}/{cpID}/
  index.html                        # entry point, references hashed files
  client-d793988a4ea55a66.js       # wasm-bindgen glue
  client-d793988a4ea55a66_bg.wasm  # the actual WASM module
```

The harness serves these via `handleWASMArtifacts` at `/wasm/:worldID/:cpID/*` with auth middleware. These are **not** shared across worlds — each checkpoint has its own WASM build.

### 8. Asset Directory Structure Recommendation

```
data/
  shared-assets/            # Shared across ALL worlds, served at /assets/*
    models/                 # 3D models (.glb, .gltf)
    textures/               # Textures (.png, .jpg, .ktx2)
    audio/                  # Sound effects and music (.ogg, .mp3)
    fonts/                  # Font files (.ttf, .otf)
    scenes/                 # Pre-built scene files (.ron, .scn)
  wasm-builds/              # Per-world WASM output (existing)
    {worldID}/{cpID}/
  worlds/                   # Per-world source code (existing)
    {name}_{ts}_{id}/{cpID}/
```

Assets in `data/shared-assets/` would be:
- **Manually curated** initially (placed by operators)
- **Uploaded via admin UI** (future feature)
- **Generated by Claude Code** in world contexts (write to shared-assets instead of per-world)

### 9. Implementation Steps

#### Phase 1: Basic Asset Loading (Minimal)

1. Create `data/shared-assets/` directory
2. Add `AssetMetaCheck::Never` to `DefaultPlugins` in `template/client/src/main.rs`
3. Set `file_path: "/assets"` on `AssetPlugin` (or use full URL approach)
4. Place a test asset (e.g., a `.glb` model) in `data/shared-assets/models/`
5. Add a system in the client that loads and spawns it
6. Verify it works across multiple world iframes

#### Phase 2: Caching (When Assets Are Significant)

1. Add `Cache-Control` middleware to the `/assets` route in server.go
2. Consider content-addressed filenames for immutable caching
3. Add Brotli/gzip compression middleware for texture files

#### Phase 3: Asset Management (Future)

1. Admin UI for uploading assets to `data/shared-assets/`
2. Asset manifest system for clients to discover available assets
3. Lazy loading / streaming for large assets
4. CDN integration for production deployment

## Code References

- `harness/internal/server/server.go:100` — `/assets` static route definition
- `harness/internal/server/server.go:440-456` — WASM artifact serving handler
- `harness/internal/build/builder.go:104-112` — Trunk build with `--dist` flag
- `template/client/Trunk.toml` — Trunk configuration (filehash, minify)
- `template/client/src/main.rs:30-38` — DefaultPlugins setup (where AssetPlugin goes)
- `template/client/src/main.rs:107-125` — URL parameter reading (pattern to reuse for asset URL)
- `template/CLAUDE.md:136-137` — Documented asset loading pattern

## Architecture Insights

1. **Same-origin is the key simplification** — because the WASM iframe and the asset endpoint are both served from the harness, we avoid CORS complexity and get free browser cache sharing.

2. **Trunk's `--dist` redirection** is elegant — it means WASM builds go to a serving directory without needing post-build copies. Assets should follow a similar "single source of truth on disk" pattern.

3. **The harness is the asset server** — for now, there's no need for a separate CDN or asset service. Echo's `e.Static()` is sufficient. The architecture can evolve to S3/CDN later by just changing the URL the client loads from.

4. **Per-world assets vs shared assets** — currently all assets would be shared. If worlds need custom assets (e.g., Claude generates a texture for a specific world), we'd need either: (a) a per-world asset directory with a fallback to shared, or (b) all assets go in shared with namespacing.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/component-4-bevy-game-template.md` — Original spec for the Bevy template, includes asset loading pattern
- `thoughts/CoreyCole/plans/component-3-world-management-build.md` — World creation and build pipeline spec
- `thoughts/CoreyCole/reviews/2026-02-10_12-36-08_creative-mode-implementation_review.md` — Review mentioning Brotli compression for WASM static serving, wasm-opt size concerns
- `thoughts/CoreyCole/research/2026-02-11_12-37-13_tailwind-css-migration-research.md` — Documents the shared-assets route in the context of static serving

## Open Questions

1. **How should Claude Code-generated assets work?** If a user prompts "add a tree model", should Claude Code generate/download a .glb into `data/shared-assets/` (shared with all worlds) or into the world's own directory?

2. **Asset discovery**: Should the client know what assets are available (via a manifest), or just try to load what it needs and handle 404s gracefully?

3. **Large asset streaming**: For large models/textures, should we support HTTP range requests or progressive loading? Go's `http.ServeFile` already supports range requests, so this may be "free."

4. **Brotli/gzip compression**: Should we add compression middleware for the `/assets` route? `.glb` files can compress significantly. Echo has `middleware.Gzip()` but Brotli would need a third-party package.

5. **`/assets` path prefix vs `file_path` approach**: The simplest approach (setting `AssetPlugin.file_path = "/assets"`) needs verification that `HttpWasmAssetReader` correctly handles absolute paths starting with `/`. If it treats them as relative, we'd need the full URL approach with `window.location.origin`.
