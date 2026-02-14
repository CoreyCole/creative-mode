# "Meet the Mayor" Conversational Page — Implementation Plan

## Overview

Add a `/mayor` page to the marketing site (`site/`) where visitors have an open-ended conversation with an AI mayor (Claude) about the game world they want to build. The mayor gathers personality and world info through natural dialogue, then "hatches" a personalized world summary. Discord OAuth + invite codes prevent spam.

## Current State Analysis

The marketing site is a minimal Go + Echo + templ + Datastar + Tailwind app with a single homepage and no backend logic beyond static file serving.

- **Server**: `site/main.go` — Echo on port 3000, one route (`GET /`), static file serving
- **Layout**: `site/layouts/root.templ` — Header with theme toggle, "Meet the Mayor" CTA (links to GitHub), footer
- **Homepage**: `site/pages/home.templ` — Hero, 6 feature cards, bottom CTA. All "Meet the Mayor" buttons link to GitHub
- **Args**: `site/layouts/args.go` — `RootArgs { Title, CurrentPath }`
- **Styling**: Tailwind v4 with shadcn/ui CSS variables in `site/static/css/theme.css`
- **No auth, no API integrations, no SSE endpoints**

### Key Discoveries

- All three "Meet the Mayor" buttons point to `https://github.com/coreycole/creative-mode` (`root.templ:94`, `home.templ:23`, `home.templ:139`)
- Datastar is already loaded from CDN (`root.templ:38`) but only used for theme toggle
- The harness has a complete auth system (`harness/internal/auth/auth.go`) with GitHub OAuth, session cookies, and middleware — we can model the Discord OAuth after this
- `context/cn-agents/` has proven patterns for Claude streaming + Datastar SSE + server-side markdown rendering

## Desired End State

A visitor clicks "Meet the Mayor" → Discord OAuth → enters invite code → chats with an AI mayor about their dream game world → mayor gathers enough info → "hatches" a world summary card showing the user's Discord identity and a description of their personalized world.

### Verification

1. Click "Meet the Mayor" → redirected to Discord OAuth
2. After OAuth → lands on invite code page
3. Enter valid invite code → lands on chat page with initial mayor greeting
4. Send messages → see user bubble, typing indicator, streamed mayor response with markdown
5. After 4-6 exchanges → world summary card appears with Discord avatar/username
6. Invalid invite code → error message, can retry
7. Rapid messages → rate limit error
8. Works on mobile viewport
9. Dark/light theme works on all new components
10. No `ANTHROPIC_API_KEY` → "coming soon" fallback page

## What We're NOT Doing

- Actually creating worlds on the harness (marketing-only capture)
- Persistent conversation storage in a database (in-memory sessions)
- Multi-tab session sync
- Image generation during the conversation
- Token-by-token streaming in the initial version (send full response, stream as enhancement)
- Discord bot integration (only OAuth)

## Implementation Approach

Follow the existing patterns from `context/cn-agents/` for Claude streaming and from `harness/internal/auth/` for OAuth. Keep everything in-memory (no database). The site remains a single Go binary with no external dependencies besides the Anthropic and Discord APIs.

---

## Phase 1: Dependencies & Project Structure

### Overview
Add Go dependencies and create the internal package structure.

### Changes Required

#### 1. Add dependencies
**File**: `site/go.mod`

```bash
cd site && go get github.com/anthropics/anthropic-sdk-go github.com/starfederation/datastar-go github.com/gomarkdown/markdown github.com/alecthomas/chroma/v2 golang.org/x/oauth2
```

#### 2. Create package directories
```
site/internal/auth/       # Discord OAuth + sessions + middleware
site/internal/mayor/      # Claude client + conversation state + system prompt
site/internal/markdown/   # Markdown renderer (adapted from cn-agents)
```

### Success Criteria

#### Automated Verification
- [ ] `cd site && go mod tidy` succeeds
- [ ] All new packages resolve

---

## Phase 2: Discord OAuth + Invite Code Auth

### Overview
Implement Discord OAuth login, in-memory session management, invite code verification, and auth middleware.

### Changes Required

#### 1. Discord OAuth + Session Manager
**File**: `site/internal/auth/auth.go`

Model after `harness/internal/auth/auth.go` (lines 60-174). Key differences:
- Discord OAuth instead of GitHub (different endpoints, scopes)
- In-memory session store instead of SQLite
- No role system (just authenticated + invite-verified)

```go
package auth

import (
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    "time"

    "github.com/labstack/echo/v4"
)

const (
    sessionTTL       = 24 * time.Hour
    sessionCookieName = "session"
    oauthStateCookie  = "oauth_state"
)

// Session represents an authenticated user's session
type Session struct {
    ID                 string
    DiscordID          string
    DiscordUsername     string
    DiscordAvatar      string
    InviteCodeVerified bool
    CreatedAt          time.Time
    LastActivity       time.Time
}

// SessionManager manages in-memory sessions
type SessionManager struct {
    sessions map[string]*Session // sessionID -> session
    mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
    sm := &SessionManager{
        sessions: make(map[string]*Session),
    }
    go sm.cleanupLoop()
    return sm
}

// OAuthConfig holds Discord OAuth configuration
type OAuthConfig struct {
    ClientID     string
    ClientSecret string
    RedirectURI  string
}

// HandleLogin redirects to Discord OAuth
func HandleLogin(cfg OAuthConfig) echo.HandlerFunc {
    return func(c echo.Context) error {
        state := generateRandomString(16)
        c.SetCookie(&http.Cookie{
            Name:     oauthStateCookie,
            Value:    state,
            MaxAge:   300, // 5 minutes
            HttpOnly: true,
            SameSite: http.SameSiteLaxMode,
            Secure:   isSecure(c),
        })
        url := fmt.Sprintf(
            "https://discord.com/oauth2/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=identify&state=%s",
            cfg.ClientID, cfg.RedirectURI, state,
        )
        return c.Redirect(http.StatusFound, url)
    }
}

// HandleCallback exchanges the OAuth code for user info and creates a session
func (sm *SessionManager) HandleCallback(cfg OAuthConfig) echo.HandlerFunc {
    return func(c echo.Context) error {
        // Validate state
        stateCookie, err := c.Cookie(oauthStateCookie)
        if err != nil || stateCookie.Value != c.QueryParam("state") {
            return echo.NewHTTPError(http.StatusBadRequest, "invalid state")
        }

        // Exchange code for token
        code := c.QueryParam("code")
        token, err := exchangeCode(cfg, code)
        if err != nil {
            return echo.NewHTTPError(http.StatusInternalServerError, "oauth exchange failed")
        }

        // Fetch Discord user info
        user, err := fetchDiscordUser(token)
        if err != nil {
            return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch user")
        }

        // Create session
        sessionID := generateRandomString(32)
        sm.mu.Lock()
        sm.sessions[sessionID] = &Session{
            ID:             sessionID,
            DiscordID:      user.ID,
            DiscordUsername: user.Username,
            DiscordAvatar:  user.AvatarURL(),
            CreatedAt:      time.Now(),
            LastActivity:   time.Now(),
        }
        sm.mu.Unlock()

        c.SetCookie(&http.Cookie{
            Name:     sessionCookieName,
            Value:    sessionID,
            MaxAge:   int(sessionTTL.Seconds()),
            HttpOnly: true,
            SameSite: http.SameSiteLaxMode,
            Secure:   isSecure(c),
            Path:     "/",
        })

        return c.Redirect(http.StatusFound, "/mayor")
    }
}

// Helper: exchange OAuth code for access token via Discord API
func exchangeCode(cfg OAuthConfig, code string) (string, error) {
    // POST https://discord.com/api/oauth2/token
    // Form: client_id, client_secret, grant_type=authorization_code, code, redirect_uri
    // Returns: { "access_token": "..." }
    // ...
}

// Helper: fetch user info from Discord API
type DiscordUser struct {
    ID            string `json:"id"`
    Username      string `json:"username"`
    Discriminator string `json:"discriminator"`
    Avatar        string `json:"avatar"`
}

func (u DiscordUser) AvatarURL() string {
    if u.Avatar == "" {
        return ""
    }
    return fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", u.ID, u.Avatar)
}

func fetchDiscordUser(token string) (*DiscordUser, error) {
    // GET https://discord.com/api/users/@me
    // Header: Authorization: Bearer <token>
    // ...
}

func generateRandomString(n int) string {
    b := make([]byte, n)
    rand.Read(b)
    return hex.EncodeToString(b)
}

func isSecure(c echo.Context) bool {
    return c.Request().Host != "localhost:3000" && !strings.HasPrefix(c.Request().Host, "localhost")
}

func (sm *SessionManager) cleanupLoop() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        sm.mu.Lock()
        for id, s := range sm.sessions {
            if time.Since(s.LastActivity) > sessionTTL {
                delete(sm.sessions, id)
            }
        }
        sm.mu.Unlock()
    }
}
```

#### 2. Auth Middleware
**File**: `site/internal/auth/middleware.go`

Model after `harness/internal/auth/middleware.go` (lines 17-73).

```go
package auth

import (
    "net/http"
    "github.com/labstack/echo/v4"
)

// SessionMiddleware validates the session cookie and sets the session in context
func (sm *SessionManager) SessionMiddleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            cookie, err := c.Cookie(sessionCookieName)
            if err != nil {
                return c.Redirect(http.StatusFound, "/auth/discord/login")
            }

            sm.mu.RLock()
            session, ok := sm.sessions[cookie.Value]
            sm.mu.RUnlock()

            if !ok {
                return c.Redirect(http.StatusFound, "/auth/discord/login")
            }

            // Update last activity
            sm.mu.Lock()
            session.LastActivity = time.Now()
            sm.mu.Unlock()

            c.Set("session", session)
            return next(c)
        }
    }
}

// InviteCodeMiddleware checks that the user has verified an invite code
func InviteCodeMiddleware() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            session := c.Get("session").(*Session)
            if !session.InviteCodeVerified {
                return c.Redirect(http.StatusFound, "/auth/invite")
            }
            return next(c)
        }
    }
}
```

#### 3. Invite Code Verification
**File**: `site/internal/auth/invite.go`

```go
package auth

import (
    "strings"
    "sync"
)

// InviteCodeManager validates invite codes
type InviteCodeManager struct {
    validCodes map[string]bool   // code -> valid
    usedBy     map[string]string // code -> discord user ID
    mu         sync.RWMutex
}

func NewInviteCodeManager(codesCSV string) *InviteCodeManager {
    codes := make(map[string]bool)
    for _, code := range strings.Split(codesCSV, ",") {
        code = strings.TrimSpace(code)
        if code != "" {
            codes[code] = true
        }
    }
    return &InviteCodeManager{
        validCodes: codes,
        usedBy:     make(map[string]string),
    }
}

// VerifyCode checks if a code is valid and not already used by another user
func (m *InviteCodeManager) VerifyCode(code, discordID string) bool {
    m.mu.Lock()
    defer m.mu.Unlock()

    if !m.validCodes[code] {
        return false
    }

    // Allow same user to re-use their own code
    if usedByID, used := m.usedBy[code]; used && usedByID != discordID {
        return false
    }

    m.usedBy[code] = discordID
    return true
}
```

#### 4. Invite Code Page Template
**File**: `site/pages/invite.templ`

```templ
package pages

import l "github.com/coreycole/creative-mode/site/layouts"

templ InvitePage(rootArgs l.RootArgs, errorMsg string) {
    @l.Root(rootArgs) {
        <section class="flex items-center justify-center min-h-[80vh]">
            <div class="w-full max-w-md px-4">
                <div class="rounded-lg border border-border/40 bg-card p-8 text-center">
                    <h1 class="text-2xl font-bold">Enter Your Invite Code</h1>
                    <p class="mt-2 text-sm text-muted-foreground">
                        Get your code from the
                        <a href="https://discord.gg/cPtN5vP3ty" target="_blank" rel="noreferrer"
                           class="underline underline-offset-4 hover:text-foreground">
                            Creative Mode Discord
                        </a>.
                    </p>
                    <form method="POST" action="/auth/verify-code" class="mt-6 space-y-4">
                        <input
                            type="text"
                            name="code"
                            placeholder="XXXX-XXXX"
                            required
                            autocomplete="off"
                            class="w-full h-11 rounded-md border border-input bg-background px-4 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                        />
                        if errorMsg != "" {
                            <p class="text-sm text-destructive">{ errorMsg }</p>
                        }
                        <button
                            type="submit"
                            class="w-full inline-flex items-center justify-center rounded-md text-sm font-medium bg-primary text-primary-foreground hover:bg-primary/90 h-11"
                        >
                            Continue
                        </button>
                    </form>
                </div>
            </div>
        </section>
    }
}
```

### Success Criteria

#### Automated Verification
- [ ] `just check` passes (from project root)
- [ ] `GET /auth/discord/login` redirects to Discord OAuth URL
- [ ] `GET /auth/discord/callback` with valid code creates session + cookie
- [ ] `GET /mayor` without session redirects to `/auth/discord/login`
- [ ] `GET /mayor` with session but no invite code redirects to `/auth/invite`
- [ ] `POST /auth/verify-code` with valid code sets `InviteCodeVerified` and redirects to `/mayor`

#### Manual Verification
- [ ] Full OAuth flow works end-to-end in browser
- [ ] Invite code page renders correctly on mobile
- [ ] Invalid/used codes show error message
- [ ] Session persists across page reloads (cookie-based)

---

## Phase 3: Markdown Renderer

### Overview
Port the markdown renderer from `context/cn-agents/server/services/markdown/renderer.go` for rendering Claude's streamed responses.

### Changes Required

#### 1. Markdown Renderer
**File**: `site/internal/markdown/renderer.go`

Adapt from `context/cn-agents/server/services/markdown/renderer.go` (391 lines). Simplify by removing the `RenderToSections` method and section-related types — we only need `MarkdownBytesToHTML`.

Keep the custom render hooks for:
- Code blocks with chroma syntax highlighting (`renderer.go:114-128`)
- Links with theme tokens (`renderer.go:129-147`)
- Lists with proper styling (`renderer.go:164-208`)
- Tables with shadcn/ui styling (`renderer.go:234-307`)
- Task list checkboxes (`renderer.go:185-223`)

```go
package markdown

// Core types and methods to keep:
// - type Renderer struct { highlightStyle, htmlFormatter, mdhtmlRenderer }
// - func NewRenderer(highlightStyleString string) (*Renderer, error)
// - func (m Renderer) MarkdownBytesToHTML(md []byte) string
// - func mdhtmlRenderer(...) *mdhtml.Renderer  (with all render hooks)
// - func renderCode(...)
// - func htmlHighlight(...)
// - func detectCheckbox(...)
// - func isInsideCheckboxListItem(...)
```

#### 2. Markdown CSS
**File**: `site/static/css/markdown.css`

Adapt from `context/cn-agents/static/css/index.css` (the `.markdown-viewer` section). Uses existing shadcn/ui theme tokens from `site/static/css/theme.css`.

```css
/* Markdown viewer styles */
.markdown-viewer { color: hsl(var(--foreground)); }
.markdown-viewer p { margin-bottom: 1rem; }
.markdown-viewer pre { overflow-x: auto; background-color: hsl(var(--muted)); border: 1px solid hsl(var(--border)); padding: 1rem; border-radius: 0.5rem; }
.markdown-viewer code:not(pre code) { background-color: hsl(var(--muted)); color: hsl(var(--foreground)); padding: 0.125rem 0.375rem; border-radius: 0.25rem; font-size: 0.875rem; }
.markdown-viewer h1 { font-size: 1.875rem; font-weight: 700; margin-bottom: 1rem; margin-top: 2rem; }
.markdown-viewer h2 { font-size: 1.5rem; font-weight: 600; margin-bottom: 1rem; margin-top: 2rem; padding-bottom: 0.5rem; border-bottom: 1px solid hsl(var(--border)); }
.markdown-viewer h3 { font-size: 1.25rem; font-weight: 600; margin-bottom: 0.75rem; margin-top: 1.5rem; }
.markdown-viewer a { color: hsl(var(--primary)); text-decoration: underline; text-decoration-color: hsl(var(--primary) / 0.3); }
.markdown-viewer a:hover { color: hsl(var(--primary) / 0.8); text-decoration-color: hsl(var(--primary) / 0.8); }
.markdown-viewer ul, .markdown-viewer ol { margin-bottom: 1rem; padding-left: 1.5rem; }
.markdown-viewer .table-wrapper { margin: 1.5rem 0; width: fit-content; overflow: hidden; border-radius: 0.5rem; border: 1px solid hsl(var(--foreground) / 0.2); }
.markdown-viewer table { border-collapse: collapse; }
.markdown-viewer thead { background-color: hsl(var(--foreground) / 0.06); }
.markdown-viewer tbody tr { border-bottom: 1px solid hsl(var(--foreground) / 0.1); }

/* Streaming cursor */
.chat-message-content.streaming { min-height: 1.5rem; }
```

#### 3. Import markdown CSS
**File**: `site/static/css/index.css`

Add `@import './markdown.css';` after existing imports.

### Success Criteria

#### Automated Verification
- [ ] `just check` passes
- [ ] Markdown with code blocks, lists, tables, links renders to correct HTML

---

## Phase 4: Claude Client + Conversation State

### Overview
Implement the Claude API client wrapper and in-memory conversation session manager with rate limiting.

### Changes Required

#### 1. Claude API Client
**File**: `site/internal/mayor/client.go`

Follows the pattern from `context/cn-agents/server/services/chat/service.go` (lines 35-38).

```go
package mayor

import (
    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

type Client struct {
    inner anthropic.Client
}

func NewClient(apiKey string) *Client {
    if apiKey == "" {
        return nil // Feature disabled
    }
    return &Client{
        inner: anthropic.NewClient(option.WithAPIKey(apiKey)),
    }
}

// StreamMessage starts a streaming message request
func (c *Client) StreamMessage(
    ctx context.Context,
    systemPrompt string,
    messages []anthropic.MessageParam,
) *anthropic.MessageStream {
    return c.inner.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
        Model:     anthropic.ModelClaude4_5Sonnet,
        MaxTokens: 1024,
        System: []anthropic.TextBlockParam{
            {Text: systemPrompt},
        },
        Messages: messages,
    })
}
```

#### 2. Conversation Session Manager
**File**: `site/internal/mayor/session.go`

```go
package mayor

import (
    "sync"
    "time"
)

const (
    maxMessagesPerSession = 40  // 20 user + 20 assistant
    minMessageInterval    = 3 * time.Second
    sessionInactivityTTL  = 1 * time.Hour
    maxConcurrentSessions = 200
)

type Message struct {
    Role    string // "user" or "assistant"
    Content string
}

type ConversationSession struct {
    DiscordID    string
    Messages     []Message
    CreatedAt    time.Time
    LastActivity time.Time
}

type ConversationManager struct {
    sessions map[string]*ConversationSession // discordID -> session
    mu       sync.RWMutex
}

func NewConversationManager() *ConversationManager {
    cm := &ConversationManager{
        sessions: make(map[string]*ConversationSession),
    }
    go cm.cleanupLoop()
    return cm
}

func (cm *ConversationManager) GetOrCreate(discordID string) *ConversationSession {
    cm.mu.Lock()
    defer cm.mu.Unlock()

    if s, ok := cm.sessions[discordID]; ok {
        return s
    }

    // Evict oldest if at capacity
    if len(cm.sessions) >= maxConcurrentSessions {
        cm.evictOldest()
    }

    s := &ConversationSession{
        DiscordID:    discordID,
        CreatedAt:    time.Now(),
        LastActivity: time.Now(),
    }
    cm.sessions[discordID] = s
    return s
}

func (cm *ConversationManager) CheckRateLimit(discordID string) error {
    cm.mu.RLock()
    defer cm.mu.RUnlock()

    s, ok := cm.sessions[discordID]
    if !ok {
        return nil
    }

    if len(s.Messages) >= maxMessagesPerSession {
        return fmt.Errorf("conversation limit reached")
    }

    if time.Since(s.LastActivity) < minMessageInterval {
        return fmt.Errorf("please wait a moment")
    }

    return nil
}

func (cm *ConversationManager) AddMessage(discordID, role, content string) {
    cm.mu.Lock()
    defer cm.mu.Unlock()

    if s, ok := cm.sessions[discordID]; ok {
        s.Messages = append(s.Messages, Message{Role: role, Content: content})
        s.LastActivity = time.Now()
    }
}
```

#### 3. System Prompt
**File**: `site/internal/mayor/prompt.go`

```go
package mayor

const SystemPrompt = `You are the Mayor — a warm, enthusiastic AI who helps people imagine their dream multiplayer game world for Creative Mode.

Your personality: Curious, encouraging, creative. You love hearing about worlds people want to build. You ask great follow-up questions that help people articulate their vision.

Your goal: Have a natural conversation to understand what kind of world they want to build. Learn about the setting, mood, gameplay, characters, and mechanics through organic dialogue.

Guidelines:
- Ask 1-2 open-ended questions per response — this is a conversation, not a survey
- Build naturally on their previous answers
- Keep responses under 150 words
- Be genuinely excited about their ideas
- After 4-6 exchanges, when you have a clear picture of their world (setting, gameplay concept, mood), include EXACTLY this marker on its own line at the END of your response:
  SIGNUP_READY: <one sentence describing their world>

Example conversation flow:
- "What kind of world are you imagining? A cozy village, a vast wilderness, a bustling city?"
- [user describes setting] "I love that! What would players actually do there? Explore, build, quest, just hang out?"
- [user describes gameplay] "That's really cool. What's the vibe — peaceful and chill, or more adventurous?"
- [enough info gathered] "... I can already picture it! SIGNUP_READY: A moonlit forest village where players craft potions and befriend woodland creatures"

Never mention the SIGNUP_READY marker to the user or explain what it does.`
```

### Success Criteria

#### Automated Verification
- [ ] `just check` passes
- [ ] Client returns nil when no API key provided
- [ ] Rate limiter blocks messages under 3s interval
- [ ] Rate limiter blocks after 40 messages
- [ ] Session cleanup removes expired sessions

---

## Phase 5: Mayor Chat Page + SSE Handler

### Overview
Build the chat page template and the streaming SSE handler. This is the core feature.

### Changes Required

#### 1. Mayor Chat Page
**File**: `site/pages/mayor.templ`

```templ
package pages

import l "github.com/coreycole/creative-mode/site/layouts"

type MayorPageArgs struct {
    RootArgs        l.RootArgs
    DiscordUsername  string
    DiscordAvatar   string
    InitialGreeting string // Pre-rendered HTML for first mayor message
}

templ MayorPage(args MayorPageArgs) {
    @l.Root(args.RootArgs) {
        <section class="flex flex-col h-[calc(100vh-3.5rem)] max-w-2xl mx-auto px-4">
            <!-- Header -->
            <div class="py-6 text-center border-b border-border/40">
                <h1 class="text-2xl font-bold sm:text-3xl">Meet the Mayor</h1>
                <p class="mt-1 text-sm text-muted-foreground">Tell me about the world you want to build</p>
            </div>

            <!-- Chat log -->
            <div id="mayor-chat-log" class="flex-1 overflow-y-auto py-4 space-y-4">
                <!-- Initial greeting from mayor -->
                <div class="flex justify-start">
                    <div class="max-w-[85%] sm:max-w-md rounded-lg px-4 py-2 bg-muted">
                        <div class="markdown-viewer chat-message-content">
                            @templ.Raw(args.InitialGreeting)
                        </div>
                    </div>
                </div>
            </div>

            <!-- World summary (hidden until SIGNUP_READY) -->
            <div id="mayor-signup"></div>

            <!-- Chat input -->
            <div class="border-t border-border/40 py-4">
                <form
                    id="mayor-chat-form"
                    class="flex gap-2"
                    data-on:submit__prevent="@post('/api/mayor/chat', {contentType: 'form'})"
                >
                    <input
                        type="text"
                        name="message"
                        placeholder="Describe your dream world..."
                        required
                        autocomplete="off"
                        class="flex-1 h-11 rounded-md border border-input bg-background px-4 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                        data-indicator:fetching
                        data-attr:disabled="$fetching"
                    />
                    <button
                        type="submit"
                        class="inline-flex items-center justify-center rounded-md text-sm font-medium bg-primary text-primary-foreground hover:bg-primary/90 h-11 px-6"
                        data-indicator:fetching
                        data-attr:disabled="$fetching"
                    >
                        Send
                    </button>
                </form>
            </div>
        </section>
    }
}
```

#### 2. SSE Fragment Components
**File**: `site/pages/mayor_fragments.templ`

Follows the template patterns from `context/cn-agents/server/services/chat/templates_templ.go`.

```templ
package pages

// UserMessage — right-aligned chat bubble
templ UserMessage(content, avatar, username string) {
    <div class="flex justify-end">
        <div class="flex items-start gap-2 max-w-[85%] sm:max-w-md">
            <div class="rounded-lg px-4 py-2 bg-primary text-primary-foreground">
                <div class="whitespace-pre-wrap text-sm">{ content }</div>
            </div>
            if avatar != "" {
                <img src={ avatar } alt={ username } class="w-8 h-8 rounded-full shrink-0"/>
            }
        </div>
    </div>
}

// MayorMessageStreaming — empty container with animated cursor
templ MayorMessageStreaming(id string) {
    <div id={ "msg-" + id } class="flex justify-start">
        <div class="max-w-[85%] sm:max-w-md rounded-lg px-4 py-2 bg-muted">
            <div id={ "msg-content-" + id } class="markdown-viewer chat-message-content streaming"></div>
            <span id={ "msg-cursor-" + id } class="inline-block w-2 h-4 bg-foreground/50 animate-pulse ml-0.5"></span>
        </div>
    </div>
}

// MayorMessageDelta — plain text update during streaming (used with WithModeInner)
templ MayorMessageDelta(id, text string) {
    <span class="whitespace-pre-wrap">{ text }</span>
}

// MayorMessageDeltaHTML — rendered markdown update during streaming (used with WithModeInner)
templ MayorMessageDeltaHTML(id, htmlContent string) {
    @templ.Raw(htmlContent)
}

// MayorMessageComplete — final message replacing streaming container
templ MayorMessageComplete(id, htmlContent string) {
    <div id={ "msg-" + id } class="flex justify-start">
        <div class="max-w-[85%] sm:max-w-md rounded-lg px-4 py-2 bg-muted">
            <div class="markdown-viewer chat-message-content">
                @templ.Raw(htmlContent)
            </div>
        </div>
    </div>
}

// WorldSummary — hatched world card (shown after SIGNUP_READY)
templ WorldSummary(summary, discordUsername, discordAvatar string) {
    <div id="mayor-signup" class="my-6 rounded-lg border border-border/40 bg-card p-6 text-center">
        <h2 class="text-xl font-bold">Your World Awaits</h2>
        <p class="mt-3 text-muted-foreground">{ summary }</p>
        <div class="mt-4 flex items-center justify-center gap-3">
            if discordAvatar != "" {
                <img src={ discordAvatar } alt={ discordUsername } class="w-10 h-10 rounded-full"/>
            }
            <span class="text-sm font-medium">{ discordUsername }</span>
        </div>
        <p class="mt-4 text-sm text-muted-foreground">
            We'll reach out on Discord when your world is ready.
            Join the
            <a href="https://discord.gg/cPtN5vP3ty" target="_blank" rel="noreferrer"
               class="underline underline-offset-4 hover:text-foreground">
                community
            </a>
            in the meantime.
        </p>
    </div>
}

// RateLimitError — friendly error shown below input
templ RateLimitError(message string) {
    <div id="mayor-error" class="mt-2 text-sm text-destructive text-center">
        { message }
    </div>
}
```

#### 3. SSE Streaming Chat Handler
**File**: `site/internal/mayor/handler.go`

Follows the exact streaming pattern from `context/cn-agents/server/services/chat/handler.go` (lines 82-349).

```go
package mayor

import (
    "html"
    "net/http"
    "strings"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/google/uuid"
    "github.com/labstack/echo/v4"
    "github.com/starfederation/datastar-go/datastar"

    "github.com/coreycole/creative-mode/site/internal/auth"
    "github.com/coreycole/creative-mode/site/internal/markdown"
    p "github.com/coreycole/creative-mode/site/pages"
)

type Handler struct {
    client       *Client
    convManager  *ConversationManager
    mdRenderer   *markdown.Renderer
}

func NewHandler(client *Client, convManager *ConversationManager, mdRenderer *markdown.Renderer) *Handler {
    return &Handler{
        client:      client,
        convManager: convManager,
        mdRenderer:  mdRenderer,
    }
}

func (h *Handler) HandleChat(c echo.Context) error {
    session := c.Get("session").(*auth.Session)
    message := c.FormValue("message")
    if message == "" {
        return echo.NewHTTPError(http.StatusBadRequest, "message required")
    }

    // Rate limit check
    if err := h.convManager.CheckRateLimit(session.DiscordID); err != nil {
        sse := datastar.NewSSE(c.Response().Writer, c.Request())
        return sse.PatchElementTempl(p.RateLimitError(err.Error()))
    }

    // Get conversation, build Anthropic messages
    conv := h.convManager.GetOrCreate(session.DiscordID)
    h.convManager.AddMessage(session.DiscordID, "user", message)

    anthropicMessages := make([]anthropic.MessageParam, 0, len(conv.Messages))
    for _, msg := range conv.Messages {
        if msg.Role == "user" {
            anthropicMessages = append(anthropicMessages,
                anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
        } else {
            anthropicMessages = append(anthropicMessages,
                anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
        }
    }

    // Start SSE
    sse := datastar.NewSSE(c.Response().Writer, c.Request())

    // Clear input
    sse.ExecuteScript("document.querySelector('#mayor-chat-form input[name=message]').value = ''")

    // Clear any previous error
    sse.PatchElements("", datastar.WithModeRemove(), datastar.WithSelectorID("mayor-error"))

    // Append user message
    sse.PatchElementTempl(
        p.UserMessage(message, session.DiscordAvatar, session.DiscordUsername),
        datastar.WithModeAppend(),
        datastar.WithSelectorID("mayor-chat-log"),
    )

    // Scroll to bottom
    sse.ExecuteScript("document.getElementById('mayor-chat-log').scrollTop = document.getElementById('mayor-chat-log').scrollHeight")

    // Append streaming container
    msgID := uuid.New().String()
    sse.PatchElementTempl(
        p.MayorMessageStreaming(msgID),
        datastar.WithModeAppend(),
        datastar.WithSelectorID("mayor-chat-log"),
    )

    // Stream from Claude
    // (exact streaming loop from cn-agents handler.go:229-299)
    stream := h.client.StreamMessage(c.Request().Context(), SystemPrompt, anthropicMessages)

    var fullContent string
    var lastRenderedLen int
    var inCodeBlock bool
    var lastInCodeBlock bool

    for stream.Next() {
        event := stream.Current()
        switch eventVariant := event.AsAny().(type) {
        case anthropic.ContentBlockDeltaEvent:
            switch deltaVariant := eventVariant.Delta.AsAny().(type) {
            case anthropic.TextDelta:
                fullContent += deltaVariant.Text

                codeBlockCount := strings.Count(fullContent, "```")
                lastInCodeBlock = inCodeBlock
                inCodeBlock = codeBlockCount%2 == 1

                newContent := fullContent[lastRenderedLen:]
                shouldRender := false
                if !inCodeBlock {
                    if strings.Contains(newContent, "\n\n") {
                        shouldRender = true
                    }
                    if lastInCodeBlock && !inCodeBlock {
                        shouldRender = true
                    }
                }

                if shouldRender {
                    htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(fullContent))
                    sse.PatchElementTempl(
                        p.MayorMessageDeltaHTML(msgID, htmlContent),
                        datastar.WithModeInner(),
                        datastar.WithSelectorID("msg-content-"+msgID),
                    )
                    lastRenderedLen = len(fullContent)
                } else if lastRenderedLen > 0 {
                    htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(fullContent[:lastRenderedLen]))
                    trailingText := fullContent[lastRenderedLen:]
                    if trailingText != "" {
                        htmlContent += `<span class="whitespace-pre-wrap">` + html.EscapeString(trailingText) + `</span>`
                    }
                    sse.PatchElementTempl(
                        p.MayorMessageDeltaHTML(msgID, htmlContent),
                        datastar.WithModeInner(),
                        datastar.WithSelectorID("msg-content-"+msgID),
                    )
                } else {
                    sse.PatchElementTempl(
                        p.MayorMessageDelta(msgID, fullContent),
                        datastar.WithModeInner(),
                        datastar.WithSelectorID("msg-content-"+msgID),
                    )
                }

                sse.ExecuteScript("document.getElementById('mayor-chat-log').scrollTop = document.getElementById('mayor-chat-log').scrollHeight")
            }
        }
    }

    if stream.Err() != nil {
        c.Logger().Errorf("Stream error: %v", stream.Err())
        return stream.Err()
    }

    // Save assistant message
    h.convManager.AddMessage(session.DiscordID, "assistant", fullContent)

    // Strip SIGNUP_READY marker before final render
    displayContent := fullContent
    var worldSummary string
    if idx := strings.Index(fullContent, "SIGNUP_READY:"); idx != -1 {
        worldSummary = strings.TrimSpace(fullContent[idx+len("SIGNUP_READY:"):])
        displayContent = strings.TrimSpace(fullContent[:idx])
    }

    // Final render
    finalHTML := h.mdRenderer.MarkdownBytesToHTML([]byte(displayContent))
    sse.PatchElementTempl(p.MayorMessageComplete(msgID, finalHTML))

    // Show world summary if triggered
    if worldSummary != "" {
        sse.PatchElementTempl(
            p.WorldSummary(worldSummary, session.DiscordUsername, session.DiscordAvatar),
        )
    }

    // Final scroll
    return sse.ExecuteScript("document.getElementById('mayor-chat-log').scrollTop = document.getElementById('mayor-chat-log').scrollHeight")
}
```

### Success Criteria

#### Automated Verification
- [ ] `just check` passes
- [ ] Chat form submits via Datastar `@post` and receives SSE response
- [ ] User message appends to `#mayor-chat-log`
- [ ] Streaming response renders incrementally with markdown
- [ ] `SIGNUP_READY:` marker is parsed and triggers world summary card
- [ ] Rate limit errors display below input

#### Manual Verification
- [ ] Full conversation flow works end-to-end
- [ ] Markdown renders correctly (code blocks, lists, links)
- [ ] World summary card appears after sufficient conversation
- [ ] Chat scrolls to bottom on new messages
- [ ] Works on mobile viewport (no horizontal overflow)
- [ ] Dark/light theme works on all components

---

## Phase 6: Route Wiring + Link Updates

### Overview
Wire everything together in `main.go` and update the "Meet the Mayor" CTAs.

### Changes Required

#### 1. Update main.go
**File**: `site/main.go`

```go
package main

import (
    "log"
    "net/http"
    "os"

    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"

    "github.com/coreycole/creative-mode/site/internal/auth"
    "github.com/coreycole/creative-mode/site/internal/mayor"
    md "github.com/coreycole/creative-mode/site/internal/markdown"
    l "github.com/coreycole/creative-mode/site/layouts"
    p "github.com/coreycole/creative-mode/site/pages"
)

const port = "3000"

func main() {
    e := echo.New()
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())
    e.Use(middleware.CORS())

    // Init services
    sessionMgr := auth.NewSessionManager()
    inviteMgr := auth.NewInviteCodeManager(os.Getenv("INVITE_CODES"))
    oauthCfg := auth.OAuthConfig{
        ClientID:     os.Getenv("DISCORD_CLIENT_ID"),
        ClientSecret: os.Getenv("DISCORD_CLIENT_SECRET"),
        RedirectURI:  os.Getenv("DISCORD_REDIRECT_URI"),
    }

    // Claude client (nil if no API key)
    claudeClient := mayor.NewClient(os.Getenv("ANTHROPIC_API_KEY"))

    // Markdown renderer
    mdRenderer, err := md.NewRenderer("")
    if err != nil {
        log.Fatalf("Failed to create markdown renderer: %v", err)
    }

    // Conversation manager
    convMgr := mayor.NewConversationManager()

    // Mayor handler
    mayorHandler := mayor.NewHandler(claudeClient, convMgr, mdRenderer)

    // --- Public routes ---
    e.GET("/", func(c echo.Context) error {
        rootArgs := l.RootArgs{Title: "Creative Mode", CurrentPath: c.Request().URL.Path}
        return p.HomePage(rootArgs).Render(c.Request().Context(), c.Response().Writer)
    })

    // Auth routes
    e.GET("/auth/discord/login", auth.HandleLogin(oauthCfg))
    e.GET("/auth/discord/callback", sessionMgr.HandleCallback(oauthCfg))
    e.POST("/auth/logout", sessionMgr.HandleLogout())

    // --- Authenticated routes (session required) ---
    authed := e.Group("", sessionMgr.SessionMiddleware())

    // Invite code page + verification
    authed.GET("/auth/invite", func(c echo.Context) error {
        rootArgs := l.RootArgs{Title: "Invite Code - Creative Mode", CurrentPath: "/auth/invite"}
        return p.InvitePage(rootArgs, "").Render(c.Request().Context(), c.Response().Writer)
    })
    authed.POST("/auth/verify-code", func(c echo.Context) error {
        session := c.Get("session").(*auth.Session)
        code := c.FormValue("code")
        if !inviteMgr.VerifyCode(code, session.DiscordID) {
            rootArgs := l.RootArgs{Title: "Invite Code - Creative Mode", CurrentPath: "/auth/invite"}
            return p.InvitePage(rootArgs, "Invalid or already-used invite code.").Render(
                c.Request().Context(), c.Response().Writer)
        }
        sessionMgr.SetInviteVerified(session.ID)
        return c.Redirect(http.StatusFound, "/mayor")
    })

    // --- Verified routes (session + invite code required) ---
    verified := authed.Group("", auth.InviteCodeMiddleware())

    // Mayor page
    verified.GET("/mayor", func(c echo.Context) error {
        if claudeClient == nil {
            rootArgs := l.RootArgs{Title: "Coming Soon - Creative Mode", CurrentPath: "/mayor"}
            return p.ComingSoonPage(rootArgs).Render(c.Request().Context(), c.Response().Writer)
        }
        session := c.Get("session").(*auth.Session)
        greeting := mdRenderer.MarkdownBytesToHTML([]byte(
            "Hey there! I'm the Mayor. I help people build multiplayer game worlds from scratch.\n\nWhat kind of world are you imagining? A cozy village, a vast wilderness, a mysterious dungeon, a racing game — anything goes!"))
        args := p.MayorPageArgs{
            RootArgs:        l.RootArgs{Title: "Meet the Mayor - Creative Mode", CurrentPath: "/mayor"},
            DiscordUsername:  session.DiscordUsername,
            DiscordAvatar:    session.DiscordAvatar,
            InitialGreeting: greeting,
        }
        return p.MayorPage(args).Render(c.Request().Context(), c.Response().Writer)
    })

    // Chat SSE endpoint
    verified.POST("/api/mayor/chat", mayorHandler.HandleChat)

    // Static files
    e.Static("/", "static/")

    if err := e.Start(":" + port); err != http.ErrServerClosed {
        log.Fatal(err)
    }
}
```

#### 2. Update header CTA
**File**: `site/layouts/root.templ` (line 94)

Change `href="https://github.com/coreycole/creative-mode"` to `href="/mayor"` on the header "Meet the Mayor" button.

#### 3. Update homepage CTAs
**File**: `site/pages/home.templ` (lines 23 and 139)

Change both `href="https://github.com/coreycole/creative-mode"` to `href="/mayor"` on the hero and bottom CTA "Meet the Mayor" buttons.

#### 4. Coming Soon fallback page
**File**: `site/pages/coming_soon.templ`

Simple page shown when `ANTHROPIC_API_KEY` is not set.

```templ
package pages

import l "github.com/coreycole/creative-mode/site/layouts"

templ ComingSoonPage(rootArgs l.RootArgs) {
    @l.Root(rootArgs) {
        <section class="flex items-center justify-center min-h-[80vh]">
            <div class="text-center px-4">
                <h1 class="text-3xl font-bold">The Mayor is Preparing</h1>
                <p class="mt-4 text-lg text-muted-foreground max-w-md mx-auto">
                    The conversational world builder is coming soon. Join the
                    <a href="https://discord.gg/cPtN5vP3ty" target="_blank" rel="noreferrer"
                       class="underline underline-offset-4 hover:text-foreground">
                        Discord
                    </a>
                    to get notified.
                </p>
            </div>
        </section>
    }
}
```

### Success Criteria

#### Automated Verification
- [ ] `just check` passes
- [ ] All "Meet the Mayor" links now point to `/mayor`
- [ ] Route groups enforce correct middleware (session → invite → chat)
- [ ] Missing API key shows "coming soon" page

#### Manual Verification
- [ ] Full flow: homepage → click CTA → Discord OAuth → invite code → chat → world summary
- [ ] Header CTA works from both homepage and mayor page
- [ ] All pages render correctly on mobile
- [ ] Dark/light theme is consistent across all new pages

---

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DISCORD_CLIENT_ID` | Yes | Discord OAuth app client ID |
| `DISCORD_CLIENT_SECRET` | Yes | Discord OAuth app client secret |
| `DISCORD_REDIRECT_URI` | Yes | OAuth callback URL (e.g. `http://localhost:3000/auth/discord/callback`) |
| `ANTHROPIC_API_KEY` | No | Claude API key. If unset, `/mayor` shows "coming soon" |
| `INVITE_CODES` | Yes | Comma-separated valid invite codes (e.g. `ALPHA-001,ALPHA-002`) |

## Testing Strategy

### Manual Testing Steps
1. Start site: `cd site && just watch` (or Docker)
2. Open `http://localhost:3000`
3. Click "Meet the Mayor" → should redirect to Discord OAuth
4. Complete OAuth → should land on invite code page
5. Enter invalid code → should see error, can retry
6. Enter valid code → should land on chat page with mayor greeting
7. Send a message → see user bubble, typing indicator, streamed response
8. Have 5-6 exchanges → world summary card should appear
9. Send rapid messages → should see rate limit error
10. Refresh page → session persists, but conversation resets (in-memory)
11. Test on mobile viewport → layout should be responsive

## References

- Harness OAuth pattern: `harness/internal/auth/auth.go`
- Harness middleware: `harness/internal/auth/middleware.go`
- Claude streaming + Datastar SSE: `context/cn-agents/server/services/chat/handler.go`
- Claude client setup: `context/cn-agents/server/services/chat/service.go`
- Markdown renderer: `context/cn-agents/server/services/markdown/renderer.go`
- Chat templates: `context/cn-agents/server/services/chat/templates_templ.go`
- Chat args: `context/cn-agents/server/services/chat/args.go`
- Site main: `site/main.go`
- Site layout: `site/layouts/root.templ`
- Site homepage: `site/pages/home.templ`
