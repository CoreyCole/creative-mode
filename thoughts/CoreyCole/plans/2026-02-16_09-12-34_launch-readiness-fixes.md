---
date: 2026-02-16T09:12:34+0000
author: CoreyCole
git_commit: c3331c04e5ffa426794d72169096731fd52bbbff
branch: main
repository: creative-mode
topic: "Launch Readiness Fixes: Server Hardening, Onboarding Robustness, Discord Formatting"
tags: [plan, launch, security, scalability, onboarding, discord]
status: approved
last_updated: 2026-02-16
last_updated_by: CoreyCole
---

# Launch Readiness Fixes — Implementation Plan

**Date**: 2026-02-16T09:12:34+0000
**Author**: CoreyCole
**Git Commit**: c3331c04e5ffa426794d72169096731fd52bbbff
**Branch**: main
**Repository**: creative-mode

## Overview

We're launching creative-mode.ai to the public tomorrow. A comprehensive audit identified critical security, scalability, and UX issues. This plan covers all P0 (must-fix) and P1 (should-fix) items across server hardening, database tuning, onboarding robustness, and Discord message formatting.

## Current State Analysis

The site is a Go/Echo server with SQLite, Datastar SSE streaming, and Discord OAuth. Key problems:
- Datastar JS loaded from unpinned `@main` CDN branch (one upstream push breaks everything)
- Wildcard CORS, zero security headers, no server timeouts, no body limits
- SQLite serialized through a single connection (`MaxOpenConns(1)`)
- Static assets served without cache headers
- Malformed `WORLD_READY` markers silently fail, leaving users stuck
- Discord onboarding data pinned as raw JSON code blocks (ugly, hard to read)
- Cover art errors leak internal error strings to users

**Already done:** Re-hatch protection (`SetHatched` in `session.go:185-200`, called at `handler.go:302` and `handler.go:545`).

### Key Discoveries:
- Echo v4.13.3 has built-in `middleware.Secure` for security headers
- `devMode` variable exists at `main.go:127` — CORS needs to allow localhost in dev
- `pkg/worldchannel/client.go` uses `ChannelMessageSend` but discordgo has `ChannelMessageSendComplex` for file attachments
- `ReadOnboardingData` in `onboarding.go:100-138` parses the `🥚` marker from pinned messages — backwards compat needed

## Desired End State

After implementation:
1. CDN dependency is pinned to a release tag
2. All responses include security headers (HSTS, X-Frame-Options, CSP, etc.)
3. CORS restricts to `creative-mode.ai` (plus localhost in dev)
4. Server has read/write/idle timeouts preventing Slowloris
5. Missing env vars cause immediate startup failure with clear error
6. SQLite handles 4 concurrent connections (parallel reads)
7. Static assets cached for 24 hours
8. Messages capped at 2000 characters
9. Malformed WORLD_READY with 2 parts still hatches (empty summary)
10. Invite codes rate-limited to prevent brute force
11. Discord onboarding data posted as human-friendly message with JSON file attachment
12. Cover art errors show generic user-friendly message

## What We're NOT Doing

- Global Claude API semaphore (P2)
- Per-user concurrent request guard (P2)
- Extending conversation cleanup from 24h to 7 days (P2)
- Discord embeds for build notifications (P2)
- Custom 404/500 error pages (P2)
- Partial-stream scripted fallback fix (P2)

---

## Phase 1: Server Hardening (P0)

### Overview
Pin external dependencies, add security headers, configure CORS, set server timeouts, validate env vars at startup, and add request body limits.

### Changes Required:

#### 1.1 Pin Datastar JS version
**File:** `site/layouts/root.templ:44`

Replace `@main` with `@v1.0.0-RC.7` (latest stable release candidate, matches Go SDK v1.1.0):
```
- src="https://cdn.jsdelivr.net/gh/starfederation/datastar@main/bundles/datastar.js"
+ src="https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.0-RC.7/bundles/datastar.js"
```

#### 1.2 Security headers + CORS + body limit
**File:** `site/main.go:58-60`

Replace middleware block. CORS must allow localhost in dev mode:
```go
e.Use(middleware.Logger())
e.Use(middleware.Recover())

// CORS — restrict to creative-mode.ai in production, allow localhost in dev.
corsOrigins := []string{"https://creative-mode.ai"}
if devMode {
    corsOrigins = append(corsOrigins, "http://localhost:*")
}
e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
    AllowOrigins: corsOrigins,
    AllowMethods: []string{http.MethodGet, http.MethodPost},
}))
e.Use(middleware.BodyLimit("1M"))
e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
    XSSProtection:         "1; mode=block",
    ContentTypeNosniff:    "nosniff",
    XFrameOptions:         "DENY",
    HSTSMaxAge:            31536000,
    ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://cdn.discordapp.com",
}))
```

**Important:** Move `devMode := os.Getenv("DEV_MODE") == "true"` (currently at line 127) to before middleware setup (~line 57) so it's available for CORS config.

#### 1.3 Server timeouts
**File:** `site/main.go:311` (before `e.Start`)

```go
e.Server.ReadTimeout = 30 * time.Second
e.Server.WriteTimeout = 120 * time.Second // long for SSE streams
e.Server.IdleTimeout = 120 * time.Second
```

Add `"time"` to imports.

#### 1.4 Validate critical env vars at startup
**File:** `site/main.go` — after database setup (~line 56), before auth setup (~line 62)

```go
// Validate required env vars.
requiredEnv := []string{"DISCORD_CLIENT_ID", "DISCORD_CLIENT_SECRET", "DISCORD_REDIRECT_URI"}
var missing []string
for _, name := range requiredEnv {
    if os.Getenv(name) == "" {
        missing = append(missing, name)
    }
}
if len(missing) > 0 {
    log.Fatalf("Missing required environment variables: %s", strings.Join(missing, ", "))
}
if os.Getenv("INVITE_CODES") == "" {
    log.Printf("WARNING: INVITE_CODES is empty — all invite codes will be rejected")
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd site && go build .` compiles successfully
- [ ] `cd site && go vet ./...` passes
- [ ] `curl -I http://localhost:80/` shows `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Strict-Transport-Security` headers
- [ ] `curl -I -H "Origin: https://evil.com" http://localhost:80/` does NOT return `Access-Control-Allow-Origin: https://evil.com`
- [ ] Server rejects requests with body >1MB

#### Manual Verification:
- [ ] Page loads correctly with pinned Datastar (no JS errors in console)
- [ ] Server refuses to start if `DISCORD_CLIENT_ID` is missing
- [ ] Server logs warning if `INVITE_CODES` is empty
- [ ] Chat SSE streams still work (120s write timeout is sufficient)

---

## Phase 2: Database & Caching (P1)

### Overview
Increase SQLite connection pool for concurrent reads and add cache headers to static assets.

### Changes Required:

#### 2.1 Increase SQLite MaxOpenConns
**File:** `site/internal/db/db.go:23`

```go
- db.SetMaxOpenConns(1)
+ db.SetMaxOpenConns(4)
+ db.SetMaxIdleConns(4)
```

WAL mode supports concurrent readers. 4 connections allows parallel `GetSession()` + `GetMessages()` reads.

#### 2.2 Static asset cache headers
**File:** `site/main.go:145`

Replace `e.Static("/", "static/")` with a group that adds cache headers:
```go
staticGroup := e.Group("")
staticGroup.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        c.Response().Header().Set("Cache-Control", "public, max-age=86400")
        return next(c)
    }
})
staticGroup.Static("/", "static/")
```

### Success Criteria:

#### Automated Verification:
- [ ] `cd site && go build .` compiles
- [ ] `curl -I http://localhost:80/css/out.css` returns `Cache-Control: public, max-age=86400`
- [ ] `curl -I http://localhost:80/img/favicon.png` returns `Cache-Control: public, max-age=86400`

#### Manual Verification:
- [ ] Browser dev tools show cached static assets on second page load

---

## Phase 3: Onboarding Robustness (P1)

### Overview
Handle malformed WORLD_READY markers, cap message length, rate-limit invite codes, and sanitize cover art error messages.

### Changes Required:

#### 3.1 Handle malformed WORLD_READY
**File:** `site/internal/mayor/handler.go:267-276`

Replace the strict 3-part check with a switch:
```go
if idx := strings.Index(fullContent, "WORLD_READY|"); idx != -1 {
    parts := strings.SplitN(fullContent[idx+len("WORLD_READY|"):], "|", 3)
    switch len(parts) {
    case 3:
        mayorName = strings.TrimSpace(parts[0])
        worldName = strings.TrimSpace(parts[1])
        worldSummary = strings.TrimSpace(parts[2])
    case 2:
        mayorName = strings.TrimSpace(parts[0])
        worldName = strings.TrimSpace(parts[1])
        worldSummary = ""
    default:
        c.Logger().Errorf("Malformed WORLD_READY marker: %q", fullContent[idx:])
    }
}
```

#### 3.2 Message length validation
**File:** `site/internal/mayor/handler.go:76` — after `content := strings.TrimSpace(signals.MayorInput)`, before the empty check

```go
const maxMessageLen = 2000
if len(content) > maxMessageLen {
    content = content[:maxMessageLen]
}
```

#### 3.3 Invite code rate limiting
**File:** `site/main.go:164-179` (the `POST /invite` handler)

Add package-level rate tracker:
```go
var (
    inviteAttempts   = make(map[string]time.Time)
    inviteAttemptsMu sync.Mutex
)
```

Add `"sync"` to imports.

In the POST handler, before `inviteCodes.VerifyCode`:
```go
inviteAttemptsMu.Lock()
if last, ok := inviteAttempts[session.ID]; ok && time.Since(last) < 2*time.Second {
    inviteAttemptsMu.Unlock()
    rootArgs := l.RootArgs{
        Title:       "Invite Code - Creative Mode",
        CurrentPath: c.Request().URL.Path,
        Commit:      commit,
    }
    return p.InvitePage(rootArgs, "Please wait a moment before trying again.").Render(c.Request().Context(), c.Response().Writer)
}
inviteAttempts[session.ID] = time.Now()
inviteAttemptsMu.Unlock()
```

#### 3.4 Cover art error sanitization
**File:** `site/internal/mayor/handler.go` — lines 345, 358, 496, 509

Replace all `p.CoverArtError(err.Error(), ...)` with:
```go
p.CoverArtError("Cover art generation failed. You can try again or hatch without it.", worldName, mayorName)
```

(4 occurrences total)

### Success Criteria:

#### Automated Verification:
- [ ] `cd site && go build .` compiles
- [ ] `cd site && go vet ./...` passes

#### Manual Verification:
- [ ] Pasting a 5000-char message sends only 2000 chars
- [ ] Rapid-fire invite code submissions show "Please wait" message
- [ ] Cover art generation failure shows friendly message (not internal error)

---

## Phase 4: Discord Formatting (P1)

### Overview
Replace raw JSON pinned messages with human-friendly messages plus JSON file attachments. Maintain backwards compatibility for reading old-format channels.

### Changes Required:

#### 4.1 Add SendComplexMessage to Client
**File:** `pkg/worldchannel/client.go`

Add method after `SendMessage`:
```go
// SendComplexMessage sends a message with optional file attachments.
func (c *Client) SendComplexMessage(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
    m, err := c.session.ChannelMessageSendComplex(channelID, msg)
    if err != nil {
        return nil, fmt.Errorf("sending complex message to channel %s: %w", channelID, err)
    }
    return m, nil
}
```

#### 4.2 Rewrite PinOnboardingData
**File:** `pkg/worldchannel/onboarding.go:53-96`

Replace the entire `PinOnboardingData` function. New version sends a single human-friendly pinned message with the JSON as a file attachment:

```go
func (c *Client) PinOnboardingData(channelID string, data OnboardingData) error {
    fullJSON, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return fmt.Errorf("marshaling onboarding data: %w", err)
    }

    // Human-friendly message content.
    summary := data.World.Summary
    if len(summary) > 200 {
        summary = summary[:200] + "..."
    }
    content := fmt.Sprintf("%s\n\n**World**: %s\n**Mayor**: %s\n**Creator**: <@%s>\n\n> %s",
        onboardingMarker, data.World.Name, data.Mayor.Name, data.Creator.DiscordID, summary)

    msg := &discordgo.MessageSend{
        Content: content,
        Files: []*discordgo.File{{
            Name:        "onboarding.json",
            ContentType: "application/json",
            Reader:      bytes.NewReader(fullJSON),
        }},
    }

    sent, err := c.SendComplexMessage(channelID, msg)
    if err != nil {
        return fmt.Errorf("sending onboarding message: %w", err)
    }
    if err := c.session.ChannelMessagePin(channelID, sent.ID); err != nil {
        c.logger.Warn("failed to pin onboarding message", "message_id", sent.ID, "error", err)
    }
    return nil
}
```

Add `"bytes"` to imports. Remove `sendAndPin` and `formatOnboardingMessage` functions (no longer used by writer). Keep `splitConversation` and `extractJSON` for backwards-compat reader.

#### 4.3 Update ReadOnboardingData for dual-format support
**File:** `pkg/worldchannel/onboarding.go:100-138`

New reader checks for file attachments first, falls back to legacy code-block format:

```go
func (c *Client) ReadOnboardingData(channelID string) (*OnboardingData, error) {
    pins, err := c.session.ChannelMessagesPinned(channelID)
    if err != nil {
        return nil, fmt.Errorf("fetching pinned messages: %w", err)
    }

    // Try new format first: file attachment on message with 🥚 marker.
    for _, pin := range pins {
        if !strings.HasPrefix(pin.Content, onboardingMarker) {
            continue
        }
        for _, att := range pin.Attachments {
            if att.Filename == "onboarding.json" {
                return c.downloadOnboardingJSON(att.URL)
            }
        }
    }

    // Fall back to legacy code-block format for existing channels.
    var mainMsg string
    var continuationMsgs []string
    for i := len(pins) - 1; i >= 0; i-- {
        content := pins[i].Content
        if j := extractJSON(content, onboardingMarker); j != "" {
            mainMsg = j
        } else if j := extractJSON(content, onboardingContinuation); j != "" {
            continuationMsgs = append(continuationMsgs, j)
        }
    }

    if mainMsg == "" {
        return nil, nil
    }

    var data OnboardingData
    if err := json.Unmarshal([]byte(mainMsg), &data); err != nil {
        return nil, fmt.Errorf("parsing onboarding data: %w", err)
    }
    for _, contJSON := range continuationMsgs {
        var msgs []OnboardingMessage
        if err := json.Unmarshal([]byte(contJSON), &msgs); err != nil {
            c.logger.Warn("skipping malformed onboarding continuation", "error", err)
            continue
        }
        data.Messages = append(data.Messages, msgs...)
    }
    return &data, nil
}
```

Add `downloadOnboardingJSON` helper:
```go
func (c *Client) downloadOnboardingJSON(url string) (*OnboardingData, error) {
    resp, err := http.Get(url)
    if err != nil {
        return nil, fmt.Errorf("downloading onboarding attachment: %w", err)
    }
    defer resp.Body.Close()

    var data OnboardingData
    if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
        return nil, fmt.Errorf("decoding onboarding attachment: %w", err)
    }
    return &data, nil
}
```

Add `"net/http"` to imports.

### Success Criteria:

#### Automated Verification:
- [ ] `cd site && go build ./...` compiles (site uses this package)
- [ ] `go build ./...` from repo root compiles (harness uses `ReadOnboardingData`)
- [ ] `go vet ./...` passes

#### Manual Verification:
- [ ] Hatch a test world -> Discord channel has pinned message with human-friendly format
- [ ] The pinned message has an `onboarding.json` file attachment
- [ ] Harness can still read onboarding data from OLD channels (code-block format)
- [ ] Harness can read onboarding data from NEW channels (file attachment format)

---

## Implementation Order

1. **Phase 1** — Server hardening (`main.go`, `root.templ`)
2. **Phase 2** — Database & caching (`db.go`, `main.go`)
3. **Phase 3** — Onboarding robustness (`handler.go`, `main.go`)
4. **Phase 4** — Discord formatting (`onboarding.go`, `client.go`)

Each phase is independently deployable. Phase 1 is highest priority for launch day.

## Files Modified

| File | Changes |
|------|---------|
| `site/layouts/root.templ` | Pin Datastar to `@v1.0.0-RC.7` |
| `site/main.go` | Move devMode up, security headers, CORS, body limit, timeouts, env validation, static caching, invite rate limit |
| `site/internal/db/db.go` | `MaxOpenConns(4)`, `MaxIdleConns(4)` |
| `site/internal/mayor/handler.go` | Malformed WORLD_READY, message length cap, cover art error sanitization |
| `pkg/worldchannel/client.go` | Add `SendComplexMessage` |
| `pkg/worldchannel/onboarding.go` | Rewrite `PinOnboardingData` (file attachment), update `ReadOnboardingData` (dual-format), add `downloadOnboardingJSON` |

## Final Verification

After all phases:
1. `cd site && go build .` — site compiles
2. `go build ./...` from repo root — everything compiles (harness uses `pkg/worldchannel`)
3. `go vet ./...` — no issues
4. Deploy to EC2 and verify with `curl -I https://creative-mode.ai/` that security headers are present
5. Complete full onboarding flow end-to-end: login -> invite code -> mayor chat -> hatch -> verify Discord channel has human-friendly pinned message with JSON attachment

## References

- Research audit: `thoughts/CoreyCole/research/2026-02-16_08-44-34_launch-readiness-audit.md`
- Site architecture: `site/CLAUDE.md`
- Echo middleware docs: labstack/echo v4.13.3
