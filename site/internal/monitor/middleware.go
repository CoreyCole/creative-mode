package monitor

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
)

// PageViewMiddleware records each page view into the page_views table.
// It skips SSE endpoints, static files, and health checks.
func PageViewMiddleware(db *sql.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path

			if !shouldTrack(path) {
				return next(c)
			}

			visitorHash := hashIP(c.RealIP())
			_, _ = db.Exec("INSERT INTO page_views (path, visitor_hash) VALUES (?, ?)", path, visitorHash)

			return next(c)
		}
	}
}

// hashIP returns a truncated SHA-256 hex digest of the IP address.
func hashIP(ip string) string {
	h := sha256.Sum256([]byte(ip))
	return fmt.Sprintf("%x", h[:8])
}

func shouldTrack(path string) bool {
	// Skip static files.
	if strings.HasPrefix(path, "/css/") ||
		strings.HasPrefix(path, "/js/") ||
		strings.HasPrefix(path, "/img/") ||
		strings.HasPrefix(path, "/favicon") {
		return false
	}
	// Skip SSE endpoints.
	if strings.HasSuffix(path, "/events") {
		return false
	}
	// Skip health checks and webhooks.
	if path == "/health" || strings.HasPrefix(path, "/webhook/") {
		return false
	}
	return true
}
