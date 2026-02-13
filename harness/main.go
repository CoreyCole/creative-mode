package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/auth"
	"creative-mode/harness/internal/claude"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/gemini"
	"creative-mode/harness/internal/logging"
	"creative-mode/harness/internal/server"
	"creative-mode/harness/internal/world"
)

func main() {
	// Resolve directories before opening DB (avoids exitAfterDefer).
	dataDir, err := filepath.Abs(filepath.Join("..", "data"))
	if err != nil {
		log.Fatalf("Failed to resolve data directory: %v", err)
	}

	if mkdirErr := os.MkdirAll(dataDir, 0o750); mkdirErr != nil {
		log.Fatalf("Failed to create data directory: %v", mkdirErr)
	}

	sharedAssetsDir := filepath.Join(dataDir, "shared-assets")
	if mkdirErr := os.MkdirAll(sharedAssetsDir, 0o750); mkdirErr != nil {
		log.Fatalf("Failed to create shared-assets directory: %v", mkdirErr)
	}

	templateDirs := map[string]string{}
	for _, tmplType := range []string{"3d", "2d"} {
		dir, tmplErr := filepath.Abs(filepath.Join("..", "templates", tmplType))
		if tmplErr != nil {
			log.Fatalf("Failed to resolve %s template directory: %v", tmplType, tmplErr)
		}
		templateDirs[tmplType] = dir
	}

	// Seed default 2D room JSON files to shared-assets/rooms/.
	roomsSrcDir := filepath.Join(templateDirs["2d"], "rooms")
	roomsDstDir := filepath.Join(sharedAssetsDir, "rooms")
	if mkdirErr := os.MkdirAll(roomsDstDir, 0o750); mkdirErr != nil {
		log.Fatalf("Failed to create rooms directory: %v", mkdirErr)
	}
	if entries, readErr := os.ReadDir(roomsSrcDir); readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			dst := filepath.Join(roomsDstDir, entry.Name())
			if _, statErr := os.Stat(dst); statErr == nil {
				continue // don't overwrite existing (user-edited) files
			}
			src := filepath.Join(roomsSrcDir, entry.Name())
			data, rdErr := os.ReadFile(src) //nolint:gosec // trusted path
			if rdErr != nil {
				continue
			}
			_ = os.WriteFile(dst, data, 0o600)
		}
	}

	// Initialize structured logger.
	logger, err := logging.NewLogger(filepath.Join(dataDir, "logs"))
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	// Initialize database.
	database, err := db.New(filepath.Join(dataDir, "creative-mode.db"))
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer func() { _ = database.Close() }()

	// Graceful shutdown context (created early for periodic goroutines).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Clean up expired sessions on startup.
	if cleanErr := database.DeleteExpiredSessions(context.Background()); cleanErr != nil {
		logger.Error("failed to clean expired sessions", "error", cleanErr)
	}

	// Periodically clean expired sessions.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanupErr := database.DeleteExpiredSessions(context.Background())
				if cleanupErr != nil {
					logger.Error("periodic session cleanup failed", "error", cleanupErr)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Set up auth (optional — requires GITHUB_CLIENT_ID).
	var authHandler *auth.Handler

	ghClientID := os.Getenv("GITHUB_CLIENT_ID")
	ghClientSecret := os.Getenv("GITHUB_CLIENT_SECRET")

	baseURL := os.Getenv("HARNESS_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	switch {
	case ghClientID != "" && ghClientSecret != "":
		authHandler = auth.NewHandler(database, &auth.Config{
			GitHubClientID:     ghClientID,
			GitHubClientSecret: ghClientSecret,
			BaseURL:            baseURL,
		}, logger)
		logger.Info("GitHub OAuth enabled")
	case os.Getenv("DEV_MODE") == "true":
		authHandler = auth.NewHandler(database, &auth.Config{
			BaseURL: baseURL,
		}, logger)
		logger.Info("Dev auth enabled (no GitHub OAuth)")
	default:
		logger.Warn(
			"GitHub OAuth disabled (GITHUB_CLIENT_ID/GITHUB_CLIENT_SECRET not set)",
		)
	}

	// Set up world manager.
	worldManager := world.NewManager(database, logger, dataDir, templateDirs)

	// Recover game servers from surviving tmux sessions.
	worldManager.GameServers.Recover()
	for _, srv := range worldManager.GameServers.RecoveredServers() {
		// Validate checkpoint still exists in DB.
		cp, cpErr := database.GetCheckpoint(context.Background(), srv.CPID)
		if cpErr != nil {
			logger.Warn("recovered server has no DB checkpoint, killing",
				"cpID", srv.CPID, "port", srv.Port)
			worldManager.GameServers.Disconnect(srv.WorldID, srv.CPID)

			continue
		}

		// Check if this is a template world's dev server — keep it alive.
		isTemplateDev := false
		if srv.Mode == world.GameServerModeDev {
			w, wErr := database.GetWorld(context.Background(), srv.WorldID)
			if wErr == nil && strings.HasSuffix(w.Name, "Template World") {
				isTemplateDev = true
			}
		}

		if !isTemplateDev {
			// Only keep prod servers for ready checkpoints.
			if srv.Mode == world.GameServerModeDev || cp.Status != "ready" {
				logger.Info("killing non-ready recovered server",
					"cpID", srv.CPID, "status", cp.Status, "mode", srv.Mode)
				worldManager.GameServers.Disconnect(srv.WorldID, srv.CPID)

				continue
			}
		}

		if _, syncErr := database.UpdateCheckpointServerPort(
			context.Background(),
			sqlc.UpdateCheckpointServerPortParams{
				ServerPort: sql.NullInt64{Int64: int64(srv.Port), Valid: true},
				ID:         srv.CPID,
			},
		); syncErr != nil {
			logger.Warn("failed to sync recovered server port",
				"cpID", srv.CPID, "port", srv.Port, "error", syncErr)
		}

		if isTemplateDev {
			logger.Info("kept template dev server alive",
				"worldID", srv.WorldID, "cpID", srv.CPID,
				"port", srv.Port, "trunkPort", srv.TrunkPort)
		}
	}

	// Auto-provision template worlds for all template types (non-fatal).
	if templateErr := worldManager.EnsureTemplateWorlds(ctx); templateErr != nil {
		logger.Error("failed to ensure template worlds", "error", templateErr)
	}

	// Set up event bus.
	eventBus := events.NewEventBus()

	// Set up Claude Code orchestrator.
	orchestrator := claude.NewOrchestrator(
		database, logger, worldManager, worldManager.Builder, eventBus,
		filepath.Join(dataDir, "logs"), baseURL,
	)

	// Periodically reap orphaned tmux sessions.
	go func() {
		ticker := time.NewTicker(5 * time.Minute) //nolint:mnd // reap interval
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				orchestrator.ReapOrphanedSessions()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Set up Gemini image generation (optional — requires GEMINI_API_KEY).
	geminiClient, geminiErr := gemini.NewClient(ctx, os.Getenv("GEMINI_API_KEY"), logger)
	if geminiErr != nil {
		logger.Error("failed to create Gemini client", "error", geminiErr)
	}
	if geminiClient != nil {
		logger.Info("Gemini image generation enabled")
	}

	// Set up Echo server.
	e := echo.New()
	srv := server.New(database, logger)
	srv.AuthHandler = authHandler
	srv.WorldManager = worldManager
	srv.Orchestrator = orchestrator
	srv.EventBus = eventBus
	srv.GeminiClient = geminiClient
	srv.DataDir = dataDir
	srv.RegisterRoutes(e)

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down server...")
		worldManager.Shutdown()

		if shutdownErr := e.Shutdown(context.Background()); shutdownErr != nil {
			logger.Error("Server shutdown error", "error", shutdownErr)
		}
	}()

	logger.Info("Harness server starting on :8080")

	if startErr := e.Start(":8080"); startErr != nil {
		// echo.ErrServerClosed is expected on graceful shutdown.
		logger.Info("Server stopped", "reason", startErr.Error())
	}
}
