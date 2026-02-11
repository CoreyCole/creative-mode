package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"creative-mode/harness/internal/db"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// Config holds GitHub OAuth configuration.
type Config struct {
	GitHubClientID     string
	GitHubClientSecret string
	BaseURL            string // e.g. "http://localhost:8080"
}

// Handler implements GitHub OAuth login, callback, and logout.
type Handler struct {
	db     *db.DB
	config *Config
	logger *slog.Logger
}

// NewHandler creates a new auth handler.
func NewHandler(database *db.DB, config *Config, logger *slog.Logger) *Handler {
	return &Handler{db: database, config: config, logger: logger}
}

// HandleLogin redirects to GitHub OAuth authorize URL with a CSRF state token.
func (h *Handler) HandleLogin(c echo.Context) error {
	state, err := randomHex(16)
	if err != nil {
		return fmt.Errorf("generating state: %w", err)
	}

	c.SetCookie(&http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !isLocalhost(h.config.BaseURL),
	})

	redirectURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user&state=%s",
		url.QueryEscape(h.config.GitHubClientID),
		url.QueryEscape(h.config.BaseURL+"/auth/github/callback"),
		url.QueryEscape(state),
	)

	return c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// HandleCallback processes the GitHub OAuth callback.
func (h *Handler) HandleCallback(c echo.Context) error {
	// Validate state parameter (CSRF protection).
	stateCookie, err := c.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing OAuth state cookie")
	}
	if c.QueryParam("state") != stateCookie.Value {
		return echo.NewHTTPError(http.StatusBadRequest, "OAuth state mismatch")
	}

	// Clear the state cookie.
	c.SetCookie(&http.Cookie{
		Name:   "oauth_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	code := c.QueryParam("code")
	if code == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing OAuth code")
	}

	// Exchange code for access token.
	accessToken, err := h.exchangeCode(code)
	if err != nil {
		h.logger.Error("failed to exchange OAuth code", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "OAuth token exchange failed")
	}

	// Fetch user info from GitHub.
	ghUser, err := h.fetchGitHubUser(accessToken)
	if err != nil {
		h.logger.Error("failed to fetch GitHub user", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch GitHub user info")
	}

	// Determine role: first user is admin, others are pending.
	// Check if user already exists first.
	existingUser, err := h.db.GetUserByGitHubID(ghUser.ID)
	if err != nil {
		return fmt.Errorf("checking existing user: %w", err)
	}

	var userID string
	if existingUser != nil {
		// Existing user: update username/avatar, keep existing role.
		userID = existingUser.ID
		if err := h.db.UpsertUser(existingUser.ID, ghUser.ID, ghUser.Login, sql.NullString{String: ghUser.AvatarURL, Valid: ghUser.AvatarURL != ""}, existingUser.Role); err != nil {
			return fmt.Errorf("updating user: %w", err)
		}
	} else {
		// New user: determine role.
		role := "pending"
		count, err := h.db.CountUsers()
		if err != nil {
			return fmt.Errorf("counting users: %w", err)
		}
		if count == 0 {
			role = "admin"
		}

		userID = uuid.New().String()
		if err := h.db.UpsertUser(userID, ghUser.ID, ghUser.Login, sql.NullString{String: ghUser.AvatarURL, Valid: ghUser.AvatarURL != ""}, role); err != nil {
			return fmt.Errorf("creating user: %w", err)
		}
	}

	// Create session (32 bytes, hex-encoded = 64 chars).
	sessionID, err := randomHex(32)
	if err != nil {
		return fmt.Errorf("generating session ID: %w", err)
	}

	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	if err := h.db.CreateSession(sessionID, userID, expiresAt); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	c.SetCookie(&http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   7 * 24 * 3600, // 7 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !isLocalhost(h.config.BaseURL),
	})

	h.logger.Info("user logged in", "userID", userID, "github_username", ghUser.Login)
	return c.Redirect(http.StatusTemporaryRedirect, "/")
}

// HandleLogout clears the session.
func (h *Handler) HandleLogout(c echo.Context) error {
	cookie, err := c.Cookie("session")
	if err == nil && cookie.Value != "" {
		if err := h.db.DeleteSession(cookie.Value); err != nil {
			h.logger.Error("failed to delete session", "error", err)
		}
	}

	c.SetCookie(&http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
}

// HandlePendingApproval renders a page for users awaiting admin approval.
func (h *Handler) HandlePendingApproval(c echo.Context) error {
	user, ok := c.Get("user").(*db.User)
	if !ok {
		return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
	}
	return c.JSON(http.StatusOK, map[string]string{
		"status":   "pending",
		"username": user.GitHubUsername,
		"message":  "Your request to join has been submitted. An admin will approve your access.",
	})
}

// HandleAdminUsers returns all users for admin management.
func (h *Handler) HandleAdminUsers(c echo.Context) error {
	users, err := h.db.ListUsers()
	if err != nil {
		return fmt.Errorf("listing users: %w", err)
	}

	type userResp struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Avatar   string `json:"avatar_url"`
		Role     string `json:"role"`
	}
	resp := make([]userResp, len(users))
	for i, u := range users {
		avatar := ""
		if u.AvatarURL.Valid {
			avatar = u.AvatarURL.String
		}
		resp[i] = userResp{ID: u.ID, Username: u.GitHubUsername, Avatar: avatar, Role: u.Role}
	}
	return c.JSON(http.StatusOK, resp)
}

// HandleApproveUser promotes a pending user to "user" role.
func (h *Handler) HandleApproveUser(c echo.Context) error {
	userID := c.Param("userID")
	if err := h.db.UpdateUserRole(userID, "user"); err != nil {
		return fmt.Errorf("approving user: %w", err)
	}
	h.logger.Info("user approved", "userID", userID)
	return c.JSON(http.StatusOK, map[string]string{"status": "approved"})
}

// HandleRejectUser deletes a user and their sessions.
func (h *Handler) HandleRejectUser(c echo.Context) error {
	userID := c.Param("userID")
	if err := h.db.DeleteSessionsByUserID(userID); err != nil {
		h.logger.Error("failed to delete user sessions", "error", err, "userID", userID)
	}
	if err := h.db.DeleteUser(userID); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	h.logger.Info("user rejected", "userID", userID)
	return c.JSON(http.StatusOK, map[string]string{"status": "rejected"})
}

// gitHubUser represents the subset of GitHub API user response we need.
type gitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// exchangeCode exchanges an OAuth code for a GitHub access token.
func (h *Handler) exchangeCode(code string) (string, error) {
	data := url.Values{
		"client_id":     {h.config.GitHubClientID},
		"client_secret": {h.config.GitHubClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", fmt.Errorf("OAuth error: %s: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	return tokenResp.AccessToken, nil
}

// fetchGitHubUser fetches user info from the GitHub API.
func (h *Handler) fetchGitHubUser(accessToken string) (*gitHubUser, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, body)
	}

	var user gitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("parsing user response: %w", err)
	}
	return &user, nil
}

// randomHex generates n random bytes and returns them as a hex string.
func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// isLocalhost returns true if the base URL refers to localhost.
func isLocalhost(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
