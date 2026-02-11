package build

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"creative-mode/harness/internal/db"
)

const (
	BuildTimeoutIncremental = 5 * time.Minute
	BuildTimeoutInitial     = 15 * time.Minute
)

// Builder manages the cargo + Trunk build pipeline.
type Builder struct {
	db            *db.DB
	logger        *slog.Logger
	wasmBuildsDir string // data/wasm-builds
	logsDir       string // data/logs
}

// NewBuilder creates a new builder.
func NewBuilder(database *db.DB, logger *slog.Logger, wasmBuildsDir, logsDir string) *Builder {
	return &Builder{
		db:            database,
		logger:        logger,
		wasmBuildsDir: wasmBuildsDir,
		logsDir:       logsDir,
	}
}

// Build runs the cargo build for the game server and Trunk build for the WASM
// client. Updates the checkpoint's WasmPath and BuildDurationMs on success.
func (b *Builder) Build(cp *db.Checkpoint, isInitial bool) error {
	timeout := BuildTimeoutIncremental
	if isInitial {
		timeout = BuildTimeoutInitial
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	startTime := time.Now()

	wasmDir := filepath.Join(b.wasmBuildsDir, cp.WorldID, cp.ID)
	if err := os.MkdirAll(wasmDir, 0755); err != nil {
		return fmt.Errorf("creating wasm dir: %w", err)
	}

	logDir := filepath.Join(b.logsDir, "worlds", cp.WorldID, cp.ID)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}

	buildLogPath := filepath.Join(logDir, "build.jsonl")
	buildLog, err := os.Create(buildLogPath)
	if err != nil {
		return fmt.Errorf("creating build log: %w", err)
	}
	defer buildLog.Close()

	writer := &jsonlLineWriter{
		file:    buildLog,
		worldID: cp.WorldID,
		cpID:    cp.ID,
		event:   "build.output",
	}

	// Step 1: Build game server (native binary).
	b.logger.Info("building game server", "worldID", cp.WorldID, "cpID", cp.ID)
	serverCmd := exec.CommandContext(ctx, "cargo", "build", "--release", "-p", "server")
	serverCmd.Dir = cp.DirPath
	serverCmd.Stdout = writer
	serverCmd.Stderr = writer
	if err := serverCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("server build timed out after %v", timeout)
		}
		return fmt.Errorf("server build failed: %w", err)
	}

	// Step 2: Build game client (WASM via Trunk).
	b.logger.Info("building WASM client", "worldID", cp.WorldID, "cpID", cp.ID)
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
	cp.BuildDurationMs.Int64 = buildDuration
	cp.BuildDurationMs.Valid = true
	cp.WasmPath.String = wasmDir
	cp.WasmPath.Valid = true

	b.logger.Info("build complete", "worldID", cp.WorldID, "cpID", cp.ID, "durationMs", buildDuration)
	return nil
}

// PostBuild extracts work summaries and file change lists after a successful build.
func (b *Builder) PostBuild(cp *db.Checkpoint) {
	// Read Claude's summary from CHANGES.txt if it exists.
	changesPath := filepath.Join(cp.DirPath, "CHANGES.txt")
	var workSummary string
	if summary, err := os.ReadFile(changesPath); err == nil {
		workSummary = string(summary)
	}

	// Parse edited files from claude.jsonl.
	claudeLogPath := filepath.Join(b.logsDir, "worlds", cp.WorldID, cp.ID, "claude.jsonl")
	filesChanged := parseEditedFiles(claudeLogPath)

	if workSummary != "" || filesChanged != "" {
		_ = b.db.UpdateCheckpointSummary(cp.ID, workSummary, filesChanged, cp.BuildDurationMs.Int64)
	}
}

// parseEditedFiles reads claude.jsonl and extracts unique file paths from
// Edit/Write tool uses. Returns a JSON array string.
func parseEditedFiles(claudeLogPath string) string {
	f, err := os.Open(claudeLogPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	seen := make(map[string]bool)
	var files []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		event, _ := entry["event"].(string)
		if !strings.Contains(event, "claude.tool_use") {
			continue
		}

		tool, _ := entry["tool"].(string)
		if tool != "Edit" && tool != "Write" {
			continue
		}

		input, _ := entry["input"].(map[string]any)
		if input == nil {
			continue
		}

		filePath, _ := input["file_path"].(string)
		if filePath == "" {
			continue
		}

		if !seen[filePath] {
			seen[filePath] = true
			files = append(files, filePath)
		}
	}

	if len(files) == 0 {
		return ""
	}

	result, _ := json.Marshal(files)
	return string(result)
}

// jsonlLineWriter wraps output into structured JSONL entries.
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
		_, _ = w.file.Write(append(entry, '\n'))
	}
	return len(p), nil
}
