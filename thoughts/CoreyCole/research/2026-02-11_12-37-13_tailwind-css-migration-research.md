---
date: 2026-02-11T12:37:13-0800
researcher: CoreyCole
git_commit: f8fc608
branch: main
repository: creative-mode
topic: "Tailwind CSS Migration - DatastarUI Reference Architecture"
tags: [research, codebase, tailwind, css, cache-busting, datastarui, harness]
status: complete
last_updated: 2026-02-11
last_updated_by: CoreyCole
---

# Research: Tailwind CSS Migration - DatastarUI Reference Architecture

**Date**: 2026-02-11T12:37:13-0800
**Researcher**: CoreyCole
**Git Commit**: f8fc608
**Branch**: main
**Repository**: creative-mode

## Research Question

The harness currently uses custom CSS. We want to migrate to Tailwind. Research `context/datastarui` for their Tailwind setup — particularly how their generate step creates a hashed CSS file for automatic cache busting, which would simplify the harness SSE cache-bust setup.

## Summary

DatastarUI uses **Tailwind CSS v4** with the standalone `@tailwindcss/cli`. Their build pipeline compiles CSS, computes an 8-character SHA-256 hash, and copies the output to `out.<hash>.css`. At runtime, a Go templ template uses `filepath.Glob` to discover the hashed filename and inject it into the HTML `<link>` tag. This approach provides automatic cache busting with zero runtime overhead beyond a single glob call — no SSE, no query parameters, no JavaScript needed.

The harness currently serves a hand-written `styles.css` with no build pipeline and no production cache busting. Dev-mode cache busting uses a complex SSE pub/sub system that broadcasts JavaScript to append `?v=<timestamp>` to stylesheet hrefs. Adopting the datastarui pattern would eliminate the need for the CSS-specific SSE reload mechanism entirely.

## Detailed Findings

### DatastarUI Tailwind Setup

#### Tailwind v4 Configuration (No Config File Needed)

DatastarUI uses Tailwind CSS v4, which moves configuration into CSS itself via the `@theme` directive. No `postcss.config.js` is needed — `@tailwindcss/cli` runs standalone.

**Entry point** — `context/datastarui/static/css/index.css`:
```css
@import "tailwindcss";
@import "./theme.css";
```

**Theme** — `context/datastarui/static/css/theme.css:15-45`:
```css
@theme {
  --color-background: hsl(var(--background));
  --color-foreground: hsl(var(--foreground));
  --color-primary: hsl(var(--primary));
  /* ... semantic color tokens */
}
```

Light/dark mode CSS custom properties are defined in `:root` (lines 79-113) and `.dark` (lines 116-149).

**Dependencies** — `context/datastarui/package.json:7-11`:
```json
"devDependencies": {
    "@tailwindcss/cli": "^4.1.12",
    "tailwindcss": "^4.1.12"
}
```

#### CSS Build + Hash Pipeline

The entire pipeline is a single justfile recipe — `context/datastarui/justfile:16-26`:

```bash
build-tailwind:
    @pnpm exec tailwindcss -i static/css/index.css -o static/css/out.css \
        --content "./components/**/*" \
        --content "./pages/**/*" \
        --content "./layouts/**/*"
    @if [ -f static/css/out.css ]; then \
        HASH=$(sha256sum static/css/out.css | cut -d' ' -f1 | head -c8); \
        rm -f static/css/out.*.css; \
        cp static/css/out.css static/css/out.$HASH.css; \
    fi
```

Steps:
1. **Compile**: `tailwindcss` scans content paths for class usage, outputs `static/css/out.css`
2. **Hash**: `sha256sum out.css | head -c8` → 8-character hex hash
3. **Cleanup**: Remove previous `out.*.css` files
4. **Copy**: Create `out.<hash>.css`

#### Runtime Hash Discovery

**`context/datastarui/layouts/root.templ:31-40`** — templ discovers the hashed file via `filepath.Glob`:

```go
templ Root(args RootArgs) {
    {{
        cssFile := "/css/out.css" // fallback
        if files, err := filepath.Glob("static/css/out.*.css"); err == nil && len(files) > 0 {
            fileName := filepath.Base(files[0])
            cssFile = "/css/" + fileName
        }
    }}
    <!-- later in <head>: -->
    <link rel="stylesheet" href={ cssFile }/>
}
```

**Key design**: Falls back to unhashed `out.css` during watch mode development when no hashed file exists.

#### Watch Mode (Development)

`context/datastarui/justfile:28-29` — no hashing in watch mode:
```bash
tailwind:
  @pnpm exec tailwindcss -i static/css/index.css -o static/css/out.css --watch \
      --content "./components/**/*" --content "./pages/**/*" --content "./layouts/**/*"
```

#### Air Configuration

`context/datastarui/.air.toml:11` — excludes CSS output to prevent infinite rebuild loops:
```toml
exclude_file = ["static/css/out*"]
```

#### Git Ignore

`context/datastarui/.gitignore:6` — all generated CSS is gitignored: `static/css/out*`

### Current Harness CSS Setup

#### No Build Pipeline

The harness has a single hand-written CSS file at `harness/static/styles.css` (404 lines). No Tailwind, no PostCSS, no package.json.

#### Static Serving

`harness/internal/server/server.go:99-101`:
```go
e.Static("/assets", filepath.Join(s.DataDir, "shared-assets"))
e.Static("/static", "static")
```

#### CSS Reference in Layout

`harness/views/layout/layout.templ:12`:
```html
<link rel="stylesheet" href="/static/styles.css"/>
```

#### Production: No Cache Busting

In production, CSS is served with only Go's default `Last-Modified` / `If-Modified-Since` handling. No hash, no version, no `Cache-Control` headers.

#### Development: Complex SSE Cache Busting

Dev mode uses a 3-part system:

1. **fswatch** (`harness/justfile:66-68`) detects `*.css` changes, POSTs to `/dev/reload-static`
2. **devState pub/sub** (`harness/internal/server/dev.go:23-27`) broadcasts `"reload-static"` to all connected SSE clients
3. **Datastar ExecuteScript** (`harness/internal/server/dev.go:78-85`) sends JS to append `?v=Date.now()` to all stylesheet hrefs:
   ```go
   sse.ExecuteScript(
       `document.querySelectorAll('link[rel="stylesheet"]').forEach(` +
           `l=>{const u=new URL(l.href);` +
           `u.searchParams.set("v",Date.now());l.href=u.toString()})`,
   )
   ```

## Architecture Insights

### What Changes with Hashed CSS

Adopting the datastarui pattern means:

1. **Production cache busting for free** — The hash in the filename changes whenever CSS content changes. Browsers treat `out.a1b2c3d4.css` and `out.e5f6g7h8.css` as completely different resources. No need for `Cache-Control` headers or ETags.

2. **Simplified dev reload** — The SSE `reload-static` mechanism + `ExecuteScript` JavaScript for CSS cache busting becomes unnecessary. During dev, `tailwind --watch` outputs `out.css` (unhashed), and the templ fallback serves it directly. A page refresh or Datastar morph picks up the new CSS. The existing dev rebuild/morph pipeline already handles page refreshes on `.go`/`.templ` changes.

3. **The dev SSE system still has value** — The `devState` pub/sub, `handleDevRebuild`, binary restart loop, and page morph are still needed for Go/templ hot-reload. Only the CSS-specific `reload-static` broadcast and the ExecuteScript JS can be removed.

### Migration Data Flow

**Build step** (added to `just generate` or `just build`):
```
static/css/index.css → tailwindcss CLI → static/css/out.css → sha256sum → static/css/out.<hash>.css
```

**Runtime** (per request, in layout templ):
```
filepath.Glob("static/css/out.*.css") → "/css/out.<hash>.css" → <link href="..."/>
```

**Static serving** (modify Echo route):
```go
// Change from:
e.Static("/static", "static")
// To (or add):
e.Static("/css", "static/css")  // serve CSS at /css/ path
```

### Key Differences to Account For

| Aspect | DatastarUI | Harness (current) |
|--------|-----------|-------------------|
| CSS source | `static/css/index.css` + `theme.css` | `static/styles.css` |
| Build tool | `@tailwindcss/cli` v4 | None |
| Package manager | pnpm | None |
| Cache busting | Filename hash | Dev: SSE + JS, Prod: none |
| CSS serving path | `/css/out.<hash>.css` | `/static/styles.css` |
| Fallback | `out.css` (unhashed) | N/A |
| Git ignore | `static/css/out*` | N/A |

## Code References

- `context/datastarui/justfile:16-26` — build-tailwind recipe (hash generation)
- `context/datastarui/layouts/root.templ:31-40` — filepath.Glob hash discovery
- `context/datastarui/static/css/index.css:1-4` — Tailwind v4 entry point
- `context/datastarui/static/css/theme.css:15-45` — @theme configuration
- `context/datastarui/package.json:7-11` — Tailwind dependencies
- `context/datastarui/.air.toml:11` — CSS output exclusion from watch
- `context/datastarui/.gitignore:6` — CSS output gitignored
- `harness/static/styles.css` — Current hand-written CSS (404 lines)
- `harness/views/layout/layout.templ:12` — Current CSS link tag
- `harness/internal/server/server.go:99-101` — Static file serving
- `harness/internal/server/dev.go:78-85` — SSE CSS cache-bust ExecuteScript
- `harness/internal/server/dev.go:198-202` — reload-static handler
- `harness/justfile:66-68` — fswatch CSS change handler

## Open Questions

1. **Package manager**: DatastarUI uses pnpm. Should the harness adopt pnpm, or use npm/bun? Or install `@tailwindcss/cli` as a standalone binary?
2. **Existing CSS migration**: The 404 lines of custom CSS in `styles.css` need to be converted to Tailwind utilities in the templ files, or kept as custom CSS in the Tailwind entry point.
3. **Static path change**: DatastarUI serves CSS at `/css/` while harness uses `/static/`. Should we match datastarui's convention or keep `/static/`?
4. **Docker build**: The Tailwind build step needs to run inside the Docker image build. Need to add Node.js / pnpm to the Dockerfile or use the standalone Tailwind binary.
5. **Dev watch integration**: Should `tailwind --watch` run as part of `just live`/`just watch`, or be a separate process?
6. **macOS sha256sum**: The `sha256sum` command on macOS is `shasum -a 256`. The justfile recipe may need adjustment if building locally (works in Docker/Linux as-is).
