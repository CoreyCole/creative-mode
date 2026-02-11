package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/views/pending"
)

const (
	oauthStateBytes  = 16
	oauthStateTTLSec = 300 // 5 minutes
	sessionBytes     = 32
	sessionTTLDays   = 7
	sessionMaxAgeSec = sessionTTLDays * 24 * 3600
	sessionTTLHours  = sessionTTLDays * 24
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
	state, err := randomHex(oauthStateBytes)
	if err != nil {
		return fmt.Errorf("generating state: %w", err)
	}

	c.SetCookie(&http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   oauthStateTTLSec,
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
	ctx := c.Request().Context()

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
	accessToken, err := h.exchangeCode(ctx, code)
	if err != nil {
		h.logger.Error("failed to exchange OAuth code", "error", err)

		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"OAuth token exchange failed",
		)
	}

	// Fetch user info from GitHub.
	ghUser, err := h.fetchGitHubUser(ctx, accessToken)
	if err != nil {
		h.logger.Error("failed to fetch GitHub user", "error", err)

		return echo.NewHTTPError(
			http.StatusInternalServerError,
			"failed to fetch GitHub user info",
		)
	}

	// Determine role: first user is admin, others are pending.
	// Check if user already exists first.
	existingUser, err := h.db.GetUserByGitHubID(ctx, ghUser.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("checking existing user: %w", err)
	}

	userID, err := h.resolveUser(ctx, ghUser, existingUser, err)
	if err != nil {
		return err
	}

	// Create session (32 bytes, hex-encoded = 64 chars).
	sessionID, err := randomHex(sessionBytes)
	if err != nil {
		return fmt.Errorf("generating session ID: %w", err)
	}

	expiresAt := time.Now().Add(sessionTTLHours * time.Hour)
	if err := h.db.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	c.SetCookie(&http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sessionMaxAgeSec,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !isLocalhost(h.config.BaseURL),
	})

	h.logger.Info("user logged in", "userID", userID, "github_username", ghUser.Login)

	return c.Redirect(http.StatusTemporaryRedirect, "/")
}

// resolveUser creates or updates a user based on GitHub user info. Returns
// the user ID. For new users, the first user gets the "admin" role atomically.
func (h *Handler) resolveUser(
	ctx context.Context,
	ghUser *gitHubUser,
	existingUser sqlc.User,
	lookupErr error,
) (string, error) {
	avatar := sql.NullString{
		String: ghUser.AvatarURL,
		Valid:  ghUser.AvatarURL != "",
	}

	if lookupErr == nil {
		// Existing user: update username/avatar, keep existing role.
		if upsertErr := h.db.UpsertUser(ctx, sqlc.UpsertUserParams{
			ID:             existingUser.ID,
			GitHubID:       ghUser.ID,
			GitHubUsername: ghUser.Login,
			AvatarURL:      avatar,
			Role:           existingUser.Role,
		}); upsertErr != nil {
			return "", fmt.Errorf("updating user: %w", upsertErr)
		}

		return existingUser.ID, nil
	}

	// New user: determine role atomically (prevents two admins on race).
	role := "pending"

	tx, txErr := h.db.BeginTx(ctx)
	if txErr != nil {
		return "", fmt.Errorf("begin tx: %w", txErr)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := h.db.WithTx(tx)

	count, countErr := qtx.CountUsers(ctx)
	if countErr != nil {
		return "", fmt.Errorf("counting users: %w", countErr)
	}
	if count == 0 {
		role = "admin"
	}

	userID := uuid.New().String()
	if upsertErr := qtx.UpsertUser(ctx, sqlc.UpsertUserParams{
		ID:             userID,
		GitHubID:       ghUser.ID,
		GitHubUsername: ghUser.Login,
		AvatarURL:      avatar,
		Role:           role,
	}); upsertErr != nil {
		return "", fmt.Errorf("creating user: %w", upsertErr)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return "", fmt.Errorf("commit: %w", commitErr)
	}

	return userID, nil
}

// HandleLogout clears the session.
func (h *Handler) HandleLogout(c echo.Context) error {
	ctx := c.Request().Context()

	cookie, err := c.Cookie("session")
	if err == nil && cookie.Value != "" {
		if err := h.db.DeleteSession(ctx, cookie.Value); err != nil {
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
	user, ok := c.Get("user").(*sqlc.User)
	if !ok {
		return c.Redirect(http.StatusTemporaryRedirect, "/auth/github/login")
	}

	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")

	return pending.Page(user).Render(c.Request().Context(), c.Response().Writer)
}

// HandleApproveUser promotes a pending user to "user" role.
func (h *Handler) HandleApproveUser(c echo.Context) error {
	ctx := c.Request().Context()
	userID := c.Param("userID")

	if _, err := h.db.UpdateUserRole(ctx, sqlc.UpdateUserRoleParams{
		Role: "user",
		ID:   userID,
	}); err != nil {
		return fmt.Errorf("approving user: %w", err)
	}
	h.logger.Info("user approved", "userID", userID)

	return c.JSON(http.StatusOK, map[string]string{"status": "approved"})
}

// HandleRejectUser deletes a user and all their dependent records.
func (h *Handler) HandleRejectUser(c echo.Context) error {
	ctx := c.Request().Context()
	userID := c.Param("userID")

	// Delete all dependent records before the user.
	if err := h.db.DeleteSessionsByUserID(ctx, userID); err != nil {
		h.logger.Error("failed to delete user sessions", "error", err, "userID", userID)
	}
	if err := h.db.DeleteUserPositionsByUserID(ctx, userID); err != nil {
		h.logger.Error("failed to delete user positions", "error", err, "userID", userID)
	}
	if err := h.db.DeletePromptHistoryByUserID(ctx, userID); err != nil {
		h.logger.Error("failed to delete prompt history", "error", err, "userID", userID)
	}
	nullUserID := sql.NullString{String: userID, Valid: true}
	if err := h.db.DeleteMessagesByUserID(ctx, nullUserID); err != nil {
		h.logger.Error("failed to delete messages", "error", err, "userID", userID)
	}

	if err := h.db.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	h.logger.Info("user rejected", "userID", userID)

	return c.JSON(http.StatusOK, map[string]string{"status": "rejected"})
}

// gitHubUser represents the subset of GitHub API user response we need.
type gitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"` //nolint:tagliatelle // GitHub API format
}

// exchangeCode exchanges an OAuth code for a GitHub access token.
func (h *Handler) exchangeCode(ctx context.Context, code string) (string, error) {
	data := url.Values{
		"client_id":     {h.config.GitHubClientID},
		"client_secret": {h.config.GitHubClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"` //nolint:tagliatelle // GitHub API format
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"` //nolint:tagliatelle // GitHub API format
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", fmt.Errorf("OAuth error: %s: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", errors.New("empty access token in response")
	}

	return tokenResp.AccessToken, nil
}

// fetchGitHubUser fetches user info from the GitHub API.
func (h *Handler) fetchGitHubUser(
	ctx context.Context,
	accessToken string,
) (*gitHubUser, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.github.com/user",
		http.NoBody,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
