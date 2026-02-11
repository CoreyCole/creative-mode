package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"creative-mode/harness/internal/auth"
	"creative-mode/harness/internal/claude"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/world"
)

// Server holds application dependencies and registers HTTP routes.
type Server struct {
	DB           *db.DB
	Logger       *slog.Logger
	AuthHandler  *auth.Handler
	WorldManager *world.Manager
	Orchestrator *claude.Orchestrator
	EventBus     *events.EventBus
	DataDir      string
}

// New creates a new Server with the given database and logger.
func New(database *db.DB, logger *slog.Logger) *Server {
	return &Server{DB: database, Logger: logger}
}

// RegisterRoutes configures middleware and registers all HTTP routes on the
// given Echo instance.
func (s *Server) RegisterRoutes(e *echo.Echo) {
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			s.Logger.Info("request",
				"uri", v.URI,
				"status", v.Status,
			)

			return nil
		},
	}))
	e.Use(middleware.Recover())

	// Static file serving (public, no auth required).
	e.Static("/assets", filepath.Join(s.DataDir, "shared-assets"))
	e.Static("/static", "static")

	// Health check endpoint.
	e.GET("/health", s.handleHealth)

	// Claude hook event endpoint (unprotected — internal same-machine communication).
	e.POST("/api/claude-event", s.handleClaudeEvent)

	// Auth routes (no auth middleware).
	if s.AuthHandler == nil {
		return
	}

	e.GET("/auth/github/login", s.AuthHandler.HandleLogin)
	e.GET("/auth/github/callback", s.AuthHandler.HandleCallback)
	e.POST("/auth/logout", s.AuthHandler.HandleLogout)

	// Authenticated but possibly pending.
	authed := e.Group("", auth.SessionMiddleware(s.DB))
	authed.GET("/auth/pending", s.AuthHandler.HandlePendingApproval)

	// Approved users only.
	approved := authed.Group("", auth.ApprovedMiddleware())
	s.registerWorldRoutes(approved)

	// WASM artifact serving (approved users).
	approved.GET("/wasm/:worldID/:cpID/*", s.handleWASMArtifacts)

	// Chat (approved users).
	approved.POST("/api/chat", s.handleChatMessage)

	// Admin only.
	admin := authed.Group("/admin", auth.AdminMiddleware())
	admin.GET("/users", s.AuthHandler.HandleAdminUsers)
	admin.POST("/users/:userID/approve", s.AuthHandler.HandleApproveUser)
	admin.POST("/users/:userID/reject", s.AuthHandler.HandleRejectUser)
}

// registerWorldRoutes adds world management endpoints to the approved group.
func (s *Server) registerWorldRoutes(approved *echo.Group) {
	if s.WorldManager == nil {
		return
	}
	w := approved.Group("/world")
	w.POST("/create", s.handleCreateWorld)
	w.GET("/:worldID", s.handleWorldView)
	w.GET("/:worldID/checkpoint/:cpID", s.handleCheckpointView)
	w.POST("/:worldID/prompt", s.handlePrompt)
	w.POST("/:worldID/checkpoint", s.handleSaveCheckpoint)
	w.GET("/:worldID/checkpoint/:cpID/logs/:logType", s.handleLogStream)
}

// handleHealth returns a simple JSON health check response.
func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// requireUser extracts the user from the Echo context, returning an error
// if the type assertion fails.
func requireUser(c echo.Context) (*sqlc.User, error) {
	user, ok := c.Get("user").(*sqlc.User)
	if !ok {
		return nil, echo.NewHTTPError(
			http.StatusUnauthorized,
			"user not found in session",
		)
	}

	return user, nil
}

// handleCreateWorld creates a new world from the template.
func (s *Server) handleCreateWorld(c echo.Context) error {
	ctx := c.Request().Context()

	user, err := requireUser(c)
	if err != nil {
		return err
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if bindErr := c.Bind(&req); bindErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	w, err := s.WorldManager.CreateWorld(ctx, req.Name, req.Description, user.ID)
	if err != nil {
		s.Logger.Error("failed to create world", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create world")
	}

	return c.JSON(http.StatusCreated, map[string]string{
		"id":   w.ID,
		"name": w.Name,
	})
}

// handleWorldView returns world info and the user's current position.
func (s *Server) handleWorldView(c echo.Context) error {
	ctx := c.Request().Context()

	user, err := requireUser(c)
	if err != nil {
		return err
	}
	worldID := c.Param("worldID")

	w, err := s.DB.GetWorld(ctx, worldID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "world not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get world")
	}

	cpID, _ := s.WorldManager.GetUserPosition(ctx, user.ID, worldID)

	checkpoints, err := s.WorldManager.GetCheckpointTree(ctx, worldID)
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to get checkpoints",
		)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"world":              w,
		"current_checkpoint": cpID,
		"checkpoints":        checkpoints,
	})
}

// handleCheckpointView updates the user's position and returns checkpoint info.
func (s *Server) handleCheckpointView(c echo.Context) error {
	ctx := c.Request().Context()

	user, err := requireUser(c)
	if err != nil {
		return err
	}
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")

	cp, err := s.DB.GetCheckpoint(ctx, cpID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "checkpoint not found")
	}

	_ = s.WorldManager.SetUserPosition(ctx, user.ID, worldID, cpID)

	return c.JSON(http.StatusOK, cp)
}

// handlePrompt forks a checkpoint, launches a Claude Code session, and starts
// the prompt-to-build pipeline. If no Orchestrator is configured, falls back
// to fork-only behavior.
func (s *Server) handlePrompt(c echo.Context) error {
	ctx := c.Request().Context()

	user, err := requireUser(c)
	if err != nil {
		return err
	}
	worldID := c.Param("worldID")

	var req struct {
		Prompt       string `json:"prompt"`
		CheckpointID string `json:"checkpoint_id"` //nolint:tagliatelle // matches frontend JSON
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.Prompt == "" || req.CheckpointID == "" {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"prompt and checkpoint_id are required",
		)
	}

	// Use Orchestrator if available (launches Claude Code in tmux).
	if s.Orchestrator != nil {
		cp, promptErr := s.Orchestrator.HandlePrompt(
			ctx,
			worldID,
			req.CheckpointID,
			req.Prompt,
			user.ID,
		)
		if promptErr != nil {
			var rateLimitErr *world.RateLimitError
			if errors.As(promptErr, &rateLimitErr) {
				return echo.NewHTTPError(http.StatusTooManyRequests, promptErr.Error())
			}
			s.Logger.Error("failed to handle prompt", "error", promptErr)

			return echo.NewHTTPError(
				http.StatusInternalServerError,
				"failed to create checkpoint",
			)
		}

		return c.JSON(http.StatusCreated, map[string]string{
			"checkpoint_id": cp.ID,
			"status":        cp.Status,
		})
	}

	// Fallback: fork-only (no Claude session).
	cp, forkErr := s.WorldManager.ForkCheckpoint(
		ctx,
		worldID,
		req.CheckpointID,
		req.Prompt,
		user.ID,
	)
	if forkErr != nil {
		var rateLimitErr *world.RateLimitError
		if errors.As(forkErr, &rateLimitErr) {
			return echo.NewHTTPError(http.StatusTooManyRequests, forkErr.Error())
		}
		s.Logger.Error("failed to fork checkpoint", "error", forkErr)

		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to create checkpoint",
		)
	}

	return c.JSON(http.StatusCreated, map[string]string{
		"checkpoint_id": cp.ID,
		"status":        cp.Status,
	})
}

// handleSaveCheckpoint updates a checkpoint's name (bookmark).
func (s *Server) handleSaveCheckpoint(c echo.Context) error {
	ctx := c.Request().Context()
	worldID := c.Param("worldID")

	var req struct {
		CheckpointID string `json:"checkpoint_id"` //nolint:tagliatelle // matches frontend JSON
		Name         string `json:"name"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	cp, err := s.DB.GetCheckpoint(ctx, req.CheckpointID)
	if err != nil || cp.WorldID != worldID {
		return echo.NewHTTPError(http.StatusNotFound, "checkpoint not found")
	}

	if err := s.DB.UpdateCheckpointName(ctx, sqlc.UpdateCheckpointNameParams{
		Name: sql.NullString{String: req.Name, Valid: req.Name != ""},
		ID:   req.CheckpointID,
	}); err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to save checkpoint",
		)
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "saved"})
}

// handleLogStream streams JSONL log content for a checkpoint.
func (s *Server) handleLogStream(c echo.Context) error {
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")
	logType := c.Param("logType") // "build", "claude", "game-server"

	logPath := filepath.Join(s.DataDir, "logs", "worlds", worldID, cpID, logType+".jsonl")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "log not found")
	}

	return c.File(logPath)
}

// handleWASMArtifacts serves static files from wasm-builds.
func (s *Server) handleWASMArtifacts(c echo.Context) error {
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")
	filePath := c.Param("*")

	fullPath := filepath.Join(s.DataDir, "wasm-builds", worldID, cpID, filePath)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "artifact not found")
	}

	return c.File(fullPath)
}

// handleClaudeEvent receives JSONL events POSTed by Claude Code hook scripts.
// This endpoint is unprotected (internal same-machine communication).
func (s *Server) handleClaudeEvent(c echo.Context) error {
	var event map[string]any
	if err := json.NewDecoder(c.Request().Body).Decode(&event); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	worldID, _ := event["worldID"].(string)
	cpID, _ := event["cpID"].(string)
	eventType, _ := event["event"].(string)

	s.Logger.Info("claude hook event", "worldID", worldID, "event", eventType)

	// Publish to world-specific bus (build progress, claude activity).
	if s.EventBus != nil {
		s.EventBus.Publish(worldID, event)
	}

	// If claude stopped, trigger the build pipeline.
	if eventType == "claude.session_stopped" && s.Orchestrator != nil {
		go s.Orchestrator.BuildCheckpoint(worldID, cpID)
	}

	return c.NoContent(http.StatusOK)
}

// handleChatMessage persists and broadcasts a chat message from an approved user.
func (s *Server) handleChatMessage(c echo.Context) error {
	ctx := c.Request().Context()

	user, err := requireUser(c)
	if err != nil {
		return err
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := c.Bind(&body); err != nil || body.Content == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	if err := s.DB.CreateMessage(ctx, sqlc.CreateMessageParams{
		ID:      uuid.New().String()[:8],
		Type:    "chat",
		UserID:  sql.NullString{String: user.ID, Valid: true},
		Content: body.Content,
	}); err != nil {
		s.Logger.Error("failed to persist chat message", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to send message")
	}

	if s.EventBus != nil {
		s.EventBus.PublishGlobal(map[string]any{
			"event":    "chat.message",
			"username": user.GitHubUsername,
			"avatar":   user.AvatarURL.String,
			"content":  body.Content,
			"ts":       time.Now().UTC().Format(time.RFC3339),
		})
	}

	return c.NoContent(http.StatusOK)
}
