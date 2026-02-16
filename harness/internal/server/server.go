package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/coreycole/creative-mode/pkg/markdown"
	"github.com/coreycole/creative-mode/pkg/mayorchat"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/auth"
	"creative-mode/harness/internal/claude"
	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/gemini"
	"creative-mode/harness/internal/mayor"
	"creative-mode/harness/internal/president"
	"creative-mode/harness/internal/world"
	"creative-mode/harness/views/admin"
	"creative-mode/harness/views/lobby"
	"creative-mode/harness/views/login"
	"creative-mode/harness/views/pending"
	worldview "creative-mode/harness/views/world"
)

const (
	debugProxyTimeout = 5 * time.Second
	logExtJSONL       = ".jsonl"
	logExtPlain       = ".log"
)

// Server holds application dependencies and registers HTTP routes.
type Server struct {
	DB               *db.DB
	Logger           *slog.Logger
	AuthHandler      *auth.Handler
	WorldManager     *world.Manager
	Orchestrator     *claude.Orchestrator
	EventBus         *events.EventBus
	GeminiClient     *gemini.Client
	MayorManager     *mayor.Manager
	PresidentManager *president.Manager
	DataDir          string
	dev              *devState // nil when DEV_MODE is not set

	// Create-world chat page dependencies.
	CreateConvMgr      *mayorchat.ConversationManager
	CreateClaudeClient *anthropic.Client
	CreateMDRenderer   *markdown.Renderer
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
		return render(c, login.Page(s.dev != nil))
	}

	ctx := c.Request().Context()

	session, err := s.DB.GetSession(ctx, cookie.Value)
	if err != nil {
		return render(c, login.Page(s.dev != nil))
	}

	user, err := s.DB.GetUserByID(ctx, session.UserID)
	if err != nil {
		return render(c, login.Page(s.dev != nil))
	}

	if user.Role == auth.RolePending {
		return render(c, pending.Page(&user))
	}

	worlds, err := s.DB.ListWorlds(ctx)
	if err != nil {
		s.Logger.Error("failed to list worlds", "error", err)
	}

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

	// Shared game assets (public, no auth required).
	e.GET("/assets/*", s.handleSharedAssets)
	e.Static("/static", "static")

	// Dev hot-reload endpoints (only when DEV_MODE=true).
	if os.Getenv("DEV_MODE") == "true" {
		s.dev = newDevState()
		e.GET("/dev/sse", s.handleDevSSE)
		e.POST("/dev/rebuild", s.handleDevRebuild)
		e.POST("/dev/rebuild-template", s.handleDevRebuildTemplate)
		e.POST("/dev/reload-static", s.handleDevReloadStatic)
		if s.AuthHandler != nil {
			e.POST("/dev/auth/login", s.AuthHandler.HandleDevLogin)
		}
	}

	// Health check endpoint.
	e.GET("/health", s.handleHealth)

	// Claude hook event endpoint (protected by CM_HOOK_SECRET if set).
	e.POST("/api/claude-event", s.handleClaudeEvent, hookSecretMiddleware())

	// World-hatched webhook (from site, protected by CM_HOOK_SECRET).
	e.POST("/api/world-hatched", s.handleWorldHatched, hookSecretMiddleware())

	// Mayor API (protected by per-world X-Mayor-Secret).
	mayorGroup := e.Group("/api/mayor")
	mayorGroup.Use(s.mayorAuthMiddleware)
	mayorGroup.POST("/build", s.handleMayorBuild)
	mayorGroup.GET("/status", s.handleMayorStatus)
	mayorGroup.POST("/contribute-learning", s.handleContributeLearning)

	// President API (protected by PRESIDENT_SECRET).
	presidentGroup := e.Group("/api/president")
	presidentGroup.Use(presidentAuthMiddleware())
	presidentGroup.GET("/mayor-status", s.handlePresidentMayorStatus)
	presidentGroup.POST("/repo-build", s.handlePresidentRepoBuild)
	presidentGroup.POST("/template-update", s.handlePresidentTemplateUpdate)
	presidentGroup.POST("/deploy", s.handlePresidentDeploy)

	// Root route — soft session check renders login/pending/lobby.
	e.GET("/", s.handleRoot)

	// Auth routes (no auth middleware).
	if s.AuthHandler == nil {
		return
	}

	e.GET("/auth/discord/login", s.AuthHandler.HandleLogin)
	e.GET("/auth/discord/callback", s.AuthHandler.HandleCallback)
	e.POST("/auth/logout", s.AuthHandler.HandleLogout)

	// Authenticated but possibly pending.
	authed := e.Group("", auth.SessionMiddleware(s.DB))
	authed.GET("/auth/pending", s.AuthHandler.HandlePendingApproval)

	// Approved users only.
	approved := authed.Group("", auth.ApprovedMiddleware())
	s.registerWorldRoutes(approved)

	// Mayor dashboard (approved users).
	approved.GET("/mayor/:worldID", s.handleMayorDashboard)
	approved.GET("/mayor/:worldID/events", s.handleMayorDashboardSSE)
	approved.GET("/mayor/:worldID/file", s.handleMayorFileRead)
	approved.PUT("/mayor/:worldID/file", s.handleMayorFileSave)

	// WASM artifact serving (approved users).
	approved.GET("/wasm/:worldID/:cpID/*", s.handleWASMArtifacts)

	// Create world chat page (approved users).
	approved.GET("/create", s.handleCreatePage)
	approved.POST("/create/chat", s.handleCreateChat)
	approved.GET("/create/cover-preview", s.handleCreateCoverPreview)
	approved.POST("/create/generate-cover", s.handleCreateGenerateCover)
	approved.POST("/create/hatch", s.handleCreateHatch)

	// SSE event stream (approved users — lobby).
	approved.GET("/events", s.handleGlobalSSE)

	// Chat (approved users).
	approved.POST("/api/chat", s.handleChatMessage)

	// Asset upload (approved users).
	approved.POST("/api/assets/upload", s.handleAssetUpload)

	// Cover art (approved users).
	approved.GET("/api/worlds/:worldID/cover", s.handleWorldCover)

	// Image generation (approved users).
	approved.POST("/api/images/generate", s.handleImageGenerate)
	approved.GET("/api/images/preview/:genID", s.handleImagePreview)
	approved.POST("/api/images/save", s.handleImageSave)

	// Asset tree (approved users).
	approved.GET("/api/assets/tree", s.handleAssetTree)

	// Room placement (approved users).
	approved.GET("/api/rooms", s.handleRoomList)
	approved.GET("/api/rooms/:roomID/hotspots", s.handleRoomHotspots)
	approved.POST("/api/rooms/:roomID/place/background", s.handlePlaceBackground)
	approved.POST("/api/rooms/:roomID/place/hotspot/:hotspotID", s.handlePlaceOnHotspot)
	approved.POST("/api/rooms/:roomID/place/new", s.handlePlaceNewHotspot)

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
	w.GET("/:worldID/status", s.handleWorldStatus)
	w.POST("/:worldID/debug", s.handleDebugProxy)
	w.POST("/:worldID/client-debug", s.handleClientDebug)
	w.POST("/:worldID/client-debug-response", s.handleClientDebugResponse)
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
		Name         string `json:"name"          form:"name"`
		Description  string `json:"description"   form:"description"`
		TemplateType string `json:"template_type" form:"template_type"` //nolint:tagliatelle // HTML form field name
	}
	if bindErr := c.Bind(&req); bindErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.TemplateType == "" {
		req.TemplateType = "3d"
	}
	if req.TemplateType != "3d" && req.TemplateType != "2d" &&
		req.TemplateType != "boardgame" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid template type")
	}

	w, err := s.WorldManager.CreateWorld(
		ctx,
		req.Name,
		req.Description,
		user.ID,
		req.TemplateType,
	)
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

	cpID, err := s.WorldManager.GetUserPosition(ctx, user.ID, worldID)
	if err != nil {
		s.Logger.Warn("failed to get user position", "error", err)
	}
	checkpoints, err := s.WorldManager.GetCheckpointTree(ctx, worldID)
	if err != nil {
		s.Logger.Warn("failed to get checkpoint tree", "error", err)
	}
	if cpID == "" {
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

	// Check for trunk serve (template world dev mode).
	trunkPort := 0
	if gs := s.WorldManager.GameServers.GetServer(worldID, cpID); gs != nil {
		trunkPort = gs.TrunkPort
	}

	// Auto-start trunk serve for template worlds with no static WASM build.
	if trunkPort == 0 && !cp.WasmPath.Valid && world.IsTemplateWorld(w.Name) {
		if port, err := s.WorldManager.EnsureTemplateTrunkServe(
			worldID,
			cpID,
			cp.DirPath,
		); err != nil {
			s.Logger.Warn(
				"failed to start on-demand trunk serve",
				"worldID",
				worldID,
				"error",
				err,
			)
		} else {
			trunkPort = port
		}
	}

	signals := worldview.DefaultOverlaySignals(worldID, cpID)

	return render(c, worldview.Page(
		w, cp, user, signals, serverPort, trunkPort, checkpoints,
	))
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

	if err := s.WorldManager.SetUserPosition(ctx, user.ID, worldID, cpID); err != nil {
		s.Logger.Warn("failed to set user position", "error", err)
	}

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

	if _, err := requireUser(c); err != nil {
		return err
	}

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

// handleLogStream streams log content for a checkpoint.
// Game server logs use .log extension (raw text); other logs use .jsonl.
func (s *Server) handleLogStream(c echo.Context) error {
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")
	logType := c.Param("logType") // "build", "claude", "game-server"

	baseDir := filepath.Join(s.DataDir, "logs", "worlds")

	// Game server logs are raw text (.log); others are JSONL.
	ext := logExtJSONL
	if logType == "game-server" {
		ext = logExtPlain
	}
	logPath := filepath.Clean(
		filepath.Join(baseDir, worldID, cpID, logType+ext),
	)
	if !strings.HasPrefix(
		logPath, filepath.Clean(baseDir)+string(os.PathSeparator),
	) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid path")
	}

	// Fall back to the other extension if primary doesn't exist.
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		altExt := logExtJSONL
		if ext == logExtJSONL {
			altExt = logExtPlain
		}
		altPath := filepath.Clean(
			filepath.Join(baseDir, worldID, cpID, logType+altExt),
		)
		if _, altErr := os.Stat(altPath); altErr == nil {
			return c.File(altPath)
		}
		return echo.NewHTTPError(http.StatusNotFound, "log not found")
	}

	return c.File(logPath)
}

// handleWASMArtifacts serves static files from wasm-builds.
func (s *Server) handleWASMArtifacts(c echo.Context) error {
	worldID := c.Param("worldID")
	cpID := c.Param("cpID")
	filePath := c.Param("*")

	baseDir := filepath.Join(s.DataDir, "wasm-builds")
	fullPath := filepath.Clean(filepath.Join(baseDir, worldID, cpID, filePath))
	if !strings.HasPrefix(fullPath, filepath.Clean(baseDir)+string(os.PathSeparator)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid path")
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "artifact not found")
	}

	return c.File(fullPath)
}

// handleSharedAssets serves files from data/shared-assets with cache and MIME headers.
func (s *Server) handleSharedAssets(c echo.Context) error {
	filePath := c.Param("*")

	baseDir := filepath.Join(s.DataDir, "shared-assets")
	fullPath := filepath.Clean(filepath.Join(baseDir, filePath))
	if !strings.HasPrefix(fullPath, filepath.Clean(baseDir)+string(os.PathSeparator)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid path")
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "asset not found")
	}

	// Set MIME types for game-specific formats not in Go's default registry.
	ext := strings.ToLower(filepath.Ext(fullPath))
	switch ext {
	case ".glb":
		c.Response().Header().Set("Content-Type", "model/gltf-binary")
	case ".gltf":
		c.Response().Header().Set("Content-Type", "model/gltf+json")
	case ".ktx2":
		c.Response().Header().Set("Content-Type", "image/ktx2")
	}

	// In production, enable browser caching with background revalidation.
	// Room JSON files use no-cache so placement updates are always fetched fresh.
	if os.Getenv("DEV_MODE") != "true" {
		if strings.HasSuffix(fullPath, ".room.json") {
			c.Response().Header().Set("Cache-Control", "no-cache")
		} else {
			c.Response().
				Header().
				Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
		}
	}

	return c.File(fullPath)
}

// hookSecretMiddleware validates the X-Hook-Secret header against CM_HOOK_SECRET.
// If CM_HOOK_SECRET is not set, all requests are allowed.
func hookSecretMiddleware() echo.MiddlewareFunc {
	secret := os.Getenv("CM_HOOK_SECRET")
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if secret != "" && c.Request().Header.Get("X-Hook-Secret") != secret {
				return echo.NewHTTPError(http.StatusForbidden, "invalid hook secret")
			}
			return next(c)
		}
	}
}

// handleClaudeEvent receives JSONL events POSTed by Claude Code hook scripts.
// Protected by CM_HOOK_SECRET header when the env var is set.
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
	if eventType == events.EventClaudeSessionStop && s.Orchestrator != nil {
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
			"event":    events.EventChatMessage,
			"username": user.DiscordUsername,
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

// handleDebugProxy forwards a JSON-RPC 2.0 request to the game server's BRP port.
func (s *Server) handleDebugProxy(c echo.Context) error {
	ctx := c.Request().Context()
	worldID := c.Param("worldID")

	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cpID, err := s.WorldManager.GetUserPosition(ctx, user.ID, worldID)
	if err != nil || cpID == "" {
		return echo.NewHTTPError(http.StatusNotFound, "no active checkpoint")
	}

	gs := s.WorldManager.GameServers.GetServer(worldID, cpID)
	if gs == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "game server not running")
	}

	targetURL := fmt.Sprintf("http://localhost:%d", gs.BRPPort)

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, targetURL, c.Request().Body,
	)
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError, "failed to create request",
		)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: debugProxyTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "debug server unreachable")
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return c.JSONBlob(resp.StatusCode, body)
}

// handleWorldStatus returns JSON with the world's current checkpoint and game server info.
func (s *Server) handleWorldStatus(c echo.Context) error {
	ctx := c.Request().Context()
	worldID := c.Param("worldID")

	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cpID, err := s.WorldManager.GetUserPosition(ctx, user.ID, worldID)
	if err != nil || cpID == "" {
		return echo.NewHTTPError(http.StatusNotFound, "no active checkpoint")
	}

	cp, err := s.DB.GetCheckpoint(ctx, cpID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "checkpoint not found")
	}

	w, err := s.DB.GetWorld(ctx, worldID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get world")
	}

	result := map[string]any{
		"world_id":      worldID,
		"checkpoint_id": cpID,
		"build_status":  cp.Status,
		"template_type": w.TemplateType,
	}

	if gs := s.WorldManager.GameServers.GetServer(worldID, cpID); gs != nil {
		result["game_server"] = map[string]any{
			"running":  true,
			"port":     gs.Port,
			"brp_port": gs.BRPPort,
			"mode":     gs.Mode,
		}
	} else {
		result["game_server"] = map[string]any{
			"running": false,
		}
	}

	return c.JSON(http.StatusOK, result)
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
