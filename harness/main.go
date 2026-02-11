package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/auth"
	"creative-mode/harness/internal/claude"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/events"
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

	templateDir, err := filepath.Abs(filepath.Join("..", "template"))
	if err != nil {
		log.Fatalf("Failed to resolve template directory: %v", err)
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

	if ghClientID != "" && ghClientSecret != "" {
		baseURL := os.Getenv("HARNESS_URL")
		if baseURL == "" {
			baseURL = "http://localhost:8080"
		}

		authHandler = auth.NewHandler(database, &auth.Config{
			GitHubClientID:     ghClientID,
			GitHubClientSecret: ghClientSecret,
			BaseURL:            baseURL,
		}, logger)
		logger.Info("GitHub OAuth enabled")
	} else {
		logger.Warn(
			"GitHub OAuth disabled (GITHUB_CLIENT_ID/GITHUB_CLIENT_SECRET not set)",
		)
	}

	// Set up world manager.
	worldManager := world.NewManager(database, logger, dataDir, templateDir)

	// Set up event bus.
	eventBus := events.NewEventBus()

	// Set up Claude Code orchestrator.
	harnessURL := os.Getenv("HARNESS_URL")
	if harnessURL == "" {
		harnessURL = "http://localhost:8080"
	}

	orchestrator := claude.NewOrchestrator(
		database, logger, worldManager, worldManager.Builder, eventBus,
		filepath.Join(dataDir, "logs"), harnessURL,
	)

	// Set up Echo server.
	e := echo.New()
	srv := server.New(database, logger)
	srv.AuthHandler = authHandler
	srv.WorldManager = worldManager
	srv.Orchestrator = orchestrator
	srv.EventBus = eventBus
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
