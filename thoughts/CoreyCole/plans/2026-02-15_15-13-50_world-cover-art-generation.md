# World Cover Art Generation — Implementation Plan

## Overview

Generate cover art for worlds during the site onboarding flow using Gemini image generation. Users can iterate on the generated image before hatching their world. The cover art is sent with the world-hatched webhook so the harness lobby can display it immediately. The core Gemini SDK wrapper is extracted to a shared `pkg/imagegen/` package usable by both the site and harness.

## Current State Analysis

### Existing Infrastructure:
- **Gemini client**: `harness/internal/gemini/gemini.go` — wraps `google.golang.org/genai` (v1.46.0) with model `gemini-2.5-flash-image`. Has in-memory cache, chromakey green background removal, and MIME detection. Used by the harness image gen panel for game sprite creation.
- **Site onboarding**: `site/internal/mayor/handler.go` — Claude conversation → `WORLD_READY|<mayor>|<world>|<summary>` → `hatchWorld()` immediately creates Discord channel + fires webhook. No intermediate step between WORLD_READY and hatching.
- **Lobby**: `harness/views/lobby/lobby.templ` — simple text-only world cards (name + description). No image fields.
- **World schema**: No `cover_image_path` column. Migration 004 was the latest (mayor columns).
- **Shared pkg pattern**: `pkg/worldchannel/` is already shared via `replace` directives in both `harness/go.mod` and `site/go.mod`.

### Key Discoveries:
- `harness/internal/gemini/gemini.go:95-175` — `Generate()` has chromakey suffix logic and cache management tightly coupled. Core API call (~15 lines) is extractable.
- `site/go.mod` — no `google.golang.org/genai` dependency. Will need it via the shared pkg.
- `site/internal/mayor/handler.go:270-280` — WORLD_READY triggers `hatchWorld()` immediately. Need to insert a cover art step between parsing the marker and hatching.
- `harness/internal/server/mayor_api.go:17-23` — webhook payload is JSON with `discord_channel_id`, `world_name`, `mayor_name`, `creator_discord_id`, `creator_username`. Will add `cover_image_base64` and `cover_image_mime`.
- `harness/internal/db/db.go:93-97` — migration files list pattern for adding migration 005.

## Desired End State

1. User completes onboarding conversation on the site → WORLD_READY marker parsed
2. Site auto-generates cover art from world summary via Gemini (`pkg/imagegen`)
3. User sees cover art preview with ability to regenerate
4. User clicks "Hatch World" → Discord channel created + webhook fires with cover art data
5. Harness receives webhook, saves cover art to filesystem, stores path in DB
6. Harness lobby displays worlds with cover art images

### Verification:
1. Site generates cover art after WORLD_READY marker (visible in browser)
2. User can regenerate cover art multiple times
3. Hatching sends cover art in webhook payload
4. Harness lobby shows cover art on world cards
5. `just check` passes

## What We're NOT Doing

- Changing the site's authentication or session system
- Adding cover art editing/cropping tools
- Generating multiple images in parallel for selection
- Adding cover art to existing worlds (only new worlds going forward)
- Changing the harness image gen panel (sprites/game art) — that stays as-is
- Supporting image upload (only AI-generated for now)

## Implementation Approach

Extract the core Gemini generation call to `pkg/imagegen/` (shared). The site uses it to generate cover art inline during onboarding. The harness's `internal/gemini/` package refactors to wrap the shared client with caching and chromakey. Cover art is sent as base64 in the webhook JSON payload and stored on the harness filesystem.

---

## Phase 1: Create `pkg/imagegen/` Shared Package

### Overview
Extract the core Gemini SDK wrapper into a shared Go package that both the site and harness can import.

### Changes Required:

#### 1. New shared package
**File**: `pkg/imagegen/go.mod` (new)
```go
module github.com/coreycole/creative-mode/pkg/imagegen

go 1.24.3

require (
    google.golang.org/genai v1.46.0
)
```

**File**: `pkg/imagegen/client.go` (new)
```go
package imagegen

import (
    "context"
    "errors"
    "fmt"
    "log/slog"

    "google.golang.org/genai"
)

const DefaultModel = "gemini-2.5-flash-image"

// GeneratedImage holds the raw bytes and MIME type of a generated image.
type GeneratedImage struct {
    Data     []byte
    MIMEType string
}

// GenerateOptions configures image generation.
type GenerateOptions struct {
    AspectRatio   string // e.g. "16:9", "1:1"
    PromptSuffix  string // appended to the prompt (e.g. chromakey instructions)
    Model         string // override default model
}

// Client wraps the Google Gen AI SDK for image generation.
type Client struct {
    client *genai.Client
    logger *slog.Logger
}

// NewClient creates a new Gemini image generation client.
// Returns nil, nil if apiKey is empty (feature disabled).
func NewClient(ctx context.Context, apiKey string, logger *slog.Logger) (*Client, error) {
    if apiKey == "" {
        return nil, nil
    }

    client, err := genai.NewClient(ctx, &genai.ClientConfig{
        APIKey:  apiKey,
        Backend: genai.BackendGeminiAPI,
    })
    if err != nil {
        return nil, fmt.Errorf("create genai client: %w", err)
    }

    return &Client{client: client, logger: logger}, nil
}

// Generate calls Gemini to generate an image from a text prompt.
// Returns the raw image bytes and detected MIME type.
func (c *Client) Generate(ctx context.Context, prompt string, opts GenerateOptions) (*GeneratedImage, error) {
    model := DefaultModel
    if opts.Model != "" {
        model = opts.Model
    }

    config := &genai.GenerateContentConfig{
        ResponseModalities: []string{"TEXT", "IMAGE"},
    }
    if opts.AspectRatio != "" {
        config.ImageConfig = &genai.ImageConfig{
            AspectRatio: opts.AspectRatio,
        }
    }

    fullPrompt := prompt
    if opts.PromptSuffix != "" {
        fullPrompt += opts.PromptSuffix
    }

    result, err := c.client.Models.GenerateContent(ctx, model, genai.Text(fullPrompt), config)
    if err != nil {
        return nil, fmt.Errorf("generate content: %w", err)
    }

    for _, candidate := range result.Candidates {
        if candidate.Content == nil {
            continue
        }
        for _, part := range candidate.Content.Parts {
            if part.InlineData == nil || part.InlineData.Data == nil {
                continue
            }
            return &GeneratedImage{
                Data:     part.InlineData.Data,
                MIMEType: DetectMIMEType(part.InlineData.Data),
            }, nil
        }
    }

    return nil, errors.New("no image in response")
}

// DetectMIMEType returns the MIME type based on magic bytes.
func DetectMIMEType(data []byte) string {
    if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
        return "image/jpeg"
    }
    if len(data) >= 4 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
        return "image/webp"
    }
    return "image/png"
}
```

#### 2. Refactor harness `internal/gemini/` to wrap shared client
**File**: `harness/internal/gemini/gemini.go` — refactor to use `pkg/imagegen.Client` as the underlying generator:

- Replace `client *genai.Client` field with `core *imagegen.Client`
- Remove `NewClient` logic (delegate to `imagegen.NewClient`)
- `Generate()` calls `c.core.Generate()` then applies chromakey post-processing and caching
- Remove `detectMIMEType()` (use `imagegen.DetectMIMEType()`)
- Keep `removeGreenBackground()`, `rgbToHSV()`, `dilateMask()`, `isChromakeyGreen()`, `evictExpired()` — these are harness-specific

#### 3. Update go.mod files
**File**: `harness/go.mod` — add replace directive:
```
replace github.com/coreycole/creative-mode/pkg/imagegen => ../pkg/imagegen
```
Add `require` for `github.com/coreycole/creative-mode/pkg/imagegen`.

**File**: `site/go.mod` — add replace directive:
```
replace github.com/coreycole/creative-mode/pkg/imagegen => ../pkg/imagegen
```
Add `require` for `github.com/coreycole/creative-mode/pkg/imagegen`.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes (Go + Rust compile)
- [ ] Existing harness image gen panel still works (test via playwright-cli)

#### Manual Verification:
- [ ] Generate an image in the harness image gen panel — same behavior as before

---

## Phase 2: DB Schema + Cover Art Storage

### Overview
Add a `cover_image_path` column to the worlds table. Add an endpoint to serve cover art. Update the world-hatched webhook to accept cover art data.

### Changes Required:

#### 1. Database migration
**File**: `harness/internal/db/migrations/005_cover_image.sql` (new)
```sql
ALTER TABLE worlds ADD COLUMN cover_image_path TEXT;
```

#### 2. Register migration
**File**: `harness/internal/db/db.go`
- Add `"migrations/005_cover_image.sql"` to `migrationFiles` slice
- Add bootstrap check for `cover_image_path` column in `bootstrapExistingMigrations`

#### 3. Update SQL queries
**File**: `harness/internal/db/queries/worlds.sql`
- Update all SELECT queries to include `cover_image_path` column
- Add new query:
```sql
-- name: UpdateWorldCoverImage :exec
UPDATE worlds SET cover_image_path = ? WHERE id = ?;
```

#### 4. Update sqlc config
**File**: `harness/sqlc.yaml` — add rename:
```yaml
cover_image_path: "CoverImagePath"
```

#### 5. Cover art serving endpoint
**File**: `harness/internal/server/server.go` — add route:
```go
e.GET("/api/worlds/:worldID/cover", s.handleWorldCover)
```

**File**: `harness/internal/server/imagegen.go` (or new file `cover.go`) — handler:
```go
func (s *Server) handleWorldCover(c echo.Context) error {
    worldID := c.Param("worldID")
    world, err := s.DB.GetWorld(c.Request().Context(), worldID)
    if err != nil {
        return echo.NewHTTPError(http.StatusNotFound, "world not found")
    }
    if !world.CoverImagePath.Valid {
        return echo.NewHTTPError(http.StatusNotFound, "no cover image")
    }
    return c.File(world.CoverImagePath.String)
}
```

#### 6. Update webhook to handle cover art
**File**: `harness/internal/server/mayor_api.go` — in `handleWorldHatched`:
- Add `CoverImageBase64 string` and `CoverImageMIME string` to request struct
- After world creation, if cover art present: decode base64, save to filesystem, update DB

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] sqlc generates cleanly (World struct has `CoverImagePath sql.NullString`)

#### Manual Verification:
- [ ] Harness starts without DB errors
- [ ] `curl -s http://localhost:8080/api/worlds/<id>/cover` returns 404 for worlds without cover art

---

## Phase 3: Site Cover Art Generation UI

### Overview
Add cover art generation to the site onboarding flow. After the WORLD_READY marker is parsed, show a cover art preview with regeneration. User explicitly hatches after accepting the cover art.

### Changes Required:

#### 1. Add imagegen client to site
**File**: `site/main.go` — create `imagegen.Client` from `GEMINI_API_KEY` env var, pass to mayor handler.

**File**: `site/internal/mayor/handler.go` — add `imagegenClient *imagegen.Client` field to `Handler` struct.

#### 2. Cover art generation endpoint
**File**: `site/internal/mayor/handler.go` — new handler:
```go
// POST /mayor/generate-cover — generates cover art from world summary
func (h *Handler) HandleGenerateCover(c echo.Context) error
```

This endpoint:
- Reads signals: `cover_prompt` (auto-populated from world summary, editable by user)
- Calls `h.imagegenClient.Generate()` with aspect ratio "16:9"
- Caches result in a simple map (session-scoped, keyed by Discord ID)
- Streams SSE: patches in cover art preview image

#### 3. Cover art templ fragments
**File**: `site/pages/mayor_fragments.templ` — add new fragments:

```go
// WorldPreview shows the world summary + cover art generation after WORLD_READY
templ WorldPreview(worldName, mayorName, summary string)

// CoverArtGenerating shows a loading spinner during generation
templ CoverArtGenerating()

// CoverArtPreview shows the generated cover art with regenerate + hatch buttons
templ CoverArtPreview(previewURL string, worldName, mayorName string)

// CoverArtError shows generation error with retry button
templ CoverArtError(errMsg string)
```

#### 4. Modify WORLD_READY handling
**File**: `site/internal/mayor/handler.go` — change the flow after parsing WORLD_READY:

Currently (line 270-280):
```go
if mayorName != "" && worldName != "" {
    if h.wcClient != nil {
        h.hatchWorld(c, sse, session, mayorName, worldName, worldSummary)
    } else {
        // show summary card
    }
}
```

New flow:
```go
if mayorName != "" && worldName != "" {
    // Store world info in session/conversation for later hatching
    h.convMgr.SetWorldReady(session.DiscordID, mayorName, worldName, worldSummary)

    // Show world preview + auto-generate cover art
    if err := sse.PatchElementTempl(p.WorldPreview(worldName, mayorName, worldSummary)); err != nil {
        return err
    }

    // Auto-generate cover art if Gemini is available
    if h.imagegenClient != nil {
        coverPrompt := buildCoverArtPrompt(worldName, worldSummary)
        img, err := h.imagegenClient.Generate(ctx, coverPrompt, imagegen.GenerateOptions{
            AspectRatio: "16:9",
        })
        if err == nil {
            h.convMgr.SetCoverArt(session.DiscordID, img.Data, img.MIMEType)
            // Patch in the cover art preview
            if err := sse.PatchElementTempl(p.CoverArtPreview(
                "/mayor/cover-preview", worldName, mayorName,
            )); err != nil {
                return err
            }
        } else {
            if err := sse.PatchElementTempl(p.CoverArtError(err.Error())); err != nil {
                return err
            }
        }
    }
}
```

#### 5. Cover art preview serving
**File**: `site/main.go` or `site/internal/mayor/handler.go` — add routes:
- `GET /mayor/cover-preview` — serves the cached cover art for the current session
- `POST /mayor/generate-cover` — regenerates cover art with optional custom prompt

#### 6. Hatch with cover art
**File**: `site/internal/mayor/handler.go` — add new handler for explicit hatching:
- `POST /mayor/hatch` — reads from conversation manager's stored world-ready state, includes cover art bytes in the webhook payload

#### 7. Build cover art prompt
**File**: `site/internal/mayor/cover.go` (new)
```go
func buildCoverArtPrompt(worldName, summary string) string {
    return fmt.Sprintf(
        "Cover art for a multiplayer game world called %q. %s. "+
            "Digital art style, vibrant colors, wide landscape composition, "+
            "game cover aesthetic. No text or logos.",
        worldName, summary,
    )
}
```

#### 8. Conversation manager updates
**File**: `site/internal/mayor/session.go` — add methods:
- `SetWorldReady(discordID, mayorName, worldName, summary)` — stores pending world info
- `GetWorldReady(discordID) (mayorName, worldName, summary, ok)`
- `SetCoverArt(discordID, data []byte, mimeType string)` — stores generated cover art
- `GetCoverArt(discordID) (data []byte, mimeType string, ok)`
- `ClearWorldReady(discordID)` — cleanup after hatching

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes
- [ ] Site builds cleanly with new dependencies

#### Manual Verification:
- [ ] Complete onboarding conversation → WORLD_READY → cover art auto-generates
- [ ] Cover art preview visible in browser
- [ ] Regenerate button works (new image generated)
- [ ] Hatch button creates Discord channel + fires webhook with cover art

---

## Phase 4: Wire Cover Art Through Webhook

### Overview
Update the webhook payload to include cover art. Harness saves it to the filesystem and records the path.

### Changes Required:

#### 1. Update webhook sender (site)
**File**: `site/internal/mayor/handler.go` — in the new hatch handler, encode cover art as base64:
```go
payload["cover_image_base64"] = base64.StdEncoding.EncodeToString(coverArt.Data)
payload["cover_image_mime"] = coverArt.MIMEType
```

#### 2. Update webhook receiver (harness)
**File**: `harness/internal/server/mayor_api.go` — in `handleWorldHatched`:
- Parse `cover_image_base64` and `cover_image_mime` from request body
- Decode base64 to bytes
- Determine file extension from MIME type
- Save to `{dataDir}/cover-images/{worldID}.{ext}`
- Call `UpdateWorldCoverImage(ctx, path, worldID)` to store path in DB

#### 3. Mayor provisioning passes cover art path
**File**: `harness/internal/mayor/manager.go` — `ProvisionFromWebhook` already creates the world. After the cover art is saved, update the world record with the cover image path.

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes

#### Manual Verification:
- [ ] Hatch a world on the site → cover art appears in harness DB
- [ ] `GET /api/worlds/:id/cover` returns the cover art image
- [ ] World without cover art returns 404 from cover endpoint

---

## Phase 5: Lobby Display

### Overview
Update the harness lobby to display cover art on world cards. Graceful fallback for worlds without cover art.

### Changes Required:

#### 1. Update lobby template
**File**: `harness/views/lobby/lobby.templ` — update world card grid:

```go
for _, w := range worlds {
    <a
        href={ templ.SafeURL("/world/" + w.ID) }
        class="block border border-border rounded-lg bg-card hover:border-muted-foreground/40 transition-colors no-underline text-inherit overflow-hidden"
    >
        if w.CoverImagePath.Valid {
            <img
                src={ "/api/worlds/" + w.ID + "/cover" }
                alt={ w.Name + " cover art" }
                class="w-full h-36 object-cover"
                loading="lazy"
            />
        } else {
            <div class="w-full h-36 bg-muted/30 flex items-center justify-center">
                <span class="text-3xl text-muted-foreground/30">🌍</span>
            </div>
        }
        <div class="p-4">
            <h3 class="text-base mb-1">{ w.Name }</h3>
            if w.Description.Valid {
                <p class="text-muted-foreground text-[13px]">{ w.Description.String }</p>
            }
        </div>
    </a>
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `just check` passes

#### Manual Verification:
- [ ] Lobby shows cover art on world cards that have them
- [ ] Worlds without cover art show placeholder
- [ ] Images load lazily (no layout shift)
- [ ] Cards still link to `/world/:id` correctly

---

## Testing Strategy

### Unit Tests:
- `pkg/imagegen`: `DetectMIMEType()` with PNG, JPEG, WebP magic bytes
- `site/internal/mayor`: `buildCoverArtPrompt()` output format
- `harness/internal/server`: Cover art base64 decoding + file extension mapping

### Integration Tests:
- End-to-end: site hatch with cover art → webhook → harness stores → lobby displays

### Manual Testing Steps:
1. Start site + harness (Docker)
2. Complete "Meet the Mayor" onboarding conversation
3. After WORLD_READY: verify cover art auto-generates
4. Click "Regenerate" — verify new image appears
5. Click "Hatch World" — verify Discord channel created
6. Check harness lobby — verify cover art displays on world card
7. Test without `GEMINI_API_KEY` on site — verify graceful degradation (skip cover art step, hatch immediately)

## Performance Considerations

- Gemini image generation takes 3-10 seconds. The user sees a loading spinner during generation. This is acceptable since it's a one-time step during world creation.
- Cover art images are typically 100-500KB. Base64 encoding in the webhook adds ~33% overhead (~130-670KB). This is a single request per world creation, so it's fine.
- Cover art is served from the filesystem via `c.File()`, which is efficient and supports HTTP caching headers.
- Lobby images use `loading="lazy"` to avoid blocking page load.

## Migration Notes

- Migration 005 adds a nullable `cover_image_path` column. Existing worlds will have NULL (no cover art).
- No data migration needed — cover art is only generated for new worlds.
- The site gracefully degrades if `GEMINI_API_KEY` is not set: skips cover art step entirely, hatches immediately (current behavior).

## Environment Variables

| Variable | Service | Purpose |
|----------|---------|---------|
| `GEMINI_API_KEY` | **Site** (new) + Harness (existing) | Google AI API key for image generation |

The site needs `GEMINI_API_KEY` added to `site.env` / `site.env.example`.

## File Inventory

### New Files (5)
| File | Phase |
|------|-------|
| `pkg/imagegen/go.mod` | 1 |
| `pkg/imagegen/client.go` | 1 |
| `harness/internal/db/migrations/005_cover_image.sql` | 2 |
| `site/internal/mayor/cover.go` | 3 |
| `site/internal/mayor/coverstore.go` | 3 |

### Modified Files (12)
| File | Phase |
|------|-------|
| `harness/internal/gemini/gemini.go` | 1 |
| `harness/go.mod` + `go.sum` | 1 |
| `site/go.mod` + `go.sum` | 1, 3 |
| `harness/internal/db/db.go` | 2 |
| `harness/internal/db/queries/worlds.sql` | 2 |
| `harness/sqlc.yaml` | 2 |
| `harness/internal/server/server.go` | 2 |
| `harness/internal/server/mayor_api.go` | 2, 4 |
| `site/main.go` | 3 |
| `site/internal/mayor/handler.go` | 3 |
| `site/internal/mayor/session.go` | 3 |
| `site/pages/mayor_fragments.templ` | 3 |
| `harness/views/lobby/lobby.templ` | 5 |

## References

- Master plan: `thoughts/CoreyCole/plans/2026-02-15_18-43-12_world-agents-president-mayors.md`
- Existing Gemini client: `harness/internal/gemini/gemini.go`
- Site mayor handler: `site/internal/mayor/handler.go`
- Lobby template: `harness/views/lobby/lobby.templ`
- World schema: `harness/internal/db/migrations/004_mayor_and_instrumentation.sql`
- Shared pkg pattern: `pkg/worldchannel/`
