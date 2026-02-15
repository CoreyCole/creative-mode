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
				return c.Redirect(http.StatusFound, "/auth/discord/login")
			}

			session := sm.GetSession(cookie.Value)
			if session == nil {
				return c.Redirect(http.StatusFound, "/auth/discord/login")
			}

			c.Set("session", session)
			return next(c)
		}
	}
}

// GuildMemberMiddleware checks that the session has verified Discord guild membership.
// Redirects to /join-discord if not verified.
func GuildMemberMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			session, ok := c.Get("session").(*Session)
			if !ok {
				return c.Redirect(http.StatusFound, "/auth/discord/login")
			}

			if !session.GuildMemberVerified {
				return c.Redirect(http.StatusFound, "/join-discord")
			}

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
				return c.Redirect(http.StatusFound, "/auth/discord/login")
			}

			if !session.InviteCodeVerified {
				return c.Redirect(http.StatusFound, "/invite")
			}

			return next(c)
		}
	}
}
