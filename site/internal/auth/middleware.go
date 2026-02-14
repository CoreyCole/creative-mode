package auth

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// SessionMiddleware reads the session cookie, validates it, and sets
// the session in the Echo context. Redirects to login if invalid.
func SessionMiddleware(sm *SessionManager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cookie, err := c.Cookie("session")
			if err != nil || cookie.Value == "" {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/discord/login")
			}

			session := sm.GetSession(cookie.Value)
			if session == nil {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/discord/login")
			}

			c.Set("session", session)
			return next(c)
		}
	}
}

// InviteCodeMiddleware checks that the session has a verified invite code.
// Redirects to /invite if not verified.
func InviteCodeMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			session, ok := c.Get("session").(*Session)
			if !ok {
				return c.Redirect(http.StatusTemporaryRedirect, "/auth/discord/login")
			}

			if !session.InviteCodeVerified {
				return c.Redirect(http.StatusTemporaryRedirect, "/invite")
			}

			return next(c)
		}
	}
}
