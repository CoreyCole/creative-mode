package world

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"

	"creative-mode/harness/internal/build"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
)

// Manager handles world creation, checkpoint forking, and user positions.
type Manager struct {
	db          *db.DB
	logger      *slog.Logger
	dataDir     string // absolute path to data/
	templateDir string // absolute path to template/
	Builder     *build.Builder
	GameServers *GameServerManager
	rateLimiter *RateLimiter
}

// NewManager creates a new world manager.
func NewManager(
	database *db.DB,
	logger *slog.Logger,
	dataDir, templateDir string,
) *Manager {
	return &Manager{
		db:          database,
		logger:      logger,
		dataDir:     dataDir,
		templateDir: templateDir,
		Builder: build.NewBuilder(
			database,
			logger,
			filepath.Join(dataDir, "wasm-builds"),
			filepath.Join(dataDir, "logs"),
		),
		GameServers: NewGameServerManager(logger, filepath.Join(dataDir, "logs")),
		rateLimiter: NewRateLimiter(database),
	}
}

// CreateWorld creates a new world by copying the template, inserting DB records,
// and triggering an initial build.
func (m *Manager) CreateWorld(
	ctx context.Context,
	name, description, userID string,
) (*sqlc.World, error) {
	worldID := uuid.New().String()[:8]
	cpID := uuid.New().String()[:8]

	cpDir := filepath.Join(m.dataDir, "worlds", worldID, cpID)
	if err := os.MkdirAll(cpDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating checkpoint directory: %w", err)
	}

	// Copy template (excluding target/).
	rsync := exec.CommandContext( //nolint:gosec // G204: internal command with controlled args
		ctx,
		"rsync",
		"-a",
		"--exclude=target",
		m.templateDir+"/",
		cpDir+"/",
	)
	if out, err := rsync.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copying template: %s: %w", out, err)
	}

	// Clone pre-built target/ if it exists in the template.
	if err := cloneBuildCache(ctx, m.templateDir, cpDir); err != nil {
		m.logger.Warn("failed to clone template build cache", "error", err)
	}

	if err := m.db.CreateWorld(ctx, sqlc.CreateWorldParams{
		ID:          worldID,
		Name:        name,
		Description: sql.NullString{String: description, Valid: description != ""},
		CreatedBy:   sql.NullString{String: userID, Valid: userID != ""},
	}); err != nil {
		return nil, fmt.Errorf("inserting world: %w", err)
	}

	cp := &sqlc.Checkpoint{
		ID:      cpID,
		WorldID: worldID,
		Status:  "ready",
		DirPath: cpDir,
	}
	if err := m.db.CreateCheckpoint(ctx, sqlc.CreateCheckpointParams{
		ID:        cpID,
		WorldID:   worldID,
		Status:    "ready",
		DirPath:   cpDir,
		CreatedBy: sql.NullString{String: userID, Valid: userID != ""},
	}); err != nil {
		return nil, fmt.Errorf("inserting root checkpoint: %w", err)
	}

	if err := m.db.SetUserPosition(ctx, sqlc.SetUserPositionParams{
		UserID:       userID,
		WorldID:      worldID,
		CheckpointID: cpID,
	}); err != nil {
		m.logger.Error("failed to set initial user position", "error", err)
	}

	// Trigger initial build in background.
	go func() {
		bgCtx := context.Background()

		if buildErr := m.Builder.Build(cp, true); buildErr != nil {
			m.logger.Error(
				"initial build failed",
				"worldID",
				worldID,
				"cpID",
				cpID,
				"error",
				buildErr,
			)
			_, _ = m.db.UpdateCheckpointStatus(bgCtx, sqlc.UpdateCheckpointStatusParams{
				Status:   "failed",
				BuildLog: sql.NullString{String: buildErr.Error(), Valid: true},
				ID:       cpID,
			})

			return
		}

		_, _ = m.db.UpdateCheckpointStatus(bgCtx, sqlc.UpdateCheckpointStatusParams{
			Status: "ready",
			ID:     cpID,
		})
		m.Builder.PostBuild(cp)
	}()

	return &sqlc.World{
		ID:          worldID,
		Name:        name,
		Description: sql.NullString{String: description, Valid: description != ""},
		CreatedBy:   sql.NullString{String: userID, Valid: userID != ""},
	}, nil
}

// ForkCheckpoint creates a new checkpoint by copying the source checkpoint's
// project directory and cloning the build cache.
func (m *Manager) ForkCheckpoint(
	ctx context.Context,
	worldID, sourceCPID, prompt, userID string,
) (*sqlc.Checkpoint, error) {
	if err := m.rateLimiter.Check(ctx, userID); err != nil {
		return nil, err
	}

	sourceCP, err := m.db.GetCheckpoint(ctx, sourceCPID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("source checkpoint %s not found", sourceCPID)
		}
		return nil, fmt.Errorf("getting source checkpoint: %w", err)
	}

	newID := uuid.New().String()[:8]
	newDir := filepath.Join(m.dataDir, "worlds", worldID, newID)

	// Copy source files (excluding target/).
	rsync := exec.CommandContext( //nolint:gosec // G204: internal command with controlled args
		ctx,
		"rsync",
		"-a",
		"--exclude=target",
		sourceCP.DirPath+"/",
		newDir+"/",
	)
	if out, err := rsync.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copying checkpoint: %s: %w", out, err)
	}

	// Clone build cache (non-fatal if it fails).
	if err := cloneBuildCache(ctx, sourceCP.DirPath, newDir); err != nil {
		m.logger.Warn("failed to clone build cache", "error", err)
	}

	if err := m.db.CreateCheckpoint(ctx, sqlc.CreateCheckpointParams{
		ID:                 newID,
		WorldID:            worldID,
		ParentCheckpointID: sql.NullString{String: sourceCPID, Valid: true},
		Prompt:             sql.NullString{String: prompt, Valid: prompt != ""},
		Status:             "building",
		DirPath:            newDir,
		CreatedBy:          sql.NullString{String: userID, Valid: userID != ""},
	}); err != nil {
		return nil, fmt.Errorf("inserting checkpoint: %w", err)
	}

	_ = m.db.CreatePromptHistory(ctx, sqlc.CreatePromptHistoryParams{
		ID:           uuid.New().String()[:8],
		CheckpointID: newID,
		WorldID:      worldID,
		UserID:       userID,
		PromptText:   prompt,
	})
	_ = m.db.SetUserPosition(ctx, sqlc.SetUserPositionParams{
		UserID:       userID,
		WorldID:      worldID,
		CheckpointID: newID,
	})

	return &sqlc.Checkpoint{
		ID:      newID,
		WorldID: worldID,
		Status:  "building",
		DirPath: newDir,
	}, nil
}

// GetCheckpointTree returns all checkpoints for a world.
func (m *Manager) GetCheckpointTree(
	ctx context.Context,
	worldID string,
) ([]sqlc.Checkpoint, error) {
	return m.db.GetCheckpointTree(ctx, worldID)
}

// GetUserPosition returns the user's current checkpoint in a world.
func (m *Manager) GetUserPosition(
	ctx context.Context,
	userID, worldID string,
) (string, error) {
	cpID, err := m.db.GetUserPosition(ctx, sqlc.GetUserPositionParams{
		UserID:  userID,
		WorldID: worldID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return cpID, err
}

// SetUserPosition updates the user's current checkpoint in a world.
func (m *Manager) SetUserPosition(
	ctx context.Context,
	userID, worldID, cpID string,
) error {
	return m.db.SetUserPosition(ctx, sqlc.SetUserPositionParams{
		UserID:       userID,
		WorldID:      worldID,
		CheckpointID: cpID,
	})
}

// Shutdown stops all game servers.
func (m *Manager) Shutdown() {
	m.GameServers.Shutdown()
}

// cloneBuildCache copies the target/ directory using platform-appropriate
// methods: cp -cR (APFS clone) on macOS, cp -al (hardlinks) on Linux.
func cloneBuildCache(ctx context.Context, sourceDir, newDir string) error {
	src := filepath.Join(sourceDir, "target")
	dst := filepath.Join(newDir, "target")

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // No cache to clone.
	}

	if runtime.GOOS == "darwin" {
		cmd := exec.CommandContext( //nolint:gosec // G204: internal paths
			ctx,
			"cp",
			"-cR",
			src,
			dst,
		)

		return cmd.Run()
	}

	cmd := exec.CommandContext( //nolint:gosec // G204: internal paths
		ctx,
		"cp",
		"-al",
		src,
		dst,
	)

	return cmd.Run()
}
