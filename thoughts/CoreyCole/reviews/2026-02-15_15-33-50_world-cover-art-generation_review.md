---
date: 2026-02-15T15:33:50-08:00
reviewer: Claude (Staff Eng Review)
git_commit: 074fcb7774ee0c205a6aadd7ab0e64a5498acf7d
branch: main
repository: creative-mode
plan_reviewed: thoughts/CoreyCole/plans/2026-02-15_15-13-50_world-cover-art-generation.md
status: complete
type: plan_review
---

# Plan Review: World Cover Art Generation

### Summary

Well-structured plan with strong code-level detail and correct analysis of existing infrastructure. However, the plan has four significant gaps: the scripted fallback path is not addressed, cover art generation blocks the SSE handler synchronously, in-memory cover art storage is fragile, and the webhook-to-provisioning flow has a race condition for cover art persistence.

### Critical Issues (Must Address Before Implementation)

1. **Scripted fallback path bypasses cover art entirely**
   - Problem: The plan modifies `HandleChat` (handler.go:276-284) to insert a cover art step between WORLD_READY and hatching. But `scripted.go:58-73` (`handleScriptedResponse`) and `scripted.go:109-117` (`handleScriptedForceCreate`) ALSO call `hatchWorld()` directly. These paths are not mentioned anywhere in the plan.
   - Risk: When the Anthropic API is unavailable (billing error, rate limit, overload — a known failure mode that already has a fallback), worlds get hatched without any cover art step. The user never sees the preview or gets a chance to regenerate.
   - Suggestion: Either (a) modify the scripted path to also call through a shared cover art generation step, or (b) extract the cover art + hatch flow into a shared function (e.g., `prepareCoverArtAndHatch()`) that both `HandleChat` and the scripted handlers call.

2. **Cover art generation synchronously blocks the SSE handler for 3-10 seconds**
   - Problem: Phase 3 shows calling `h.imagegenClient.Generate()` inline inside `HandleChat`, after streaming the Claude response. The plan's own Performance section acknowledges Gemini takes 3-10 seconds. During this time, the SSE connection is open but idle — the user sees the final message but no hatched card appears for several seconds with no feedback.
   - Risk: Users think the app is broken/frozen. If Gemini is slow or errors, the entire SSE response hangs.
   - Suggestion: Show a loading indicator fragment (e.g., `CoverArtGenerating()`) BEFORE calling `Generate()`, so the user sees progress. The plan already defines this templ component but the Phase 3 code flow doesn't use it — it jumps straight from the WORLD_READY detection to calling `Generate()`.

3. **In-memory cover art storage in ConversationManager is fragile**
   - Problem: Phase 3 adds `SetCoverArt(discordID, data []byte, mimeType)` to the ConversationManager, which stores 100-500KB byte slices per user in a mutex-protected Go map. The ConversationManager currently stores only small transient flags (scripted bool, lastMessage time) in memory — messages are in SQLite. Adding large binary data to this map mixes concerns.
   - Risk: (a) Server restart between generation and hatching loses the cover art silently. (b) The cleanup loop in session.go:124-138 removes transient state after 24h, but there's no guarantee the user hatches before then. (c) Memory pressure if many users generate cover art concurrently. (d) No persistence — if the site process crashes, all pending cover art is lost.
   - Suggestion: Either persist cover art to disk immediately after generation (e.g., a temp directory cleaned up after hatching or on a timer), or store it as a base64 blob in the site's SQLite database. This also simplifies the cover preview serving — instead of a special in-memory endpoint, serve from disk.

4. **Race condition between webhook cover art save and async provisioning**
   - Problem: Phase 4 says to decode/save cover art in `handleWorldHatched` (mayor_api.go). But Phase 2 also says `ProvisionFromWebhook` creates the world record — and `handleWorldHatched` fires `ProvisionFromWebhook` in a goroutine (line 43-57). The `UpdateWorldCoverImage` DB call needs the world to exist first, but `handleWorldHatched` returns 202 immediately while provisioning runs async.
   - Risk: Cover art could be saved to disk, but the `UpdateWorldCoverImage` call runs before `ProvisionFromWebhook` creates the world record, causing a foreign key error or silent failure.
   - Suggestion: Either (a) save cover art synchronously in `handleWorldHatched` BEFORE launching the provisioning goroutine (requires the world to already exist — but it doesn't yet), or (b) have `ProvisionFromWebhook` accept the cover art data as parameters and handle saving+DB update as part of its flow after `CreateWorld`. Option (b) is cleaner — pass cover art bytes through to `ProvisionFromWebhook` and let it handle the full flow.

### Concerns (Should Address)

1. **`coverstore.go` is in file inventory but never described**
   - Observation: The File Inventory (line 558) lists `site/internal/mayor/coverstore.go` as a new file in Phase 3, but the phase description never explains its contents. Is this where the in-memory cover art storage goes? How does it differ from `cover.go`?
   - Suggestion: Clarify the purpose and contents of this file, or merge it into `cover.go` or `session.go` if the separation isn't justified.

2. **Cover art serving endpoint needs auth consideration**
   - Observation: Phase 2 adds `GET /api/worlds/:worldID/cover` but doesn't specify which middleware group it's registered in. The route could be public, approved-only, or auth-gated.
   - Suggestion: Since the lobby is already behind auth middleware (approved group), register this endpoint in the same group. If cover art should be publicly accessible (e.g., for Discord embeds or OpenGraph), document that explicitly as a design decision.

3. **Regeneration endpoint SSE behavior is underspecified**
   - Observation: Phase 3 lists `POST /mayor/generate-cover` for regeneration. It says "Streams SSE: patches in cover art preview image." But a POST endpoint that returns SSE needs careful Datastar wiring — the client needs to know it's an SSE response, not a JSON response.
   - Suggestion: Use the same pattern as `POST /mayor/chat`: create a Datastar SSE generator, patch in a loading fragment, generate the image, then patch in the preview. Document the Datastar attributes needed on the regenerate button (`data-on:click="@post('/mayor/generate-cover')"` with indicator).

4. **SELECT queries need manual update across 7 queries**
   - Observation: Phase 2 says to "update all SELECT queries to include `cover_image_path`" — there are 7 queries in worlds.sql that explicitly list columns. Missing even one will cause sqlc compilation failures or silent null returns.
   - Suggestion: Consider adding `cover_image_path` to the column list in a comment at the top of worlds.sql as a reminder, and verify all 7 queries are updated.

### Questions (Need Clarification)

1. When `GEMINI_API_KEY` is NOT set on the site, does the flow fall back to the current immediate-hatch behavior? The plan mentions graceful degradation but doesn't show the code path. Specifically: does `WorldPreview` still render? Does the hatch button appear without cover art? Or does the flow skip straight to `hatchWorld()` as it does today?

2. What is the directory structure for cover art files on the harness? The plan says `{dataDir}/cover-images/{worldID}.{ext}` but existing assets use `{dataDir}/shared-assets/`. Should this match? Does the Docker bind mount include this new directory?

3. The plan says `notifyHarnessWorldHatched` (which runs in a goroutine) will carry the cover art payload. But this function currently takes 5 string params and constructs a JSON map inline (handler.go:385-391). Will it need a signature change to accept []byte, or will the cover art be encoded to base64 string before passing?

### Suggestions (Nice to Have)

1. **Add a timeout to the Gemini generation call**: Wrap the `Generate()` call with a context timeout (e.g., 15 seconds) so the SSE handler doesn't hang indefinitely if Gemini is slow.

2. **Consider storing cover art at the world-hatched webhook level**: Instead of base64 in JSON, the webhook could use multipart/form-data to avoid the 33% base64 overhead. This is minor for a one-time operation but is a cleaner HTTP pattern.

3. **Add a simple default cover art**: For worlds created without Gemini (scripted fallback, API key not set), consider generating a simple gradient or placeholder image with the world name overlaid, rather than showing a generic emoji placeholder in the lobby.

### What's Good

- **Thorough analysis of existing code**: The plan correctly identifies all relevant files, line numbers, and data flows. Every claim I verified was accurate.
- **Clean extraction to `pkg/imagegen/`**: The shared package design follows the established `pkg/worldchannel/` pattern with replace directives. The interface (`GenerateOptions`, `GeneratedImage`) is well-designed and appropriately minimal.
- **Clear phasing**: The 5 phases are well-ordered with each building on the previous. Phase 1 (shared package) can be implemented and verified independently.
- **Explicit "What We're NOT Doing" section**: Helps prevent scope creep — no image editing, no parallel generation, no backfilling existing worlds.
- **Correct Datastar patterns**: The plan follows the established `PatchElementTempl` pattern for server-rendered state (not signals), which aligns with the project's Datastar best practices.

### Recommended Next Steps

1. Address the scripted fallback path gap — this is the most likely source of production bugs since it's a known failure mode that triggers regularly.
2. Resolve the cover art storage design (in-memory vs. disk/DB) before implementation — this affects the data flow in Phases 3 and 4.
3. Clarify the `ProvisionFromWebhook` integration — decide whether cover art saving happens in the webhook handler or inside the provisioning flow.
4. Add the loading indicator fragment to the Phase 3 code flow (the component exists but isn't wired up in the sequence).
