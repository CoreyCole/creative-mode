package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	oauthStateBytes  = 16
	oauthStateTTLSec = 300 // 5 minutes
	sessionBytes     = 32
	sessionTTLDays   = 7
	sessionMaxAgeSec = sessionTTLDays * 24 * 3600
)

// Config holds Discord OAuth configuration.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	BotToken     string
	GuildID      string
}

// DiscordUser represents the Discord API /users/@me response.
type DiscordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Avatar        string `json:"avatar"`
	GlobalName    string `json:"global_name"`
}

// AvatarURL returns the user's Discord avatar CDN URL.
func (u *DiscordUser) AvatarURL() string {
	if u.Avatar == "" {
		return ""
	}
	return fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", u.ID, u.Avatar)
}

// Session holds session data for a logged-in user.
type Session struct {
	ID                  string
	DiscordID           string
	DiscordUsername     string
	DiscordAvatar       string
	GuildMemberVerified bool
	InviteCodeVerified  bool
	CreatedAt           time.Time
	SystemPrompt        string // Built once per page load with taken names
}

// SessionManager manages in-memory sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	config   *Config
}

// NewSessionManager creates a new session manager.
func NewSessionManager(config *Config) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		config:   config,
	}
	// Start cleanup goroutine.
	go sm.cleanupLoop()
	return sm
}

// GetSession returns a session by cookie value.
func (sm *SessionManager) GetSession(sessionID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.sessions[sessionID]
	if !ok {
		return nil
	}
	if time.Since(s.CreatedAt) > sessionTTLDays*24*time.Hour {
		return nil
	}
	return s
}

// SetInviteVerified marks a session's invite code as verified.
func (sm *SessionManager) SetInviteVerified(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[sessionID]; ok {
		s.InviteCodeVerified = true
	}
}

// SetGuildVerified marks a session's guild membership as verified.
func (sm *SessionManager) SetGuildVerified(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[sessionID]; ok {
		s.GuildMemberVerified = true
	}
}

// SetSystemPrompt stores the system prompt on the session.
func (sm *SessionManager) SetSystemPrompt(sessionID, prompt string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[sessionID]; ok {
		s.SystemPrompt = prompt
	}
}

// HandleLogin redirects to Discord OAuth authorize URL.
func (sm *SessionManager) HandleLogin(c echo.Context) error {
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
		Secure:   isSecure(sm.config.RedirectURI),
	})

	redirectURL := fmt.Sprintf(
		"https://discord.com/api/oauth2/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=identify&state=%s",
		url.QueryEscape(sm.config.ClientID),
		url.QueryEscape(sm.config.RedirectURI),
		url.QueryEscape(state),
	)

	return c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// HandleCallback processes the Discord OAuth callback.
func (sm *SessionManager) HandleCallback(c echo.Context) error {
	// Validate state (CSRF protection).
	stateCookie, err := c.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing OAuth state cookie")
	}
	if c.QueryParam("state") != stateCookie.Value {
		return echo.NewHTTPError(http.StatusBadRequest, "OAuth state mismatch")
	}

	// Clear state cookie.
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
	accessToken, err := sm.exchangeCode(c, code)
	if err != nil {
		c.Logger().Errorf("failed to exchange OAuth code: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "OAuth token exchange failed")
	}

	// Fetch user info from Discord.
	discordUser, err := sm.fetchDiscordUser(c, accessToken)
	if err != nil {
		c.Logger().Errorf("failed to fetch Discord user: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch Discord user info")
	}

	// Create session.
	sessionID, err := randomHex(sessionBytes)
	if err != nil {
		return fmt.Errorf("generating session ID: %w", err)
	}

	guildVerified := sm.CheckGuildMembership(c, discordUser.ID)

	session := &Session{
		ID:                  sessionID,
		DiscordID:           discordUser.ID,
		DiscordUsername:     discordUser.Username,
		DiscordAvatar:       discordUser.AvatarURL(),
		GuildMemberVerified: guildVerified,
		CreatedAt:           time.Now(),
	}

	sm.mu.Lock()
	sm.sessions[sessionID] = session
	sm.mu.Unlock()

	c.SetCookie(&http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sessionMaxAgeSec,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecure(sm.config.RedirectURI),
	})

	return c.Redirect(http.StatusTemporaryRedirect, "/mayor")
}

// HandleLogout clears the session cookie and deletes the session.
func (sm *SessionManager) HandleLogout(c echo.Context) error {
	cookie, err := c.Cookie("session")
	if err == nil && cookie.Value != "" {
		sm.mu.Lock()
		delete(sm.sessions, cookie.Value)
		sm.mu.Unlock()
	}

	c.SetCookie(&http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	return c.Redirect(http.StatusSeeOther, "/")
}

// exchangeCode exchanges an OAuth code for a Discord access token.
func (sm *SessionManager) exchangeCode(c echo.Context, code string) (string, error) {
	data := url.Values{
		"client_id":     {sm.config.ClientID},
		"client_secret": {sm.config.ClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {sm.config.RedirectURI},
	}

	req, err := http.NewRequestWithContext(
		c.Request().Context(),
		http.MethodPost,
		"https://discord.com/api/oauth2/token",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return "", err
	}
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

// fetchDiscordUser fetches user info from the Discord API.
func (sm *SessionManager) fetchDiscordUser(c echo.Context, accessToken string) (*DiscordUser, error) {
	req, err := http.NewRequestWithContext(
		c.Request().Context(),
		http.MethodGet,
		"https://discord.com/api/users/@me",
		http.NoBody,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord API request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discord API returned %d: %s", resp.StatusCode, body)
	}

	var user DiscordUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("parsing user response: %w", err)
	}

	return &user, nil
}

// CheckGuildMembership checks if a Discord user is a member of the configured guild
// using the bot token. Returns true on 200, false otherwise.
func (sm *SessionManager) CheckGuildMembership(c echo.Context, userID string) bool {
	if sm.config.BotToken == "" || sm.config.GuildID == "" {
		return true // skip check if not configured
	}

	url := fmt.Sprintf("https://discord.com/api/guilds/%s/members/%s", sm.config.GuildID, userID)
	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		c.Logger().Errorf("failed to create guild member request: %v", err)
		return false
	}
	req.Header.Set("Authorization", "Bot "+sm.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Logger().Errorf("guild member check request failed: %v", err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	return resp.StatusCode == http.StatusOK
}

// cleanupLoop periodically removes expired sessions.
func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		sm.mu.Lock()
		for id, s := range sm.sessions {
			if time.Since(s.CreatedAt) > sessionTTLDays*24*time.Hour {
				delete(sm.sessions, id)
			}
		}
		sm.mu.Unlock()
	}
}

func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func isSecure(redirectURI string) bool {
	return strings.HasPrefix(redirectURI, "https://")
}
