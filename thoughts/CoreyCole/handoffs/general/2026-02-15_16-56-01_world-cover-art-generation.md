---
date: 2026-02-15T16:56:01-08:00
researcher: CoreyCole
git_commit: 326c63a0f31e965a7dc6ea892462ea2731d5a9c3
branch: main
repository: creative-mode
topic: "World Cover Art Generation Implementation"
tags: [implementation, imagegen, gemini, cover-art, site, harness, lobby]
status: complete
last_updated: 2026-02-15
last_updated_by: CoreyCole
type: implementation_strategy
---

# Handoff: World Cover Art Generation

## Task(s)

Implemented a full cover art generation pipeline for world onboarding, following the plan at `thoughts/CoreyCole/plans/2026-02-15_15-13-50_world-cover-art-generation.md` and incorporating fixes from the review at `thoughts/CoreyCole/reviews/2026-02-15_15-33-50_world-cover-art-generation_review.md`.

| Phase | Status |
|-------|--------|
| Phase 1: Create `pkg/imagegen/` shared package | **Completed** |
| Phase 2: DB schema + cover art storage/serving | **Completed** |
| Phase 3: Site cover art generation UI | **Completed** |
| Phase 4: (Absorbed into 2+3) | N/A |
| Phase 5: Lobby display with cover art | **Completed** |

All 5 phases are implemented and `just check` passes cleanly.

**Next step: Review the implementation** before committing.

## Critical References

- **Updated plan**: `thoughts/CoreyCole/plans/2026-02-15_15-13-50_world-cover-art-generation.md`
- **Review with 4 critical issues**: `thoughts/CoreyCole/reviews/2026-02-15_15-33-50_world-cover-art-generation_review.md`
- **CLAUDE.md**: Project root — documents build rules (never run cargo/go build directly on macOS), Datastar gotchas, Docker workflow

## Recent changes

All changes are unstaged on `main`. Key modifications:

### Phase 1 — Shared imagegen package
- `pkg/imagegen/go.mod` (NEW): Standalone Go module wrapping `google.golang.org/genai`
- `pkg/imagegen/client.go` (NEW): `NewClient`, `Generate`, `DetectMIMEType` — returns nil,nil when API key empty
- `harness/internal/gemini/gemini.go`: Refactored to wrap `imagegen.Client` as `core` field instead of raw `genai.Client`. Removed `detectMIMEType` (moved to shared package). Cache + chromakey logic preserved.
- `harness/go.mod`, `site/go.mod`: Added `replace` directive + `require` for `pkg/imagegen`

### Phase 2 — DB + harness cover art
- `harness/internal/db/migrations/005_cover_image.sql` (NEW): `ALTER TABLE worlds ADD COLUMN cover_image_path TEXT`
- `harness/internal/db/db.go:98,172-183`: Migration registration + bootstrap detection
- `harness/internal/db/queries/worlds.sql`: Added `cover_image_path` to all 5 SELECT queries + new `UpdateWorldCoverImage` query
- `harness/sqlc.yaml`: Added `cover_image_path: "CoverImagePath"` rename
- `harness/internal/db/sqlc/`: Regenerated (models, querier, worlds.sql.go)
- `harness/internal/server/cover.go` (NEW): `handleWorldCover` serves cover image file, registered at `GET /api/worlds/:worldID/cover`
- `harness/internal/server/server.go:190`: Route registration in `approved` group
- `harness/internal/server/mayor_api.go:22-23,34-49,64-65`: Webhook accepts `cover_image_base64` + `cover_image_mime`, decodes gracefully
- `harness/internal/mayor/mayor.go:23,31,40,50-52,103-127,200-212`: `dataDir` field, `ProvisionFromWebhook` saves cover art to `{dataDir}/cover-images/{worldID}.{ext}` + updates DB, added `mimeToExt`
- `harness/main.go:361`: Passes `dataDir` to `mayor.NewManager`

### Phase 3 — Site cover art UI
- `site/internal/mayor/cover.go` (NEW): `buildCoverArtPrompt`, `savePendingCoverArt`, `mimeToExt`
- `site/internal/mayor/session.go:16-22,127-210`: `transientState` extended with `WorldReady`, `MayorName`, `WorldName`, `WorldSummary`, `CoverArtPath`, `CoverArtMIME`. New methods: `SetWorldReady`, `GetWorldReady`, `SetCoverArtPath`, `GetCoverArtPath`, `ClearWorldReady`. `cleanupLoop` removes stale cover art files.
- `site/pages/mayor_fragments.templ:125-230`: Three new fragments: `CoverArtGenerating` (loading spinner), `CoverArtPreview` (image + Hatch/Regenerate buttons with Datastar indicators), `CoverArtError` (error + Hatch Without Art/Try Again)
- `site/internal/mayor/handler.go`: Major changes:
  - Added `imagegenClient`, `dataDir`, `logger` fields; updated `NewHandler` signature
  - `prepareCoverArtAndHatch()` — unified entry point replacing all 3 `hatchWorld()` call sites. Handles: no wcClient (summary card), no imagegen (immediate hatch), Gemini available (loading spinner → generate → preview or error)
  - `hatchWorldWithCover()` — replaces old `hatchWorld()`, includes cover art in webhook
  - `HandleCoverPreview` (GET), `HandleGenerateCover` (POST/SSE), `HandleHatch` (POST/SSE)
  - `notifyHarnessWorldHatchedWithCover()` — includes `cover_image_base64` + `cover_image_mime` in webhook JSON, 30s timeout (up from 10s for large payloads)
  - Removed old `hatchWorld()` and `notifyHarnessWorldHatched()`
- `site/internal/mayor/scripted.go:58-66,103-105`: Both `hatchWorld()` calls replaced with `prepareCoverArtAndHatch()`
- `site/main.go:88-103,268-283`: Gemini client init from `GEMINI_API_KEY`, `SITE_DATA_DIR` env var, updated `NewHandler`, 3 new routes
- `site/site.env.example:20-21`: Added `GEMINI_API_KEY`

### Phase 5 — Lobby display
- `harness/views/lobby/lobby.templ:44-65`: World cards now show cover art images (`/api/worlds/:worldID/cover`) with `loading="lazy"`, or a globe emoji placeholder div if no cover art. Card layout changed from flat padding to image-above-text with `overflow-hidden`.

## Learnings

1. **All 3 hatch paths must be unified**: The original plan missed that scripted fallback and force-create also call `hatchWorld()`. The `prepareCoverArtAndHatch()` method is the single entry point — this was Critical Issue #1 in the review.

2. **Cover art stored on disk, not in memory**: `ConversationManager.transientState` stores only the file path string, not image bytes. Files go to `data/cover-art-pending/{discordID}.{ext}` on site, `{dataDir}/cover-images/{worldID}.{ext}` on harness. This survives server restarts and avoids memory bloat.

3. **Loading spinner BEFORE Gemini call**: `CoverArtGenerating` fragment is patched via SSE before the 3-10s `Generate()` call, so the user sees immediate feedback.

4. **Cover art passes through webhook as base64**: Site encodes to base64, harness decodes. Both sides handle failure gracefully (log warning, continue without cover art).

5. **Cache-busting on regenerate**: `HandleGenerateCover` appends `?t={timestamp}` to the preview URL so the browser doesn't serve a stale cached image.

## Artifacts

- `thoughts/CoreyCole/plans/2026-02-15_15-13-50_world-cover-art-generation.md` — original plan
- `thoughts/CoreyCole/reviews/2026-02-15_15-33-50_world-cover-art-generation_review.md` — review with 4 critical issues
- `pkg/imagegen/go.mod` + `pkg/imagegen/client.go` — new shared package
- `harness/internal/db/migrations/005_cover_image.sql` — new migration
- `harness/internal/server/cover.go` — new cover art serving endpoint
- `site/internal/mayor/cover.go` — new cover art helpers
- `site/pages/mayor_fragments.templ` — 3 new templ fragments (lines 125-230)

## Action Items & Next Steps

1. **Review the implementation** — all changes are unstaged on `main`. Run `git diff` to see the full diff. Verify the approach matches the plan and review fixes.
2. **Commit and push** — once review is satisfactory
3. **Test locally**:
   - Without `GEMINI_API_KEY`: onboarding should hatch immediately (zero regression)
   - With `GEMINI_API_KEY`: WORLD_READY → loading spinner → cover art preview → Regenerate works → Hatch sends webhook with cover art
   - Scripted fallback: force billing error, verify same cover art flow
   - Lobby: world cards show cover art or placeholder
4. **Deploy to VPS**: Add `GEMINI_API_KEY` to site env, run `just vps-deploy`

## Other Notes

- The `imagegen` package has its own `go.mod` under `pkg/imagegen/` — both `harness/go.mod` and `site/go.mod` use `replace` directives to point at it locally (same pattern as `pkg/worldchannel`).
- The harness webhook timeout was increased from 10s to 30s in `notifyHarnessWorldHatchedWithCover` because base64-encoded cover art images can be several MB.
- Templ fragments all target `id="mayor-signup"` which is the existing placeholder div in the mayor page — no new DOM mount points needed.
- Datastar button indicators use underscore-prefixed signals (`$_hatching`, `$_regenerating`) with `data-indicator:` and `data-show`/`data-attr:disabled` for loading states.
