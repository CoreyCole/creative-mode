package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"creative-mode/harness/internal/auth"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/logging"
	"creative-mode/harness/internal/server"
	"creative-mode/harness/internal/world"

	"github.com/labstack/echo/v4"
)

func main() {
	// Ensure the data directory exists.
	dataDir, err := filepath.Abs(filepath.Join("..", "data"))
	if err != nil {
		log.Fatalf("Failed to resolve data directory: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
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
	defer database.Close()

	// Clean up expired sessions on startup.
	if err := database.DeleteExpiredSessions(); err != nil {
		logger.Error("failed to clean expired sessions", "error", err)
	}

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
		logger.Warn("GitHub OAuth disabled (GITHUB_CLIENT_ID/GITHUB_CLIENT_SECRET not set)")
	}

	// Set up world manager.
	templateDir, err := filepath.Abs(filepath.Join("..", "template"))
	if err != nil {
		log.Fatalf("Failed to resolve template directory: %v", err)
	}
	worldManager := world.NewManager(database, logger, dataDir, templateDir)

	// Set up Echo server.
	e := echo.New()
	srv := server.New(database, logger)
	srv.AuthHandler = authHandler
	srv.WorldManager = worldManager
	srv.DataDir = dataDir
	srv.RegisterRoutes(e)

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logger.Info("Shutting down server...")
		worldManager.Shutdown()
		if err := e.Shutdown(context.Background()); err != nil {
			logger.Error("Server shutdown error", "error", err)
		}
	}()

	logger.Info("Harness server starting on :8080")
	if err := e.Start(":8080"); err != nil {
		// echo.ErrServerClosed is expected on graceful shutdown.
		logger.Info("Server stopped", "reason", err.Error())
	}
}
