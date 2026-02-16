package server

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// handleWorldCover serves the cover art image for a world.
// GET /api/worlds/:worldID/cover — Auth: approved users.
func (s *Server) handleWorldCover(c echo.Context) error {
	worldID := c.Param("worldID")

	w, err := s.DB.GetWorld(c.Request().Context(), worldID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "world not found")
	}

	if !w.CoverImagePath.Valid {
		return echo.NewHTTPError(http.StatusNotFound, "no cover image")
	}

	return c.File(w.CoverImagePath.String)
}
