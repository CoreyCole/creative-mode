---
date: 2026-02-16T09:34:48-08:00
researcher: CoreyCole
git_commit: 829d88abce4ee7b1121712ee02afed5ccc0b34a1
branch: main
repository: creative-mode
topic: "Onboarding Mayor Chat Improvements: Textarea, Shift+Enter, Image Upload"
tags: [research, codebase, mayor-chat, onboarding, image-upload, datastar, textarea]
status: complete
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Research: Onboarding Mayor Chat Improvements

**Date**: 2026-02-16T09:34:48-08:00
**Researcher**: CoreyCole
**Git Commit**: 829d88abce4ee7b1121712ee02afed5ccc0b34a1
**Branch**: main
**Repository**: creative-mode

## Research Question

We want to improve the onboarding mayor chat:
1. Change the input to a textarea with shift+enter for new lines
2. Support image/asset uploads in the chat to help the mayor understand the vibe
3. Have the mayor mention that images will help get on the same page

## Summary

The current mayor chat uses a single-line `<input type="text">` with Enter-to-send. Switching to a `<textarea>` with shift+enter is straightforward — modify the templ component and add `!evt.shiftKey` to the keydown guard. Image upload requires more work: a new upload endpoint, multipart handling, DB schema changes, Anthropic vision API integration (SDK already supports it), and UI changes for both sending and displaying images. The mayor's system prompt and greeting need minor edits to mention images.

## Detailed Findings

### 1. Current Chat Input (Textarea + Shift+Enter)

**File**: `site/pages/mayor.templ:50-90`

The input area is currently:
```html
<input
    type="text"
    data-bind:mayor_input
    data-on:keydown="evt.key === 'Enter' && !$_sending && $mayor_input.trim() !== '' && @post('/mayor/chat')"
    data-attr:disabled="$_sending"
    placeholder="Type your message..."
    class="..."
    autocomplete="off"
/>
```

**What needs to change:**
- Replace `<input type="text">` with `<textarea>` — `data-bind:mayor_input` works the same on textareas
- Add `!evt.shiftKey` to the keydown guard: `evt.key === 'Enter' && !evt.shiftKey && ...`
- Add `evt.preventDefault()` before the PostSSE call to prevent textarea from inserting a newline on send
- Auto-resize behavior via CSS (`resize-none`, `overflow-hidden`) and a small JS snippet or `rows="1"` with `field-sizing: content` (CSS native, supported in Chrome 123+/Firefox 133+)
- The `whitespace-pre-wrap` class on `UserMessage` (`mayor_fragments.templ:47`) already preserves newlines, so multi-line user messages will display correctly

**Datastar compatibility**: `data-bind` works on `<textarea>` elements the same as `<input>` — the signal value includes newlines as `\n`. No server-side changes needed for multi-line text.

**Server-side**: The handler at `site/internal/mayor/handler.go:76-79` trims whitespace and caps at 2000 runes — this already works for multi-line strings.

**No textarea exists anywhere in the codebase currently** — this will be the first one.

### 2. Image Upload Support

This is the most complex change. Here's the current state and what's needed:

#### Current Constraints

| Concern | Current State | Needed |
|---------|--------------|--------|
| **Body limit** | 1 MB global (`site/main.go:96`) | Increase for mayor routes (5-10 MB) |
| **CSP img-src** | `'self' data: https://cdn.discordapp.com` | Add `blob:` for client-side preview |
| **File upload** | Site has NONE (harness has `POST /api/assets/upload`) | New endpoint or multipart on `/mayor/chat` |
| **Message DB schema** | Text-only (`role` + `content`) | Need image reference storage |
| **Anthropic SDK** | v1.22.1 — supports `NewImageBlockBase64` | Currently only sends `NewTextBlock` |
| **UserMessage templ** | Text only (`mayor_fragments.templ:44-57`) | Add image thumbnail rendering |

#### Approach: Separate Upload Endpoint

The cleanest approach is a **separate upload endpoint** that returns a reference, rather than embedding images in the Datastar signal POST. This avoids fighting Datastar's JSON signal model (which doesn't support file inputs natively).

**Proposed flow:**
1. User selects image via `<input type="file">` in the chat UI
2. Client-side JS previews the image (blob URL) and uploads via `fetch()` to `POST /mayor/upload`
3. Server validates (type, size), saves to `data/chat-uploads/{discordID}/{uuid}.{ext}`, returns `{id, url}`
4. The upload ID/URL is stored in a Datastar signal (e.g., `mayor_images` array)
5. On send, the signal includes the image references alongside `mayor_input` text
6. Server retrieves the image data, builds Anthropic messages with `NewImageBlockBase64`
7. Image thumbnails appear in the user message bubble

#### Anthropic Vision Integration

**File**: `pkg/mayorchat/stream.go:82-92`

Currently builds text-only messages:
```go
result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
```

With images, this becomes:
```go
blocks := []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)}
for _, img := range msg.Images {
    blocks = append(blocks, anthropic.NewImageBlockBase64(img.MIMEType, img.Base64Data))
}
result = append(result, anthropic.NewUserMessage(blocks...))
```

The SDK supports `image/jpeg`, `image/png`, `image/gif`, `image/webp`.

#### Message Storage

**File**: `site/internal/db/db.go:49-55` (schema) and `site/internal/mayor/store.go` (queries)

Current schema:
```sql
CREATE TABLE conversation_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    discord_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**Option A — New table for message images:**
```sql
CREATE TABLE conversation_images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES conversation_messages(id),
    file_path TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    original_filename TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**Option B — JSON column on messages:**
Add an `images TEXT` column that stores JSON array of `{path, mime, filename}`. Simpler but less queryable.

**Either way**, the `Message` struct at `pkg/mayorchat/message.go:6-9` needs an `Images` field, and the store methods need updating.

#### Cleanup

Uploaded images should be cleaned up alongside messages in `cleanupLoop` (`pkg/mayorchat/conversation.go:223-240`) — when conversations older than 24 hours are deleted, their uploaded images should be removed from disk too.

#### Onboarding Data Pinning

The `pinOnboardingConversation` method at `site/internal/mayor/handler.go:597-619` pins conversation JSON to Discord. Image data should either be:
- Included as base64 in the pinned JSON (increases message size, Discord has 8MB limit for bot messages)
- Uploaded as Discord attachments alongside the pinned message
- Referenced by URL (would need the images served until the agent bootstraps)

This is a secondary concern — can be deferred.

### 3. Mayor Mentioning Images

**System prompt file**: `pkg/mayorchat/prompt.go:39-90`

Two places to add image awareness:

#### A. System Prompt — Add to "How to talk" section (around line 57)

Add after the existing conversation style rules:
```
- If the user shares images (concept art, references, screenshots), acknowledge them
  and use them to refine the world's direction. Let the user know early on that sharing
  images — mood boards, concept art, game screenshots, anything visual — helps you get
  on the same page about the vibe.
```

#### B. Greeting — `site/main.go:322-325`

Current greeting:
```go
greetingMD := fmt.Sprintf("Hey %s. I'm the Mayor — though I don't have a real name yet. "+
    "I just came online and this world is... empty. Which is actually kind of exciting.\n\n"+
    "So. What are we building?", session.DiscordUsername)
```

Could add a subtle mention:
```go
greetingMD := fmt.Sprintf("Hey %s. I'm the Mayor — though I don't have a real name yet. "+
    "I just came online and this world is... empty. Which is actually kind of exciting.\n\n"+
    "So. What are we building? If you've got reference images — concept art, "+
    "screenshots, mood boards — drop them in. Helps me see what you're seeing.", session.DiscordUsername)
```

The greeting approach is better because the mayor naturally invites images upfront rather than only reacting when images appear.

## Code References

### Files to Modify

| File | Lines | Change |
|------|-------|--------|
| `site/pages/mayor.templ` | 59-66 | Replace `<input>` with `<textarea>`, add shift+enter |
| `site/pages/mayor_fragments.templ` | 44-57 | Add image thumbnails to `UserMessage` |
| `site/pages/types.go` | 4-10 | Add `Images` field to `ChatMessage` |
| `pkg/mayorchat/message.go` | 6-9 | Add `Images` field to `Message` |
| `pkg/mayorchat/stream.go` | 82-92 | Add image blocks to `BuildAnthropicMessages` |
| `pkg/mayorchat/prompt.go` | 39-90 | Add image awareness to system prompt |
| `site/main.go` | 96 | Increase body limit (or add route-specific) |
| `site/main.go` | 103 | Add `blob:` to CSP img-src |
| `site/main.go` | 322-325 | Update greeting to mention images |
| `site/internal/mayor/handler.go` | 32-35 | Add image refs to `ChatSignals` |
| `site/internal/mayor/handler.go` | 63-274 | Handle image refs in `HandleChat` |
| `site/internal/mayor/store.go` | 22-48 | Store/retrieve image references |
| `site/internal/db/db.go` | 49-55 | Schema migration for images |

### New Files

| File | Purpose |
|------|---------|
| `site/internal/mayor/upload.go` | Image upload endpoint handler |

### Existing Reference Patterns

| Pattern | Location | Reusable? |
|---------|----------|-----------|
| Harness asset upload | `harness/internal/server/assets.go:23-100` | MIME validation, path sanitization |
| Cover art disk storage | `pkg/mayorchat/cover.go:21-40` | File naming, cleanup pattern |
| Anthropic image blocks | SDK `anthropic.NewImageBlockBase64` | Direct use |

## Architecture Insights

1. **Datastar + file upload**: Datastar signals are JSON — they can't carry binary file data. The recommended pattern is a separate REST upload endpoint that returns a reference, then include the reference in signals. This matches the existing cover art pattern (Gemini generates → disk → reference).

2. **Two-phase approach**: The textarea change is completely independent of image upload. Ship textarea + shift+enter first as a quick win, then build image upload as a follow-up.

3. **Claude vision cost**: Claude Sonnet 4.5 vision is priced per image token. Each uploaded image costs roughly 1600 tokens for a typical web image. Worth capping at 3-4 images per conversation to control costs.

4. **Scripted fallback**: The scripted fallback path (`pkg/mayorchat/scripted.go`) doesn't use the Anthropic API, so images have no effect in scripted mode. The uploaded images would simply be ignored, which is acceptable.

## Historical Context (from thoughts/)

- `thoughts/CoreyCole/plans/2026-02-14_03-02-42_meet-the-mayor-site-page.md` — Original "Meet the Mayor" implementation plan
- `thoughts/CoreyCole/reviews/2026-02-14_03-15-25_meet-the-mayor-site-page_review.md` — Review that suggested shift+enter handling (line 95), never implemented
- `thoughts/CoreyCole/plans/2026-02-15_15-13-50_world-cover-art-generation.md` — Cover art generation plan (reference for image handling patterns)
- `thoughts/CoreyCole/research/2026-02-11_16-21-45_asset-serving-shared-worlds.md` — Asset serving research (reference for file storage patterns)

## Open Questions

1. **Max image size per upload?** The harness uses no limit; the site has 1MB global. Suggest 5MB per image, 3-4 images max per conversation.
2. **Image persistence after hatch?** Currently conversations are cleaned up after 24 hours. Should uploaded images be included in the Discord pinned onboarding data?
3. **Image display in conversation history?** On page reload, should images be re-rendered in the chat? This requires storing image references in the DB and serving them back.
4. **Drag-and-drop?** Beyond the file input button, should we support drag-and-drop onto the chat area? More polished but more JS.
5. **Paste from clipboard?** Users often paste screenshots — `ctrl+v` / `cmd+v` image paste support would be a nice UX touch.
