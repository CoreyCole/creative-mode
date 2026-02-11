package claude

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"creative-mode/harness/internal/build"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/tmux"
	"creative-mode/harness/internal/world"
)

const (
	truncatePromptLen = 100
	promptSnippetLen  = 60
)

// Orchestrator manages the prompt-to-build pipeline: forking checkpoints,
// launching Claude Code in tmux sessions, triggering builds on completion,
// and publishing events through the EventBus.
type Orchestrator struct {
	db           *db.DB
	logger       *slog.Logger
	worldManager *world.Manager
	builder      *build.Builder
	eventBus     *events.EventBus
	logsDir      string
	harnessURL   string
}

// NewOrchestrator creates a new Claude Code orchestrator.
func NewOrchestrator(
	database *db.DB,
	logger *slog.Logger,
	worldManager *world.Manager,
	builder *build.Builder,
	eventBus *events.EventBus,
	logsDir string,
	harnessURL string,
) *Orchestrator {
	return &Orchestrator{
		db:           database,
		logger:       logger,
		worldManager: worldManager,
		builder:      builder,
		eventBus:     eventBus,
		logsDir:      logsDir,
		harnessURL:   harnessURL,
	}
}

// HandlePrompt forks a checkpoint, updates MEMORY.md, creates a tmux session,
// and sends the prompt to Claude Code. Returns immediately — hooks drive the
// rest of the pipeline asynchronously.
func (o *Orchestrator) HandlePrompt(
	ctx context.Context,
	worldID, sourceCPID, prompt, userID string,
) (*sqlc.Checkpoint, error) {
	// Fork checkpoint (includes rate-limit check).
	cp, err := o.worldManager.ForkCheckpoint(ctx, worldID, sourceCPID, prompt, userID)
	if err != nil {
		return nil, err
	}

	// Update MEMORY.md with prompt context.
	updateMemory(cp.DirPath, prompt)

	// Create tmux session with CM_* env vars.
	session := tmux.NewSession(worldID, cp.ID, cp.DirPath)
	if err := session.Create(ctx, worldID, cp.ID, o.logsDir, o.harnessURL); err != nil {
		o.markCheckpointFailed(ctx, cp.ID, "tmux session creation failed")
		return nil, fmt.Errorf("creating tmux session: %w", err)
	}

	// Send prompt via --input-file.
	if err := session.SendPrompt(ctx, prompt); err != nil {
		_ = session.Kill()
		o.markCheckpointFailed(ctx, cp.ID, "sending prompt failed")
		return nil, fmt.Errorf("sending prompt: %w", err)
	}

	o.logger.Info("claude session started",
		"worldID", worldID, "cpID", cp.ID, "prompt", truncate(prompt, truncatePromptLen))

	return cp, nil
}

// BuildCheckpoint runs the build pipeline for a checkpoint. Called when a
// claude.session_stopped event arrives from a hook script.
func (o *Orchestrator) BuildCheckpoint(worldID, cpID string) {
	ctx := context.Background()

	cp, err := o.db.GetCheckpoint(ctx, cpID)
	if err != nil {
		o.logger.Error("checkpoint not found for build", "cpID", cpID, "error", err)

		return
	}

	worldName := o.worldName(ctx, worldID)

	// Notify: build started.
	o.createAndPublishMessage(ctx, events.EventBuildStarted, worldID, cpID,
		fmt.Sprintf("Building in %s...", worldName))

	// Build (server binary + WASM client).
	isInitial := !cp.ParentCheckpointID.Valid
	if buildErr := o.builder.Build(&cp, isInitial); buildErr != nil {
		o.logger.Error("build failed", "cpID", cpID, "error", buildErr)
		_, _ = o.db.UpdateCheckpointStatus(ctx, sqlc.UpdateCheckpointStatusParams{
			Status:   "failed",
			BuildLog: sql.NullString{String: buildErr.Error(), Valid: true},
			ID:       cpID,
		})

		o.createAndPublishMessage(ctx, events.EventBuildFailed, worldID, cpID,
			"Build failed: "+buildErr.Error())

		o.eventBus.Publish(worldID, map[string]any{
			"event":   events.EventBuildFailed,
			"worldID": worldID,
			"cpID":    cpID,
			"error":   buildErr.Error(),
		})

		return
	}

	// Post-build: extract work summary and files changed.
	o.builder.PostBuild(&cp)

	// Update status to ready.
	_, _ = o.db.UpdateCheckpointStatus(ctx, sqlc.UpdateCheckpointStatusParams{
		Status: "ready",
		ID:     cpID,
	})

	// Start game server.
	srv, err := o.worldManager.GameServers.Connect(worldID, cpID, cp.DirPath)
	if err != nil {
		o.logger.Error("failed to start game server", "cpID", cpID, "error", err)
	} else {
		_, _ = o.db.UpdateCheckpointServerPort(ctx, sqlc.UpdateCheckpointServerPortParams{
			ServerPort: sql.NullInt64{Int64: int64(srv.Port), Valid: true},
			ID:         cpID,
		})
	}

	// Notify: build completed.
	promptSnippet := cp.Prompt.String
	if len(promptSnippet) > promptSnippetLen {
		promptSnippet = promptSnippet[:promptSnippetLen] + "..."
	}

	o.createAndPublishMessage(ctx, events.EventBuildCompleted, worldID, cpID,
		fmt.Sprintf("%s checkpoint ready: '%s'", worldName, promptSnippet))

	o.eventBus.Publish(worldID, map[string]any{
		"event":     events.EventBuildCompleted,
		"worldID":   worldID,
		"cpID":      cpID,
		"worldName": worldName,
	})
}

// createAndPublishMessage persists a message to the DB and publishes it to
// the global event bus.
func (o *Orchestrator) createAndPublishMessage(
	ctx context.Context,
	msgType, worldID, cpID, content string,
) {
	if err := o.db.CreateMessage(ctx, sqlc.CreateMessageParams{
		ID:           uuid.New().String()[:8],
		Type:         msgType,
		WorldID:      sql.NullString{String: worldID, Valid: worldID != ""},
		CheckpointID: sql.NullString{String: cpID, Valid: cpID != ""},
		Content:      content,
	}); err != nil {
		o.logger.Error("failed to persist message", "type", msgType, "error", err)
	}

	o.eventBus.PublishGlobal(map[string]any{
		"event":   msgType,
		"worldID": worldID,
		"cpID":    cpID,
		"content": content,
		"ts":      time.Now().UTC().Format(time.RFC3339),
	})
}

// worldName fetches the display name for a world, falling back to the ID.
func (o *Orchestrator) worldName(ctx context.Context, worldID string) string {
	w, err := o.db.GetWorld(ctx, worldID)
	if err != nil {
		return worldID
	}

	return w.Name
}

// markCheckpointFailed updates a checkpoint to "failed" status.
func (o *Orchestrator) markCheckpointFailed(ctx context.Context, cpID, reason string) {
	_, _ = o.db.UpdateCheckpointStatus(ctx, sqlc.UpdateCheckpointStatusParams{
		Status:   "failed",
		BuildLog: sql.NullString{String: reason, Valid: true},
		ID:       cpID,
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "..."
}
