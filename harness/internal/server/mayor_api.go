package server

import (
	"database/sql"
	"encoding/base64"
	"net/http"

	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/db/sqlc"
)

// handleWorldHatched receives a webhook from the site when a world is hatched.
// It creates a harness world record and kicks off mayor agent provisioning.
// Auth: hookSecretMiddleware (CM_HOOK_SECRET).
func (s *Server) handleWorldHatched(c echo.Context) error {
	//nolint:tagliatelle // snake_case JSON is the public API contract
	var req struct {
		DiscordChannelID string `json:"discord_channel_id"`
		WorldName        string `json:"world_name"`
		MayorName        string `json:"mayor_name"`
		CreatorDiscordID string `json:"creator_discord_id"`
		CreatorUsername  string `json:"creator_username"`
		CoverImageBase64 string `json:"cover_image_base64"`
		CoverImageMIME   string `json:"cover_image_mime"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.DiscordChannelID == "" || req.WorldName == "" || req.MayorName == "" {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"discord_channel_id, world_name, and mayor_name are required",
		)
	}

	// Decode cover art if present (graceful — log warning on failure, continue without).
	var coverImageData []byte
	coverImageMIME := req.CoverImageMIME
	if req.CoverImageBase64 != "" {
		var decodeErr error
		coverImageData, decodeErr = base64.StdEncoding.DecodeString(req.CoverImageBase64)
		if decodeErr != nil {
			s.Logger.Warn("failed to decode cover art base64, continuing without",
				"error", decodeErr)
			coverImageData = nil
		}
	}

	s.Logger.Info("world-hatched webhook received",
		"world_name", req.WorldName,
		"mayor_name", req.MayorName,
		"discord_channel_id", req.DiscordChannelID,
		"creator", req.CreatorUsername,
		"has_cover_art", len(coverImageData) > 0,
	)

	// Provision mayor agent asynchronously if MayorManager is available.
	if s.MayorManager != nil {
		go func() {
			if err := s.MayorManager.ProvisionFromWebhook(
				req.DiscordChannelID,
				req.WorldName,
				req.MayorName,
				req.CreatorDiscordID,
				req.CreatorUsername,
				coverImageData,
				coverImageMIME,
			); err != nil {
				s.Logger.Error("failed to provision mayor agent",
					"world_name", req.WorldName,
					"error", err,
				)
			}
		}()
	}

	return c.JSON(http.StatusAccepted, map[string]string{"status": "accepted"})
}

// mayorAuthMiddleware validates the X-Mayor-Secret header against per-world
// secrets in the DB. Sets "mayor_world" on the context.
func (s *Server) mayorAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		secret := c.Request().Header.Get("X-Mayor-Secret")
		if secret == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing mayor secret")
		}

		w, err := s.DB.GetWorldByMayorSecret(c.Request().Context(), sql.NullString{
			String: secret,
			Valid:  true,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "invalid mayor secret")
		}

		c.Set("mayor_world", &w)
		return next(c)
	}
}

// requireMayorWorld extracts the authenticated world from the context.
func requireMayorWorld(c echo.Context) (*sqlc.World, error) {
	w, ok := c.Get("mayor_world").(*sqlc.World)
	if !ok {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "mayor world not found")
	}
	return w, nil
}

// handleMayorBuild triggers a build for the mayor's world.
// POST /api/mayor/build — Auth: X-Mayor-Secret
func (s *Server) handleMayorBuild(c echo.Context) error {
	ctx := c.Request().Context()

	w, err := requireMayorWorld(c)
	if err != nil {
		return err
	}

	var req struct {
		Prompt string `json:"prompt"`
	}
	if bindErr := c.Bind(&req); bindErr != nil || req.Prompt == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "prompt is required")
	}

	if s.Orchestrator == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"orchestrator not configured",
		)
	}

	// Find the latest ready checkpoint for this world.
	checkpoints, err := s.DB.GetCheckpointTree(ctx, w.ID)
	if err != nil || len(checkpoints) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no checkpoints found for world")
	}

	// Use the most recent ready checkpoint, or the last one if none are ready.
	var sourceCPID string
	for i := len(checkpoints) - 1; i >= 0; i-- {
		if checkpoints[i].Status == "ready" {
			sourceCPID = checkpoints[i].ID
			break
		}
	}
	if sourceCPID == "" {
		sourceCPID = checkpoints[len(checkpoints)-1].ID
	}

	// Use the world creator as the build user, or fall back to empty string.
	userID := ""
	if w.CreatedBy.Valid {
		userID = w.CreatedBy.String
	}

	cp, err := s.Orchestrator.HandlePrompt(ctx, w.ID, sourceCPID, req.Prompt, userID)
	if err != nil {
		s.Logger.Error("mayor build failed", "world_id", w.ID, "error", err)
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"build failed: "+err.Error(),
		)
	}

	return c.JSON(http.StatusAccepted, map[string]string{
		"status":        "building",
		"checkpoint_id": cp.ID,
		"world_id":      w.ID,
	})
}

// handleMayorStatus returns the current state of the mayor's world.
// GET /api/mayor/status — Auth: X-Mayor-Secret
func (s *Server) handleMayorStatus(c echo.Context) error {
	ctx := c.Request().Context()

	w, err := requireMayorWorld(c)
	if err != nil {
		return err
	}

	checkpoints, err := s.DB.GetCheckpointTree(ctx, w.ID)
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to get checkpoints",
		)
	}

	result := map[string]any{
		"world_id":      w.ID,
		"world_name":    w.Name,
		"template_type": w.TemplateType,
		"mayor_name":    w.MayorName.String,
		"checkpoints":   len(checkpoints),
	}

	if len(checkpoints) > 0 {
		latest := checkpoints[len(checkpoints)-1]
		result["latest_checkpoint"] = map[string]any{
			"id":     latest.ID,
			"status": latest.Status,
			"prompt": latest.Prompt.String,
		}

		if s.WorldManager != nil {
			if gs := s.WorldManager.GameServers.GetServer(w.ID, latest.ID); gs != nil {
				result["game_server"] = map[string]any{
					"running":  true,
					"port":     gs.Port,
					"brp_port": gs.BRPPort,
					"mode":     gs.Mode,
				}
			}
		}
	}

	return c.JSON(http.StatusOK, result)
}

// handleContributeLearning creates a PR with knowledge the mayor wants to share.
// POST /api/mayor/contribute-learning — Auth: X-Mayor-Secret
func (s *Server) handleContributeLearning(c echo.Context) error {
	w, err := requireMayorWorld(c)
	if err != nil {
		return err
	}

	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Path    string `json:"path"` // e.g., "templates/2d/CLAUDE.md"
	}
	if err := c.Bind(&req); err != nil || req.Title == "" || req.Content == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "title and content are required")
	}

	if s.MayorManager == nil {
		return echo.NewHTTPError(
			http.StatusServiceUnavailable,
			"mayor manager not configured",
		)
	}

	s.Logger.Info("mayor contribute-learning request",
		"world_id", w.ID,
		"title", req.Title,
	)

	go func() {
		if err := s.MayorManager.ContributeLearning(
			w.ID,
			req.Title,
			req.Content,
			req.Path,
		); err != nil {
			s.Logger.Error("failed to contribute learning",
				"world_id", w.ID,
				"error", err,
			)
		}
	}()

	return c.JSON(http.StatusAccepted, map[string]string{
		"status":  "accepted",
		"message": "learning contribution queued",
	})
}
