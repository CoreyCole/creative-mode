---
date: 2026-02-13T14:46:53-08:00
researcher: CoreyCole
git_commit: 1c35fda
branch: main
repository: creative-mode
topic: "Mayor Prompt Attenuation for Image Generation"
tags: [research, codebase, imagegen, mayor, openclaw, prompt-engineering, gemini]
status: complete
last_updated: 2026-02-13
last_updated_by: CoreyCole
---

# Research: Mayor Prompt Attenuation for Image Generation

**Date**: 2026-02-13T14:46:53-08:00
**Researcher**: CoreyCole
**Git Commit**: 1c35fda
**Branch**: main
**Repository**: creative-mode

## Research Question

Explore the idea of "prompt attenuation" — where the world mayor (OpenClaw agent) suggests an improved prompt for image generation based on its personality and memory of the world. The user can accept or modify the suggestion before sending to Google APIs, helping keep images generated in a similar style.

## Summary

The current image generation pipeline sends the user's raw prompt directly to Gemini with no enhancement. The planned mayor system provides a natural insertion point for prompt enrichment: the mayor has personality (SOUL.md), structured workflow (AGENTS.md), and self-evolving world memory (MEMORY.md). By adding a "suggest" step between user input and API call, the mayor can embellish prompts with world-specific style anchors, character details, and aesthetic preferences — all while the user retains final control.

The implementation can be broken into three layers:
1. **UI layer**: A new "Suggest" button alongside "Generate" that sends the prompt to the mayor for enrichment, displaying the suggestion in an editable preview before generation
2. **Mayor layer**: An OpenClaw skill or HTTP endpoint that receives a raw prompt and returns an enhanced version using the mayor's personality and world context
3. **Style consistency layer**: World-level style prefixes and reference images that get prepended/attached to every generation, ensuring visual cohesion

## Detailed Findings

### 1. Current Image Generation Pipeline

The pipeline is straightforward with **no prompt enhancement**:

1. User types prompt in input bound to `image_prompt` signal (`imagegen.templ:129`)
2. User clicks "Generate" → `@post('/api/images/generate')` sends all signals (`imagegen.templ:134`)
3. Server reads `image_prompt`, `image_aspect_ratio`, `image_transparent_bg` (`imagegen.go:43-48`)
4. Prompt is trimmed and passed verbatim to `GeminiClient.Generate()` (`imagegen.go:48-63`)
5. Inside `Generate()`, the only modification is appending a chromakey suffix when `transparentBG` is true (`gemini.go:112-115`)
6. Result is cached in-memory, preview is patched back via SSE (`imagegen.go:73-81`)

Key files:
- `harness/internal/gemini/gemini.go:95-175` — `Generate()` method
- `harness/internal/server/imagegen.go:32-82` — `handleImageGenerate` handler
- `harness/views/imagegen/imagegen.templ:125-141` — input bar UI
- `harness/views/world/signals.go:17-19` — image-related signals

### 2. Mayor Architecture (Planned, Not Yet Implemented)

The mayor system described in `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` provides:

- **SOUL.md** — personality definition generated from user input at world creation, autonomously evolved by OpenClaw over time (plan lines 936-967)
- **AGENTS.md** — structured workflow: understand → plan → build → report (plan lines 969-1028)
- **MEMORY.md** — self-managed by OpenClaw; accumulates knowledge about world history, user preferences, past builds
- **Skills** — `world-build` and `world-status` skills that call harness APIs with mayor-secret auth

The mayor acts as a prompt enrichment layer between the user and Claude Code for builds. The same pattern applies naturally to image generation: user says "a fountain," mayor composes "a decorative stone fountain in the medieval town square, matching the warm earth-tone pixel art style of the existing buildings, with flowing water particles similar to the river added last week."

### 3. Proposed Prompt Attenuation Flow

```
User types: "a dragon"
       |
       v
[Suggest] button → POST /api/images/suggest
       |
       v
Mayor enriches → "A fearsome crimson dragon perched atop the obsidian
                   tower, rendered in the hand-painted watercolor style
                   of the world's existing assets. Wings spread against
                   a stormy sky, scales catching firelight. Detailed
                   fantasy illustration with bold outlines and the
                   warm amber-and-deep-teal palette established in
                   previous generations."
       |
       v
User sees suggestion in editable textarea (can modify or accept)
       |
       v
[Generate] button → POST /api/images/generate (with final prompt)
       |
       v
Gemini API → image result
```

### 4. UI Design — Using Existing Datastar Patterns

The imagegen UI already implements a "preview then confirm" workflow through server-driven fragment replacement. All state fragments share `id="image-gen-content"`, and `PatchElementTempl` swaps between them. The suggestion step fits naturally into this pattern.

**New signals** (add to `OverlaySignals` in `signals.go`):
```go
ImageSuggestedPrompt string `json:"image_suggested_prompt"` // mayor's suggestion
```

**New UI states** (add to `imagegen.templ`):
- `ImageGenSuggesting()` — spinner while mayor thinks ("Mayor is composing a suggestion...")
- `ImageGenSuggestion(original, suggested string)` — shows original and suggested side-by-side, with an editable textarea for the suggestion and Accept/Edit/Regenerate Suggestion buttons

**Button layout change** — the input bar gets two buttons:
```
[prompt input] [Suggest ✨] [Generate]
```

- **Suggest** → `@post('/api/images/suggest')` — sends `image_prompt` to mayor, patches suggestion preview
- **Generate** → `@post('/api/images/generate')` — sends `image_prompt` (or `image_suggested_prompt` if suggestion was accepted) to Gemini

**Accepting a suggestion**: The suggestion view has an "Accept" button that sets `$image_prompt = $image_suggested_prompt` via a client-side signal assignment (same pattern as aspect ratio selector in `expressions.go:31-33`), then returns to the idle/input state. User can also edit the suggestion inline before accepting.

**Alternative: inline replacement** — instead of a separate suggestion view, the server could simply patch the `image_prompt` signal with the suggested text via `MarshalAndPatchSignals({"image_prompt": suggestedPrompt})`. The user sees their prompt replaced in the input with the mayor's version and can edit or generate from there. This is simpler but less transparent (user doesn't see original vs. suggested side-by-side).

### 5. Mayor-Side Implementation Options

#### Option A: OpenClaw Skill (Mayor Agent Orchestration)

Add a `prompt-enhance` skill to the mayor's workspace. When the harness receives `POST /api/images/suggest`, it routes through Discord or the OpenClaw HTTP hooks API to ask the mayor to enhance the prompt.

**Pros**: Full access to MEMORY.md, SOUL.md, conversation context. Mayor can reference past images and builds.
**Cons**: Latency (OpenClaw round-trip through Discord or HTTP hooks). Requires mayor infrastructure (Phase 3+ of the mayor plan) to be in place.

#### Option B: Direct Gemini Text API (No Mayor Dependency)

Use the Gemini text model (not image model) to enhance the prompt, injecting the world's style description and mayor personality as system context.

```go
func (c *Client) SuggestPrompt(ctx context.Context, userPrompt, mayorPersonality, worldStyle string) (string, error) {
    systemPrompt := fmt.Sprintf(`You are %s. Enhance this image generation prompt
        while maintaining the user's intent. Add style details based on the world's
        aesthetic: %s. Keep the enhanced prompt under 200 words.`,
        mayorPersonality, worldStyle)

    result, err := c.client.Models.GenerateContent(ctx, "gemini-2.5-flash",
        genai.Text(userPrompt), &genai.GenerateContentConfig{
            SystemInstruction: genai.NewContentFromText(systemPrompt),
        })
    // extract text response...
}
```

**Pros**: Fast (~1-2 seconds). No OpenClaw dependency. Can ship before mayor infrastructure exists.
**Cons**: No access to mayor's evolving memory or conversation history. World style must be explicitly stored and passed.

#### Option C: Hybrid (Recommended)

Ship Option B first as an interim solution. When the mayor system is deployed, upgrade to Option A. The UI stays the same either way — only the backend changes.

- **Phase 1 (now)**: `POST /api/images/suggest` calls Gemini Flash text model with world metadata (name, description, template type) and a hardcoded or user-defined style description
- **Phase 2 (after mayor deployment)**: `POST /api/images/suggest` routes through the mayor agent, which has full access to MEMORY.md, SOUL.md, and past conversation context

### 6. World-Level Style Consistency

Beyond prompt attenuation, maintaining visual consistency requires:

#### Style Prefix (Stored Per-World)

A "style anchor" string stored on the world, prepended to every image generation. This could be user-defined or mayor-generated after the first few images.

**New DB column**: `worlds.image_style TEXT` — e.g., "hand-painted pixel art RPG with warm earth tones, soft cel-shading, and bold clean outlines"

**New signal**: `image_style` for editing in the UI

**Usage in generation**:
```go
fullPrompt := worldStyle + ": " + userPrompt
```

#### Reference Images (Future — Gemini 3 Pro Image)

`gemini-3-pro-image-preview` supports up to 14 reference images per call. A world could maintain a curated set of "style reference" images — either automatically selected from saved assets or manually pinned by the user/mayor.

```go
parts := []*genai.Part{
    genai.NewPartFromText(fullPrompt),
}
for _, ref := range world.StyleReferences {
    parts = append(parts, genai.NewPartFromBytes(ref.Data, ref.MIMEType))
}
```

This requires upgrading from `gemini-2.5-flash-image` to `gemini-3-pro-image-preview` and refactoring `Generate()` to accept `[]*genai.Content` with multiple parts.

### 7. Gemini API Prompt Engineering Best Practices

From Google's official guidance:

1. **Style anchors**: Describe the rendering technique explicitly — "digital painting with flat colors and sharp outlines" rather than "artistic"
2. **Consistent palette**: Name specific hex colors — "using a palette of dusty rose (#D4A0A0), deep teal (#2A6B6B), warm cream (#F5E6D3)"
3. **Character sheets**: Reuse verbatim character descriptions across prompts for consistency
4. **Trigger phrase**: Start with "Generate an image of" or "Create an image of" to reliably trigger image output
5. **Narrative style**: Describe scenes narratively, not as keyword lists
6. **Avoid "in the style of [artist]"**: Instead, describe specific visual qualities (brushstrokes, textures, color relationships)

These best practices should inform both the mayor's prompt enhancement logic and the world style prefix.

## Code References

- `harness/internal/gemini/gemini.go:95-175` — `Generate()` method, prompt construction at lines 112-115
- `harness/internal/gemini/gemini.go:25` — model name `gemini-2.5-flash-image`
- `harness/internal/gemini/gemini.go:28-31` — `chromakeySuffix` constant (only current prompt modification)
- `harness/internal/server/imagegen.go:32-82` — `handleImageGenerate` handler
- `harness/internal/server/imagegen.go:20-24` — `imageGenSignals` struct
- `harness/views/imagegen/imagegen.templ:125-141` — image gen input bar
- `harness/views/imagegen/imagegen.templ:42-122` — state fragment components (Idle, Generating, Done, Saved, Error)
- `harness/views/imagegen/expressions.go:6-33` — `ImageGenExpr` expression helper
- `harness/views/world/signals.go:6-20` — `OverlaySignals` struct
- `harness/views/world/signals.go:23-33` — `DefaultOverlaySignals` with image defaults
- `harness/internal/world/manager.go:63-220` — `CreateWorld` (where mayor fields will be added)
- `harness/internal/db/sqlc/models.go:73-80` — current `World` struct

## Architecture Insights

### Insertion Point

The cleanest insertion point is between `handleImageGenerate` reading the prompt (line 48) and calling `GeminiClient.Generate()` (line 58). A new `handleImageSuggest` handler follows the same SSE pattern but patches a suggestion view instead of triggering generation.

### Signal Flow

```
image_prompt (user input)
    → POST /api/images/suggest
    → mayor/Gemini enhances
    → SSE patches suggestion view with editable textarea
    → user accepts/modifies → image_prompt updated
    → POST /api/images/generate (normal flow)
```

### No Breaking Changes

The suggestion step is entirely additive. The existing Generate button continues to work as-is, sending raw prompts directly. The Suggest button is a new, optional step in the flow.

### Fragment State Machine (Extended)

```
Idle → [user clicks Suggest] → Suggesting → Suggestion
     → [user clicks Generate] → Generating → Done → Saved
                                           → Error

Suggestion → [user clicks Accept] → Idle (with enriched prompt in input)
           → [user clicks Generate from suggestion] → Generating → Done → Saved
           → [user clicks Dismiss] → Idle
```

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md` — Comprehensive mayor implementation plan. Phase 3 covers agent provisioning with SOUL.md, AGENTS.md, and skill definitions. The plan explicitly notes "Nano Banana image generation" as a future mayor capability (line 120: "What We're NOT Doing").

## Open Questions

1. **Latency tolerance**: How long is acceptable for the suggestion step? OpenClaw round-trip through Discord may take 5-10 seconds. Direct Gemini Flash text call takes ~1-2 seconds. This may determine whether Option A or B is preferred.

2. **Style definition UX**: Should the world style prefix be user-defined (text field on world settings), mayor-generated (after first few images), or inferred from saved assets? A combination may work best.

3. **Style reference image management**: When reference images become available (Gemini 3 Pro), how should the user curate them? Auto-select from recent saves? Manual pin/unpin? Mayor-curated?

4. **Mayor availability**: The prompt attenuation feature could ship before the mayor system is deployed (using Option B — direct Gemini text API). Should we build the UI with the full mayor flow in mind from the start, or iterate?

5. **Prompt transparency**: Should the user always see what was enhanced (diff view), or is a simple "replace prompt text" sufficient? The diff view is more transparent but adds UI complexity.

6. **Cost**: Each suggestion is an additional API call. At scale, this doubles the per-image API cost (one text call + one image call). Is this acceptable?

7. **World style storage**: Where should the style prefix live? Options: new `image_style` column on worlds table, in the mayor's MEMORY.md (accessible only via mayor), or both (DB for direct Gemini path, MEMORY.md for mayor path).
