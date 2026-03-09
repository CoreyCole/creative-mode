package world

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"creative-mode/harness/internal/builder"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
)

// Manager handles world creation, checkpoint forking, and user positions.
type Manager struct {
	db           *db.DB
	logger       *slog.Logger
	dataDir      string            // absolute path to data/
	templateDirs map[string]string // template type → absolute path
	Builder      *builder.Builder
	GameServers  *GameServerManager
	rateLimiter  *RateLimiter
}

// NewManager creates a new world manager.
func NewManager(
	database *db.DB,
	logger *slog.Logger,
	dataDir string,
	templateDirs map[string]string,
) *Manager {
	return &Manager{
		db:           database,
		logger:       logger,
		dataDir:      dataDir,
		templateDirs: templateDirs,
		Builder: builder.NewBuilder(
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
	name, description, userID, templateType string,
) (*sqlc.World, error) {
	templateDir, ok := m.templateDirs[templateType]
	if !ok {
		return nil, fmt.Errorf("unknown template type: %s", templateType)
	}

	worldID := uuid.New().String()[:8]
	cpID := uuid.New().String()[:8]

	// Directory name: <name>_<timestamp>_<id> for human-readable ls output.
	dirName := fmt.Sprintf(
		"%s_%s_%s",
		sanitizeName(name),
		time.Now().Format("2006-01-02_15-04-05"),
		worldID,
	)
	worldDir := filepath.Join(m.dataDir, "worlds", dirName)
	cpDir := filepath.Join(worldDir, cpID)
	if err := os.MkdirAll(cpDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating checkpoint directory: %w", err)
	}

	// Copy template (excluding target/).
	if err := copyDir(templateDir, cpDir, []string{"target"}); err != nil {
		_ = os.RemoveAll(worldDir)
		return nil, fmt.Errorf("copying template: %w", err)
	}

	// Clone pre-built target/ if it exists in the template.
	if err := cloneBuildCache(ctx, templateDir, cpDir); err != nil {
		m.logger.Warn("failed to clone template build cache", "error", err)
	}
	tx, txErr := m.db.BeginTx(ctx)
	if txErr != nil {
		_ = os.RemoveAll(worldDir)
		return nil, fmt.Errorf("beginning transaction: %w", txErr)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			m.logger.Error("rollback failed", "error", rbErr)
		}
	}()

	qtx := m.db.WithTx(tx)

	tt := sqlc.TemplateType(templateType)
	if err := qtx.CreateWorld(ctx, sqlc.CreateWorldParams{
		ID:           worldID,
		Name:         name,
		Description:  sql.NullString{String: description, Valid: description != ""},
		CreatedBy:    sql.NullString{String: userID, Valid: userID != ""},
		TemplateType: tt,
	}); err != nil {
		_ = os.RemoveAll(worldDir)
		return nil, fmt.Errorf("inserting world: %w", err)
	}

	cp := &sqlc.Checkpoint{
		ID:      cpID,
		WorldID: worldID,
		Status:  sqlc.CheckpointStatusReady,
		DirPath: cpDir,
	}
	if err := qtx.CreateCheckpoint(ctx, sqlc.CreateCheckpointParams{
		ID:        cpID,
		WorldID:   worldID,
		Status:    sqlc.CheckpointStatusReady,
		DirPath:   cpDir,
		CreatedBy: sql.NullString{String: userID, Valid: userID != ""},
	}); err != nil {
		_ = os.RemoveAll(worldDir)
		return nil, fmt.Errorf("inserting root checkpoint: %w", err)
	}

	if err := qtx.SetUserPosition(ctx, sqlc.SetUserPositionParams{
		UserID:       userID,
		WorldID:      worldID,
		CheckpointID: cpID,
	}); err != nil {
		m.logger.Error("failed to set initial user position", "error", err)
	}

	if err := tx.Commit(); err != nil {
		_ = os.RemoveAll(worldDir)
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	// Trigger initial build in background.
	go func() {
		bgCtx := context.Background()

		if buildErr := m.Builder.Build(cp, true, templateType); buildErr != nil {
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
				Status:   sqlc.CheckpointStatusFailed,
				BuildLog: sql.NullString{String: buildErr.Error(), Valid: true},
				ID:       cpID,
			})

			return
		}

		_, _ = m.db.UpdateCheckpointStatus(bgCtx, sqlc.UpdateCheckpointStatusParams{
			Status: sqlc.CheckpointStatusReady,
			ID:     cpID,
		})
		if cp.WasmPath.Valid {
			_ = m.db.UpdateCheckpointWasmPath(bgCtx, sqlc.UpdateCheckpointWasmPathParams{
				WasmPath: cp.WasmPath,
				ID:       cpID,
			})
		}
		m.Builder.PostBuild(cp)

		// Client-only templates have no game server.
		if tt == sqlc.TemplateType2D || tt == sqlc.TemplateTypeBoardgame {
			return
		}

		// Start game server so the world is immediately playable.
		srv, srvErr := m.GameServers.Connect(worldID, cpID, cp.DirPath)
		if srvErr != nil {
			m.logger.Error("failed to start game server after initial build",
				"worldID", worldID, "cpID", cpID, "error", srvErr)
		} else {
			_, _ = m.db.UpdateCheckpointServerPort(
				bgCtx,
				sqlc.UpdateCheckpointServerPortParams{
					ServerPort: sql.NullInt64{
						Int64: int64(srv.Port),
						Valid: true,
					},
					ID: cpID,
				},
			)
			m.logger.Info("game server started after initial build",
				"worldID", worldID, "cpID", cpID, "port", srv.Port)
		}
	}()

	return &sqlc.World{
		ID:           worldID,
		Name:         name,
		Description:  sql.NullString{String: description, Valid: description != ""},
		CreatedBy:    sql.NullString{String: userID, Valid: userID != ""},
		TemplateType: tt,
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
	if err := copyDir(sourceCP.DirPath, newDir, []string{"target"}); err != nil {
		return nil, fmt.Errorf("copying checkpoint: %w", err)
	}

	// Clone build cache (non-fatal if it fails).
	if err := cloneBuildCache(ctx, sourceCP.DirPath, newDir); err != nil {
		m.logger.Warn("failed to clone build cache", "error", err)
	}

	// DB inserts in a transaction.
	tx, txErr := m.db.BeginTx(ctx)
	if txErr != nil {
		_ = os.RemoveAll(newDir)
		return nil, fmt.Errorf("beginning transaction: %w", txErr)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			m.logger.Error("rollback failed", "error", rbErr)
		}
	}()

	qtx := m.db.WithTx(tx)

	if err := qtx.CreateCheckpoint(ctx, sqlc.CreateCheckpointParams{
		ID:                 newID,
		WorldID:            worldID,
		ParentCheckpointID: sql.NullString{String: sourceCPID, Valid: true},
		Prompt:             sql.NullString{String: prompt, Valid: prompt != ""},
		Status:             sqlc.CheckpointStatusBuilding,
		DirPath:            newDir,
		CreatedBy:          sql.NullString{String: userID, Valid: userID != ""},
	}); err != nil {
		_ = os.RemoveAll(newDir)
		return nil, fmt.Errorf("inserting checkpoint: %w", err)
	}

	if err := qtx.CreatePromptHistory(ctx, sqlc.CreatePromptHistoryParams{
		ID:           uuid.New().String()[:8],
		CheckpointID: newID,
		WorldID:      worldID,
		UserID:       userID,
		PromptText:   prompt,
	}); err != nil {
		m.logger.Error("failed to create prompt history", "error", err)
	}
	if err := qtx.SetUserPosition(ctx, sqlc.SetUserPositionParams{
		UserID:       userID,
		WorldID:      worldID,
		CheckpointID: newID,
	}); err != nil {
		m.logger.Error("failed to set user position", "error", err)
	}

	if err := tx.Commit(); err != nil {
		_ = os.RemoveAll(newDir)
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return &sqlc.Checkpoint{
		ID:      newID,
		WorldID: worldID,
		Status:  sqlc.CheckpointStatusBuilding,
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

// templateDisplayName returns a display-friendly name prefix for a template type.
func templateDisplayName(tmplType string) string {
	return strings.ToUpper(tmplType)
}

// templateWorldName returns the full world name for a template type,
// e.g. "3D Template World", "2D Template World".
func templateWorldName(tmplType string) string {
	return templateDisplayName(tmplType) + " Template World"
}

// EnsureTemplateWorlds is an idempotent startup function that ensures a
// template world exists for each template type with running dev servers.
// Creates them if missing, recovers them if stale from a crash.
func (m *Manager) EnsureTemplateWorlds(ctx context.Context) error {
	for tmplType, tmplDir := range m.templateDirs {
		if err := m.ensureSingleTemplateWorld(ctx, tmplType, tmplDir); err != nil {
			m.logger.Error("failed to ensure template world",
				"templateType", tmplType, "error", err)
		}
	}
	return nil
}

// ensureSingleTemplateWorld ensures a single template world exists and has
// running dev servers.
func (m *Manager) ensureSingleTemplateWorld(
	ctx context.Context, templateType, templateDir string,
) error {
	expectedName := templateWorldName(templateType)

	// Search for existing template world.
	worlds, err := m.db.ListWorlds(ctx)
	if err != nil {
		return fmt.Errorf("listing worlds: %w", err)
	}

	for _, w := range worlds {
		if w.Name != expectedName {
			continue
		}

		_, ensureErr := m.ensureTemplateDevReady(ctx, w.ID, templateType, templateDir)
		if ensureErr != nil {
			return fmt.Errorf("ensuring template dev ready: %w", ensureErr)
		}

		m.logger.Info("template world ready",
			"templateType", templateType, "worldID", w.ID)
		return nil
	}

	// No template world of this type — create one in dev mode.
	worldID, createErr := m.createTemplateWorldDev(ctx, templateType, templateDir)
	if createErr != nil {
		return fmt.Errorf("creating template world (dev): %w", createErr)
	}

	m.logger.Info("template world ready",
		"templateType", templateType, "worldID", worldID)
	return nil
}

// createTemplateWorldDev creates a template world that points directly at the
// template directory (no file copy). Dev servers handle compilation automatically.
func (m *Manager) createTemplateWorldDev(
	ctx context.Context, templateType, templateDir string,
) (string, error) {
	worldID := uuid.New().String()[:8]
	cpID := uuid.New().String()[:8]

	tx, txErr := m.db.BeginTx(ctx)
	if txErr != nil {
		return "", fmt.Errorf("beginning transaction: %w", txErr)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			m.logger.Error("rollback failed", "error", rbErr)
		}
	}()

	qtx := m.db.WithTx(tx)

	if err := qtx.CreateWorld(ctx, sqlc.CreateWorldParams{
		ID:   worldID,
		Name: templateWorldName(templateType),
		Description: sql.NullString{
			String: "Auto-provisioned " + templateType + " template world",
			Valid:  true,
		},
		TemplateType: sqlc.TemplateType(templateType),
	}); err != nil {
		return "", fmt.Errorf("inserting world: %w", err)
	}

	if err := qtx.CreateCheckpoint(ctx, sqlc.CreateCheckpointParams{
		ID:      cpID,
		WorldID: worldID,
		Status:  sqlc.CheckpointStatusReady,
		DirPath: templateDir,
	}); err != nil {
		return "", fmt.Errorf("inserting root checkpoint: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("committing transaction: %w", err)
	}

	// Set cover image if a template cover exists in shared-assets.
	coverPath := filepath.Join(m.dataDir, "shared-assets",
		templateDisplayName(templateType)+"-template-cover.png")
	if _, statErr := os.Stat(coverPath); statErr == nil {
		if dbErr := m.db.UpdateWorldCoverImage(ctx, sqlc.UpdateWorldCoverImageParams{
			CoverImagePath: sql.NullString{String: coverPath, Valid: true},
			ID:             worldID,
		}); dbErr != nil {
			m.logger.Warn("failed to set template cover image", "error", dbErr)
		}
	}

	// Symlink dist/ into wasm-builds so handleWASMArtifacts can serve it.
	m.symlinkTemplateDist(ctx, worldID, cpID, templateType, templateDir)

	// Start dev servers (game server only, no trunk serve).
	startErr := m.startTemplateDevServers(
		ctx, worldID, cpID, templateType, templateDir,
	)
	if startErr != nil {
		return "", fmt.Errorf("starting dev servers: %w", startErr)
	}

	m.logger.Info("template world created (dev mode)",
		"templateType", templateType, "worldID", worldID, "cpID", cpID)

	return worldID, nil
}

// ensureTemplateDevReady handles an existing template world: ensures dir_path
// points at the correct template directory (migrates if stale) and starts dev servers.
func (m *Manager) ensureTemplateDevReady(
	ctx context.Context, worldID, templateType, templateDir string,
) (string, error) {
	checkpoints, err := m.db.GetCheckpointTree(ctx, worldID)
	if err != nil {
		return "", fmt.Errorf("getting checkpoint tree: %w", err)
	}

	if len(checkpoints) == 0 {
		return "", fmt.Errorf("template world %s has no checkpoints", worldID)
	}

	cp := checkpoints[0]

	// Migrate dir_path to templateDir if stale (e.g. from a prior prod setup).
	if cp.DirPath != templateDir {
		m.logger.Info("migrating template world dir_path",
			"worldID", worldID, "cpID", cp.ID,
			"old", cp.DirPath, "new", templateDir)
		if err := m.db.UpdateCheckpointDirPath(ctx, sqlc.UpdateCheckpointDirPathParams{
			DirPath: templateDir,
			ID:      cp.ID,
		}); err != nil {
			return "", fmt.Errorf("updating dir_path: %w", err)
		}
	}

	// Ensure dist/ symlink exists for static WASM serving.
	m.symlinkTemplateDist(ctx, worldID, cp.ID, templateType, templateDir)

	// Start dev servers (game server only, no trunk serve).
	startErr := m.startTemplateDevServers(
		ctx, worldID, cp.ID, templateType, templateDir,
	)
	if startErr != nil {
		return "", fmt.Errorf("starting dev servers: %w", startErr)
	}

	return worldID, nil
}

// symlinkTemplateDist creates a symlink from data/wasm-builds/{worldID}/{cpID}/
// to the template's dist/ directory so handleWASMArtifacts can serve static WASM.
// Also sets WasmPath on the checkpoint.
func (m *Manager) symlinkTemplateDist(
	ctx context.Context, worldID, cpID, templateType, templateDir string,
) {
	distDir := filepath.Join(templateDir, "dist")
	if templateType == "3d" {
		distDir = filepath.Join(templateDir, "client", "dist")
	}

	if _, err := os.Stat(distDir); err != nil {
		m.logger.Warn("template dist/ not found, skipping symlink",
			"distDir", distDir, "error", err)
		return
	}

	wasmDir := filepath.Join(m.dataDir, "wasm-builds", worldID, cpID)

	// If the symlink already exists and points to the right place, skip.
	if target, err := os.Readlink(wasmDir); err == nil && target == distDir {
		return
	}

	// Remove any stale entry (old symlink or directory).
	_ = os.Remove(wasmDir)

	if err := os.MkdirAll(filepath.Dir(wasmDir), 0o750); err != nil {
		m.logger.Error("failed to create wasm-builds parent dir", "error", err)
		return
	}

	if err := os.Symlink(distDir, wasmDir); err != nil {
		m.logger.Error("failed to symlink template dist",
			"distDir", distDir, "wasmDir", wasmDir, "error", err)
		return
	}

	// Update checkpoint WasmPath so the iframe loads static WASM.
	_ = m.db.UpdateCheckpointWasmPath(ctx, sqlc.UpdateCheckpointWasmPathParams{
		WasmPath: sql.NullString{String: wasmDir, Valid: true},
		ID:       cpID,
	})

	m.logger.Info("symlinked template dist for static WASM",
		"worldID", worldID, "cpID", cpID, "distDir", distDir)
}

// startTemplateDevServers starts dev servers for a template world.
// 3D: cargo watch (game server) only. Trunk serve is on-demand.
// 2D: no servers needed (client-only).
func (m *Manager) startTemplateDevServers(
	ctx context.Context, worldID, cpID, templateType, templateDir string,
) error {
	// 3D worlds need a game server (cargo watch).
	if templateType == "3d" {
		srv, err := m.GameServers.ConnectDev(worldID, cpID, templateDir)
		if err != nil {
			return fmt.Errorf("starting dev game server: %w", err)
		}

		// Sync server port to DB.
		_, _ = m.db.UpdateCheckpointServerPort(ctx, sqlc.UpdateCheckpointServerPortParams{
			ServerPort: sql.NullInt64{Int64: int64(srv.Port), Valid: true},
			ID:         cpID,
		})

		m.logger.Info("template dev servers running",
			"templateType", templateType,
			"worldID", worldID, "cpID", cpID,
			"gamePort", srv.Port)
	}

	// No trunk serve on boot — started on-demand when a user visits the world.
	return nil
}

// EnsureTemplateTrunkServe starts trunk serve for a template world on demand.
// Only one trunk serve can run at a time (memory constraint), so any existing
// trunk serve is stopped first.
func (m *Manager) EnsureTemplateTrunkServe(
	worldID, cpID, checkpointDir string,
) (int, error) {
	// Already running for this world?
	if gs := m.GameServers.GetServer(worldID, cpID); gs != nil && gs.TrunkPort > 0 {
		return gs.TrunkPort, nil
	}

	// Stop all other trunk serves (only one at a time due to memory).
	m.GameServers.StopAllTrunkServes()

	// Start trunk serve.
	port, err := m.GameServers.StartTrunkServe(worldID, cpID, checkpointDir)
	if err != nil {
		return 0, fmt.Errorf("starting trunk serve: %w", err)
	}

	m.logger.Info("on-demand trunk serve started",
		"worldID", worldID, "cpID", cpID, "port", port)
	return port, nil
}

// IsTemplateWorld returns true if the world name matches the template naming convention.
func IsTemplateWorld(name string) bool {
	return strings.HasSuffix(name, "Template World")
}

// Shutdown stops all game servers.
func (m *Manager) Shutdown() {
	m.GameServers.Shutdown()
}

var multiHyphen = regexp.MustCompile(`-{2,}`)

const maxSlugLen = 48

// sanitizeName converts a world name to a filesystem-safe slug:
// lowercase, non-alphanum replaced with hyphens, trimmed, max 48 chars.
func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteByte('-')
		}
	}
	s := multiHyphen.ReplaceAllString(b.String(), "-")
	s = strings.Trim(s, "-")
	if len(s) > maxSlugLen {
		s = s[:maxSlugLen]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "world"
	}
	return s
}

// copyDir recursively copies src into dst, skipping any top-level directories
// whose names appear in exclude. File permissions are preserved.
func copyDir(src, dst string, exclude []string) error {
	excluded := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		excluded[name] = true
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip excluded top-level directories.
		if d.IsDir() && filepath.Dir(rel) == "." && excluded[d.Name()] {
			return filepath.SkipDir
		}

		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, d.Type().Perm()|0o750)
		}

		// Copy symlinks as symlinks.
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}

		return copyFile(path, target)
	})
}

// copyFile copies a single file, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(
		dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm(),
	)
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}

	return out.Close()
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
		cmd := exec.CommandContext(
			ctx,
			"cp",
			"-cR",
			src,
			dst,
		)

		return cmd.Run()
	}

	cmd := exec.CommandContext(
		ctx,
		"cp",
		"-al",
		src,
		dst,
	)

	return cmd.Run()
}
