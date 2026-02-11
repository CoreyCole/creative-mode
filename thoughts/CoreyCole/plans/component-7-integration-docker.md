# Component 7: End-to-End Integration + Docker

## Overview

Wire all components together, create the Docker environment, setup script, pre-build template dependencies, and validate the end-to-end flow. This is the final integration pass that ensures everything works together.

**Dependencies**: All other components (1-6) must be substantially complete
**Depends on this**: Nothing (final component)

## Directory Layout

```
Dockerfile                      # Ubuntu 24.04 build/runtime image
docker-compose.yml              # Local dev: harness + tmux + game servers
.dockerignore                   # Exclude .git, data/, target/, etc.
scripts/
├── setup.sh                    # Install deps + pre-build template
└── build-game.sh               # Standalone build script (optional)
```

## Implementation Details

### Dockerfile

```dockerfile
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TERM=xterm-256color

# System dependencies for Bevy native (headless server) + WASM builds
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    git \
    pkg-config \
    libssl-dev \
    libasound2-dev \
    libudev-dev \
    libx11-dev \
    libxkbcommon-x11-0 \
    tmux \
    jq \
    sqlite3 \
    && rm -rf /var/lib/apt/lists/*

# Rust
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"
RUN rustup target add wasm32-unknown-unknown

# Trunk (pre-built binary)
ARG TRUNK_VERSION=0.21.14
RUN curl -L "https://github.com/trunk-rs/trunk/releases/download/v${TRUNK_VERSION}/trunk-x86_64-unknown-linux-gnu.tar.gz" \
    -o /tmp/trunk.tar.gz && \
    tar -xzf /tmp/trunk.tar.gz -C /usr/local/bin && \
    rm /tmp/trunk.tar.gz

# Go
ARG GO_VERSION=1.23.6
RUN curl -L "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -xz -C /usr/local
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Claude Code CLI
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g @anthropic-ai/claude-code@latest

WORKDIR /app
```

### docker-compose.yml

```yaml
services:
  creative-mode:
    build: .
    ports:
      - "8080:8080"            # Harness server
      - "9001-9020:9001-9020"  # Game server port range
    volumes:
      - .:/app
      - cargo-registry:/root/.cargo/registry
      - cargo-git:/root/.cargo/git
      - template-target:/app/template/target
    environment:
      - GITHUB_CLIENT_ID
      - GITHUB_CLIENT_SECRET
      - ANTHROPIC_API_KEY
      - HARNESS_URL=http://localhost:8080
    stdin_open: true
    tty: true  # Required for tmux

volumes:
  cargo-registry:
  cargo-git:
  template-target:
```

> **Note**: `libasound2-dev` retains its name on Ubuntu 24.04. Cargo registry and template
> target are volume-mounted to persist across container restarts and speed up rebuilds.

### .dockerignore

```
.git
data/
template/target/
**/target/
**/dist/
node_modules/
*.db
.env
thoughts/
```

### Setup Script (`scripts/setup.sh`)

```bash
#!/bin/bash
set -e

echo "Setting up Creative Mode..."

# Verify Docker is installed
if ! command -v docker &>/dev/null; then
    echo "Error: Docker is required. Install Docker Desktop for macOS."
    exit 1
fi

# Build the dev image
docker compose build

# Pre-build template dependencies inside the container
echo "Pre-building template dependencies (this may take a few minutes)..."
docker compose run --rm creative-mode bash -c \
    "cd /app/template && cargo build --release -p server && cd client && trunk build --release"

echo ""
echo "Setup complete!"
echo ""
echo "Next steps:"
echo "  1. Create a GitHub OAuth App at https://github.com/settings/developers"
echo "     - Homepage URL: http://localhost:8080"
echo "     - Callback URL: http://localhost:8080/auth/github/callback"
echo "  2. Copy .env.example to .env and fill in your credentials"
echo "  3. Run 'docker compose up' to start"
```

### Integration Wiring

This component ensures all pieces connect correctly in `harness/main.go`:

```go
func main() {
    dataDir := filepath.Join("..", "data")
    templateDir := filepath.Join("..", "template")

    // Ensure data directories exist
    for _, dir := range []string{
        dataDir,
        filepath.Join(dataDir, "logs"),
        filepath.Join(dataDir, "worlds"),
        filepath.Join(dataDir, "wasm-builds"),
        filepath.Join(dataDir, "shared-assets"),
    } {
        os.MkdirAll(dir, 0755)
    }

    // Initialize components
    logger := logging.NewLogger(filepath.Join(dataDir, "logs"))
    database, err := db.New(filepath.Join(dataDir, "creative-mode.db"))
    if err != nil {
        log.Fatal(err)
    }
    defer database.Close()

    eventBus := events.NewEventBus()

    builder := build.NewBuilder(database, logger,
        filepath.Join(dataDir, "wasm-builds"),
        filepath.Join(dataDir, "logs"),
        os.Getenv("CARGO_HOME"),
    )

    worldManager := world.NewManager(database, logger, dataDir, templateDir)

    orchestrator := claude.NewOrchestrator(
        database, logger, worldManager, builder, eventBus,
        filepath.Join(dataDir, "logs"),
        os.Getenv("HARNESS_URL"),
    )

    authCfg := &auth.Config{
        GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
        GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
        BaseURL:            os.Getenv("HARNESS_URL"),
    }

    // Echo server
    e := echo.New()
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    srv := server.New(database, logger, eventBus, worldManager, orchestrator)
    authHandler := auth.NewHandler(database, authCfg)

    // Register all routes
    // Public
    e.Static("/assets", filepath.Join(dataDir, "shared-assets"))
    e.Static("/static", "static")
    e.GET("/health", srv.HandleHealth)
    e.POST("/api/claude-event", srv.HandleClaudeEvent)

    // Auth (no middleware)
    e.GET("/auth/github/login", authHandler.HandleLogin)
    e.GET("/auth/github/callback", authHandler.HandleCallback)
    e.POST("/auth/logout", authHandler.HandleLogout)

    // Authenticated
    authed := e.Group("", auth.SessionMiddleware(database))
    authed.GET("/auth/pending", srv.HandlePendingApproval)

    // Approved users
    approved := e.Group("", auth.SessionMiddleware(database), auth.ApprovedMiddleware())
    approved.GET("/", srv.HandleLobby)
    approved.GET("/events", srv.HandleGlobalSSE)
    approved.POST("/api/chat", srv.HandleChatMessage)

    // World routes
    w := approved.Group("/world")
    w.POST("/create", srv.HandleCreateWorld)
    w.GET("/:worldID", srv.HandleWorldView)
    w.GET("/:worldID/checkpoint/:cpID", srv.HandleCheckpointView)
    w.POST("/:worldID/prompt", srv.HandlePrompt)
    w.POST("/:worldID/checkpoint", srv.HandleSaveCheckpoint)
    w.GET("/:worldID/events", srv.HandleSSEEvents)
    w.GET("/:worldID/lineage/:cpID", srv.HandleLineage)
    w.GET("/:worldID/checkpoint/:cpID/logs/:logType", srv.HandleLogStream)

    // WASM artifacts
    approved.GET("/wasm/:worldID/:cpID/*", srv.HandleWASMArtifacts)

    // Admin
    admin := e.Group("/admin", auth.SessionMiddleware(database), auth.AdminMiddleware())
    admin.GET("/users", srv.HandleAdminUsers)
    admin.POST("/users/:userID/approve", srv.HandleApproveUser)
    admin.POST("/users/:userID/reject", srv.HandleRejectUser)

    // Graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    go func() {
        <-ctx.Done()
        logger.Info("Shutting down...")
        worldManager.GameServers().Shutdown()
        // Kill all tmux sessions
        exec.Command("tmux", "kill-server").Run()
        e.Shutdown(context.Background())
    }()

    logger.Info("Harness server starting on :8080")
    e.Logger.Fatal(e.Start(":8080"))
}
```

### Multi-User Position Tracking

Ensure these handlers correctly manage user positions:

- `handleWorldView`: reads user position from `user_positions`, defaults to root checkpoint
- `handleCheckpointView`: updates user position
- `handlePrompt`: forks from user's current checkpoint, records `user_id` in `prompt_history`
- SSE connections track connected users for player count display

### Pre-Build Template Dependencies

Part of setup: the initial `cargo build` in the template directory compiles all Bevy/Lightyear dependencies once. When new worlds are created, they copy this pre-built `target/` directory so the first incremental build is fast.

The docker-compose volume `template-target` persists the template's `target/` directory across container restarts.

### Environment Variables

Create a `.env.example`:
```
GITHUB_CLIENT_ID=your_github_client_id
GITHUB_CLIENT_SECRET=your_github_client_secret
ANTHROPIC_API_KEY=your_anthropic_api_key
HARNESS_URL=http://localhost:8080
```

## Testing Checklist

### Full End-to-End Manual Testing (22 steps)

1. Start harness: `docker compose up`
2. Open browser to `http://localhost:8080`
3. Sign in with GitHub (first user becomes admin)
4. Create "Test World" from lobby
5. Wait for initial build to complete
6. Fly around with WASD + mouse, verify fly camera works
7. Open second browser tab, verify both players visible as pill meshes
8. Submit prompt: "change the ground color to blue"
9. Watch build progress in status bar
10. Verify "Building..." notification appears in chat log
11. Verify `build.completed` notification appears with [Play] button
12. Click [Play] -> verify ground changes to blue
13. Send a chat message -> verify it appears in both tabs
14. Save checkpoint "blue ground"
15. Submit prompt: "add 10 randomly placed red cubes"
16. Minimize the overlay -> verify floating button appears
17. Verify unread badge increments as build progresses and completes
18. Expand overlay -> verify badge resets, chat log shows full history
19. Click [Play] on the build notification -> verify cubes appear
20. Go back to "blue ground" checkpoint via checkpoint tree
21. Create a second world, verify build notifications from first world appear
22. Click cross-world [Play] -> verify full page navigation to other world

### Infrastructure Verification

- [ ] `docker compose build` succeeds
- [ ] `docker compose up` starts harness on :8080
- [ ] Template pre-build completes inside container
- [ ] Game server ports (9001-9020) are accessible from host
- [ ] `tmux ls` inside container shows active sessions
- [ ] `tmux attach -t cm-{world}-{cp}` allows inspecting claude session
- [ ] MEMORY.md accumulates design decisions across prompts
- [ ] Graceful shutdown kills game servers and tmux sessions
- [ ] SQLite database survives container restarts (if volume-mounted)

### Known Edge Cases to Verify

- User disconnects during build -> build still completes, notification appears on reconnect
- Two users fork from the same checkpoint simultaneously -> both builds proceed independently
- Build fails -> status shows "failed", user can retry with new prompt
- Game server crashes -> logged to JSONL, no cascade failure
- Rate limiter prevents spam (30s cooldown, one active build per user)
- Cross-world navigation from notification works (full page load)

## Deferred Items (From Staff Review)

These were deprioritized and can be addressed as follow-ups:

1. **Shared CARGO_HOME race conditions** (review concern #3): Test concurrent builds sharing CARGO_HOME. Cargo uses file locks, so it may work. Consider sccache if issues arise.
2. **Input sanitization** (review concern #4): World/checkpoint names use UUID-based IDs for filesystem/tmux, so this is low risk. Sanitize display names if needed.
3. **MEMORY.md per-checkpoint semantics** (review concern #8): MEMORY.md diverges at fork points (like code). This is correct behavior for the fork model.
4. **Claude Code hook payload format** (review concern #9): Verify actual hook JSON schema against what the scripts parse. Test with real Claude Code sessions.
5. **Game server health check** (review concern #10): Currently just checks if process is alive. Could add periodic heartbeats to JSONL log.

## Nice-to-Have Improvements (From Staff Review Suggestions)

1. `just verify` command — checks all prerequisites are installed
2. `wasm-opt -Oz` + brotli compression for WASM artifacts
3. Playground mode without GitHub OAuth (`HARNESS_DEV_MODE=true`)
4. Structured Bevy logging (JSON formatter via `LogPlugin`)
5. `claude --output-format stream-json` for richer observability
6. APFS clonefile on macOS (already implemented in Component 3 via `cp -cR`)
