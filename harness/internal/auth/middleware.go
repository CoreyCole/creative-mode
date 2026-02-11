package auth

import (
	"net/http"

	"creative-mode/harness/internal/db"

	"github.com/labstack/echo/v4"
)

// SessionMiddleware reads the session cookie, validates the session, loads the
// user, and sets it in the Echo context as "user". Redirects to login if
// the session is invalid or missing.
func SessionMiddleware(database *db.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cookie, err := c.Cookie("session")
			if err != nil || cookie.Value == "" {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
			}

			session, err := database.GetSession(cookie.Value)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "session lookup failed")
			}
			if session == nil {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
			}

			user, err := database.GetUserByID(session.UserID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "user lookup failed")
			}
			if user == nil {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
			}

			_ = database.UpdateLastSeen(user.ID)
			c.Set("user", user)

			return next(c)
		}
	}
}

// ApprovedMiddleware rejects users whose role is "pending" by redirecting
// them to the pending approval page.
func ApprovedMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, ok := c.Get("user").(*db.User)
			if !ok {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
			}
			if user.Role == "pending" {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/pending")
			}
			return next(c)
		}
	}
}

// AdminMiddleware rejects non-admin users with a 403 Forbidden response.
func AdminMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, ok := c.Get("user").(*db.User)
			if !ok {
				return echo.NewHTTPError(http.StatusForbidden, "forbidden")
			}
			if user.Role != "admin" {
				return echo.NewHTTPError(http.StatusForbidden, "admin access required")
			}
			return next(c)
		}
	}
}
