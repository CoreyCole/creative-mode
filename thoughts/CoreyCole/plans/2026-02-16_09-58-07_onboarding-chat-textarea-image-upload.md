# Onboarding Chat Improvements — Textarea, Shift+Enter, Image Upload

## Overview

Improve the mayor onboarding chat with three changes shipped together:
1. **Textarea with Shift+Enter** — replace `<input type="text">` with auto-growing `<textarea>`, Enter sends, Shift+Enter inserts newline (site already done, harness pending)
2. **Image upload** — file picker + drag-drop so users can share concept art, mood boards, screenshots, sent to Claude via vision API
3. **Mayor prompt/greeting** — invite and acknowledge images

## Current State Analysis

The site's `mayor.templ` already has the textarea + shift+enter + auto-resize changes (uncommitted on `main`). The harness `create/page.templ` still uses `<input type="text">`.

Image upload does not exist anywhere on the site. The harness has a multipart upload at `POST /api/assets/upload` (`harness/internal/server/assets.go:23-100`) that we can reference.

The Anthropic SDK (v1.22.1) supports vision via `anthropic.NewImageBlockBase64(mediaType, base64Data)` but the codebase currently only sends text blocks (`pkg/mayorchat/stream.go:86`).

### Key Discoveries
- `site/pages/mayor.templ` — textarea already implemented (uncommitted)
- `harness/views/create/page.templ:53-61` — still `<input type="text">`, needs same treatment
- `site/main.go:96` — global body limit `1M`, too small for image upload
- `site/main.go:103` — CSP `img-src` missing `blob:` for drag-drop preview
- `pkg/mayorchat/message.go:6-9` — `Message` struct is text-only
- `pkg/mayorchat/stream.go:82-92` — `BuildAnthropicMessages` only uses `NewTextBlock`
- `site/internal/db/db.go:49-55` — `conversation_messages` table has no image support
- `pkg/mayorchat/cover.go:43` — `MimeToExt` already exists, reusable
- `harness/internal/server/assets.go:14-17` — `allowedImageMIME` map pattern, reusable

## Desired End State

1. Both site and harness chat inputs are `<textarea>` elements that auto-grow, send on Enter, and insert newlines on Shift+Enter
2. Site mayor chat has a file picker button + drag-drop zone for uploading images (JPEG, PNG, WebP, GIF, max 5MB each, max 4 per conversation)
3. Uploaded images appear as thumbnails in the input area before send, and in the user message bubble after send
4. Images are sent to Claude via `NewImageBlockBase64` so the mayor can "see" them
5. Images persist across page reload (stored in DB + on disk, cleaned up with conversations after 24h)
6. The mayor's greeting invites image sharing; the system prompt instructs the mayor to acknowledge images

### Verification
- `just check` passes from project root
- Manual test with `playwright-cli --headed --persistent`:
  - Textarea appears, auto-grows, Enter sends, Shift+Enter inserts newline
  - Upload image via file picker → thumbnail appears in preview area
  - Drag-drop image → thumbnail appears
  - Remove button removes thumbnail
  - Send message with image → image shows in user message bubble
  - Mayor's response acknowledges the image content
  - Page reload → images persist in conversation history
  - Fresh conversation → greeting mentions images
  - Harness `/create` page → textarea works (no image upload needed)

## What We're NOT Doing

- No image upload on the harness create page (site-only)
- No paste-from-clipboard support (future enhancement)
- No image data in Discord onboarding pin (deferred — just note filenames)
- No image lightbox/full-size view (thumbnails only)
- No image compression/resizing server-side

## Implementation Approach

Separate upload endpoint (not multipart on `/mayor/chat`) because Datastar signals are JSON — can't carry binary data. Upload returns a reference, reference stored in transient state, attached to message on send. Separate `ImageStore` interface keeps the harness `MessageStore` unchanged.

---

## Phase 1: Textarea + Shift+Enter on Harness Create Page

### Overview
Apply the same textarea change already done on `site/pages/mayor.templ` to the harness create page.

### Changes Required

#### 1. Harness create page
**File**: `harness/views/create/page.templ`
**Lines**: 50-61

Replace `<input type="text">` with `<textarea>`:
- Add `items-end` to the flex container (line 51)
- Replace `<input type="text" .../>` with `<textarea ...></textarea>`
- Add `!evt.shiftKey` and `evt.preventDefault()` to keydown guard
- Add `rows="1"`, `resize-none`, `overflow-y-auto`, `max-h-32` classes
- Update placeholder to mention Shift+Enter
- Add `self-end` to Send button classes
- Add auto-resize `<script>` block (same pattern as site)

### Success Criteria

#### Automated:
- [ ] `just check` passes

#### Manual:
- [ ] Textarea appears at `/create`
- [ ] Enter sends, Shift+Enter inserts newline
- [ ] Textarea auto-grows with content, caps at ~5 lines

---

## Phase 2: Image Upload Backend

### Overview
Add DB schema, types, interfaces, store methods, and upload/serve/delete handlers.

### Changes Required

#### 1. Message struct
**File**: `pkg/mayorchat/message.go`

Add `ImageAttachment` struct and `Images` field to `Message`:

```go
// ImageAttachment represents an image attached to a chat message.
type ImageAttachment struct {
    ID       string // UUID (filename stem on disk)
    FilePath string // on-disk path
    MIMEType string // image/jpeg, image/png, image/webp, image/gif
    Filename string // original user filename
}

type Message struct {
    Role    string
    Content string
    Images  []ImageAttachment // nil for text-only messages
}
```

#### 2. DB schema
**File**: `site/internal/db/db.go` — add in `createTables` after line 55

```sql
CREATE TABLE IF NOT EXISTS conversation_images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    discord_id TEXT NOT NULL,
    message_index INTEGER NOT NULL,
    file_path TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    original_filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_conv_images_discord_id ON conversation_images(discord_id);
```

`message_index` = ordinal position of the user message in the conversation.

#### 3. ImageStore interface
**File**: `pkg/mayorchat/conversation.go`

Add optional `ImageStore` interface (keeps `MessageStore` unchanged — harness unaffected):

```go
type ImageStore interface {
    AddImage(userID string, messageIndex int, filePath, mimeType, filename string) error
    GetImages(userID string) ([]ImageRecord, error)
    DeleteImages(userID string) error
    DeleteImagesOlderThan(d time.Duration) ([]string, error) // returns file paths for disk cleanup
}

type ImageRecord struct {
    MessageIndex     int
    FilePath         string
    MIMEType         string
    OriginalFilename string
}
```

Add `imageStore ImageStore` field to `ConversationManager`, `SetImageStore()` setter.

Update `GetMessages` to merge images from `imageStore` into `Message.Images` when available.

Update `ResetConversation` and `cleanupLoop` to delete image DB records + disk files.

Add pending image tracking to `transientState`:

```go
PendingImages []PendingImage
ImageCount    int // total across conversation, cap at 4
```

With methods: `AddPendingImage`, `GetPendingImages`, `ClearPendingImages`, `GetImageCount`, `RemovePendingImage`.

#### 4. SQLite image store methods
**File**: `site/internal/mayor/store.go`

Implement `ImageStore` on `SQLiteMessageStore` — `AddImage`, `GetImages`, `DeleteImages`, `DeleteImagesOlderThan`. Reuse existing `*sql.DB`.

#### 5. Upload/serve/delete handlers
**New file**: `site/internal/mayor/upload.go`

**`HandleImageUpload`** (`POST /mayor/upload`):
1. Auth check (session from middleware)
2. Check image count cap (4 per conversation) via `convMgr.GetImageCount`
3. `c.Request().FormFile("file")` for multipart
4. Validate MIME: `image/png`, `image/jpeg`, `image/webp`, `image/gif`
5. Content-sniff first 512 bytes via `http.DetectContentType` (don't trust header alone)
6. Validate size <= 5MB
7. Save to `{dataDir}/chat-uploads/{discordID}/{uuid}{ext}` (reuse `mayorchat.MimeToExt`)
8. Add to `convMgr.AddPendingImage`
9. Return JSON: `{"id": "uuid", "url": "/mayor/image/uuid", "filename": "original.png"}`

**`HandleImageServe`** (`GET /mayor/image/:imageID`):
- Look up file path from pending images or DB images (scan by UUID match)
- Serve via `c.File(path)`

**`HandleImageDelete`** (`DELETE /mayor/image/:imageID`):
- Remove from pending images + delete file from disk

#### 6. Route registration + body limit
**File**: `site/main.go`

Increase global body limit from `"1M"` to `"6M"` (line 96). The chat handler caps text at 2000 runes; the upload handler validates 5MB per file. Other endpoints parse specific small payloads and are unaffected.

Register routes after existing mayor routes (after line 378):

```go
mayorGroup.POST("/mayor/upload", mayorHandler.HandleImageUpload)
mayorGroup.GET("/mayor/image/:imageID", mayorHandler.HandleImageServe)
mayorGroup.DELETE("/mayor/image/:imageID", mayorHandler.HandleImageDelete)
```

#### 7. CSP update
**File**: `site/main.go` (line 103)

Add `blob:` to `img-src`:
```
img-src 'self' data: blob: https://cdn.discordapp.com
```

#### 8. Wire ImageStore
**File**: `site/main.go` (around line 163)

After creating `convMgr`:
```go
convMgr.SetImageStore(store)
```

### Success Criteria

#### Automated:
- [ ] `just check` passes
- [ ] DB migration creates `conversation_images` table on startup

#### Manual:
- [ ] `POST /mayor/upload` with valid image returns JSON with id/url
- [ ] `GET /mayor/image/:imageID` serves the uploaded file
- [ ] `DELETE /mayor/image/:imageID` removes pending image
- [ ] 5th image upload returns 400
- [ ] >5MB file rejected
- [ ] Non-image MIME type rejected

---

## Phase 3: Image Upload in Chat Handler

### Overview
Wire pending images into the chat send flow and the Anthropic API.

### Changes Required

#### 1. Attach pending images on send
**File**: `site/internal/mayor/handler.go` — in `HandleChat`, after `AddMessage` (line 95)

```go
pendingImages := h.convMgr.GetPendingImages(session.DiscordID)
if len(pendingImages) > 0 {
    msgs := h.convMgr.GetMessages(session.DiscordID)
    msgIndex := len(msgs) - 1
    for _, img := range pendingImages {
        h.convMgr.AddImageToMessage(session.DiscordID, msgIndex, img)
    }
    h.convMgr.ClearPendingImages(session.DiscordID)
}
```

#### 2. SSE user message with images
**File**: `site/internal/mayor/handler.go` — update user message SSE patch (line 116)

When `len(pendingImages) > 0`, use `UserMessageWithImages` component instead of `UserMessage`, passing image URLs.

#### 3. Clear image previews via SSE
After SSE stream starts, patch `#image-previews` to empty:

```go
sse.PatchElements(`<div id="image-previews" class="flex flex-wrap gap-2"></div>`,
    datastar.WithSelectorID("image-previews"))
```

#### 4. Anthropic vision API integration
**File**: `pkg/mayorchat/stream.go` — update `BuildAnthropicMessages` (lines 82-92)

```go
if msg.Role == "user" {
    if len(msg.Images) == 0 {
        result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
    } else {
        blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Images)+1)
        for _, img := range msg.Images {
            data, err := os.ReadFile(img.FilePath)
            if err != nil { continue }
            b64 := base64.StdEncoding.EncodeToString(data)
            blocks = append(blocks, anthropic.NewImageBlockBase64(img.MIMEType, b64))
        }
        blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
        result = append(result, anthropic.NewUserMessage(blocks...))
    }
}
```

SDK supports: `image/jpeg`, `image/png`, `image/gif`, `image/webp`.

### Success Criteria

#### Automated:
- [ ] `just check` passes

#### Manual:
- [ ] Send message with image → image appears in user bubble, mayor acknowledges image content
- [ ] Send text-only message → works as before (no regression)
- [ ] Image-only message (no text) → works
- [ ] Multiple images in one message → all shown

---

## Phase 4: Image Upload UI

### Overview
Add upload button, drag-drop zone, preview area, and image display in message bubbles.

### Changes Required

#### 1. ChatMessage type
**File**: `site/pages/types.go`

Add `ImageURLs []string` to `ChatMessage`.

#### 2. UserMessageWithImages component
**File**: `site/pages/mayor_fragments.templ`

New component — same as `UserMessage` but with image grid above text:

```go
templ UserMessageWithImages(msgID string, content string, avatarURL string, imageURLs []string) {
    <div id={ "msg-" + msgID } class="max-w-2xl mx-auto flex items-end gap-2 justify-end">
        <div class="max-w-[85%] rounded-2xl rounded-br-sm bg-primary text-primary-foreground px-4 py-3">
            if len(imageURLs) > 0 {
                <div class="flex flex-wrap gap-2 mb-2">
                    for _, url := range imageURLs {
                        <img src={ url } alt="Uploaded image"
                            class="max-w-[200px] max-h-[150px] rounded-lg object-cover" loading="lazy"/>
                    }
                </div>
            }
            if content != "" {
                <p class="text-sm whitespace-pre-wrap">{ content }</p>
            }
        </div>
        // avatar (same as UserMessage)
    </div>
}
```

#### 3. Upload UI in mayor.templ
**File**: `site/pages/mayor.templ`

Add above the textarea (inside the input area div):
- `<div id="image-previews" class="flex flex-wrap gap-2">` — preview thumbnails with remove buttons
- Hidden `<input type="file" id="image-file-input" accept="image/*" multiple>`
- Image button (paperclip/photo icon) next to textarea

Wrap the input area in a drop zone with `dragover`/`dragleave`/`drop` handlers.

#### 4. Upload + preview JavaScript
**File**: `site/pages/mayor.templ`

Inline `<script>` block:
1. File input `change` → upload each file via `fetch('/mayor/upload')` → add thumbnail to `#image-previews`
2. Drop zone `drop` → same upload flow
3. Remove button → `DELETE /mayor/image/{id}` → remove thumbnail
4. Track upload count, disable Send while uploads in progress
5. Show visual drop indicator (border highlight) on dragover
6. Server clears `#image-previews` via SSE after send

#### 5. Page-load rendering with images
**File**: `site/main.go` (lines 329-342)

When building `ChatMessage` from `mayorchat.Message`, populate `ImageURLs` from `msg.Images` → `/mayor/image/{id}`. In the `MayorPage` template, use `UserMessageWithImages` when `len(msg.ImageURLs) > 0`.

### Success Criteria

#### Automated:
- [ ] `just check` passes

#### Manual:
- [ ] Upload button opens file picker
- [ ] Drag-drop onto chat area uploads image
- [ ] Thumbnails appear in preview area with remove buttons
- [ ] Remove button removes thumbnail and calls DELETE endpoint
- [ ] Send button disabled while upload in progress
- [ ] After send, preview area clears
- [ ] Images persist in message bubbles on page reload

---

## Phase 5: Mayor Prompt + Greeting

### Overview
Update the system prompt and greeting to invite and acknowledge images.

### Changes Required

#### 1. System prompt
**File**: `pkg/mayorchat/prompt.go` — add to "How to talk" section (after line 57)

```
- If the user shares images (concept art, references, screenshots), acknowledge them
  and use them to refine the world's direction. Describe what you see briefly and
  connect it to the world being built. Don't narrate every detail of the image.
```

#### 2. Greeting
**File**: `site/main.go` (lines 322-325)

```go
greetingMD := fmt.Sprintf("Hey %s. I'm the Mayor — though I don't have a real name yet. "+
    "I just came online and this world is... empty. Which is actually kind of exciting.\n\n"+
    "So. What are we building? If you've got reference images — concept art, "+
    "screenshots, mood boards — drop them in. Helps me see what you're seeing.", session.DiscordUsername)
```

### Success Criteria

#### Automated:
- [ ] `just check` passes

#### Manual:
- [ ] Fresh conversation shows greeting mentioning images
- [ ] Mayor acknowledges uploaded images in responses
- [ ] Mayor doesn't over-describe images (brief acknowledgment)

---

## Testing Strategy

### Manual Testing Steps
1. Start Docker with `just live` from `harness/`
2. Open `playwright-cli open http://localhost:8080 --headed --persistent`
3. Navigate to `/mayor` — verify textarea and greeting
4. Type multi-line message with Shift+Enter — verify newlines preserved in sent message
5. Upload image via file picker — verify thumbnail preview
6. Drag-drop image — verify thumbnail preview
7. Click remove on thumbnail — verify removed
8. Send message with text + image — verify both appear in bubble
9. Verify mayor's response references the image content
10. Reload page — verify images still visible in history
11. Upload 4 images across messages — verify 5th is rejected
12. Upload >5MB file — verify rejected
13. Navigate to harness `/create` — verify textarea works

### Edge Cases
- Image-only message (empty text) — should work
- Send while upload in progress — should be blocked
- Scripted fallback mode — images stored/displayed but not sent to API
- Server restart — pending (unsent) images cleaned up within 24h
- Concurrent image upload and send — upload counter prevents premature send

## Performance Considerations

- Claude vision: ~1600 tokens per typical web image. Max 4 images per conversation caps at ~6400 extra input tokens.
- `BuildAnthropicMessages` reads images from disk and base64-encodes on each chat request. Max 4 images x 5MB = 20MB disk reads + ~27MB base64. Acceptable for single-user onboarding flow.
- Upload endpoint writes to disk synchronously — fine for occasional image uploads.

## References

- Research: `thoughts/CoreyCole/research/2026-02-16_09-34-48_onboarding-chat-improvements.md`
- Harness asset upload pattern: `harness/internal/server/assets.go:23-100`
- Cover art disk save pattern: `pkg/mayorchat/cover.go:21-40`
- Anthropic SDK vision: `anthropic.NewImageBlockBase64` in `github.com/anthropics/anthropic-sdk-go@v1.22.1`
- Existing `MimeToExt`: `pkg/mayorchat/cover.go:43`
