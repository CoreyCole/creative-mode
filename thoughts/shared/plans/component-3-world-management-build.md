# Component 3: World Management + Build Pipeline

## Overview

Implement world creation, checkpoint forking with build cache preservation, the Trunk/cargo build pipeline, game server lifecycle management with reference counting, port allocation, and prompt rate limiting.

**Dependencies**: Component 1 (harness server + DB layer)
**Depends on this**: Components 5, 6, 7

## Directory Layout

```
harness/internal/
├── world/
│   ├── manager.go          # World creation, checkpoint forking, user positions
│   ├── game_server.go      # Game server lifecycle (start/stop/ref-count)
│   ├── ports.go            # Port allocator for game servers (9001-9999)
│   └── rate_limit.go       # Prompt rate limiting (one build per user, cooldown)
├── build/
│   └── builder.go          # Build pipeline (cargo + Trunk), JSONL log capture
scripts/
└── build-game.sh           # Optional: standalone build script
```

Also ensures these data directories exist at runtime:
```
data/
├── worlds/
│   └── {world-id}/
│       └── {checkpoint-id}/    # Full Rust project copy
├── wasm-builds/                # Trunk dist output
│   └── {world-id}/
│       └── {checkpoint-id}/
│           ├── index.html
│           ├── client-{hash}_bg.wasm
│           └── client-{hash}.js
└── logs/
    └── worlds/
        └── {world-id}/
            └── {checkpoint-id}/
                ├── claude.jsonl
                ├── build.jsonl
                └── game-server.jsonl
```

## Implementation Details

### World Manager (`harness/internal/world/manager.go`)

```go
package world

type Manager struct {
    db           *db.DB
    logger       *slog.Logger
    dataDir      string          // "../data"
    templateDir  string          // "../template"
    builder      *build.Builder
    gameServers  *GameServerManager
}

func NewManager(database *db.DB, logger *slog.Logger, dataDir, templateDir string) *Manager {
    return &Manager{
        db:          database,
        logger:      logger,
        dataDir:     dataDir,
        templateDir: templateDir,
        gameServers: NewGameServerManager(logger),
    }
}
```

#### Core Operations

**`CreateWorld(name, description, userID) (*World, error)`**
1. Generate world ID: `uuid.New().String()[:8]`
2. Create root checkpoint ID: `uuid.New().String()[:8]`
3. Create world directory: `data/worlds/{worldID}/{cpID}/`
4. Copy template: `rsync -a --exclude=target template/ data/worlds/{worldID}/{cpID}/`
5. Clone pre-built target directory (see Build Cache below)
6. Insert world record in DB
7. Insert root checkpoint record (status: `"ready"`, no parent, no prompt)
8. Set user position to root checkpoint
9. Trigger initial build: `go builder.Build(checkpoint, true)`
10. Return world

**`ForkCheckpoint(worldID, sourceCPID, prompt, userID) (*Checkpoint, error)`**
1. Check rate limits (see Rate Limiting below) — return error if blocked
2. Generate new checkpoint ID: `uuid.New().String()[:8]`
3. Get source checkpoint's directory
4. Create new directory: `data/worlds/{worldID}/{newCPID}/`
5. Copy source files (excluding target/): `rsync -a --exclude=target sourceDir/ newDir/`
6. Clone build cache: `cloneBuildCache(sourceDir, newDir)`
7. Insert checkpoint record (status: `"building"`, parent: sourceCPID, prompt)
8. Insert prompt_history record
9. Return checkpoint (caller triggers claude code session)

```go
func (m *Manager) ForkCheckpoint(worldID, sourceCPID, prompt, userID string) (*Checkpoint, error) {
    // Rate limit check
    if err := m.rateLimiter.Check(userID); err != nil {
        return nil, err
    }

    newID := uuid.New().String()[:8]
    sourceCP, err := m.db.GetCheckpoint(sourceCPID)
    if err != nil {
        return nil, err
    }

    newDir := filepath.Join(m.dataDir, "worlds", worldID, newID)

    // Copy source files (excluding target/)
    if err := exec.Command("rsync", "-a", "--exclude=target",
        sourceCP.DirPath+"/", newDir+"/").Run(); err != nil {
        return nil, fmt.Errorf("copying checkpoint: %w", err)
    }

    // Clone build cache
    if err := m.cloneBuildCache(sourceCP.DirPath, newDir); err != nil {
        m.logger.Warn("failed to clone build cache", "error", err)
        // Non-fatal: build will just take longer
    }

    cp := &db.Checkpoint{
        ID:                 newID,
        WorldID:            worldID,
        ParentCheckpointID: sourceCPID,
        Prompt:             prompt,
        Status:             "building",
        DirPath:            newDir,
        CreatedBy:          userID,
    }
    if err := m.db.CreateCheckpoint(cp); err != nil {
        return nil, err
    }

    m.db.CreatePromptHistory(uuid.New().String()[:8], newID, worldID, userID, prompt)
    m.db.SetUserPosition(userID, worldID, newID)

    return cp, nil
}
```

**Build Cache Cloning:**

```go
// cloneBuildCache hardlinks the target/ directory for instant, space-efficient copies.
// Uses platform-appropriate method:
// - Linux: cp -al (hardlinks)
// - macOS: cp -cR (APFS copy-on-write clones, superior to hardlinks)
func (m *Manager) cloneBuildCache(sourceDir, newDir string) error {
    src := filepath.Join(sourceDir, "target")
    dst := filepath.Join(newDir, "target")

    // Check if source target/ exists
    if _, err := os.Stat(src); os.IsNotExist(err) {
        return nil // No cache to clone
    }

    if runtime.GOOS == "darwin" {
        // macOS APFS: copy-on-write clone (instant, shares blocks until modified)
        return exec.Command("cp", "-cR", src, dst).Run()
    }
    // Linux: hardlink clone
    return exec.Command("cp", "-al", src, dst).Run()
}
```

**Other methods:**
- `GetCheckpointTree(worldID) ([]Checkpoint, error)` — delegates to `db.GetCheckpointTree()`
- `GetUserPosition(userID, worldID) (string, error)` — delegates to `db.GetUserPosition()`
- `SetUserPosition(userID, worldID, cpID) error` — delegates to `db.SetUserPosition()`

### Build Pipeline (`harness/internal/build/builder.go`)

```go
package build

const (
    BuildTimeoutIncremental = 5 * time.Minute
    BuildTimeoutInitial     = 15 * time.Minute
)

type Builder struct {
    db              *db.DB
    logger          *slog.Logger
    wasmBuildsDir   string  // "data/wasm-builds"
    logsDir         string  // "data/logs"
    sharedCargoHome string  // shared CARGO_HOME for all builds
}
```

**`Build(cp *Checkpoint, isInitial bool) error`:**

1. Set build timeout (5min incremental, 15min initial)
2. Create WASM output directory: `data/wasm-builds/{worldID}/{cpID}/`
3. Create log directory: `data/logs/worlds/{worldID}/{cpID}/`
4. **Step 1**: Build game server (native binary)
   ```
   cargo build --release -p server
   ```
   - Working dir: checkpoint directory
   - Env: `CARGO_HOME={sharedCargoHome}`
   - Capture stdout/stderr as JSONL to `build.jsonl`
5. **Step 2**: Build game client (WASM via Trunk)
   ```
   trunk build --release --dist {wasmBuildsDir}/{worldID}/{cpID}/
   ```
   - Working dir: `{checkpointDir}/client`
   - Capture stdout/stderr as JSONL to `build.jsonl`
6. If build fails and timeout exceeded, return timeout error
7. If build fails otherwise, return build error with output

```go
func (b *Builder) Build(cp *db.Checkpoint, isInitial bool) error {
    timeout := BuildTimeoutIncremental
    if isInitial {
        timeout = BuildTimeoutInitial
    }
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    startTime := time.Now()
    wasmDir := filepath.Join(b.wasmBuildsDir, cp.WorldID, cp.ID)
    os.MkdirAll(wasmDir, 0755)

    logDir := filepath.Join(b.logsDir, "worlds", cp.WorldID, cp.ID)
    os.MkdirAll(logDir, 0755)
    buildLogPath := filepath.Join(logDir, "build.jsonl")
    buildLog, _ := os.Create(buildLogPath)
    defer buildLog.Close()

    writer := &jsonlLineWriter{
        file: buildLog, worldID: cp.WorldID, cpID: cp.ID, event: "build.output",
    }

    // Step 1: Build game server
    serverCmd := exec.CommandContext(ctx, "cargo", "build", "--release", "-p", "server")
    serverCmd.Dir = cp.DirPath
    serverCmd.Env = append(os.Environ(), "CARGO_HOME="+b.sharedCargoHome)
    serverCmd.Stdout = writer
    serverCmd.Stderr = writer
    if err := serverCmd.Run(); err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("server build timed out after %v", timeout)
        }
        return fmt.Errorf("server build failed: %w", err)
    }

    // Step 2: Build game client (WASM via Trunk)
    clientCmd := exec.CommandContext(ctx, "trunk", "build", "--release", "--dist", wasmDir)
    clientCmd.Dir = filepath.Join(cp.DirPath, "client")
    clientCmd.Stdout = writer
    clientCmd.Stderr = writer
    if err := clientCmd.Run(); err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("client build timed out after %v", timeout)
        }
        return fmt.Errorf("client build failed: %w", err)
    }

    buildDuration := time.Since(startTime).Milliseconds()
    cp.BuildDurationMs = buildDuration
    cp.WasmPath = wasmDir
    return nil
}
```

**`PostBuild(cp *Checkpoint)`** — called after successful build:
```go
func (b *Builder) PostBuild(cp *db.Checkpoint) {
    // Read Claude's summary from CHANGES.txt
    changesPath := filepath.Join(cp.DirPath, "CHANGES.txt")
    if summary, err := os.ReadFile(changesPath); err == nil {
        cp.WorkSummary = string(summary)
    }

    // Auto-generate file list from claude.jsonl
    cp.FilesChanged = parseEditedFiles(filepath.Join(
        b.logsDir, "worlds", cp.WorldID, cp.ID, "claude.jsonl"))

    b.db.UpdateCheckpointSummary(cp.ID, cp.WorkSummary, cp.FilesChanged, cp.BuildDurationMs)
}

// parseEditedFiles reads claude.jsonl and extracts unique file paths from Edit/Write tool uses
func parseEditedFiles(claudeLogPath string) string {
    // Read file, parse each line as JSON, collect unique files from
    // events where event contains "claude.tool_use" and tool is "Edit" or "Write"
    // Return as JSON array string: '["client/src/main.rs", "shared/src/lib.rs"]'
}
```

**JSONL Line Writer:**
```go
type jsonlLineWriter struct {
    file    *os.File
    worldID string
    cpID    string
    event   string
}

func (w *jsonlLineWriter) Write(p []byte) (n int, err error) {
    lines := strings.Split(string(p), "\n")
    for _, line := range lines {
        if line == "" {
            continue
        }
        entry, _ := json.Marshal(map[string]any{
            "ts":      time.Now().UTC().Format(time.RFC3339),
            "level":   "info",
            "event":   w.event,
            "worldID": w.worldID,
            "cpID":    w.cpID,
            "line":    line,
        })
        w.file.Write(append(entry, '\n'))
    }
    return len(p), nil
}
```

### Game Server Lifecycle (`harness/internal/world/game_server.go`)

Reference-counted game server management. Multiple users can be on different checkpoints; each active checkpoint needs a running game server.

```go
type GameServer struct {
    Cmd    *exec.Cmd
    Port   int
    WorldID string
    CPID    string
}

type GameServerManager struct {
    mu       sync.Mutex
    servers  map[string]*GameServer  // key: "{worldID}/{cpID}"
    refCount map[string]int
    ports    *PortAllocator
    logger   *slog.Logger
    logsDir  string
}

func (m *GameServerManager) Connect(worldID, cpID, checkpointDir string) (*GameServer, error) {
    key := worldID + "/" + cpID
    m.mu.Lock()
    defer m.mu.Unlock()

    if srv, ok := m.servers[key]; ok {
        m.refCount[key]++
        return srv, nil
    }

    // Start new game server
    port, err := m.ports.Allocate()
    if err != nil {
        return nil, fmt.Errorf("no available ports: %w", err)
    }

    srv, err := m.startServer(worldID, cpID, checkpointDir, port)
    if err != nil {
        m.ports.Release(port)
        return nil, err
    }

    m.servers[key] = srv
    m.refCount[key] = 1
    return srv, nil
}

func (m *GameServerManager) Disconnect(worldID, cpID string) {
    key := worldID + "/" + cpID
    m.mu.Lock()
    defer m.mu.Unlock()

    m.refCount[key]--
    if m.refCount[key] <= 0 {
        // Grace period before stopping (2 minutes)
        go m.stopAfterDelay(key, 2*time.Minute)
    }
}

func (m *GameServerManager) startServer(worldID, cpID, checkpointDir string, port int) (*GameServer, error) {
    serverBin := filepath.Join(checkpointDir, "target", "release", "server")

    logDir := filepath.Join(m.logsDir, "worlds", worldID, cpID)
    os.MkdirAll(logDir, 0755)
    logFile, _ := os.Create(filepath.Join(logDir, "game-server.jsonl"))

    writer := &jsonlLineWriter{
        file: logFile, worldID: worldID, cpID: cpID, event: "game_server.output",
    }

    cmd := exec.Command(serverBin)
    cmd.Env = append(os.Environ(), fmt.Sprintf("GAME_PORT=%d", port))
    cmd.Stdout = writer
    cmd.Stderr = writer

    if err := cmd.Start(); err != nil {
        return nil, err
    }

    srv := &GameServer{Cmd: cmd, Port: port, WorldID: worldID, CPID: cpID}

    // Monitor for crashes
    go func() {
        if err := cmd.Wait(); err != nil {
            m.logger.Error("game server crashed", "worldID", worldID, "cpID", cpID, "error", err)
        }
    }()

    return srv, nil
}

func (m *GameServerManager) stopAfterDelay(key string, delay time.Duration) {
    time.Sleep(delay)
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.refCount[key] <= 0 {
        if srv, ok := m.servers[key]; ok {
            srv.Cmd.Process.Kill()
            m.ports.Release(srv.Port)
            delete(m.servers, key)
            delete(m.refCount, key)
        }
    }
}

// Shutdown stops all running game servers (called on harness shutdown)
func (m *GameServerManager) Shutdown() {
    m.mu.Lock()
    defer m.mu.Unlock()
    for key, srv := range m.servers {
        srv.Cmd.Process.Kill()
        m.ports.Release(srv.Port)
        delete(m.servers, key)
    }
}
```

### Port Allocator (`harness/internal/world/ports.go`)

```go
type PortAllocator struct {
    mu       sync.Mutex
    inUse    map[int]bool
    minPort  int  // 9001
    maxPort  int  // 9999
}

func NewPortAllocator() *PortAllocator {
    return &PortAllocator{
        inUse:   make(map[int]bool),
        minPort: 9001,
        maxPort: 9999,
    }
}

func (p *PortAllocator) Allocate() (int, error) {
    p.mu.Lock()
    defer p.mu.Unlock()
    for port := p.minPort; port <= p.maxPort; port++ {
        if !p.inUse[port] {
            p.inUse[port] = true
            return port, nil
        }
    }
    return 0, fmt.Errorf("no available ports in range %d-%d", p.minPort, p.maxPort)
}

func (p *PortAllocator) Release(port int) {
    p.mu.Lock()
    defer p.mu.Unlock()
    delete(p.inUse, port)
}
```

### Rate Limiting (`harness/internal/world/rate_limit.go`)

```go
type RateLimiter struct {
    db       *db.DB
    mu       sync.Mutex
    lastSubmit map[string]time.Time  // userID → last prompt submission time
    cooldown   time.Duration          // 30 seconds
    maxCPPerWorld int                  // 50
}

func (r *RateLimiter) Check(userID string) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    // Check cooldown
    if last, ok := r.lastSubmit[userID]; ok {
        remaining := r.cooldown - time.Since(last)
        if remaining > 0 {
            return &RateLimitError{
                Message:       "Please wait before submitting another prompt",
                RetryAfterSec: int(remaining.Seconds()),
            }
        }
    }

    // Check if user has an active build
    // Query checkpoints where created_by = userID AND status = 'building'
    activeBuilds, _ := r.db.CountActiveBuilds(userID)
    if activeBuilds > 0 {
        return &RateLimitError{
            Message: "You already have a build in progress",
        }
    }

    r.lastSubmit[userID] = time.Now()
    return nil
}

type RateLimitError struct {
    Message       string
    RetryAfterSec int
}

func (e *RateLimitError) Error() string { return e.Message }
```

Add to DB queries: `CountActiveBuilds(userID string) (int, error)` — counts checkpoints where `created_by = userID AND status = 'building'`.

### Route Registration

These routes are registered on the `approved` group (requires auth from Component 2):

```go
w := approved.Group("/world")
w.POST("/create", s.handleCreateWorld)
w.GET("/:worldID", s.handleWorldView)
w.GET("/:worldID/checkpoint/:cpID", s.handleCheckpointView)
w.POST("/:worldID/prompt", s.handlePrompt)
w.POST("/:worldID/checkpoint", s.handleSaveCheckpoint)
w.GET("/:worldID/checkpoint/:cpID/logs/:logType", s.handleLogStream)

// WASM artifacts
approved.GET("/wasm/:worldID/:cpID/*", s.handleWASMArtifacts)
```

**Handler implementations:**

- `handleCreateWorld`: creates world via manager, returns redirect to world view
- `handleWorldView`: reads user position, serves world page (Component 6 provides the template)
- `handleCheckpointView`: updates user position, serves checkpoint view
- `handlePrompt`: calls `manager.ForkCheckpoint()`, returns 429 on rate limit, otherwise starts claude session (Component 5)
- `handleSaveCheckpoint`: names/bookmarks current checkpoint
- `handleLogStream`: streams JSONL log content for a checkpoint
- `handleWASMArtifacts`: serves static files from `data/wasm-builds/{worldID}/{cpID}/`

## Interface Contract

This component provides to other components:

1. **`WorldManager`** — Component 5 calls `ForkCheckpoint()` in the prompt-to-build pipeline
2. **`Builder`** — Component 5 calls `Build()` after claude finishes editing
3. **`GameServerManager`** — Component 6 uses `Connect()`/`Disconnect()` for SSE connection lifecycle
4. **`Builder.PostBuild()`** — Component 5 calls this after build to extract work summaries
5. **WASM artifact serving** — Component 6 references `/wasm/{worldID}/{cpID}/index.html` in iframe URLs
6. **Checkpoint directories** — Component 5 runs claude code in these directories

## Success Criteria

### Automated Verification
- [ ] `POST /world/create` creates a world directory with template files
- [ ] Template directory is correctly copied (excluding target/)
- [ ] Build cache is cloned (hardlinks on Linux, cp -cR on macOS)
- [ ] Fork creates new directory with cloned build cache
- [ ] Build pipeline produces `index.html`, `.wasm`, `.js` in wasm-builds directory
- [ ] Game server starts on allocated port
- [ ] Game server stops after disconnect + grace period
- [ ] Port allocator doesn't assign duplicate ports
- [ ] Rate limiter rejects concurrent builds from same user
- [ ] Rate limiter enforces 30-second cooldown
- [ ] Build logs are written as JSONL

### Manual Verification
- [ ] Create world -> initial build completes -> WASM loads in browser
- [ ] Fork checkpoint -> new build completes -> can switch between versions
- [ ] Build timeout fires for pathologically long builds
- [ ] `data/logs/worlds/{worldID}/{cpID}/build.jsonl` has structured entries
