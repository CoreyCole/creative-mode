package server

import (
	"log/slog"
	"net/http"

	"creative-mode/harness/internal/db"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server holds application dependencies and registers HTTP routes.
type Server struct {
	DB     *db.DB
	Logger *slog.Logger
}

// New creates a new Server with the given database and logger.
func New(database *db.DB, logger *slog.Logger) *Server {
	return &Server{DB: database, Logger: logger}
}

// RegisterRoutes configures middleware and registers all HTTP routes on the
// given Echo instance.
func (s *Server) RegisterRoutes(e *echo.Echo) {
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Static file serving (public, no auth required).
	e.Static("/assets", "../data/shared-assets")
	e.Static("/static", "static")

	// Health check endpoint.
	e.GET("/health", s.handleHealth)

	// Auth routes will be registered by Component 2.
	// World routes will be registered by Component 3.
	// Claude event endpoint will be registered by Component 5.
	// UI views will be registered by Component 6.
}

// handleHealth returns a simple JSON health check response.
func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
