package auth

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
)

// SessionMiddleware reads the session cookie, validates the session, loads the
// user, and sets it in the Echo context as "user". Redirects to login if
// the session is invalid or missing.
func SessionMiddleware(database *db.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()

			cookie, err := c.Cookie("session")
			if err != nil || cookie.Value == "" {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
			}

			session, err := database.GetSession(ctx, cookie.Value)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
				}
				return echo.NewHTTPError(
					http.StatusInternalServerError,
					"session lookup failed",
				)
			}

			user, err := database.GetUserByID(ctx, session.UserID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
				}
				return echo.NewHTTPError(
					http.StatusInternalServerError,
					"user lookup failed",
				)
			}

			_ = database.UpdateLastSeen(ctx, user.ID)
			c.Set("user", &user)

			return next(c)
		}
	}
}

// ApprovedMiddleware rejects users whose role is "pending" by redirecting
// them to the pending approval page.
func ApprovedMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, ok := c.Get("user").(*sqlc.User)
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
			user, ok := c.Get("user").(*sqlc.User)
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
