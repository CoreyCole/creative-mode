package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coreycole/creative-mode/pkg/imagegen"
	"github.com/coreycole/creative-mode/pkg/markdown"
	"github.com/coreycole/creative-mode/pkg/mayorchat"
	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/coreycole/creative-mode/site/internal/auth"
	"github.com/coreycole/creative-mode/site/internal/db"
	"github.com/coreycole/creative-mode/site/internal/mayor"
	"github.com/coreycole/creative-mode/site/internal/monitor"
	"github.com/coreycole/creative-mode/site/internal/webhook"
	l "github.com/coreycole/creative-mode/site/layouts"
	p "github.com/coreycole/creative-mode/site/pages"
)

var (
	inviteAttempts   = make(map[string]time.Time)
	inviteAttemptsMu sync.Mutex
)

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(string(out))
}

func main() {
	commit := gitCommit()
	logger := slog.Default()

	// Graceful shutdown context.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Database setup ---
	dbPath := os.Getenv("SITE_DB_PATH")
	if dbPath == "" {
		dbPath = "data/site.db"
	}
	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer func() { _ = database.Close() }()

	devMode := os.Getenv("DEV_MODE") == "true"

	// Validate required env vars.
	requiredEnv := []string{"DISCORD_CLIENT_ID", "DISCORD_CLIENT_SECRET", "DISCORD_REDIRECT_URI"}
	var missing []string
	for _, name := range requiredEnv {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		log.Fatalf("Missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if os.Getenv("INVITE_CODES") == "" {
		log.Printf("WARNING: INVITE_CODES is empty — all invite codes will be rejected")
	}

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// CORS — restrict to creative-mode.ai in production, allow localhost in dev.
	corsOrigins := []string{"https://creative-mode.ai"}
	if devMode {
		corsOrigins = append(corsOrigins, "http://localhost:*")
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: corsOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost},
	}))
	e.Use(middleware.BodyLimit("1M"))
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:         "1; mode=block",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "DENY",
		HSTSMaxAge:            31536000,
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://cdn.discordapp.com",
	}))
	e.Use(monitor.PageViewMiddleware(database))

	// --- Auth setup ---
	authConfig := &auth.Config{
		ClientID:     os.Getenv("DISCORD_CLIENT_ID"),
		ClientSecret: os.Getenv("DISCORD_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("DISCORD_REDIRECT_URI"),
		BotToken:     os.Getenv("DISCORD_BOT_TOKEN"),
		GuildID:      os.Getenv("DISCORD_GUILD_ID"),
	}
	sessionMgr := auth.NewSessionManager(authConfig, database)
	inviteCodes := auth.NewInviteCodeManager(os.Getenv("INVITE_CODES"))

	// --- World channel client (optional) ---
	var wcClient *worldchannel.Client
	if botToken := os.Getenv("DISCORD_BOT_TOKEN"); botToken != "" {
		var wcErr error
		wcClient, wcErr = worldchannel.NewClient(worldchannel.Config{
			BotToken:         botToken,
			GuildID:          os.Getenv("DISCORD_GUILD_ID"),
			WorldsCategoryID: os.Getenv("DISCORD_WORLDS_CATEGORY_ID"),
		}, logger)
		if wcErr != nil {
			log.Printf("WARNING: Failed to init Discord bot client: %v (channel creation disabled)", wcErr)
		} else {
			defer func() { _ = wcClient.Close() }()
		}
	}

	// --- Markdown renderer (shared across handlers) ---
	mdRenderer, err := markdown.NewRenderer()
	if err != nil {
		log.Fatalf("Failed to create markdown renderer: %v", err)
	}

	// --- Image generation client (optional) ---
	var imagegenClient *imagegen.Client
	if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
		var igErr error
		imagegenClient, igErr = imagegen.NewClient(ctx, geminiKey, logger)
		if igErr != nil {
			log.Printf("WARNING: Failed to init Gemini client: %v (cover art generation disabled)", igErr)
		}
	}

	// --- Data directory for pending cover art ---
	dataDir := os.Getenv("SITE_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	// --- Claude client + mayor handler (optional) ---
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	var mayorHandler *mayor.Handler
	var convMgr *mayorchat.ConversationManager
	if apiKey != "" {
		client := mayor.NewClient(apiKey)
		store := mayor.NewSQLiteMessageStore(database)
		convMgr = mayorchat.NewConversationManager(store)
		mayorHandler = mayor.NewHandler(client, convMgr, mdRenderer, wcClient, imagegenClient, dataDir, logger)
		mayorHandler.HarnessURL = os.Getenv("HARNESS_URL")
	}

	// --- Webhook handler (self-rebuild on GitHub push) ---
	wh := webhook.New(logger, os.Getenv("WEBHOOK_SECRET"))
	e.GET("/health", wh.HandleHealth)
	e.POST("/webhook/github", wh.HandleGitHub)

	// --- Monitor handler (public /status page) ---
	monitorHandler := monitor.NewHandler(database, logger,
		os.Getenv("HARNESS_URL"), os.Getenv("PRESIDENT_SECRET"), commit, wcClient)
	e.GET("/status", monitorHandler.HandlePage)
	e.GET("/status/events", monitorHandler.HandleEvents)
	e.POST("/status/graph", monitorHandler.HandleGraphUpdate)

	// --- Dev auth route (only in dev mode) ---
	if devMode {
		e.POST("/dev/auth/login", sessionMgr.HandleDevLogin)
	}

	// --- Public routes ---
	e.GET("/", func(c echo.Context) error {
		rootArgs := l.RootArgs{
			Title:       "Creative Mode",
			CurrentPath: c.Request().URL.Path,
			Commit:      commit,
		}
		return p.HomePage(rootArgs, devMode).Render(c.Request().Context(), c.Response().Writer)
	})

	// Serve static files with cache headers.
	staticGroup := e.Group("")
	staticGroup.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("Cache-Control", "public, max-age=86400")
			return next(c)
		}
	})
	staticGroup.Static("/", "static/")

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
			Commit:      commit,
		}
		return p.InvitePage(rootArgs, "").Render(c.Request().Context(), c.Response().Writer)
	})

	sessionGroup.POST("/invite", func(c echo.Context) error {
		session := c.Get("session").(*auth.Session)
		code := c.FormValue("code")

		inviteAttemptsMu.Lock()
		if last, ok := inviteAttempts[session.ID]; ok && time.Since(last) < 2*time.Second {
			inviteAttemptsMu.Unlock()
			c.Logger().Warnf("Rate-limited invite attempt from session %s", session.ID)
			rootArgs := l.RootArgs{
				Title:       "Invite Code - Creative Mode",
				CurrentPath: c.Request().URL.Path,
				Commit:      commit,
			}
			return p.InvitePage(rootArgs, "Please wait a moment before trying again.").Render(c.Request().Context(), c.Response().Writer)
		}
		inviteAttempts[session.ID] = time.Now()
		inviteAttemptsMu.Unlock()

		if !inviteCodes.VerifyCode(code) {
			rootArgs := l.RootArgs{
				Title:       "Invite Code - Creative Mode",
				CurrentPath: c.Request().URL.Path,
				Commit:      commit,
			}
			return p.InvitePage(rootArgs, "Invalid invite code. Please try again.").Render(c.Request().Context(), c.Response().Writer)
		}

		sessionMgr.SetInviteVerified(session.ID)
		return c.Redirect(http.StatusSeeOther, "/mayor")
	})

	// --- Join Discord (requires session) ---
	sessionGroup.GET("/join-discord", func(c echo.Context) error {
		rootArgs := l.RootArgs{
			Title:       "Join Discord - Creative Mode",
			CurrentPath: c.Request().URL.Path,
			Commit:      commit,
		}
		return p.JoinDiscordPage(rootArgs, "").Render(c.Request().Context(), c.Response().Writer)
	})

	sessionGroup.POST("/join-discord", func(c echo.Context) error {
		session, ok := c.Get("session").(*auth.Session)
		if !ok {
			return c.Redirect(http.StatusFound, "/auth/discord/login")
		}
		if sessionMgr.CheckGuildMembership(c, session.DiscordID) {
			sessionMgr.SetGuildVerified(session.ID)
			return c.Redirect(http.StatusSeeOther, "/mayor")
		}
		rootArgs := l.RootArgs{
			Title:       "Join Discord - Creative Mode",
			CurrentPath: c.Request().URL.Path,
			Commit:      commit,
		}
		return p.JoinDiscordPage(rootArgs, "We couldn't find you in the server yet — make sure you've joined and try again.").Render(c.Request().Context(), c.Response().Writer)
	})

	// --- Dev-only reset conversation (requires session) ---
	if devMode {
		sessionGroup.POST("/dev/reset-conversation", func(c echo.Context) error {
			session := c.Get("session").(*auth.Session)
			if err := convMgr.ResetConversation(session.DiscordID); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to reset conversation")
			}
			return c.Redirect(http.StatusSeeOther, "/mayor")
		})
	}

	// --- Mayor page (requires session + guild membership + invite code) ---
	guildGroup := sessionGroup.Group("", auth.GuildMemberMiddleware())
	mayorGroup := guildGroup.Group("", auth.InviteCodeMiddleware())

	mayorGroup.GET("/mayor", func(c echo.Context) error {
		// If no API key, show coming soon page.
		if mayorHandler == nil {
			rootArgs := l.RootArgs{
				Title:       "Coming Soon - Creative Mode",
				CurrentPath: c.Request().URL.Path,
				Commit:      commit,
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

		// Seed greeting into conversation only if conversation is empty.
		if len(convMgr.GetMessages(session.DiscordID)) == 0 {
			greetingMD := fmt.Sprintf("Hey %s. I'm the Mayor — though I don't have a real name yet. "+
				"I just came online and this world is... empty. Which is actually kind of exciting.\n\n"+
				"So. What are we building?", session.DiscordUsername)
			convMgr.AddMessage(session.DiscordID, "assistant", greetingMD)
		}

		// Build chat messages from full conversation history.
		messages := convMgr.GetMessages(session.DiscordID)
		chatMessages := make([]p.ChatMessage, len(messages))
		for i, msg := range messages {
			chatMessages[i] = p.ChatMessage{
				ID:   uuid.New().String(),
				Role: msg.Role,
			}
			if msg.Role == "assistant" {
				chatMessages[i].HTMLContent = mdRenderer.MarkdownBytesToHTML([]byte(msg.Content))
			} else {
				chatMessages[i].Content = msg.Content
				chatMessages[i].AvatarURL = session.DiscordAvatar
			}
		}

		rootArgs := l.RootArgs{
			Title:       "Creative Mode - Meet the Mayor",
			CurrentPath: c.Request().URL.Path,
			Commit:      commit,
		}
		return p.MayorPage(rootArgs, chatMessages, devMode).Render(c.Request().Context(), c.Response().Writer)
	})

	mayorGroup.POST("/mayor/chat", func(c echo.Context) error {
		if mayorHandler == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "mayor not available")
		}
		return mayorHandler.HandleChat(c)
	})

	mayorGroup.GET("/mayor/cover-preview", func(c echo.Context) error {
		if mayorHandler == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "mayor not available")
		}
		return mayorHandler.HandleCoverPreview(c)
	})

	mayorGroup.POST("/mayor/generate-cover", func(c echo.Context) error {
		if mayorHandler == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "mayor not available")
		}
		return mayorHandler.HandleGenerateCover(c)
	})

	mayorGroup.POST("/mayor/hatch", func(c echo.Context) error {
		if mayorHandler == nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "mayor not available")
		}
		return mayorHandler.HandleHatch(c)
	})

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down server...")
		monitorHandler.Stop()
		if shutdownErr := e.Shutdown(context.Background()); shutdownErr != nil {
			logger.Error("Server shutdown error", "error", shutdownErr)
		}
	}()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	e.Server.ReadTimeout = 30 * time.Second
	e.Server.WriteTimeout = 180 * time.Second // long for SSE streams + cover art gen
	e.Server.IdleTimeout = 120 * time.Second

	if err := e.Start(":" + port); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
