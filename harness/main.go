package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/coreycole/creative-mode/pkg/markdown"
	"github.com/coreycole/creative-mode/pkg/mayorchat"
	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/auth"
	"creative-mode/harness/internal/claude"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	discordlistener "creative-mode/harness/internal/discord"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/gemini"
	"creative-mode/harness/internal/logging"
	"creative-mode/harness/internal/mayor"
	"creative-mode/harness/internal/president"
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
	for _, tmplType := range []string{"3d", "2d", "boardgame"} {
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
			data, rdErr := os.ReadFile(src)
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

	// Clean up orphaned swarm spans on startup (from prior crashes).
	if cleanErr := database.CleanupOrphanedSpans(context.Background()); cleanErr != nil {
		logger.Error("failed to clean orphaned swarm spans", "error", cleanErr)
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

	// Periodically clean up old swarm JSONL logs (7-day retention).
	swarmLogDir := filepath.Join(dataDir, "logs", "swarm")
	_ = os.MkdirAll(swarmLogDir, 0o750)
	go func() {
		const swarmLogMaxAge = 7 * 24 * time.Hour
		ticker := time.NewTicker(swarmLogMaxAge / 7) //nolint:mnd // daily = maxAge/7
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanSwarmLogs(swarmLogDir, swarmLogMaxAge, logger)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Set up auth (optional — requires DISCORD_CLIENT_ID).
	var authHandler *auth.Handler

	discordClientID := os.Getenv("DISCORD_CLIENT_ID")
	discordClientSecret := os.Getenv("DISCORD_CLIENT_SECRET")

	baseURL := os.Getenv("HARNESS_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	switch {
	case discordClientID != "" && discordClientSecret != "":
		authHandler = auth.NewHandler(database, &auth.Config{
			DiscordClientID:     discordClientID,
			DiscordClientSecret: discordClientSecret,
			BaseURL:             baseURL,
		}, logger)
		logger.Info("Discord OAuth enabled")
	case os.Getenv("DEV_MODE") == "true":
		authHandler = auth.NewHandler(database, &auth.Config{
			BaseURL: baseURL,
		}, logger)
		logger.Info("Dev auth enabled (no Discord OAuth)")
	default:
		logger.Warn(
			"Discord OAuth disabled (DISCORD_CLIENT_ID/DISCORD_CLIENT_SECRET not set)",
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

	// Set up create-world chat page dependencies.
	createStore := server.NewInMemoryMessageStore()
	createConvMgr := mayorchat.NewConversationManager(createStore)
	mdRenderer, mdErr := markdown.NewRenderer()
	if mdErr != nil {
		logger.Error("failed to create markdown renderer", "error", mdErr)
	}
	var createClaudeClient *anthropic.Client
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		client := mayorchat.NewClient(apiKey)
		createClaudeClient = &client
		logger.Info("Create-world Claude chat enabled")
	}

	// Set up mayor manager (optional — requires DISCORD_BOT_TOKEN).
	mayorManager := initMayorManager(baseURL, dataDir, database, logger)

	// Set up president manager (optional — requires DISCORD_PRESIDENT_CHANNEL_ID + PRESIDENT_SECRET).
	presidentManager := initPresidentManager(baseURL, dataDir, database, logger)

	// Start Discord gateway listener (mirrors messages to DB + EventBus).
	var discordListener *discordlistener.Listener
	if botToken := os.Getenv("DISCORD_BOT_TOKEN"); botToken != "" && mayorManager != nil {
		var listenerErr error
		discordListener, listenerErr = discordlistener.NewListener(
			botToken,
			database,
			eventBus,
			logger,
		)
		if listenerErr != nil {
			logger.Error("failed to create discord listener", "error", listenerErr)
		} else if startErr := discordListener.Start(); startErr != nil {
			logger.Error("failed to start discord listener", "error", startErr)
			discordListener = nil
		} else {
			logger.Info("Discord gateway listener started")
		}
	}

	// Wire build-complete notifications to Discord via MayorManager.
	if mayorManager != nil {
		orchestrator.OnBuildComplete = func(worldID, cpID string, success bool, summary string) {
			w, wErr := database.GetWorld(context.Background(), worldID)
			if wErr != nil || !w.DiscordChannelID.Valid {
				return
			}
			var msg string
			if success {
				msg = fmt.Sprintf("[BUILD COMPLETE] Checkpoint `%s` — %s", cpID, summary)
			} else {
				msg = fmt.Sprintf("[BUILD FAILED] Checkpoint `%s` — %s", cpID, summary)
			}
			mayorManager.PostToDiscord(w.DiscordChannelID.String, msg)
		}
	}

	// Set up Echo server.
	e := echo.New()
	srv := server.New(database, logger)
	srv.AuthHandler = authHandler
	srv.WorldManager = worldManager
	srv.Orchestrator = orchestrator
	srv.EventBus = eventBus
	srv.GeminiClient = geminiClient
	srv.MayorManager = mayorManager
	srv.PresidentManager = presidentManager
	srv.DataDir = dataDir
	srv.CreateConvMgr = createConvMgr
	srv.CreateClaudeClient = createClaudeClient
	srv.CreateMDRenderer = mdRenderer
	srv.RegisterRoutes(e)

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down server...")
		worldManager.Shutdown()

		if discordListener != nil {
			_ = discordListener.Stop()
		}

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

func resolveOpenclawPaths(dataDir string) (home, bin string) {
	home = os.Getenv("OPENCLAW_HOME")
	if home == "" {
		home = filepath.Join(dataDir, "openclaw")
	}
	bin = os.Getenv("OPENCLAW_BIN")
	if bin == "" {
		bin = "/opt/openclaw/openclaw.mjs"
	}
	return home, bin
}

func initMayorManager(
	baseURL, dataDir string,
	database *db.DB,
	logger *slog.Logger,
) *mayor.Manager {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	if botToken == "" {
		return nil
	}

	guildID := os.Getenv("DISCORD_GUILD_ID")
	categoryID := os.Getenv("DISCORD_WORLDS_CATEGORY_ID")
	if guildID == "" || categoryID == "" {
		logger.Warn(
			"DISCORD_BOT_TOKEN set but DISCORD_GUILD_ID or DISCORD_WORLDS_CATEGORY_ID missing — mayors disabled",
		)
		return nil
	}

	wcClient, wcErr := worldchannel.NewClient(worldchannel.Config{
		BotToken:         botToken,
		GuildID:          guildID,
		WorldsCategoryID: categoryID,
	}, logger)
	if wcErr != nil {
		logger.Error("failed to create worldchannel client", "error", wcErr)
		return nil
	}

	openclawHome, openclawBin := resolveOpenclawPaths(dataDir)
	mgr := mayor.NewManager(
		openclawHome, openclawBin, baseURL, dataDir,
		wcClient, database, logger,
	)
	logger.Info("Mayor manager enabled",
		"openclaw_home", openclawHome,
		"guild_id", guildID,
	)
	return mgr
}

func cleanSwarmLogs(dir string, maxAge time.Duration, logger *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if rmErr := os.Remove(filepath.Join(dir, entry.Name())); rmErr != nil {
			logger.Warn(
				"failed to remove old swarm log",
				"file",
				entry.Name(),
				"error",
				rmErr,
			)
		}
	}
}

func initPresidentManager(
	baseURL, dataDir string,
	database *db.DB,
	logger *slog.Logger,
) *president.Manager {
	presidentChannelID := os.Getenv("DISCORD_PRESIDENT_CHANNEL_ID")
	if presidentChannelID == "" {
		return nil
	}

	presidentSecret := os.Getenv("PRESIDENT_SECRET")
	if presidentSecret == "" {
		logger.Warn(
			"DISCORD_PRESIDENT_CHANNEL_ID set but PRESIDENT_SECRET missing — president disabled",
		)
		return nil
	}

	openclawHome, openclawBin := resolveOpenclawPaths(dataDir)
	mgr := president.NewManager(
		openclawHome, openclawBin, baseURL,
		presidentSecret, presidentChannelID,
		database, logger,
	)

	if err := mgr.Provision(); err != nil {
		logger.Error("failed to provision president agent", "error", err)
	} else {
		logger.Info("President agent ready", "channel_id", presidentChannelID)
	}
	return mgr
}
