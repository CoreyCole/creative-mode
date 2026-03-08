package builder

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
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
func NewBuilder(
	database *db.DB,
	logger *slog.Logger,
	wasmBuildsDir, logsDir string,
) *Builder {
	return &Builder{
		db:            database,
		logger:        logger,
		wasmBuildsDir: wasmBuildsDir,
		logsDir:       logsDir,
	}
}

// Build runs the cargo build for the game server and Trunk build for the WASM
// client. Updates the checkpoint's WasmPath and BuildDurationMs on success.
// 2D templates skip the server build and run trunk from the project root.
func (b *Builder) Build(cp *sqlc.Checkpoint, isInitial bool, templateType string) error {
	timeout := BuildTimeoutIncremental
	if isInitial {
		timeout = BuildTimeoutInitial
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	startTime := time.Now()

	wasmDir := filepath.Join(b.wasmBuildsDir, cp.WorldID, cp.ID)
	if err := os.MkdirAll(wasmDir, 0o750); err != nil {
		return fmt.Errorf("creating wasm dir: %w", err)
	}

	logDir := filepath.Join(b.logsDir, "worlds", cp.WorldID, cp.ID)
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}

	buildLogPath := filepath.Join(logDir, "build.jsonl")

	buildLog, err := os.Create(buildLogPath)
	if err != nil {
		return fmt.Errorf("creating build log: %w", err)
	}
	defer func() { _ = buildLog.Close() }()

	writer := &jsonlLineWriter{
		file:    buildLog,
		worldID: cp.WorldID,
		cpID:    cp.ID,
		event:   "build.output",
	}

	// Step 1: Build game server (native binary) — only for 3D templates.
	if templateType == "3d" {
		b.logger.Info("building game server", "worldID", cp.WorldID, "cpID", cp.ID)

		serverCmd := exec.CommandContext(
			ctx,
			"cargo",
			"build",
			"--release",
			"-p",
			"server",
		)
		serverCmd.Dir = cp.DirPath
		serverCmd.Stdout = writer
		serverCmd.Stderr = writer

		if err := serverCmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("server build timed out after %v", timeout)
			}

			return fmt.Errorf("server build failed: %w", err)
		}
	}

	// Step 2: Build game client (WASM via Trunk).
	b.logger.Info("building WASM client", "worldID", cp.WorldID, "cpID", cp.ID)

	// 2D templates are single-crate projects — trunk runs from root.
	// 3D templates have a client/ subdirectory.
	trunkDir := filepath.Join(cp.DirPath, "client")
	if _, statErr := os.Stat(filepath.Join(trunkDir, "index.html")); statErr != nil {
		trunkDir = cp.DirPath
	}

	clientCmd := exec.CommandContext(
		ctx,
		"trunk",
		"build",
		"--release",
		"--dist",
		wasmDir,
	)
	clientCmd.Dir = trunkDir
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

	b.logger.Info(
		"build complete",
		"worldID",
		cp.WorldID,
		"cpID",
		cp.ID,
		"durationMs",
		buildDuration,
	)

	return nil
}

// PostBuild extracts work summaries and file change lists after a successful build.
func (b *Builder) PostBuild(cp *sqlc.Checkpoint) {
	ctx := context.Background()

	// Read Claude's summary from CHANGES.txt if it exists.
	changesPath := filepath.Join(cp.DirPath, "CHANGES.txt")

	var workSummary string

	if summary, readErr := os.ReadFile(
		changesPath,
	); readErr == nil {
		workSummary = string(summary)
	}

	// Parse edited files from claude.jsonl.
	claudeLogPath := filepath.Join(b.logsDir, "worlds", cp.WorldID, cp.ID, "claude.jsonl")
	filesChanged := parseEditedFiles(claudeLogPath)

	if workSummary != "" || filesChanged != "" {
		_, _ = b.db.UpdateCheckpointSummary(ctx, sqlc.UpdateCheckpointSummaryParams{
			WorkSummary: sql.NullString{
				String: workSummary,
				Valid:  workSummary != "",
			},
			FilesChanged: sql.NullString{
				String: filesChanged,
				Valid:  filesChanged != "",
			},
			BuildDurationMs: sql.NullInt64{Int64: cp.BuildDurationMs.Int64, Valid: true},
			ID:              cp.ID,
		})
	}
}

// parseEditedFiles reads claude.jsonl and extracts unique file paths from
// Edit/Write tool uses. Returns a JSON array string.
func parseEditedFiles(claudeLogPath string) string {
	f, openErr := os.Open(claudeLogPath)
	if openErr != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

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

	result, err := json.Marshal(files)
	if err != nil {
		return ""
	}

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

		entry, marshalErr := json.Marshal(map[string]any{
			"ts":      time.Now().UTC().Format(time.RFC3339),
			"level":   "info",
			"event":   w.event,
			"worldID": w.worldID,
			"cpID":    w.cpID,
			"line":    line,
		})
		if marshalErr != nil {
			continue
		}

		_, _ = w.file.Write(append(entry, '\n'))
	}

	return len(p), nil
}
