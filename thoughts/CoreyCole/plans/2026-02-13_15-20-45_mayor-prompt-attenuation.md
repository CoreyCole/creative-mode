# Mayor Prompt Attenuation + Memory Inspector — Implementation Plan

## Overview

Add two features that depend on the OpenClaw mayor infrastructure being deployed:
1. **Prompt attenuation** — "Suggest" button on the Assets tab sends the user's prompt to the OpenClaw mayor agent for enhancement. The mayor uses its full context (personality, memory, conversation history) to embellish the prompt. The suggestion appears in an editable preview.
2. **Memory inspector** — new "Mayor" tab in the chat panel for browsing and editing the mayor's workspace files (all bootstrap files editable, skills read-only).

## Current State Analysis

### Image Generation Pipeline
- User types prompt → `POST /api/images/generate` → `GeminiClient.Generate()` → SSE patches result
- Prompt sent verbatim except for chromakey suffix (`harness/internal/gemini/gemini.go:112-115`)
- Fragments share `id="image-gen-content"` for state machine: Idle → Generating → Done → Saved/Error
- Input bar has single "Generate" button (`harness/views/imagegen/imagegen.templ:129-145`)
- Signals: `image_prompt`, `image_aspect_ratio`, `image_transparent_bg` in `OverlaySignals` (`harness/views/world/signals.go:17-19`)

### Mayor Plan (Not Yet Implemented)
- Each world gets an OpenClaw agent at `/data/openclaw/workspaces/{worldID}/`
- Workspace files: SOUL.md (personality), MEMORY.md (world knowledge, self-evolving), AGENTS.md (workflow), IDENTITY.md, USER.md, skills/
- Skills use curl to call harness APIs with `X-Mayor-Secret` auth header
- `POST /hooks/agent` (OpenClaw HTTP hooks API) sends messages to agents — fire-and-forget, no response body
- Mayor communicates with harness via skills that POST back (callback pattern)
- Full mayor plan: `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md`

### Key Discoveries
- OpenClaw hooks API is fire-and-forget — no response body. To get a response, the mayor uses a skill that POSTs back to the harness (same callback pattern as the `world-build` skill in the mayor plan, lines 1062-1122)
- The `world-build` skill template shows the exact pattern: mayor receives a message, processes it, then calls `curl POST /api/mayor/build` with `X-Mayor-Secret` auth
- Chat panel tabs (`harness/views/chat/chat.templ:11-24`): Global, World, Lineage, Assets — adding Mayor makes 5
- File serving security pattern (`harness/internal/server/server.go:523-556`): `filepath.Clean` + `strings.HasPrefix` path traversal check
- SSE handler pattern (`harness/internal/server/imagegen.go:32-89`): ReadSignals before NewSSE, patch spinner, blocking call, patch result

## Desired End State

1. User types "a dragon" in image prompt input, clicks **Suggest**
2. Harness sends the prompt to the mayor via OpenClaw hooks API
3. Mayor composes an enhanced prompt using its personality and world knowledge
4. Mayor calls the `prompt-enhance` skill, which POSTs the enhanced prompt back to the harness
5. Harness patches the UI with an editable suggestion preview
6. User accepts/modifies/dismisses the suggestion, then clicks Generate
7. In a separate Mayor tab, users can browse and edit SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md, and view skill files read-only

### Verification
- Create a world with a mayor provisioned → workspace directory exists with files
- Type a prompt, click Suggest → spinner shows, mayor enhances, suggestion appears
- Accept suggestion → prompt input populated with enhanced text
- Generate with suggestion → image generated using the enhanced prompt
- Mayor tab → file list loads → click SOUL.md → editor opens → edit + save → file updated on disk
- Skills shown as read-only (no save button)

## What We're NOT Doing

- **Style reference images** — Gemini 3 Pro Image reference images are a future enhancement
- **World-level style prefix in DB** — The mayor's MEMORY.md IS the style knowledge store
- **Direct Gemini text fallback** — No bypass; always route through the mayor
- **Mayor-to-mayor communication** — Worlds are independent
- **Chat with mayor from UI** — This plan covers prompt enhancement only; full chat is in the Discord plan
- **Implementing the mayor infrastructure itself** — This plan assumes Phase 1-3 of the mayor plan are complete

## Implementation Approach

The core challenge is getting a synchronous response from the mayor when OpenClaw's hooks API is fire-and-forget. We solve this with the **async callback pattern**: the harness holds an SSE connection open, sends a message to the mayor via hooks, and blocks on a Go channel. The mayor composes the enhanced prompt and calls back to the harness via a new `prompt-enhance` skill, which resolves the channel and unblocks the SSE handler.

---

## Phase 1: Prompt Attenuation (Mayor Callback Pattern)

### Overview
Add a "Suggest" button to the Assets input bar. When clicked, the harness sends the prompt to the OpenClaw mayor via the hooks API. The mayor enhances the prompt and calls back via a new `prompt-enhance` skill. The harness patches the suggestion into an editable preview.

### Architecture: Async Callback Pattern

```
User clicks [Suggest]
  → POST /api/images/suggest (SSE connection stays open)
  → Handler generates requestID, stores channel in pendingMap
  → Handler patches spinner fragment immediately
  → Handler calls POST /hooks/agent with enhancement request
  → Handler blocks on channel (30s timeout)

Mayor receives message via OpenClaw
  → Composes enhanced prompt using SOUL.md + MEMORY.md context
  → Uses prompt-enhance skill:
    curl POST /api/mayor/suggest-callback
      -H "X-Mayor-Secret: {secret}"
      -d '{"request_id": "{id}", "enhanced_prompt": "..."}'

Harness callback endpoint
  → Validates mayor secret
  → Looks up channel in pendingMap by requestID
  → Sends enhanced prompt on channel

Original handler unblocks
  → Patches ImageGenSuggested fragment with enhanced prompt
  → Patches suggested_prompt signal
```

### Changes Required

#### 1. New signal
**File**: `harness/views/world/signals.go`

Add to `OverlaySignals`:
```go
SuggestedPrompt string `json:"suggested_prompt"` //nolint:tagliatelle // Datastar signal names use snake_case
```

#### 2. New templ fragments
**File**: `harness/views/imagegen/imagegen.templ`

Add two new fragments (sharing `id="image-gen-content"`):

```go
// ImageGenSuggesting — spinner while mayor is thinking
templ ImageGenSuggesting() {
    <div id="image-gen-content" class="flex flex-col items-center gap-2 py-8">
        <div class="w-6 h-6 border-2 border-amber-500 border-t-transparent rounded-full animate-spin"></div>
        <span class="text-[12px] text-muted-foreground">Mayor is enhancing your prompt...</span>
    </div>
}

// ImageGenSuggested — editable suggestion preview with action buttons
templ ImageGenSuggested(rawPrompt string) {
    <div id="image-gen-content" class="flex flex-col gap-2 py-2">
        <span class="text-[11px] text-muted-foreground uppercase tracking-wider">Mayor's Suggestion</span>
        <textarea
            class="w-full min-h-[80px] rounded border border-amber-500/50 bg-amber-950/20 px-2 py-1.5 text-[12px] resize-y focus:outline-none focus:ring-1 focus:ring-amber-500"
            data-bind:suggested_prompt
            rows="4"
        ></textarea>
        <div class="text-[10px] text-muted-foreground/60">
            Original: { rawPrompt }
        </div>
        <div class="flex items-center gap-2">
            // Generate with Suggestion: copy to image_prompt, trigger generation
            <button data-on:click="$image_prompt = $suggested_prompt"
                    ...PostSSE("/api/images/generate")>
                Generate with Suggestion
            </button>
            // Accept: copy to image_prompt, return to idle
            <button data-on:click="$image_prompt = $suggested_prompt"
                    ...GetSSE("/api/images/idle")>
                Accept
            </button>
            // Dismiss: return to idle without changing prompt
            <button ...GetSSE("/api/images/idle")>
                Dismiss
            </button>
        </div>
    </div>
}
```

#### 3. Update input bar
**File**: `harness/views/imagegen/imagegen.templ` — `ImageGenInputBar()`

Add "Suggest" button (outline variant) between input and Generate:
```
[prompt input] [Suggest] [Generate]
```
- Suggest: `datastar.PostSSE("/api/images/suggest")`
- Both buttons get `data-indicator-fetching` + `data-attr-disabled="$fetching"`

#### 4. Pending suggestions map
**File**: `harness/internal/server/suggest.go` (new)

Thread-safe map of pending enhancement requests with Go channels:
```go
type pendingSuggestion struct {
    ch      chan string
    created time.Time
}

type SuggestionTracker struct {
    mu      sync.Mutex
    pending map[string]*pendingSuggestion
}

func NewSuggestionTracker() *SuggestionTracker
func (t *SuggestionTracker) Create(requestID string) <-chan string
func (t *SuggestionTracker) Resolve(requestID, enhancedPrompt string) bool
func (t *SuggestionTracker) Cleanup(maxAge time.Duration) // evict stale entries
```

Add `SuggestionTracker` as a field on the `Server` struct, initialized in `main.go`.

#### 5. Suggest handler
**File**: `harness/internal/server/imagegen.go`

New `handleImageSuggest` handler (follows existing `handleImageGenerate` pattern):
1. Check `s.OpenClawHome == ""` → error "Mayor not configured"
2. `ReadSignals` to get `image_prompt` + `current_world_id` (before `NewSSE`)
3. Look up world in DB to get `mayor_secret` (for validating callback)
4. `NewSSE`, patch `ImageGenSuggesting()` spinner
5. Generate requestID (uuid), create channel via `SuggestionTracker.Create()`
6. Send to mayor via `POST {openclawGatewayURL}/hooks/agent`:
   ```json
   {
     "agentId": "world-{worldID}",
     "message": "[ENHANCE PROMPT] request_id={id} | {rawPrompt}",
     "sessionKey": "suggest-{worldID}"
   }
   ```
7. Block on channel with 30s `context.WithTimeout`
8. On receive: patch `ImageGenSuggested(rawPrompt)` + signal `suggested_prompt = enhancedPrompt`
9. On timeout: patch `ImageGenError("Mayor didn't respond in time")`

New `handleImageIdle` handler (GET SSE):
- Patches `ImageGenIdle()` back — used by Accept/Dismiss buttons to return to idle state

#### 6. Callback handler
**File**: `harness/internal/server/imagegen.go`

New `handleSuggestCallback` handler (`POST /api/mayor/suggest-callback`):
1. Validate `X-Mayor-Secret` header against the world's stored secret
2. Read JSON body: `{request_id, world_id, enhanced_prompt}`
3. Call `SuggestionTracker.Resolve(requestID, enhancedPrompt)`
4. Return 200 OK (or 404 if requestID not found / expired)

This is NOT an SSE handler — it's a regular JSON endpoint called by the mayor's skill.

#### 7. New mayor skill: `prompt-enhance`
**File**: `harness/internal/mayor/skills.go` — add to the existing skill templates

Written to `{workspace}/skills/prompt-enhance/SKILL.md` during `ProvisionAgent`:

```markdown
---
name: prompt-enhance
description: Enhance an image generation prompt and return it to the harness
---

# Prompt Enhance

When you receive a message with the [ENHANCE PROMPT] prefix, compose an
improved image generation prompt and send it back to the harness.

## Input Format
[ENHANCE PROMPT] request_id=abc123 | a dragon sitting on a throne

## Your Task
Using your knowledge of this world and your aesthetic sensibilities:
- Add style details consistent with the world's visual identity
- Include specific colors, textures, lighting, and composition details
- Reference existing world elements when relevant
- Keep the user's core intent — embellish, don't replace
- Keep the enhanced prompt under 200 words

## Callback
curl -X POST {{.HarnessURL}}/api/mayor/suggest-callback \
  -H "Content-Type: application/json" \
  -H "X-Mayor-Secret: {{.MayorSecret}}" \
  -d '{"request_id": "<from input>", "world_id": "{{.WorldID}}", "enhanced_prompt": "<your version>"}'
```

#### 8. Update AGENTS.md template
**File**: `harness/internal/mayor/agents.go`

Add workflow section for prompt enhancement:
```markdown
## Workflow: When you receive an [ENHANCE PROMPT] message
- Extract the request_id and raw prompt from the message
- Compose an enhanced prompt using your world knowledge and personality
- Use the prompt-enhance skill to send it back
- Do NOT ask clarifying questions — enhance immediately and return
```

#### 9. Route registration
**File**: `harness/internal/server/server.go`

```go
approved.POST("/api/images/suggest", s.handleImageSuggest)
approved.GET("/api/images/idle", s.handleImageIdle)

// Mayor callback — NOT in approved group (uses X-Mayor-Secret auth, not session)
e.POST("/api/mayor/suggest-callback", s.handleSuggestCallback)
```

#### 10. Server struct additions
**File**: `harness/internal/server/server.go`

```go
OpenClawHome       string               // empty = mayor features disabled
OpenClawGatewayURL string               // e.g., "http://localhost:18789"
Suggestions        *SuggestionTracker
```

**File**: `harness/main.go`
- Read `OPENCLAW_HOME` and `OPENCLAW_GATEWAY_URL` env vars
- Initialize `SuggestionTracker`
- Pass to server struct

### Success Criteria

#### Automated Verification:
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`
- [ ] Suggest button appears in Assets input bar
- [ ] Suggest handler returns error fragment when `OpenClawHome` is empty
- [ ] Callback handler validates mayor secret, rejects invalid tokens
- [ ] SuggestionTracker handles timeout correctly (channel cleaned up after 30s)

#### Manual Verification:
- [ ] With mock OpenClaw: click Suggest → spinner → callback delivers result → suggestion preview appears
- [ ] Accept copies suggestion to prompt input, returns to idle
- [ ] Dismiss returns to idle without changing prompt
- [ ] Generate with Suggestion triggers image generation with the enhanced prompt
- [ ] Timeout: if mayor never responds, error message appears after 30s

---

## Phase 2: Mayor Memory Inspector

### Overview
Add a "Mayor" tab to the chat panel. Lists the mayor's workspace files. Clicking a file opens it in a textarea editor. Users can edit and save bootstrap files (SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md). Skill files are shown read-only.

### Changes Required

#### 1. New view package
**File**: `harness/views/mayor/mayor.templ` (new)

Components (all use SSE fragment replacement pattern):

- `MayorPanel()` — outer container with `data-show="$active_tab === 'mayor'"`. Contains `#mayor-files-list` and `#mayor-file-editor` divs.
- `MayorFileList(files []MayorFileInfo)` — clickable list of workspace files, targets `#mayor-files-list`. Each entry triggers `@get('/api/mayor/file?name=FILENAME')`.
- `MayorFileEditor(filename string, editable bool)` — textarea editor with `data-bind:mayor_file_content`. Has Save button (editable files only) and Back button. Save triggers `@put('/api/mayor/file?name=FILENAME')`.
- `MayorFileSaved()` — green confirmation targeting `#mayor-save-status`
- `MayorNotProvisioned()` — placeholder when workspace dir doesn't exist

**File**: `harness/views/mayor/types.go` (new)
```go
type MayorFileInfo struct {
    Name     string // "SOUL.md" or "skills/prompt-enhance/SKILL.md"
    Size     string // "2.1 KB"
    Exists   bool   // false for MEMORY.md before first run
    Editable bool   // false for skill files
}
```

#### 2. New signal
**File**: `harness/views/world/signals.go`

```go
MayorFileContent string `json:"mayor_file_content"` //nolint:tagliatelle
```

#### 3. Add Mayor tab to chat panel
**File**: `harness/views/chat/chat.templ`

- Add "Mayor" tab button after "Assets" (line 22-23)
- Add `@mayor.MayorPanel()` after `@imagegen.ImageGenPanel()` (after line 27)
- Update `data-show` conditions on input bar divs (lines 28-33) to hide when mayor tab active

#### 4. Server handlers
**File**: `harness/internal/server/mayor.go` (new)

**Security**: File allowlist + path traversal check (reusing pattern from `handleSharedAssets` at `server.go:523-556`).

```go
var editableFiles = map[string]bool{
    "SOUL.md": true, "MEMORY.md": true, "AGENTS.md": true,
    "IDENTITY.md": true, "USER.md": true,
}

var readOnlyFiles = map[string]bool{
    "skills/world-build/SKILL.md":     true,
    "skills/world-status/SKILL.md":    true,
    "skills/prompt-enhance/SKILL.md":  true,
}
```

**`handleMayorFiles`** — `GET /api/mayor/files` (SSE):
- Read `current_world_id` from signals
- Construct workspace path: `{OpenClawHome}/workspaces/{worldID}/`
- If dir doesn't exist: patch `MayorNotProvisioned()`
- List allowed files (editable + read-only), collect sizes
- Patch `MayorFileList(files)`

**`handleMayorFileRead`** — `GET /api/mayor/file?name=SOUL.md` (SSE):
- Validate filename in allowlist (editable OR read-only)
- Path traversal check: `filepath.Clean` + `strings.HasPrefix`
- Read file content (empty string if file doesn't exist yet, e.g., MEMORY.md)
- Determine if editable: `editableFiles[filename]`
- Patch `MayorFileEditor(filename, editable)` + signal `mayor_file_content = content`

**`handleMayorFileSave`** — `PUT /api/mayor/file?name=SOUL.md` (SSE):
- `ReadSignals` to get `mayor_file_content` + `current_world_id` (before `NewSSE`)
- Validate filename in `editableFiles` only (reject read-only files)
- Path traversal check
- Write atomically: write to `.tmp`, `os.Rename` to final path
- Patch `MayorFileSaved()`

#### 5. Route registration
**File**: `harness/internal/server/server.go`

```go
approved.GET("/api/mayor/files", s.handleMayorFiles)
approved.GET("/api/mayor/file", s.handleMayorFileRead)
approved.PUT("/api/mayor/file", s.handleMayorFileSave)
```

### Success Criteria

#### Automated Verification:
- [ ] `cd /Users/coreycole/cdev/creative-mode/harness && just generate && go build ./... && just lint`
- [ ] Mayor tab appears in chat panel
- [ ] File list loads from workspace directory
- [ ] Path traversal check rejects `../../etc/passwd`
- [ ] Read-only files cannot be saved (handler rejects PUT for skill files)

#### Manual Verification:
- [ ] Click Mayor tab → file list shows SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md + skill files
- [ ] MEMORY.md shows as not yet created if it doesn't exist, opens empty editor
- [ ] Edit SOUL.md → Save → verify file updated on disk
- [ ] Skill files open with disabled textarea and "read-only" label, no Save button
- [ ] Back button returns to file list
- [ ] When no workspace exists: "Mayor not yet provisioned" message

---

## Implementation Sequence

1. **Signals** — Add `SuggestedPrompt`, `MayorFileContent` to `OverlaySignals`. Run `just generate`.
2. **SuggestionTracker** — Thread-safe pending map with channels (`suggest.go`).
3. **Suggest UI** — `ImageGenSuggesting`, `ImageGenSuggested` fragments + update `ImageGenInputBar` with Suggest button + `ImageGenIdle` handler.
4. **Suggest handler + callback** — `handleImageSuggest` (hooks API call + channel wait) + `handleSuggestCallback` (mayor secret auth + channel resolve).
5. **Prompt-enhance skill** — Add to mayor skill templates in `skills.go`, update `AGENTS.md` template.
6. **Mayor tab UI** — Create `views/mayor/` package with templ components.
7. **File handlers** — `handleMayorFiles`, `handleMayorFileRead`, `handleMayorFileSave` with allowlist security.
8. **Wire routes + main.go** — Register all routes, read env vars, init tracker.
9. **Generate + build + lint** — `just generate && go build ./... && just lint`

---

## File Inventory

### New files
| File | Purpose |
|------|---------|
| `harness/internal/server/suggest.go` | SuggestionTracker (pending map + channels) |
| `harness/internal/server/mayor.go` | File list/read/save handlers + allowlist security |
| `harness/views/mayor/mayor.templ` | Mayor tab UI components |
| `harness/views/mayor/types.go` | MayorFileInfo struct |

### Modified files
| File | Changes |
|------|---------|
| `harness/views/world/signals.go` | Add `SuggestedPrompt`, `MayorFileContent` |
| `harness/views/imagegen/imagegen.templ` | Add `ImageGenSuggesting`, `ImageGenSuggested` fragments; add Suggest button to input bar |
| `harness/views/chat/chat.templ` | Add Mayor tab button + `@mayor.MayorPanel()`; update input bar visibility conditions |
| `harness/internal/server/imagegen.go` | Add `handleImageSuggest`, `handleImageIdle`, `handleSuggestCallback` |
| `harness/internal/server/server.go` | Add `OpenClawHome`, `OpenClawGatewayURL`, `Suggestions` to Server; register routes |
| `harness/main.go` | Read env vars, init SuggestionTracker |
| `harness/internal/mayor/skills.go` | Add `prompt-enhance` skill template (modifies planned file, not yet existing) |
| `harness/internal/mayor/agents.go` | Add `[ENHANCE PROMPT]` workflow section to AGENTS.md template (modifies planned file) |

## Performance Considerations

- **Timeout handling**: The suggest handler holds an SSE connection for up to 30s while waiting for the mayor callback. Each request uses one goroutine. With the expected usage pattern (one suggestion at a time per user), this is not a concern.
- **Channel cleanup**: `SuggestionTracker.Cleanup()` should be called periodically (or on create) to evict stale entries where the mayor never responded and the handler already timed out.
- **Concurrent suggestions**: The `SuggestionTracker` supports multiple concurrent suggestions (different requestIDs). No need to limit to one per world.

## References

- Mayor plan: `thoughts/CoreyCole/plans/2026-02-13_10-20-05_openclaw-world-mayors.md`
- Research: `thoughts/CoreyCole/research/2026-02-13_14-46-53_mayor-prompt-attenuation.md`
- Image gen handler: `harness/internal/server/imagegen.go:32-89`
- Gemini client: `harness/internal/gemini/gemini.go:95-175`
- Chat tab system: `harness/views/chat/chat.templ:11-24`
- File serving security: `harness/internal/server/server.go:523-556`
- Mayor skill pattern: Mayor plan lines 1062-1122 (world-build skill template)
