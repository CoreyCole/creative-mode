package world

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"creative-mode/harness/internal/build"
	"creative-mode/harness/internal/db"

	"github.com/google/uuid"
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
func NewManager(database *db.DB, logger *slog.Logger, dataDir, templateDir string) *Manager {
	return &Manager{
		db:          database,
		logger:      logger,
		dataDir:     dataDir,
		templateDir: templateDir,
		Builder:     build.NewBuilder(database, logger, filepath.Join(dataDir, "wasm-builds"), filepath.Join(dataDir, "logs")),
		GameServers: NewGameServerManager(logger, filepath.Join(dataDir, "logs")),
		rateLimiter: NewRateLimiter(database),
	}
}

// CreateWorld creates a new world by copying the template, inserting DB records,
// and triggering an initial build.
func (m *Manager) CreateWorld(name, description, userID string) (*db.World, error) {
	worldID := uuid.New().String()[:8]
	cpID := uuid.New().String()[:8]

	cpDir := filepath.Join(m.dataDir, "worlds", worldID, cpID)
	if err := os.MkdirAll(cpDir, 0755); err != nil {
		return nil, fmt.Errorf("creating checkpoint directory: %w", err)
	}

	// Copy template (excluding target/).
	rsync := exec.Command("rsync", "-a", "--exclude=target", m.templateDir+"/", cpDir+"/")
	if out, err := rsync.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copying template: %s: %w", out, err)
	}

	// Clone pre-built target/ if it exists in the template.
	if err := cloneBuildCache(m.templateDir, cpDir); err != nil {
		m.logger.Warn("failed to clone template build cache", "error", err)
	}

	w := &db.World{
		ID:          worldID,
		Name:        name,
		Description: sql.NullString{String: description, Valid: description != ""},
		CreatedBy:   sql.NullString{String: userID, Valid: userID != ""},
	}
	if err := m.db.CreateWorld(w); err != nil {
		return nil, fmt.Errorf("inserting world: %w", err)
	}

	cp := &db.Checkpoint{
		ID:      cpID,
		WorldID: worldID,
		Status:  "ready",
		DirPath: cpDir,
		CreatedBy: sql.NullString{String: userID, Valid: userID != ""},
	}
	if err := m.db.CreateCheckpoint(cp); err != nil {
		return nil, fmt.Errorf("inserting root checkpoint: %w", err)
	}

	if err := m.db.SetUserPosition(userID, worldID, cpID); err != nil {
		m.logger.Error("failed to set initial user position", "error", err)
	}

	// Trigger initial build in background.
	go func() {
		if err := m.Builder.Build(cp, true); err != nil {
			m.logger.Error("initial build failed", "worldID", worldID, "cpID", cpID, "error", err)
			_ = m.db.UpdateCheckpointStatus(cpID, "failed", err.Error())
			return
		}
		_ = m.db.UpdateCheckpointStatus(cpID, "ready", "")
		m.Builder.PostBuild(cp)
	}()

	return w, nil
}

// ForkCheckpoint creates a new checkpoint by copying the source checkpoint's
// project directory and cloning the build cache.
func (m *Manager) ForkCheckpoint(worldID, sourceCPID, prompt, userID string) (*db.Checkpoint, error) {
	if err := m.rateLimiter.Check(userID); err != nil {
		return nil, err
	}

	sourceCP, err := m.db.GetCheckpoint(sourceCPID)
	if err != nil {
		return nil, fmt.Errorf("getting source checkpoint: %w", err)
	}
	if sourceCP == nil {
		return nil, fmt.Errorf("source checkpoint %s not found", sourceCPID)
	}

	newID := uuid.New().String()[:8]
	newDir := filepath.Join(m.dataDir, "worlds", worldID, newID)

	// Copy source files (excluding target/).
	rsync := exec.Command("rsync", "-a", "--exclude=target", sourceCP.DirPath+"/", newDir+"/")
	if out, err := rsync.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copying checkpoint: %s: %w", out, err)
	}

	// Clone build cache (non-fatal if it fails).
	if err := cloneBuildCache(sourceCP.DirPath, newDir); err != nil {
		m.logger.Warn("failed to clone build cache", "error", err)
	}

	cp := &db.Checkpoint{
		ID:                 newID,
		WorldID:            worldID,
		ParentCheckpointID: sql.NullString{String: sourceCPID, Valid: true},
		Prompt:             sql.NullString{String: prompt, Valid: prompt != ""},
		Status:             "building",
		DirPath:            newDir,
		CreatedBy:          sql.NullString{String: userID, Valid: userID != ""},
	}
	if err := m.db.CreateCheckpoint(cp); err != nil {
		return nil, fmt.Errorf("inserting checkpoint: %w", err)
	}

	_ = m.db.CreatePromptHistory(uuid.New().String()[:8], newID, worldID, userID, prompt)
	_ = m.db.SetUserPosition(userID, worldID, newID)

	return cp, nil
}

// GetCheckpointTree returns all checkpoints for a world.
func (m *Manager) GetCheckpointTree(worldID string) ([]db.Checkpoint, error) {
	return m.db.GetCheckpointTree(worldID)
}

// GetUserPosition returns the user's current checkpoint in a world.
func (m *Manager) GetUserPosition(userID, worldID string) (string, error) {
	return m.db.GetUserPosition(userID, worldID)
}

// SetUserPosition updates the user's current checkpoint in a world.
func (m *Manager) SetUserPosition(userID, worldID, cpID string) error {
	return m.db.SetUserPosition(userID, worldID, cpID)
}

// Shutdown stops all game servers.
func (m *Manager) Shutdown() {
	m.GameServers.Shutdown()
}

// cloneBuildCache copies the target/ directory using platform-appropriate
// methods: cp -cR (APFS clone) on macOS, cp -al (hardlinks) on Linux.
func cloneBuildCache(sourceDir, newDir string) error {
	src := filepath.Join(sourceDir, "target")
	dst := filepath.Join(newDir, "target")

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // No cache to clone.
	}

	if runtime.GOOS == "darwin" {
		return exec.Command("cp", "-cR", src, dst).Run()
	}
	return exec.Command("cp", "-al", src, dst).Run()
}
