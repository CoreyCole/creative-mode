package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/logging"
	"creative-mode/harness/internal/server"

	"github.com/labstack/echo/v4"
)

func main() {
	// Ensure the data directory exists.
	dataDir := filepath.Join("..", "data")
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

	// Set up Echo server.
	e := echo.New()
	srv := server.New(database, logger)
	srv.RegisterRoutes(e)

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logger.Info("Shutting down server...")
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
