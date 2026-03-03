package server

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/db/sqlc"
	mayorview "creative-mode/harness/views/mayor"
)

// handleMayorDashboard renders the mayor dashboard for a world.
func (s *Server) handleMayorDashboard(c echo.Context) error {
	ctx := c.Request().Context()
	worldID := c.Param("worldID")

	w, err := s.DB.GetWorld(ctx, worldID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "world not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get world")
	}

	checkpoints, _ := s.DB.GetCheckpointTree(ctx, worldID)
	builds, _ := s.DB.GetMayorBuilds(ctx, sqlc.GetMayorBuildsParams{
		WorldID: worldID, Limit: defaultQueryLimit,
	})
	activity, _ := s.DB.GetMayorActivity(ctx, sqlc.GetMayorActivityParams{
		WorldID: worldID, Limit: defaultQueryLimit,
	})
	messages, _ := s.DB.GetMayorMessages(ctx, worldID)
	sessions, _ := s.DB.GetMayorSessions(ctx, sqlc.GetMayorSessionsParams{
		WorldID: worldID, Limit: defaultQueryLimit,
	})

	// Read and render mayor workspace files for the Memory tab.
	var soulHTML, memoryHTML string
	openclawHome := os.Getenv("OPENCLAW_HOME")
	if openclawHome == "" {
		openclawHome = filepath.Join(s.DataDir, "openclaw")
	}
	wsDir := filepath.Join(openclawHome, "workspaces", "world-"+worldID)
	p1 := filepath.Join(wsDir, "SOUL.md")
	p2 := filepath.Join(wsDir, "MEMORY.md")
	if b, err := os.ReadFile(p1); err == nil { //nolint:gosec // hardcoded filename
		soulHTML = s.CreateMDRenderer.MarkdownBytesToHTML(b)
	}
	if b, err := os.ReadFile(p2); err == nil { //nolint:gosec // hardcoded filename
		memoryHTML = s.CreateMDRenderer.MarkdownBytesToHTML(b)
	}

	data := mayorview.DashboardData{
		World:       w,
		Checkpoints: checkpoints,
		Builds:      builds,
		Activity:    activity,
		Messages:    messages,
		Sessions:    sessions,
		SoulHTML:    soulHTML,
		MemoryHTML:  memoryHTML,
	}

	return render(c, mayorview.Page(data))
}

// handleMayorDashboardSSE provides live updates for the mayor dashboard.
func (s *Server) handleMayorDashboardSSE(c echo.Context) error {
	worldID := c.Param("worldID")
	r := c.Request()
	sse := datastar.NewSSE(c.Response().Writer, r)

	worldCh := s.EventBus.Subscribe(worldID)
	defer s.EventBus.Unsubscribe(worldID, worldCh)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	// Stream events until client disconnects.
	for {
		select {
		case <-worldCh:
			// Refresh builds, activity, and messages on any world event.
			ctx := r.Context()
			builds, _ := s.DB.GetMayorBuilds(ctx, sqlc.GetMayorBuildsParams{
				WorldID: worldID, Limit: defaultQueryLimit,
			})
			activity, _ := s.DB.GetMayorActivity(ctx, sqlc.GetMayorActivityParams{
				WorldID: worldID, Limit: defaultQueryLimit,
			})
			messages, _ := s.DB.GetMayorMessages(ctx, worldID)
			if err := sse.PatchElementTempl(
				mayorview.BuildsTab(builds),
				datastar.WithSelectorID("mayor-builds-tab"),
			); err != nil {
				return nil
			}
			if err := sse.PatchElementTempl(
				mayorview.ActivityTab(activity),
				datastar.WithSelectorID("mayor-activity-tab"),
			); err != nil {
				return nil
			}
			if err := sse.PatchElementTempl(
				mayorview.MessagesTab(messages),
				datastar.WithSelectorID("mayor-messages-tab"),
			); err != nil {
				return nil
			}
		case <-heartbeat.C:
			if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
				return nil
			}
		case <-r.Context().Done():
			return nil
		}
	}
}

// handleMayorFileRead reads a workspace file for the mayor.
// Allowlist: SOUL.md, MEMORY.md, AGENTS.md, IDENTITY.md, USER.md
func (s *Server) handleMayorFileRead(c echo.Context) error {
	worldID := c.Param("worldID")
	filename := c.QueryParam("name")

	if !isAllowedMayorFile(filename) {
		return echo.NewHTTPError(http.StatusBadRequest, "file not allowed")
	}

	openclawHome := os.Getenv("OPENCLAW_HOME")
	if openclawHome == "" {
		openclawHome = filepath.Join(s.DataDir, "openclaw")
	}

	baseDir := filepath.Join(openclawHome, "workspaces", "world-"+worldID)
	fullPath := filepath.Clean(filepath.Join(baseDir, filename))
	if !strings.HasPrefix(fullPath, filepath.Clean(baseDir)+string(os.PathSeparator)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid path")
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "file not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read file")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"name":    filename,
		"content": string(content),
	})
}

// handleMayorFileSave writes a workspace file for the mayor.
// Only SOUL.md, MEMORY.md, AGENTS.md are editable.
func (s *Server) handleMayorFileSave(c echo.Context) error {
	worldID := c.Param("worldID")

	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	if !isEditableMayorFile(req.Name) {
		return echo.NewHTTPError(http.StatusBadRequest, "file not editable")
	}

	openclawHome := os.Getenv("OPENCLAW_HOME")
	if openclawHome == "" {
		openclawHome = filepath.Join(s.DataDir, "openclaw")
	}

	baseDir := filepath.Join(openclawHome, "workspaces", "world-"+worldID)
	fullPath := filepath.Clean(filepath.Join(baseDir, req.Name))
	if !strings.HasPrefix(fullPath, filepath.Clean(baseDir)+string(os.PathSeparator)) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid path")
	}

	if err := os.WriteFile(fullPath, []byte(req.Content), 0o600); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to write file")
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "saved"})
}

var allowedMayorFiles = map[string]bool{
	"SOUL.md":     true,
	"MEMORY.md":   true,
	"AGENTS.md":   true,
	"IDENTITY.md": true,
	"USER.md":     true,
}

var editableMayorFiles = map[string]bool{
	"SOUL.md":   true,
	"MEMORY.md": true,
	"AGENTS.md": true,
}

func isAllowedMayorFile(name string) bool {
	return allowedMayorFiles[name]
}

func isEditableMayorFile(name string) bool {
	return editableMayorFiles[name]
}
