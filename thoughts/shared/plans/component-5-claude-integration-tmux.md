# Component 5: Claude Code Integration + tmux

## Overview

Connect the harness to Claude Code via tmux sessions for AI-powered game development. This component handles the prompt-to-build pipeline: creating tmux sessions, delivering prompts safely via `--input-file`, receiving structured events from Claude Code hooks, triggering builds, and managing the event bus that powers real-time UI updates.

**Dependencies**: Component 3 (world management + build pipeline), Component 4 (Bevy game template with hook scripts)
**Depends on this**: Components 6, 7

## Directory Layout

```
harness/internal/
├── tmux/
│   └── session.go          # tmux session management (create, send prompt, kill)
├── claude/
│   ├── claude.go           # Prompt-to-build orchestrator (hooks-driven)
│   └── memory.go           # MEMORY.md management (read/update before/after sessions)
├── events/
│   └── bus.go              # EventBus (global + per-world pub/sub channels)
└── server/
    └── events.go           # SSE endpoint, handleClaudeEvent, handleChatMessage
```

## Implementation Details

### tmux Session Manager (`harness/internal/tmux/session.go`)

```go
package tmux

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
)

type Session struct {
    Name    string  // cm-{worldID}-{cpID}
    WorkDir string  // checkpoint directory
}

func NewSession(worldID, cpID, workDir string) *Session {
    return &Session{
        Name:    fmt.Sprintf("cm-%s-%s", worldID, cpID),
        WorkDir: workDir,
    }
}

// Create starts a new tmux session with CM_* environment variables.
// Hook scripts in .claude/hooks/ use these env vars to tag their JSONL events
// and POST to the harness.
func (s *Session) Create(worldID, cpID, logsDir, harnessURL string) error {
    logDir := filepath.Join(logsDir, "worlds", worldID, cpID)
    os.MkdirAll(logDir, 0755)

    return exec.Command("tmux", "new-session", "-d",
        "-s", s.Name, "-c", s.WorkDir,
        "-e", "CM_WORLD_ID="+worldID,
        "-e", "CM_CHECKPOINT_ID="+cpID,
        "-e", "CM_HARNESS_URL="+harnessURL,
        "-e", "CM_LOG_DIR="+logDir,
    ).Run()
}

// SendPrompt writes the prompt to a file and passes it via --input-file.
// This avoids shell injection via tmux send-keys — the prompt never passes
// through shell interpolation.
func (s *Session) SendPrompt(prompt string) error {
    // Write prompt to a temp file
    promptFile := filepath.Join(s.WorkDir, ".claude-prompt.txt")
    if err := os.WriteFile(promptFile, []byte(prompt), 0644); err != nil {
        return fmt.Errorf("writing prompt file: %w", err)
    }

    // Use --input-file for safe prompt delivery
    // --dangerously-skip-permissions is required for unattended operation in tmux
    cmd := fmt.Sprintf("claude --dangerously-skip-permissions --input-file %q", promptFile)
    return exec.Command("tmux", "send-keys", "-t", s.Name, cmd, "Enter").Run()
}

// Kill terminates the tmux session
func (s *Session) Kill() error {
    return exec.Command("tmux", "kill-session", "-t", s.Name).Run()
}

// IsAlive checks if the tmux session still exists
func (s *Session) IsAlive() bool {
    err := exec.Command("tmux", "has-session", "-t", s.Name).Run()
    return err == nil
}
```

> **Security note**: Even with file-based prompt delivery, `--dangerously-skip-permissions`
> allows Claude Code to execute arbitrary commands. This is an accepted risk for managed
> tmux sessions. The key improvement is that user input never passes through shell interpolation.

### Event Bus (`harness/internal/events/bus.go`)

Supports both global (all players) and per-world subscriptions. SSE handlers subscribe to both.

```go
package events

import "sync"

type EventBus struct {
    mu         sync.RWMutex
    worldSubs  map[string][]chan any  // worldID -> subscriber channels
    globalSubs []chan any             // all-player subscribers
}

func NewEventBus() *EventBus {
    return &EventBus{
        worldSubs: make(map[string][]chan any),
    }
}

// SubscribeGlobal creates a channel that receives all global events
// (chat messages, build notifications visible to all players)
func (b *EventBus) SubscribeGlobal() chan any {
    b.mu.Lock()
    defer b.mu.Unlock()
    ch := make(chan any, 100)
    b.globalSubs = append(b.globalSubs, ch)
    return ch
}

// UnsubscribeGlobal removes a global subscriber channel
func (b *EventBus) UnsubscribeGlobal(ch chan any) {
    b.mu.Lock()
    defer b.mu.Unlock()
    for i, sub := range b.globalSubs {
        if sub == ch {
            b.globalSubs = append(b.globalSubs[:i], b.globalSubs[i+1:]...)
            close(ch)
            break
        }
    }
}

// PublishGlobal sends an event to all global subscribers
func (b *EventBus) PublishGlobal(event any) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for _, ch := range b.globalSubs {
        select {
        case ch <- event:
        default: // drop if subscriber is slow
        }
    }
}

// Subscribe creates a channel for world-specific events
func (b *EventBus) Subscribe(worldID string) chan any {
    b.mu.Lock()
    defer b.mu.Unlock()
    ch := make(chan any, 100)
    b.worldSubs[worldID] = append(b.worldSubs[worldID], ch)
    return ch
}

// Unsubscribe removes a world-specific subscriber
func (b *EventBus) Unsubscribe(worldID string, ch chan any) {
    b.mu.Lock()
    defer b.mu.Unlock()
    subs := b.worldSubs[worldID]
    for i, sub := range subs {
        if sub == ch {
            b.worldSubs[worldID] = append(subs[:i], subs[i+1:]...)
            close(ch)
            break
        }
    }
}

// Publish sends an event to all subscribers of a specific world
func (b *EventBus) Publish(worldID string, event any) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for _, ch := range b.worldSubs[worldID] {
        select {
        case ch <- event:
        default:
        }
    }
}
```

### Claude Code Orchestrator (`harness/internal/claude/claude.go`)

The prompt-to-build pipeline, event-driven via hooks:

```go
package claude

type Orchestrator struct {
    db           *db.DB
    logger       *slog.Logger
    worldManager *world.Manager
    builder      *build.Builder
    eventBus     *events.EventBus
    logsDir      string
    harnessURL   string
    maxRetries   int  // 3 — rebuild attempts if code doesn't compile
}
```

**`HandlePrompt(worldID, sourceCPID, prompt, userID string) error`:**

1. Fork checkpoint via `worldManager.ForkCheckpoint()`
2. Update MEMORY.md with new prompt context
3. Create tmux session with `CM_*` env vars
4. Send prompt via `--input-file`
5. Return immediately (hooks will drive the rest asynchronously)

```go
func (o *Orchestrator) HandlePrompt(worldID, sourceCPID, prompt, userID string) (*db.Checkpoint, error) {
    // Fork
    cp, err := o.worldManager.ForkCheckpoint(worldID, sourceCPID, prompt, userID)
    if err != nil {
        return nil, err
    }

    // Update MEMORY.md
    o.updateMemory(cp.DirPath, prompt)

    // Create tmux session
    session := tmux.NewSession(worldID, cp.ID, cp.DirPath)
    if err := session.Create(worldID, cp.ID, o.logsDir, o.harnessURL); err != nil {
        return nil, fmt.Errorf("creating tmux session: %w", err)
    }

    // Send prompt
    if err := session.SendPrompt(prompt); err != nil {
        return nil, fmt.Errorf("sending prompt: %w", err)
    }

    o.logger.Info("claude session started",
        "worldID", worldID, "cpID", cp.ID, "prompt", prompt[:min(len(prompt), 100)])

    return cp, nil
}
```

**`BuildCheckpoint(worldID, cpID string)`** — called when `claude.session_stopped` event arrives:

```go
func (o *Orchestrator) BuildCheckpoint(worldID, cpID string) {
    cp, err := o.db.GetCheckpoint(cpID)
    if err != nil {
        o.logger.Error("checkpoint not found", "cpID", cpID, "error", err)
        return
    }

    // Notify: build started
    o.createAndPublishMessage("build.started", worldID, cpID,
        fmt.Sprintf("Building in %s...", o.worldName(worldID)))

    // Build
    isInitial := cp.ParentCheckpointID == ""
    if err := o.builder.Build(cp, isInitial); err != nil {
        o.logger.Error("build failed", "cpID", cpID, "error", err)
        o.db.UpdateCheckpointStatus(cpID, "failed", err.Error())

        o.createAndPublishMessage("build.failed", worldID, cpID,
            fmt.Sprintf("Build failed: %s", err.Error()))

        // Publish to world bus for build status UI
        o.eventBus.Publish(worldID, map[string]any{
            "event": "build.failed", "worldID": worldID, "cpID": cpID,
            "error": err.Error(),
        })
        return
    }

    // Post-build: extract work summary, files changed
    o.builder.PostBuild(cp)

    // Update status
    o.db.UpdateCheckpointStatus(cpID, "ready", "")

    // Start game server
    srv, err := o.worldManager.GameServers().Connect(worldID, cpID, cp.DirPath)
    if err != nil {
        o.logger.Error("failed to start game server", "cpID", cpID, "error", err)
    } else {
        o.db.UpdateCheckpointServerPort(cpID, srv.Port)
    }

    // Notify: build completed
    worldName := o.worldName(worldID)
    promptSnippet := cp.Prompt
    if len(promptSnippet) > 60 {
        promptSnippet = promptSnippet[:60] + "..."
    }
    o.createAndPublishMessage("build.completed", worldID, cpID,
        fmt.Sprintf("%s checkpoint ready: '%s'", worldName, promptSnippet))

    // Publish to world bus
    o.eventBus.Publish(worldID, map[string]any{
        "event": "build.completed", "worldID": worldID, "cpID": cpID,
    })
}
```

**`createAndPublishMessage`** — persists to DB and publishes to global bus:

```go
func (o *Orchestrator) createAndPublishMessage(msgType, worldID, cpID, content string) {
    msg := &db.Message{
        ID:           uuid.New().String()[:8],
        Type:         msgType,
        WorldID:      worldID,
        CheckpointID: cpID,
        Content:      content,
    }
    o.db.CreateMessage(msg)

    o.eventBus.PublishGlobal(map[string]any{
        "event":   msgType,
        "worldID": worldID,
        "cpID":    cpID,
        "content": content,
        "ts":      time.Now().UTC().Format(time.RFC3339),
    })
}
```

### Claude Event Endpoint (`POST /api/claude-event`)

Receives JSONL events POSTed by Claude Code hook scripts. This endpoint is **unprotected** (internal same-machine communication).

```go
// POST /api/claude-event - receives JSONL events from Claude Code hooks
func (s *Server) handleClaudeEvent(c echo.Context) error {
    var event map[string]any
    if err := json.NewDecoder(c.Request().Body).Decode(&event); err != nil {
        return c.NoContent(400)
    }

    worldID, _ := event["worldID"].(string)
    cpID, _ := event["cpID"].(string)
    eventType, _ := event["event"].(string)

    s.logger.Info("claude hook event", "worldID", worldID, "event", eventType)

    // Publish to world-specific bus (build progress, claude activity)
    s.eventBus.Publish(worldID, event)

    // If claude stopped, trigger the build pipeline
    if eventType == "claude.session_stopped" {
        go s.orchestrator.BuildCheckpoint(worldID, cpID)
    }

    return c.NoContent(200)
}
```

Register: `e.POST("/api/claude-event", s.handleClaudeEvent)` — no auth middleware.

### Chat Message Endpoint (`POST /api/chat`)

```go
func (s *Server) handleChatMessage(c echo.Context) error {
    user := c.Get("user").(*db.User)
    var body struct {
        Content string `json:"content"`
    }
    if err := c.Bind(&body); err != nil || body.Content == "" {
        return c.NoContent(400)
    }

    msg := &db.Message{
        ID:      uuid.New().String()[:8],
        Type:    "chat",
        UserID:  user.ID,
        Content: body.Content,
    }
    s.db.CreateMessage(msg)

    s.eventBus.PublishGlobal(map[string]any{
        "event":    "chat.message",
        "username": user.GitHubUsername,
        "avatar":   user.AvatarURL,
        "content":  body.Content,
        "ts":       time.Now().UTC().Format(time.RFC3339),
    })
    return c.NoContent(200)
}
```

Register: `approved.POST("/api/chat", s.handleChatMessage)` — requires auth.

### MEMORY.md Management (`harness/internal/claude/memory.go`)

Before each claude code session, update MEMORY.md with context:

```go
func (o *Orchestrator) updateMemory(checkpointDir, prompt string) {
    memoryPath := filepath.Join(checkpointDir, "MEMORY.md")
    content, _ := os.ReadFile(memoryPath)

    // Append the new prompt as context
    addition := fmt.Sprintf("\n\n## Latest Prompt\n%s\n", prompt)
    os.WriteFile(memoryPath, append(content, []byte(addition)...), 0644)
}
```

After claude finishes (during PostBuild), key decisions can be extracted from CHANGES.txt and appended to MEMORY.md for future sessions in this lineage.

### SSE Event Stream (World-Scoped)

`GET /world/:worldID/events` — the main SSE endpoint. Subscribes to both global and world buses. Covered in Component 6 (UI overlay) which implements the full SSE handler with Datastar integration. This component provides the EventBus and event publishing infrastructure that the SSE handler consumes.

### Global SSE Stream (Lobby)

`GET /events` — SSE for the lobby page (before entering a world). Subscribes only to the global bus. Shows chat and system notifications. Also covered in Component 6.

## Interface Contract

This component provides to other components:

1. **`EventBus`** — Component 6 subscribes to global + world channels for SSE
2. **`Orchestrator.HandlePrompt()`** — Component 6's prompt handler calls this
3. **`POST /api/claude-event`** — Component 4's hook scripts POST to this
4. **`POST /api/chat`** — Component 6's chat input calls this
5. **`createAndPublishMessage()`** — publishes structured messages to the global bus (Component 6 renders these)

This component consumes from other components:
1. **Component 3** — `WorldManager.ForkCheckpoint()`, `Builder.Build()`, `GameServerManager`
2. **Component 4** — Hook scripts in `.claude/hooks/` POST to `/api/claude-event`
3. **Component 1** — `DB` for message persistence, checkpoint updates

## Success Criteria

### Automated Verification
- [ ] tmux sessions create with correct `CM_*` env vars
- [ ] `.claude-prompt.txt` is written with correct content
- [ ] Claude code receives prompt via `--input-file` in tmux
- [ ] Hook scripts write JSONL to `claude.jsonl`
- [ ] `POST /api/claude-event` publishes events to EventBus subscribers
- [ ] Build pipeline triggers when `claude.session_stopped` event arrives
- [ ] `build.started` and `build.completed` messages are persisted in DB
- [ ] Messages are published to global bus
- [ ] Chat messages are persisted and published
- [ ] EventBus handles subscribe/unsubscribe without goroutine leaks

### Manual Verification
- [ ] Submit prompt -> see claude activity events in SSE stream -> build completes
- [ ] MEMORY.md is updated with prompt context before claude starts
- [ ] CHANGES.txt is read after build for work summary
- [ ] Power user can `tmux attach -t cm-{worldID}-{cpID}` to inspect claude session
- [ ] `tmux ls` shows active sessions with correct naming
- [ ] Chat messages sent by one user appear for all SSE subscribers
