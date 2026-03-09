package claude

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"creative-mode/harness/internal/builder"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/tmux"
	"creative-mode/harness/internal/world"
)

const (
	truncatePromptLen = 100
	promptSnippetLen  = 60

	// claudeSessionParts is the number of hyphen-separated parts in a
	// Claude session name: cm-{worldID}-{cpID}.
	claudeSessionParts = 3
)

// BuildCompleteFunc is a callback invoked after a build completes or fails.
// Used to notify external systems (e.g., Discord via MayorManager).
type BuildCompleteFunc func(worldID, cpID string, success bool, summary string)

// Orchestrator manages the prompt-to-build pipeline: forking checkpoints,
// launching Claude Code in tmux sessions, triggering builds on completion,
// and publishing events through the EventBus.
type Orchestrator struct {
	db              *db.DB
	logger          *slog.Logger
	worldManager    *world.Manager
	bld             *builder.Builder
	eventBus        *events.EventBus
	logsDir         string
	harnessURL      string
	OnBuildComplete BuildCompleteFunc // optional callback for build notifications
}

// NewOrchestrator creates a new Claude Code orchestrator.
func NewOrchestrator(
	database *db.DB,
	logger *slog.Logger,
	worldManager *world.Manager,
	bld *builder.Builder,
	eventBus *events.EventBus,
	logsDir string,
	harnessURL string,
) *Orchestrator {
	return &Orchestrator{
		db:           database,
		logger:       logger,
		worldManager: worldManager,
		bld:          bld,
		eventBus:     eventBus,
		logsDir:      logsDir,
		harnessURL:   harnessURL,
	}
}

// HandlePrompt forks a checkpoint, updates MEMORY.md, starts a dev server,
// creates a tmux session, and sends the prompt to Claude Code. Returns
// immediately — hooks drive the rest of the pipeline asynchronously.
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

	// Look up world template type.
	w, wErr := o.db.GetWorld(ctx, worldID)
	templateType := sqlc.TemplateType3D
	if wErr == nil {
		templateType = w.TemplateType
	}

	// Start dev server (cargo watch) — non-fatal if it fails. Only for 3D.
	var extraEnv []string
	if templateType == sqlc.TemplateType3D {
		devSrv, devErr := o.worldManager.GameServers.ConnectDev(
			worldID,
			cp.ID,
			cp.DirPath,
		)
		if devErr != nil {
			o.logger.Warn("failed to start dev server",
				"worldID", worldID, "cpID", cp.ID, "error", devErr)
		} else {
			extraEnv = append(extraEnv,
				fmt.Sprintf("CM_GAME_PORT=%d", devSrv.Port),
				fmt.Sprintf("CM_BRP_PORT=%d", devSrv.BRPPort),
			)
		}
	}

	// Create tmux session with CM_* env vars (+ dev server ports if available).
	session := tmux.NewSession(worldID, cp.ID, cp.DirPath)
	createErr := session.Create(
		ctx, worldID, cp.ID, o.logsDir, o.harnessURL, extraEnv...,
	)
	if createErr != nil {
		o.worldManager.GameServers.Disconnect(worldID, cp.ID)
		o.markCheckpointFailed(ctx, cp.ID, "tmux session creation failed")
		return nil, fmt.Errorf("creating tmux session: %w", createErr)
	}

	// Send prompt via --input-file.
	if err := session.SendPrompt(ctx, prompt); err != nil {
		_ = session.Kill()
		o.worldManager.GameServers.Disconnect(worldID, cp.ID)
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

	// Kill dev server for this specific checkpoint (Claude is done editing).
	// Use targeted Disconnect to avoid killing the active prod server.
	o.worldManager.GameServers.Disconnect(worldID, cpID)

	// Kill the Claude tmux session (it's idle now).
	claudeSession := tmux.NewSession(worldID, cpID, "")
	_ = claudeSession.Kill()

	cp, err := o.db.GetCheckpoint(ctx, cpID)
	if err != nil {
		o.logger.Error("checkpoint not found for build", "cpID", cpID, "error", err)

		return
	}

	// Look up world template type.
	w, wErr := o.db.GetWorld(ctx, worldID)
	templateType := sqlc.TemplateType3D
	if wErr == nil {
		templateType = w.TemplateType
	}

	worldName := o.worldName(ctx, worldID)

	// Notify: build started.
	o.createAndPublishMessage(ctx, events.EventBuildStarted, worldID, cpID,
		fmt.Sprintf("Building in %s...", worldName))

	// Build (server binary + WASM client).
	isInitial := !cp.ParentCheckpointID.Valid
	if buildErr := o.bld.Build(&cp, isInitial, string(templateType)); buildErr != nil {
		o.logger.Error("build failed", "cpID", cpID, "error", buildErr)
		_, _ = o.db.UpdateCheckpointStatus(ctx, sqlc.UpdateCheckpointStatusParams{
			Status:   sqlc.CheckpointStatusFailed,
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

		if o.OnBuildComplete != nil {
			o.OnBuildComplete(worldID, cpID, false, buildErr.Error())
		}

		return
	}

	// Post-build: extract work summary and files changed.
	o.bld.PostBuild(&cp)

	// Update status to ready.
	_, _ = o.db.UpdateCheckpointStatus(ctx, sqlc.UpdateCheckpointStatusParams{
		Status: sqlc.CheckpointStatusReady,
		ID:     cpID,
	})
	if cp.WasmPath.Valid {
		_ = o.db.UpdateCheckpointWasmPath(ctx, sqlc.UpdateCheckpointWasmPathParams{
			WasmPath: cp.WasmPath,
			ID:       cpID,
		})
	}

	// Start game server (3D only).
	var serverPort int
	if templateType == sqlc.TemplateType3D {
		// Stop old game servers for this world before starting the new one.
		o.worldManager.GameServers.StopByWorldExcept(worldID, cpID)

		srv, srvErr := o.worldManager.GameServers.Connect(worldID, cpID, cp.DirPath)
		if srvErr != nil {
			o.logger.Error("failed to start game server", "cpID", cpID, "error", srvErr)
		} else {
			serverPort = srv.Port
			_, _ = o.db.UpdateCheckpointServerPort(
				ctx,
				sqlc.UpdateCheckpointServerPortParams{
					ServerPort: sql.NullInt64{Int64: int64(srv.Port), Valid: true},
					ID:         cpID,
				},
			)
		}
	}

	// Notify: build completed.
	promptSnippet := cp.Prompt.String
	if len(promptSnippet) > promptSnippetLen {
		promptSnippet = promptSnippet[:promptSnippetLen] + "..."
	}

	o.createAndPublishMessage(ctx, events.EventBuildCompleted, worldID, cpID,
		fmt.Sprintf("%s checkpoint ready: '%s'", worldName, promptSnippet))

	o.eventBus.Publish(worldID, map[string]any{
		"event":      events.EventBuildCompleted,
		"worldID":    worldID,
		"cpID":       cpID,
		"worldName":  worldName,
		"serverPort": serverPort,
	})

	if o.OnBuildComplete != nil {
		o.OnBuildComplete(worldID, cpID, true, promptSnippet)
	}
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

// ReapOrphanedSessions scans tmux for cm-{worldID}-{cpID} Claude sessions
// whose checkpoint is no longer in "building" status, and kills them. Also
// reaps orphaned game server sessions via GameServerManager.
func (o *Orchestrator) ReapOrphanedSessions() {
	// Reap orphaned game server sessions.
	o.worldManager.GameServers.ReapOrphans()

	// Reap orphaned Claude sessions.
	out, err := exec.CommandContext(
		context.Background(),
		"tmux", "list-sessions", "-F", "#{session_name}",
	).Output()
	if err != nil {
		return
	}

	ctx := context.Background()

	for _, line := range strings.Split(
		strings.TrimSpace(string(out)), "\n",
	) {
		// Match cm-{worldID}-{cpID} but NOT cm-server-* or cm-trunk-*.
		if !strings.HasPrefix(line, "cm-") ||
			strings.HasPrefix(line, "cm-server-") ||
			strings.HasPrefix(line, "cm-trunk-") {
			continue
		}

		parts := strings.SplitN(line, "-", claudeSessionParts)
		if len(parts) != claudeSessionParts {
			continue
		}

		cpID := parts[2]

		cp, cpErr := o.db.GetCheckpoint(ctx, cpID)
		if cpErr != nil {
			// Checkpoint deleted from DB — kill the session.
			o.killTmuxSession(line, "checkpoint not in DB")

			continue
		}

		if cp.Status != sqlc.CheckpointStatusBuilding {
			o.killTmuxSession(line, "checkpoint status: "+string(cp.Status))
		}
	}
}

func (o *Orchestrator) killTmuxSession(name, reason string) {
	_ = exec.CommandContext(
		context.Background(),
		"tmux", "kill-session", "-t", name,
	).Run()
	o.logger.Info("reaped orphaned claude session",
		"session", name, "reason", reason)
}

// markCheckpointFailed updates a checkpoint to "failed" status.
func (o *Orchestrator) markCheckpointFailed(ctx context.Context, cpID, reason string) {
	_, _ = o.db.UpdateCheckpointStatus(ctx, sqlc.UpdateCheckpointStatusParams{
		Status:   sqlc.CheckpointStatusFailed,
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
