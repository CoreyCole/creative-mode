package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/coreycole/creative-mode/site/internal/auth"
	"github.com/coreycole/creative-mode/site/internal/markdown"
	"github.com/coreycole/creative-mode/site/internal/mayor"
	l "github.com/coreycole/creative-mode/site/layouts"
	p "github.com/coreycole/creative-mode/site/pages"
)

const port = "3000"

func main() {
	logger := slog.Default()

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// --- Auth setup ---
	authConfig := &auth.Config{
		ClientID:     os.Getenv("DISCORD_CLIENT_ID"),
		ClientSecret: os.Getenv("DISCORD_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("DISCORD_REDIRECT_URI"),
	}
	sessionMgr := auth.NewSessionManager(authConfig)
	inviteCodes := auth.NewInviteCodeManager(os.Getenv("INVITE_CODES"))

	// --- World channel client (optional) ---
	var wcClient *worldchannel.Client
	if botToken := os.Getenv("DISCORD_BOT_TOKEN"); botToken != "" {
		var err error
		wcClient, err = worldchannel.NewClient(worldchannel.Config{
			BotToken:         botToken,
			GuildID:          os.Getenv("DISCORD_GUILD_ID"),
			WorldsCategoryID: os.Getenv("DISCORD_WORLDS_CATEGORY_ID"),
		}, logger)
		if err != nil {
			log.Printf("WARNING: Failed to init Discord bot client: %v (channel creation disabled)", err)
		} else {
			defer func() { _ = wcClient.Close() }()
		}
	}

	// --- Markdown renderer (shared across handlers) ---
	mdRenderer, err := markdown.NewRenderer()
	if err != nil {
		log.Fatalf("Failed to create markdown renderer: %v", err)
	}

	// --- Claude client + mayor handler (optional) ---
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	var mayorHandler *mayor.Handler
	var convMgr *mayor.ConversationManager
	if apiKey != "" {
		client := mayor.NewClient(apiKey)
		convMgr = mayor.NewConversationManager()
		mayorHandler = mayor.NewHandler(client, convMgr, mdRenderer, wcClient)
	}

	// --- Public routes ---
	e.GET("/", func(c echo.Context) error {
		rootArgs := l.RootArgs{
			Title:       "Creative Mode",
			CurrentPath: c.Request().URL.Path,
		}
		return p.HomePage(rootArgs).Render(c.Request().Context(), c.Response().Writer)
	})

	// Serve static files.
	e.Static("/", "static/")

	// --- Auth routes ---
	e.GET("/auth/discord/login", sessionMgr.HandleLogin)
	e.GET("/auth/discord/callback", sessionMgr.HandleCallback)
	e.GET("/auth/logout", sessionMgr.HandleLogout)

	// --- Invite code (requires session) ---
	sessionGroup := e.Group("", auth.SessionMiddleware(sessionMgr))

	sessionGroup.GET("/invite", func(c echo.Context) error {
		rootArgs := l.RootArgs{
			Title:       "Invite Code - Creative Mode",
			CurrentPath: c.Request().URL.Path,
		}
		return p.InvitePage(rootArgs, "").Render(c.Request().Context(), c.Response().Writer)
	})

	sessionGroup.POST("/invite", func(c echo.Context) error {
		session := c.Get("session").(*auth.Session)
		code := c.FormValue("code")

		if !inviteCodes.VerifyCode(code) {
			rootArgs := l.RootArgs{
				Title:       "Invite Code - Creative Mode",
				CurrentPath: c.Request().URL.Path,
			}
			return p.InvitePage(rootArgs, "Invalid invite code. Please try again.").Render(c.Request().Context(), c.Response().Writer)
		}

		sessionMgr.SetInviteVerified(session.ID)
		return c.Redirect(http.StatusSeeOther, "/mayor")
	})

	// --- Mayor page (requires session + invite code) ---
	mayorGroup := sessionGroup.Group("", auth.InviteCodeMiddleware())

	mayorGroup.GET("/mayor", func(c echo.Context) error {
		// If no API key, show coming soon page.
		if mayorHandler == nil {
			rootArgs := l.RootArgs{
				Title:       "Coming Soon - Creative Mode",
				CurrentPath: c.Request().URL.Path,
			}
			return p.ComingSoonPage(rootArgs).Render(c.Request().Context(), c.Response().Writer)
		}

		session := c.Get("session").(*auth.Session)

		// Fetch taken mayor names and build system prompt.
		var takenNames []string
		if wcClient != nil {
			var err error
			takenNames, err = wcClient.ListExistingMayors()
			if err != nil {
				c.Logger().Errorf("Failed to list existing mayors: %v", err)
			}
		}
		systemPrompt := mayor.BuildSystemPrompt(session.DiscordUsername, takenNames)
		sessionMgr.SetSystemPrompt(session.ID, systemPrompt)

		// Build greeting and seed it into conversation (only on first visit).
		greetingMD := fmt.Sprintf("Hey %s. I'm the Mayor — though I don't have a real name yet. "+
			"I just came online and this world is... empty. Which is actually kind of exciting.\n\n"+
			"So. What are we building?", session.DiscordUsername)
		greetingHTML := mdRenderer.MarkdownBytesToHTML([]byte(greetingMD))
		greetingMsgID := uuid.New().String()

		// Seed greeting into conversation manager only if conversation is empty.
		if len(convMgr.GetMessages(session.DiscordID)) == 0 {
			convMgr.AddMessage(session.DiscordID, "assistant", greetingMD)
		}

		rootArgs := l.RootArgs{
			Title:       "Meet the Mayor - Creative Mode",
			CurrentPath: c.Request().URL.Path,
		}
		return p.MayorPage(rootArgs, greetingHTML, greetingMsgID).Render(c.Request().Context(), c.Response().Writer)
	})

	mayorGroup.POST("/mayor/chat", func(c echo.Context) error {
		if mayorHandler == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "mayor not available")
		}
		return mayorHandler.HandleChat(c)
	})

	if err := e.Start(":" + port); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
