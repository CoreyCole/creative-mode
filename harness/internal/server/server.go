package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/auth"
	"creative-mode/harness/internal/claude"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/world"
	"creative-mode/harness/views/admin"
	"creative-mode/harness/views/lobby"
	"creative-mode/harness/views/login"
	"creative-mode/harness/views/pending"
	worldview "creative-mode/harness/views/world"
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

// handleRoot performs a soft session check and renders the appropriate page:
// login (unauthenticated), pending (awaiting approval), or lobby (approved).
func (s *Server) handleRoot(c echo.Context) error {
	cookie, err := c.Cookie("session")
	if err != nil || cookie.Value == "" {
		return render(c, login.Page())
	}

	ctx := c.Request().Context()

	session, err := s.DB.GetSession(ctx, cookie.Value)
	if err != nil {
		return render(c, login.Page())
	}

	user, err := s.DB.GetUserByID(ctx, session.UserID)
	if err != nil {
		return render(c, login.Page())
	}

	if user.Role == "pending" {
		return render(c, pending.Page(&user))
	}

	worlds, _ := s.DB.ListWorlds(ctx)

	return render(c, lobby.Page(&user, worlds))
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

	// Root route — soft session check renders login/pending/lobby.
	e.GET("/", s.handleRoot)

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

	// SSE event stream (approved users — lobby).
	approved.GET("/events", s.handleGlobalSSE)

	// Chat (approved users).
	approved.POST("/api/chat", s.handleChatMessage)

	// Admin only.
	adminGroup := authed.Group("/admin", auth.AdminMiddleware())
	adminGroup.GET("/users", s.handleAdminUsers)
	adminGroup.POST("/users/:userID/approve", s.AuthHandler.HandleApproveUser)
	adminGroup.POST("/users/:userID/reject", s.AuthHandler.HandleRejectUser)
}

// registerWorldRoutes adds world management endpoints to the approved group.
func (s *Server) registerWorldRoutes(approved *echo.Group) {
	if s.WorldManager == nil {
		return
	}
	w := approved.Group("/world")
	w.POST("/create", s.handleCreateWorld)
	w.GET("/:worldID", s.handleWorldView)
	w.GET("/:worldID/events", s.handleWorldSSE)
	w.GET("/:worldID/lineage/:cpID", s.handleLineage)
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

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	return sse.ExecuteScript(fmt.Sprintf("window.location.href='/world/%s'", w.ID))
}

// handleWorldView renders the game iframe with overlay for a world.
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
	if cpID == "" {
		checkpoints, _ := s.WorldManager.GetCheckpointTree(ctx, worldID)
		if len(checkpoints) > 0 {
			cpID = checkpoints[0].ID
		}
	}

	cp, err := s.DB.GetCheckpoint(ctx, cpID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "checkpoint not found")
	}

	// Read server port from DB — already stored by BuildCheckpoint.
	// Do NOT call GameServers.Connect here — no matching Disconnect would leak refcounts.
	serverPort := 0
	if cp.ServerPort.Valid {
		serverPort = int(cp.ServerPort.Int64)
	}

	signals := worldview.DefaultOverlaySignals(worldID, cpID)

	return render(c, worldview.Page(w, cp, user, signals, serverPort))
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

	var input struct {
		PromptText          string `json:"prompt_text"`           //nolint:tagliatelle // Datastar signal name
		CurrentCheckpointID string `json:"current_checkpoint_id"` //nolint:tagliatelle // Datastar signal name
	}
	if err := datastar.ReadSignals(c.Request(), &input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}
	if input.PromptText == "" || input.CurrentCheckpointID == "" {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"prompt_text and current_checkpoint_id are required",
		)
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Use Orchestrator if available (launches Claude Code in tmux).
	if s.Orchestrator != nil {
		_, promptErr := s.Orchestrator.HandlePrompt(
			ctx,
			worldID,
			input.CurrentCheckpointID,
			input.PromptText,
			user.ID,
		)
		if promptErr != nil {
			var rateLimitErr *world.RateLimitError
			if errors.As(promptErr, &rateLimitErr) {
				return sse.MarshalAndPatchSignals(map[string]any{
					"build_status": "rate_limited",
					"prompt_text":  "",
				})
			}
			s.Logger.Error("failed to handle prompt", "error", promptErr)

			return echo.NewHTTPError(
				http.StatusInternalServerError,
				"failed to create checkpoint",
			)
		}

		return sse.MarshalAndPatchSignals(map[string]any{
			"build_status": "editing",
			"prompt_text":  "",
		})
	}

	// Fallback: fork-only (no Claude session).
	_, forkErr := s.WorldManager.ForkCheckpoint(
		ctx,
		worldID,
		input.CurrentCheckpointID,
		input.PromptText,
		user.ID,
	)
	if forkErr != nil {
		var rateLimitErr *world.RateLimitError
		if errors.As(forkErr, &rateLimitErr) {
			return sse.MarshalAndPatchSignals(map[string]any{
				"build_status": "rate_limited",
				"prompt_text":  "",
			})
		}
		s.Logger.Error("failed to fork checkpoint", "error", forkErr)

		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to create checkpoint",
		)
	}

	return sse.MarshalAndPatchSignals(map[string]any{
		"build_status": "editing",
		"prompt_text":  "",
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

	var input struct {
		ChatText string `json:"chat_text"` //nolint:tagliatelle // Datastar signal name
	}
	if err := datastar.ReadSignals(c.Request(), &input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}
	if input.ChatText == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "empty message")
	}

	if err := s.DB.CreateMessage(ctx, sqlc.CreateMessageParams{
		ID:      uuid.New().String()[:8],
		Type:    "chat",
		UserID:  sql.NullString{String: user.ID, Valid: true},
		Content: input.ChatText,
	}); err != nil {
		s.Logger.Error("failed to persist chat message", "error", err)

		return echo.NewHTTPError(http.StatusInternalServerError, "failed to send message")
	}

	if s.EventBus != nil {
		s.EventBus.PublishGlobal(map[string]any{
			"event":    "chat.message",
			"username": user.GitHubUsername,
			"avatar":   user.AvatarURL.String,
			"content":  input.ChatText,
			"ts":       time.Now().UTC().Format("15:04"),
		})
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	return sse.MarshalAndPatchSignals(map[string]any{"chat_text": ""})
}

// handleLineage renders the checkpoint ancestry from root to the given checkpoint.
func (s *Server) handleLineage(c echo.Context) error {
	ctx := c.Request().Context()
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")

	ancestry, err := s.DB.GetCheckpointAncestry(ctx, worldID, cpID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get lineage")
	}

	return render(c, worldview.Lineage(ancestry))
}

// handleAdminUsers renders the admin user management page.
func (s *Server) handleAdminUsers(c echo.Context) error {
	ctx := c.Request().Context()

	users, err := s.DB.ListUsers(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list users")
	}

	return render(c, admin.Page(users))
}
