package server

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/db/sqlc"
)

const defaultQueryLimit = 50

// presidentAuthMiddleware validates the X-President-Secret header.
func presidentAuthMiddleware() echo.MiddlewareFunc {
	secret := os.Getenv("PRESIDENT_SECRET")
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if secret == "" {
				return echo.NewHTTPError(
					http.StatusServiceUnavailable,
					"president not configured",
				)
			}
			if c.Request().Header.Get("X-President-Secret") != secret {
				return echo.NewHTTPError(http.StatusForbidden, "invalid president secret")
			}
			return next(c)
		}
	}
}

// handlePresidentMayorStatus returns status of all worlds with mayors.
// GET /api/president/mayor-status — Auth: X-President-Secret
func (s *Server) handlePresidentMayorStatus(c echo.Context) error {
	ctx := c.Request().Context()

	worlds, err := s.DB.GetWorldsWithDiscordChannels(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to query worlds")
	}

	//nolint:tagliatelle // snake_case JSON is the public API contract
	type worldStatus struct {
		WorldID        string `json:"world_id"`
		WorldName      string `json:"world_name"`
		MayorName      string `json:"mayor_name"`
		TemplateType   string `json:"template_type"`
		ChannelID      string `json:"discord_channel_id"`
		Checkpoints    int    `json:"checkpoint_count"`
		LatestStatus   string `json:"latest_status,omitempty"`
		GameRunning    bool   `json:"game_server_running"`
		RecentBuilds   int    `json:"recent_builds"`
		RecentActivity int    `json:"recent_activity"`
	}

	results := make([]worldStatus, 0, len(worlds))
	for _, w := range worlds {
		ws := worldStatus{
			WorldID:      w.ID,
			WorldName:    w.Name,
			MayorName:    w.MayorName.String,
			TemplateType: w.TemplateType,
			ChannelID:    w.DiscordChannelID.String,
		}

		checkpoints, cpErr := s.DB.GetCheckpointTree(ctx, w.ID)
		if cpErr == nil {
			ws.Checkpoints = len(checkpoints)
			if len(checkpoints) > 0 {
				latest := checkpoints[len(checkpoints)-1]
				ws.LatestStatus = latest.Status
				if s.WorldManager != nil {
					if gs := s.WorldManager.GameServers.GetServer(
						w.ID,
						latest.ID,
					); gs != nil {
						ws.GameRunning = true
					}
				}
			}
		}

		builds, bErr := s.DB.GetMayorBuilds(ctx, sqlc.GetMayorBuildsParams{
			WorldID: w.ID, Limit: defaultQueryLimit,
		})
		if bErr == nil {
			ws.RecentBuilds = len(builds)
		}

		activity, aErr := s.DB.GetMayorActivity(ctx, sqlc.GetMayorActivityParams{
			WorldID: w.ID, Limit: defaultQueryLimit,
		})
		if aErr == nil {
			ws.RecentActivity = len(activity)
		}

		results = append(results, ws)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"worlds":    results,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// handlePresidentRepoBuild spawns a tmux session to run `just check` at the repo root.
// POST /api/president/repo-build — Auth: X-President-Secret
func (s *Server) handlePresidentRepoBuild(c echo.Context) error {
	repoRoot, err := os.Getwd()
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to get working directory",
		)
	}
	// Harness runs from harness/, so repo root is parent.
	repoRoot = filepath.Clean(filepath.Join(repoRoot, ".."))

	sessionName := fmt.Sprintf("cm-president-%d", time.Now().Unix())

	cmd := exec.CommandContext(c.Request().Context(),
		"tmux",
		"new-session",
		"-d",
		"-s",
		sessionName,
		"-c",
		repoRoot,
		"just check 2>&1; echo '[DONE] exit code:'$?",
	)
	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			fmt.Sprintf("failed to start build: %s: %s", cmdErr, string(output)))
	}

	s.Logger.Info("president repo-build started", "session", sessionName)

	return c.JSON(http.StatusAccepted, map[string]string{
		"status":  "building",
		"session": sessionName,
	})
}

// handlePresidentTemplateUpdate spawns a Claude Code session at the repo root.
// POST /api/president/template-update — Auth: X-President-Secret
func (s *Server) handlePresidentTemplateUpdate(c echo.Context) error {
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := c.Bind(&req); err != nil || req.Prompt == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "prompt is required")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to get working directory",
		)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, ".."))

	sessionName := fmt.Sprintf("cm-president-tpl-%d", time.Now().Unix())

	cmd := exec.CommandContext(c.Request().Context(),
		"tmux",
		"new-session",
		"-d",
		"-s",
		sessionName,
		"-c",
		repoRoot,
		fmt.Sprintf("claude --print '%s' 2>&1; echo '[DONE]'", req.Prompt),
	)
	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			fmt.Sprintf(
				"failed to start template update: %s: %s",
				cmdErr,
				string(output),
			),
		)
	}

	s.Logger.Info("president template-update started",
		"session", sessionName,
		"prompt_len", len(req.Prompt),
	)

	return c.JSON(http.StatusAccepted, map[string]string{
		"status":  "started",
		"session": sessionName,
	})
}

// handlePresidentDeploy runs the build pipeline and restarts the harness service.
// POST /api/president/deploy — Auth: X-President-Secret
func (s *Server) handlePresidentDeploy(c echo.Context) error {
	repoRoot, err := os.Getwd()
	if err != nil {
		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to get working directory",
		)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, ".."))

	sessionName := fmt.Sprintf("cm-president-deploy-%d", time.Now().Unix())

	cmd := exec.CommandContext(c.Request().Context(),
		"tmux",
		"new-session",
		"-d",
		"-s",
		sessionName,
		"-c",
		repoRoot,
		"just vps-deploy 2>&1; echo '[DONE] exit code:'$?",
	)
	if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			fmt.Sprintf("failed to start deploy: %s: %s", cmdErr, string(output)))
	}

	s.Logger.Info("president deploy started", "session", sessionName)

	return c.JSON(http.StatusAccepted, map[string]string{
		"status":  "deploying",
		"session": sessionName,
	})
}
