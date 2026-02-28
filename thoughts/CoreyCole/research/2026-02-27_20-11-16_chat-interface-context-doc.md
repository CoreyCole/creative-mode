---
date: 2026-02-27T20:11:16-08:00
researcher: Corey Cole
git_commit: b8ca5ea7c4c02a3f231059a68f010bbe9eecfac7
branch: main
repository: creative-mode
topic: "Chat Interface Context Document for Standalone Onboarding Chat App"
tags: [research, codebase, chat, mobile, onboarding, anthropic-api, sse, datastar]
status: complete
last_updated: 2026-02-27
last_updated_by: Corey Cole
last_updated_note: "Added SQLite database setup, schema, store code, and conversation manager details"
---

# Context Document: Building a Standalone Chat App from Creative Mode's Chat Interface

**Date**: 2026-02-27T20:11:16-08:00
**Researcher**: Corey Cole
**Git Commit**: b8ca5ea7c4c02a3f231059a68f010bbe9eecfac7
**Branch**: main
**Repository**: creative-mode

## Purpose

This document provides everything you need to build a standalone onboarding chat app with file attachments, powered by the Anthropic API. Clone the [creative-mode](https://github.com/coreycole/creative-mode) repo and use `site/` as your reference implementation. The chat UI has been battle-tested on mobile (iOS Safari, Android Chrome/Brave) with special care taken to handle virtual keyboard behavior, scroll management, and touch interactions. **Do not reinvent the wheel** -- copy the exact code patterns documented below.

## Getting Started

```bash
git clone https://github.com/coreycole/creative-mode.git
cd creative-mode/site
```

The chat lives at `/mayor` in the site. The key files are:

| File | What It Does |
|------|-------------|
| `layouts/chat.templ` | Fixed-viewport layout that prevents document scrolling on mobile |
| `layouts/head.templ` | HTML `<head>` with critical `interactive-widget=resizes-content` viewport meta |
| `layouts/args.go` | `FixedViewport` flag that switches between normal and chat layouts |
| `pages/mayor.templ` | The full chat page: message list, input area, all client-side JS |
| `pages/mayor_fragments.templ` | SSE fragment templates for user/assistant message bubbles |
| `pages/types.go` | `ChatMessage` struct and Datastar signal initialization |
| `static/css/chat.css` | Compact markdown styles for chat message bubbles |
| `static/css/theme.css` | Design tokens (shadcn/ui), scrollbar styling, dark mode |
| `internal/mayor/handler.go` | SSE streaming handler: Anthropic API streaming, DOM patching, scroll commands |
| `internal/mayor/client.go` | Anthropic API client setup |
| `internal/mayor/store.go` | SQLite persistence for messages and images |
| `pkg/mayorchat/` | Shared package: conversation manager, message types, Anthropic message building |

Read `site/CLAUDE.md` for the full architectural overview.

---

## Architecture Overview

The chat uses **SSE-over-POST with Datastar**:
1. User types a message and clicks Send (or presses Enter on desktop)
2. Browser POSTs form data (including file attachments) to `/mayor/chat`
3. Server reads form data, saves to SQLite, builds Anthropic API request
4. Server opens an SSE stream back to the browser
5. Server streams Claude's response token-by-token, sending SSE events that:
   - Patch DOM elements (append messages, update streaming content)
   - Update reactive signals (clear input field)
   - Execute JavaScript (scroll to bottom, reset form)
6. Browser (via Datastar) processes SSE events and updates the UI in real-time

**There is no WebSocket.** Every user message is a new HTTP POST that streams back SSE events.

**There is no client-side state management.** All chat state lives on the server (SQLite + in-memory `ConversationManager`). The browser is a thin rendering layer.

### Tech Stack

- **Go** backend with [Echo](https://echo.labstack.com/) HTTP framework
- **[templ](https://templ.guide/)** for HTML templates (Go-native, type-safe)
- **[Datastar](https://data-star.dev/)** for reactive UI (SSE protocol, signal binding, DOM patching)
- **[Tailwind CSS v4](https://tailwindcss.com/)** for styling (utility-first, no inline styles)
- **[Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go)** for Claude API streaming
- **SQLite** (via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)) for persistence

---

## Critical Mobile Patterns (DO NOT SKIP)

The mobile chat experience required extensive trial and error. These patterns are proven to work on iOS Safari, Android Chrome, and Android Brave. **Copy them exactly.**

### 1. Fixed-Viewport Chat Layout

The chat uses a purpose-built `ChatLayout` (`layouts/chat.templ`) instead of the normal page layout. It implements **layered defense** against unwanted document scrolling on mobile:

```html
<!-- layouts/chat.templ -->
<html lang="en" class="overflow-clip">
  <body class="bg-background font-sans antialiased overflow-clip overscroll-none">
    <div class="fixed inset-0 flex flex-col overflow-clip bg-background">
      <!-- header -->
      <!-- chat content (children) -->
    </div>
  </body>
</html>
```

Why each layer matters:

| Technique | Why |
|-----------|-----|
| `overflow-clip` on `<html>` + `<body>` | Stronger than `overflow-hidden` -- forbids ALL scrolling including programmatic. Android Chromium ignores `overflow-hidden` on body. |
| `overscroll-none` on `<body>` | Prevents rubber-band bounce effect and scroll chaining to parent document |
| `fixed inset-0` wrapper div | Removes content from document flow entirely -- body has nothing to scroll |
| `overflow-clip` on wrapper | Clips any overflow within the fixed container |

**What does NOT work** (tried and failed on Android Brave):
- `position: fixed` or `overflow: hidden` on body/html alone
- `h-[100svh]` wrapper without `position: fixed`
- Inline `style` attributes mixed with Tailwind classes

### 2. Viewport Meta Tag for Virtual Keyboard

In `layouts/head.templ`, when `FixedViewport` is true:

```html
<meta name="viewport" content="width=device-width, initial-scale=1.0, interactive-widget=resizes-content"/>
```

The `interactive-widget=resizes-content` directive tells the browser to **shrink the layout viewport** when the virtual keyboard opens, rather than just overlaying it. This means the CSS flexbox layout automatically reflows and the input stays visible above the keyboard.

### 3. Chat Page Flexbox Structure

The page content (`pages/mayor.templ`) is a three-part vertical flex column:

```
ChatLayout wrapper (fixed inset-0 flex flex-col)
  +-- Header (shrink-0)
  +-- #mayor-container (flex-1 min-h-0 flex flex-col overflow-clip)
       +-- Compact header bar (shrink-0)
       +-- #chat-messages (flex-1 min-h-0 overflow-y-auto overscroll-y-contain touch-pan-y)
       +-- Input area (shrink-0)
```

Key CSS on the chat messages container:
```html
<div id="chat-messages" class="flex-1 min-h-0 overflow-y-auto overscroll-y-contain touch-pan-y space-y-3 px-4 py-4">
```

- `flex-1 min-h-0` -- fills available space AND can shrink below content height (essential for flex scroll containers)
- `overflow-y-auto` -- enables vertical scrolling **only within this container**
- `overscroll-y-contain` -- prevents scroll chaining when hitting top/bottom
- `touch-pan-y` -- tells the browser compositor only vertical panning is allowed

### 4. Scroll-to-Bottom on Virtual Keyboard Open

```javascript
// In pages/mayor.templ
(function () {
    var el = document.getElementById("chat-messages");
    if (!el) return;
    function scrollToBottom() {
        el.scrollTop = el.scrollHeight;
    }
    requestAnimationFrame(scrollToBottom);
    if (window.visualViewport) {
        window.visualViewport.addEventListener("resize", scrollToBottom);
    }
})();
```

Two mechanisms:
1. `requestAnimationFrame(scrollToBottom)` on page load
2. `visualViewport.resize` listener -- when the virtual keyboard opens (which shrinks the layout viewport due to `interactive-widget=resizes-content`), re-scrolls so the latest message stays visible

### 5. Server-Driven Scroll During Streaming

The server sends scroll commands via SSE after every DOM mutation:

```go
// internal/mayor/handler.go
const scrollChatJS = "document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"

// Called after: appending user message, appending streaming placeholder,
// every streaming delta, final render, dialog patches, etc.
sse.ExecuteScript(scrollChatJS)
```

### 6. Desktop vs Mobile Enter Key

```javascript
data-on:keydown="evt.key === 'Enter' && !evt.shiftKey && navigator.maxTouchPoints === 0 && ..."
```

`navigator.maxTouchPoints === 0` distinguishes desktop from mobile/tablet. On touch devices, Enter inserts a newline. On desktop, Enter sends and Shift+Enter inserts a newline.

---

## Chat Input Area

### Textarea Auto-Resize

```javascript
// In pages/mayor.templ
(function () {
    var ta = document.querySelector("textarea[data-bind\\:mayor_input]");
    if (!ta) return;
    function resize() {
        ta.style.height = "auto";
        ta.style.height = Math.min(ta.scrollHeight, 128) + "px";
    }
    ta.addEventListener("input", resize);
    new MutationObserver(function () {
        if (!ta.value) { ta.style.height = "auto"; }
    }).observe(ta, { attributes: true });
})();
```

- Starts at `rows="1"`, grows up to 128px (matching `max-h-32`)
- MutationObserver resets height when Datastar clears the value after sending
- `resize-none` prevents manual resize handles

### Textarea CSS Classes

```
flex-1 min-w-0 rounded-md border border-input bg-background px-3 py-2 text-sm
text-foreground placeholder-muted-foreground focus:border-primary focus:outline-none
focus:ring-1 focus:ring-primary disabled:opacity-50 resize-none overflow-y-auto max-h-32
```

### File Attachment (Image Upload)

The file input is visually hidden inside a styled label:

```html
<label class="... cursor-pointer">
    <svg><!-- camera icon --></svg>
    <input type="file" name="images" multiple accept="image/jpeg,image/png,image/gif,image/webp" class="hidden"/>
</label>
<span id="image-count" class="hidden ..."></span>
```

Image count badge updates on file selection:

```javascript
input.addEventListener("change", function () {
    var n = input.files ? input.files.length : 0;
    if (n > 0) {
        badge.textContent = n + " image" + (n > 1 ? "s" : "");
        badge.classList.remove("hidden");
    } else {
        badge.classList.add("hidden");
    }
});
```

### Drag-and-Drop

Full drag-and-drop support with overlay indicator:

```javascript
var dragCounter = 0;
var MAX_IMAGES = 4;
container.addEventListener("dragenter", function (e) {
    e.preventDefault();
    dragCounter++;
    overlay.classList.remove("hidden");
});
container.addEventListener("drop", function (e) {
    e.preventDefault();
    dragCounter = 0;
    overlay.classList.add("hidden");
    var dt = new DataTransfer();
    for (var i = 0; i < files.length && dt.items.length < MAX_IMAGES; i++) {
        if (files[i].type.startsWith("image/")) {
            dt.items.add(files[i]);
        }
    }
    input.files = dt.files;
});
```

Uses `dragCounter` to handle nested `dragenter`/`dragleave` correctly (prevents overlay flicker).

---

## Message Bubble HTML

### Assistant Message

```html
<div class="max-w-2xl mx-auto flex items-end gap-2 justify-start">
    <img src="/img/avatar.jpeg" alt="Assistant" class="w-8 h-8 rounded-full shrink-0 object-cover"/>
    <div class="max-w-[85%]">
        <div class="rounded-2xl rounded-bl-sm bg-muted px-4 py-3">
            <div class="chat-message-content">
                <!-- rendered markdown HTML -->
            </div>
        </div>
    </div>
</div>
```

- Left-aligned (`justify-start`) with 32x32px avatar
- `rounded-2xl rounded-bl-sm` -- rounded corners with bottom-left tail
- `bg-muted` background
- `max-w-[85%]` prevents full-width spanning

### User Message

```html
<div class="max-w-2xl mx-auto flex items-end gap-2 justify-end">
    <div class="max-w-[85%] rounded-2xl rounded-br-sm bg-primary text-primary-foreground px-4 py-3">
        <p class="text-sm whitespace-pre-wrap">message text</p>
    </div>
    <img src="/avatar-url" alt="You" class="w-8 h-8 rounded-full shrink-0 object-cover"/>
</div>
```

- Right-aligned (`justify-end`) with avatar on the right
- `rounded-br-sm` -- tail on bottom-right
- `bg-primary text-primary-foreground` -- inverted color scheme

### User Message with Images

Same as user message but adds an image grid inside the bubble:

```html
<div class="flex flex-wrap gap-2 mb-2">
    <img src="/image-url" alt="Attached image" class="max-w-[200px] max-h-[150px] rounded-lg object-cover"/>
</div>
```

### Streaming Placeholder (Typing Indicator)

```html
<span class="inline-block w-2 h-5 bg-foreground/50 rounded-sm animate-pulse"></span>
```

---

## Chat Message Content CSS (`static/css/chat.css`)

Compact markdown rendering inside chat bubbles. Copy this file directly:

```css
.chat-message-content {
  @apply text-sm leading-relaxed text-foreground;
}
.chat-message-content > * + * {
  @apply mt-2;
}
.chat-message-content p {
  @apply mb-2;
}
.chat-message-content p:last-child {
  @apply mb-0;
}
.chat-message-content h1 { @apply text-lg; }
.chat-message-content h2 { @apply text-base; }
.chat-message-content h3 { @apply text-sm font-semibold; }
.chat-message-content ul { @apply list-disc; }
.chat-message-content ol { @apply list-decimal; }
.chat-message-content ul, .chat-message-content ol { @apply mb-2 pl-4; }
.chat-message-content li { @apply mb-1; }
.chat-message-content li:last-child { @apply mb-0; }
.chat-message-content pre { @apply my-2 rounded bg-background/50 p-2 text-xs overflow-x-auto; }
.chat-message-content code:not(pre code) { @apply rounded bg-background/50 px-1 py-0.5 text-xs font-mono; }
.chat-message-content blockquote { @apply my-2 border-l-2 border-border pl-3 italic text-muted-foreground; }
.chat-message-content strong { @apply font-bold; }
.chat-message-content em { @apply italic; }
.chat-message-content hr { @apply border-border my-3; }
```

---

## Theme System (`static/css/theme.css`)

Uses shadcn/ui design tokens with CSS custom properties. Supports light/dark mode via `.dark` class on `<html>`.

Key tokens used by the chat:
- `--background` / `--foreground` -- page bg and text
- `--primary` / `--primary-foreground` -- user message bubbles
- `--muted` / `--muted-foreground` -- assistant message bubbles, placeholder text
- `--border` / `--input` -- borders, input fields
- `--destructive` -- error messages

Custom scrollbar (thin, unobtrusive):
```css
::-webkit-scrollbar { width: 5px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: hsl(var(--border)); border-radius: 5px; }
* { scrollbar-width: thin; scrollbar-color: hsl(var(--border)) transparent; }
```

Theme initialization (in `<head>`, before Datastar loads):
```javascript
function initTheme() {
    const theme = localStorage.getItem("theme") ||
        (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    document.documentElement.classList.toggle("dark", theme === "dark");
    return theme;
}
initTheme();
```

---

## Backend: SSE Streaming with Anthropic API

### Anthropic Client Setup

```go
// pkg/mayorchat/client.go
import "github.com/anthropics/anthropic-sdk-go"

func NewClient(apiKey string) anthropic.Client {
    return anthropic.NewClient(option.WithAPIKey(apiKey))
}
```

### Streaming Handler Pattern

The core pattern in `internal/mayor/handler.go`:

```go
func (h *Handler) HandleChat(c echo.Context) error {
    // 1. Read form data BEFORE creating SSE (critical ordering)
    content := strings.TrimSpace(c.FormValue("mayor_input"))

    // 2. Process uploaded images
    form, _ := c.MultipartForm()
    // ... validate size, MIME type, save to disk

    // 3. Rate limit check
    if err := h.convMgr.CheckRateLimit(userID); err != nil {
        sse := datastar.NewSSE(c.Response().Writer, c.Request())
        return sse.PatchElementTempl(RateLimitError())
    }

    // 4. Save user message to conversation
    h.convMgr.AddMessage(userID, "user", content)

    // 5. Build Anthropic messages (with base64 images for vision)
    messages := h.convMgr.GetMessages(userID)
    anthropicMessages := mayorchat.BuildAnthropicMessages(messages)

    // 6. Start SSE stream
    sse := datastar.NewSSE(c.Response().Writer, c.Request())

    // 7. Clear input and reset form
    sse.MarshalAndPatchSignals(map[string]any{"mayor_input": ""})
    sse.ExecuteScript("document.getElementById('chat-form').reset()")

    // 8. Append user message bubble to DOM
    sse.PatchElementTempl(UserMessage(...),
        datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages"))
    sse.ExecuteScript(scrollChatJS)

    // 9. Append streaming placeholder
    sse.PatchElementTempl(MayorMessageStreaming(assistantMsgID),
        datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages"))

    // 10. Stream from Claude
    stream := h.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
        Model:     anthropic.ModelClaudeSonnet4_5_20250929,
        MaxTokens: 1024,
        System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
        Messages:  anthropicMessages,
    })

    for stream.Next() {
        event := stream.Current()
        // Process TextDelta events:
        // - Accumulate content
        // - Render markdown incrementally
        // - Patch assistant message inner HTML
        // - Auto-scroll after every delta
    }

    // 11. Final render
    finalHTML := mdRenderer.MarkdownBytesToHTML([]byte(displayContent))
    sse.PatchElementTempl(MayorMessageComplete(assistantMsgID, finalHTML))
    sse.ExecuteScript(scrollChatJS)

    return nil
}
```

### Image Handling for Anthropic Vision API

```go
// pkg/mayorchat/stream.go
func BuildAnthropicMessages(messages []Message) []anthropic.MessageParam {
    var params []anthropic.MessageParam
    for _, msg := range messages {
        var blocks []anthropic.ContentBlockParamUnion
        if msg.Role == "user" && len(msg.Images) > 0 {
            for _, img := range msg.Images {
                data, err := os.ReadFile(img.FilePath)
                if err != nil { continue }
                blocks = append(blocks, anthropic.NewImageBlockBase64(
                    img.MIMEType, base64.StdEncoding.EncodeToString(data)))
            }
        }
        blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
        params = append(params, anthropic.MessageParam{
            Role:    msg.Role,
            Content: blocks,
        })
    }
    return params
}
```

### Image Upload Server-Side

```go
// Constraints
const maxImageSize  = 5 << 20 // 5 MB
const maxImageCount = 4

// Validation: size, MIME type (JPEG, PNG, GIF, WebP)
// Storage: {dataDir}/chat-uploads/{userID}/{uuid}{ext}
// Serving: GET /mayor/image/:imageID
```

---

## Datastar Integration

### Signals

```json
{"mayor_input": "", "world_creating": false}
```

- `$mayor_input` -- two-way bound to textarea via `data-bind:mayor_input`
- `$_sending` -- auto-managed by `data-indicator:_sending` (true during SSE request)

### Form Submission

```html
<!-- On Enter (desktop only) -->
data-on:keydown="evt.key === 'Enter' && !evt.shiftKey && navigator.maxTouchPoints === 0 &&
    (evt.preventDefault(), !$_sending && $mayor_input.trim() !== '' &&
    @post('/mayor/chat', {contentType: 'form'}))"

<!-- On click (all devices) -->
data-on:click="!$_sending && $mayor_input.trim() !== '' &&
    @post('/mayor/chat', {contentType: 'form'})"
```

`@post('/mayor/chat', {contentType: 'form'})` serializes the enclosing `<form>` as `multipart/form-data` and opens an SSE stream.

### SSE Commands (Server -> Client)

| Method | Purpose |
|--------|---------|
| `sse.PatchElementTempl(component, WithModeAppend(), WithSelectorID("chat-messages"))` | Append message to chat |
| `sse.PatchElementTempl(component, WithModeInner(), WithSelectorID("msg-content-ID"))` | Update streaming content |
| `sse.MarshalAndPatchSignals(map[string]any{"mayor_input": ""})` | Clear input field |
| `sse.ExecuteScript("document.getElementById('chat-form').reset()")` | Reset form (clear file input) |
| `sse.ExecuteScript(scrollChatJS)` | Scroll chat to bottom |

### Datastar CDN

```html
<script type="module" src="https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.0-RC.7/bundles/datastar.js"></script>
```

---

## SQLite Database Setup

The site uses **hand-written SQL** with Go's `database/sql` -- no ORM, no sqlc. The schema is created on startup via `CREATE TABLE IF NOT EXISTS` statements.

### Database Initialization (`internal/db/db.go`)

```go
import _ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO)

func New(dbPath string) (*sql.DB, error) {
    os.MkdirAll(filepath.Dir(dbPath), 0o755)
    db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
    db.SetMaxOpenConns(4)
    db.SetMaxIdleConns(4)
    createTables(db)
    return db, nil
}
```

Key DSN pragmas:
- `journal_mode(WAL)` -- Write-Ahead Logging for concurrent reads during writes
- `busy_timeout(5000)` -- Wait up to 5s for locks instead of failing immediately
- `foreign_keys(on)` -- Enable FK constraints

### Schema (chat-relevant tables only)

```sql
-- Conversation messages (one row per chat message)
CREATE TABLE IF NOT EXISTS conversation_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    discord_id TEXT NOT NULL,       -- user identifier (replace with your user ID)
    role TEXT NOT NULL,             -- "user" or "assistant"
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_conv_messages_discord_id ON conversation_messages(discord_id);

-- Image attachments (linked to messages by user ID + message index)
CREATE TABLE IF NOT EXISTS conversation_images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    discord_id TEXT NOT NULL,       -- user identifier
    message_index INTEGER NOT NULL, -- position in conversation (0-based)
    image_id TEXT NOT NULL,         -- UUID for serving via /image/:imageID
    file_path TEXT NOT NULL,        -- disk path to saved file
    mime_type TEXT NOT NULL,
    original_filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_conv_images_discord_id ON conversation_images(discord_id);
CREATE INDEX IF NOT EXISTS idx_conv_images_image_id ON conversation_images(image_id);

-- Sessions (replace with your auth system)
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    discord_id TEXT NOT NULL,
    discord_username TEXT NOT NULL,
    discord_avatar TEXT NOT NULL DEFAULT '',
    guild_member_verified INTEGER NOT NULL DEFAULT 0,
    invite_code_verified INTEGER NOT NULL DEFAULT 0,
    system_prompt TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_discord_id ON sessions(discord_id);
```

### Message Store (`internal/mayor/store.go`)

The store implements two interfaces from `pkg/mayorchat/`:

```go
type SQLiteMessageStore struct {
    db *sql.DB
}

// Core CRUD operations:
func (s *SQLiteMessageStore) AddMessage(userID, role, content string) error
func (s *SQLiteMessageStore) GetMessages(userID string) ([]mayorchat.Message, error)
func (s *SQLiteMessageStore) DeleteUserMessages(userID string) error
func (s *SQLiteMessageStore) DeleteOlderThan(d time.Duration) error

// Image operations:
func (s *SQLiteMessageStore) AddImage(discordID string, messageIndex int, imageID, filePath, mimeType, filename string) error
func (s *SQLiteMessageStore) GetImages(discordID string) ([]mayorchat.ImageRecord, error)
func (s *SQLiteMessageStore) GetImageByID(imageID string) (*mayorchat.ImageRecord, error)
func (s *SQLiteMessageStore) DeleteImages(discordID string) error
func (s *SQLiteMessageStore) DeleteImagesOlderThan(d time.Duration) ([]string, error)
```

All queries are plain `db.Exec` / `db.Query` / `db.QueryRow` -- no query builder or ORM. Messages are ordered by autoincrement `id` (insertion order). Images are linked to messages by `(discord_id, message_index)` pair.

### Conversation Manager (`pkg/mayorchat/conversation.go`)

The `ConversationManager` wraps the store with in-memory transient state:

```go
type ConversationManager struct {
    store      MessageStore  // SQLite-backed persistence
    imageStore ImageStore    // SQLite-backed image persistence
    mu         sync.RWMutex
    transient  map[string]*transientState // keyed by user ID
}
```

**Persisted** (survives restart): messages and images in SQLite.
**Transient** (in-memory, lost on restart): rate limit timestamps, scripted mode flag, world-ready state, hatched flag, cover art path. Cleaned up hourly for entries older than 24 hours.

### Cleanup

An hourly goroutine deletes messages and images older than 24 hours:

```go
func (cm *ConversationManager) cleanupLoop(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Hour)
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            cm.store.DeleteOlderThan(24 * time.Hour)
            paths, _ := cm.imageStore.DeleteImagesOlderThan(24 * time.Hour)
            for _, p := range paths {
                os.Remove(p) // clean up files on disk
            }
        }
    }
}
```

---

## What to Copy vs What to Adapt

### Copy Exactly (don't reinvent)
- `layouts/chat.templ` -- the fixed-viewport layout pattern
- `layouts/head.templ` -- viewport meta with `interactive-widget=resizes-content`
- `pages/mayor.templ` -- all JavaScript blocks (scroll, auto-resize, image handling, drag-drop)
- `pages/mayor_fragments.templ` -- message bubble HTML structure
- `static/css/chat.css` -- chat message content typography
- `static/css/theme.css` -- design tokens and scrollbar styling
- Mobile touch handling patterns (`touch-pan-y`, `overscroll-y-contain`, etc.)
- Server-driven scroll pattern (`sse.ExecuteScript(scrollChatJS)` after every DOM mutation)

### Adapt for Your App
- **Authentication**: Replace Discord OAuth with your auth system
- **System prompt**: Replace mayor personality with your onboarding agent prompt
- **World hatching**: Replace with your onboarding completion action
- **Conversation persistence**: Keep SQLite pattern or swap for your preferred DB
- **File types**: Currently limited to images (JPEG, PNG, GIF, WebP) -- expand to other file types if needed for your onboarding
- **Rate limiting**: 2-second cooldown is hardcoded -- adjust as needed
- **Avatar**: Replace mayor avatar and user Discord avatar with your app's avatars
- **Scripted fallback**: Optional -- the fallback for API failures is nice to have

### Can Remove Entirely
- Cover art generation (Gemini integration)
- Discord channel creation / world hatching webhook
- `world_creating` signal and Create World dialog
- Invite code middleware
- Guild member verification
- All `worldchannel` and `imagegen` package references

---

## File Attachment: Extending Beyond Images

The current implementation only supports images. To add file attachments:

1. **Client**: Change `accept` attribute on the file input and update the MIME type validation
2. **Server**: Update `handler.go` MIME type switch and storage logic
3. **Anthropic API**: For non-image files, you'll need to use the [Document support](https://docs.anthropic.com/en/docs/build-with-claude/pdf-support) or pass file content as text. Images use `ImageBlockBase64`, PDFs use `DocumentBlockParam`, other files would need to be read and sent as text content.
4. **Display**: Add file preview/download UI in the message fragments

---

## Quick Start Checklist

1. Clone the repo and study `site/CLAUDE.md`
2. Copy the layout files (`chat.templ`, `head.templ`, `args.go`)
3. Copy the chat page (`mayor.templ`, `mayor_fragments.templ`, `types.go`)
4. Copy the CSS files (`chat.css`, `theme.css`)
5. Copy the handler pattern (`internal/mayor/handler.go`) -- strip out world-hatching code
6. Copy `pkg/mayorchat/` for conversation management and Anthropic message building
7. Set `ANTHROPIC_API_KEY` env var
8. Write your system prompt
9. Test on mobile -- the whole point of copying this code is the mobile experience
