package mayor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/coreycole/creative-mode/pkg/imagegen"
	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/coreycole/creative-mode/site/internal/auth"
	"github.com/coreycole/creative-mode/site/internal/markdown"
	p "github.com/coreycole/creative-mode/site/pages"
)

// scrollChatJS is the JS snippet that scrolls the chat container to the bottom.
const scrollChatJS = "document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"

// ChatSignals matches the client-side signals sent with @post.
type ChatSignals struct {
	MayorInput  string `json:"mayor_input"`
	CreateWorld bool   `json:"create_world"`
}

// Handler handles the mayor chat SSE endpoint.
type Handler struct {
	client         anthropic.Client
	convMgr        *ConversationManager
	mdRenderer     *markdown.Renderer
	wcClient       *worldchannel.Client // nil if no bot token configured
	imagegenClient *imagegen.Client     // nil if no Gemini API key configured
	dataDir        string               // data directory for pending cover art
	logger         *slog.Logger
	HarnessURL     string // optional — shown on world hatch card
}

// NewHandler creates a new mayor chat handler.
func NewHandler(client anthropic.Client, convMgr *ConversationManager, mdRenderer *markdown.Renderer, wcClient *worldchannel.Client, imagegenClient *imagegen.Client, dataDir string, logger *slog.Logger) *Handler {
	return &Handler{
		client:         client,
		convMgr:        convMgr,
		mdRenderer:     mdRenderer,
		wcClient:       wcClient,
		imagegenClient: imagegenClient,
		dataDir:        dataDir,
		logger:         logger,
	}
}

// HandleChat processes a chat message and streams the response via SSE.
func (h *Handler) HandleChat(c echo.Context) error {
	session, ok := c.Get("session").(*auth.Session)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	// Read signals BEFORE creating SSE (critical — see memory notes).
	var signals ChatSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read signals")
	}

	forceCreate := signals.CreateWorld
	content := strings.TrimSpace(signals.MayorInput)
	const maxMessageLen = 2000
	if runes := []rune(content); len(runes) > maxMessageLen {
		content = string(runes[:maxMessageLen])
	}
	if forceCreate {
		content = "I'm ready — let's create the world!"
	}
	if content == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "message content required")
	}

	// Rate limit check.
	if err := h.convMgr.CheckRateLimit(session.DiscordID); err != nil {
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(p.RateLimitError())
	}

	// Add user message to conversation.
	h.convMgr.AddMessage(session.DiscordID, "user", content)

	// Build Anthropic messages from conversation history.
	messages := h.convMgr.GetMessages(session.DiscordID)
	anthropicMessages := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "user" {
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		} else {
			anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}

	// Start SSE stream.
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Clear input and reset create_world signal.
	if err := sse.MarshalAndPatchSignals(map[string]any{"mayor_input": "", "create_world": false}); err != nil {
		c.Logger().Errorf("Failed to clear input: %v", err)
	}

	// Clear any previous rate limit error.
	if err := sse.PatchElementTempl(p.RateLimitClear()); err != nil {
		c.Logger().Errorf("Failed to clear rate limit error: %v", err)
	}

	// Append user message to chat.
	userMsgID := uuid.New().String()
	if err := sse.PatchElementTempl(p.UserMessage(userMsgID, content, session.DiscordAvatar),
		datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages")); err != nil {
		return err
	}

	// Scroll to bottom.
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Create empty assistant message for streaming.
	assistantMsgID := uuid.New().String()
	if err := sse.PatchElementTempl(p.MayorMessageStreaming(assistantMsgID),
		datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages")); err != nil {
		return err
	}

	// Scroll to streaming message.
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// If conversation is already in scripted mode, skip API entirely.
	if h.convMgr.IsScripted(session.DiscordID) {
		if forceCreate {
			return h.handleScriptedForceCreate(c, sse, session, assistantMsgID)
		}
		return h.handleScriptedResponse(c, sse, session, assistantMsgID)
	}

	// Get the system prompt stored on the session (built at page load).
	systemPrompt := session.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = BuildSystemPrompt(session.DiscordUsername, nil)
	}

	if forceCreate {
		systemPrompt += "\n\nIMPORTANT: The user has clicked 'Create World'. " +
			"If the user has NOT yet told you their preferred mayor name, you MUST ask for it now — " +
			"do NOT invent a mayor name. Say something like: \"Almost there — but I still need a name. What should I go by?\" " +
			"For all other missing details (world name, gameplay, setting), fill them in with your best creative judgment. " +
			"Once you have the mayor name, give a brief response about the world taking shape, then emit the WORLD_READY marker."
	}

	// Stream from Claude.
	stream := h.client.Messages.NewStreaming(c.Request().Context(), anthropic.MessageNewParams{
		Model:     Model,
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: anthropicMessages,
	})

	var fullContent string
	var lastRenderedLen int
	var inCodeBlock bool
	var lastInCodeBlock bool

	for stream.Next() {
		event := stream.Current()
		switch eventVariant := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			switch deltaVariant := eventVariant.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				fullContent += deltaVariant.Text

				// Check code block state.
				codeBlockCount := strings.Count(fullContent, "```")
				lastInCodeBlock = inCodeBlock
				inCodeBlock = codeBlockCount%2 == 1

				// Determine content to render — strip WORLD_READY marker.
				renderContent := fullContent
				if idx := strings.Index(renderContent, "WORLD_READY|"); idx != -1 {
					renderContent = strings.TrimSpace(renderContent[:idx])
				}

				// Decide whether to render markdown.
				newContent := renderContent[min(lastRenderedLen, len(renderContent)):]
				shouldRender := false

				if !inCodeBlock {
					if strings.Contains(newContent, "\n\n") {
						shouldRender = true
					}
					if lastInCodeBlock && !inCodeBlock {
						shouldRender = true
					}
				}

				if shouldRender {
					htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(renderContent))
					if err := sse.PatchElementTempl(p.MayorMessageDeltaHTML(assistantMsgID, htmlContent),
						datastar.WithModeInner(), datastar.WithSelectorID("msg-content-"+assistantMsgID)); err != nil {
						c.Logger().Errorf("Failed to stream markdown delta: %v", err)
					}
					lastRenderedLen = len(renderContent)
				} else if lastRenderedLen > 0 {
					htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(renderContent[:min(lastRenderedLen, len(renderContent))]))
					trailingText := ""
					if lastRenderedLen < len(renderContent) {
						trailingText = renderContent[lastRenderedLen:]
					}
					if trailingText != "" {
						htmlContent += `<span class="whitespace-pre-wrap">` + html.EscapeString(trailingText) + `</span>`
					}
					if err := sse.PatchElementTempl(p.MayorMessageDeltaHTML(assistantMsgID, htmlContent),
						datastar.WithModeInner(), datastar.WithSelectorID("msg-content-"+assistantMsgID)); err != nil {
						c.Logger().Errorf("Failed to stream delta with trailing text: %v", err)
					}
				} else {
					// No rendered content yet — show plain text (also stripped of marker).
					if err := sse.PatchElementTempl(p.MayorMessageDelta(assistantMsgID, renderContent),
						datastar.WithModeInner(), datastar.WithSelectorID("msg-content-"+assistantMsgID)); err != nil {
						c.Logger().Errorf("Failed to stream delta: %v", err)
					}
				}

				// Auto-scroll.
				if err := sse.ExecuteScript(scrollChatJS); err != nil {
					c.Logger().Errorf("Failed to scroll during streaming: %v", err)
				}
			}
		}
	}

	if stream.Err() != nil {
		c.Logger().Errorf("Stream error: %v", stream.Err())
		if isBillingOrOverloadError(stream.Err()) {
			// Mark conversation as scripted and fall back.
			h.convMgr.SetScripted(session.DiscordID, true)
			return h.handleScriptedResponse(c, sse, session, assistantMsgID)
		}
		// Non-billing error after SSE headers sent — notify client and return.
		if err := sse.ExecuteScript(`document.getElementById('msg-content-` + assistantMsgID + `').innerHTML = '<span class="text-destructive">Something went wrong. Please try again.</span>'`); err != nil {
			c.Logger().Errorf("Failed to send error to client: %v", err)
		}
		return nil
	}

	// Save assistant response to conversation manager (without WORLD_READY marker).
	displayContent := fullContent
	if idx := strings.Index(fullContent, "WORLD_READY|"); idx != -1 {
		displayContent = strings.TrimSpace(fullContent[:idx])
	}
	h.convMgr.AddMessage(session.DiscordID, "assistant", displayContent)

	// Parse WORLD_READY marker from full content.
	var mayorName, worldName, worldSummary string
	if idx := strings.Index(fullContent, "WORLD_READY|"); idx != -1 {
		parts := strings.SplitN(fullContent[idx+len("WORLD_READY|"):], "|", 3)
		switch len(parts) {
		case 3:
			mayorName = strings.TrimSpace(parts[0])
			worldName = strings.TrimSpace(parts[1])
			worldSummary = strings.TrimSpace(parts[2])
		case 2:
			mayorName = strings.TrimSpace(parts[0])
			worldName = strings.TrimSpace(parts[1])
			worldSummary = ""
		default:
			c.Logger().Errorf("Malformed WORLD_READY marker: %q", fullContent[idx:])
		}
	}

	// Final render (without marker).
	finalHTML := h.mdRenderer.MarkdownBytesToHTML([]byte(displayContent))
	if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, finalHTML)); err != nil {
		return err
	}

	// Scroll to bottom after final render.
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Create Discord channel if world is ready.
	if mayorName != "" && worldName != "" {
		h.prepareCoverArtAndHatch(c, sse, session, mayorName, worldName, worldSummary)
	}

	return nil
}

// prepareCoverArtAndHatch is the unified entry point for ALL code paths that
// reach WORLD_READY. It handles cover art generation (if Gemini is available)
// and then either hatches immediately or shows the cover art preview UI.
func (h *Handler) prepareCoverArtAndHatch(c echo.Context, sse *datastar.ServerSentEventGenerator, session *auth.Session, mayorName, worldName, worldSummary string) {
	// Prevent duplicate hatch attempts from concurrent requests.
	if !h.convMgr.SetHatched(session.DiscordID) {
		h.logger.Warn("duplicate hatch attempt blocked (prepareCoverArtAndHatch)", "user", session.DiscordID)
		return
	}

	// Store world-ready state for later use by HandleHatch/HandleGenerateCover.
	h.convMgr.SetWorldReady(session.DiscordID, mayorName, worldName, worldSummary)

	if h.wcClient == nil {
		// No Discord bot — show summary card only.
		if err := sse.PatchElementTempl(p.WorldSummaryCard(worldName, mayorName, worldSummary)); err != nil {
			c.Logger().Errorf("Failed to patch summary card: %v", err)
		}
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return
	}

	if h.imagegenClient == nil {
		// No Gemini — hatch immediately (current behavior, zero regression).
		h.hatchWorldWithCover(c, sse, session, mayorName, worldName, worldSummary, nil, "")
		return
	}

	// Show loading spinner while generating cover art.
	if err := sse.PatchElementTempl(p.CoverArtGenerating(worldName, mayorName)); err != nil {
		c.Logger().Errorf("Failed to patch cover art loading: %v", err)
		// Fall back to immediate hatch.
		h.hatchWorldWithCover(c, sse, session, mayorName, worldName, worldSummary, nil, "")
		return
	}
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Generate cover art (3-10s).
	prompt := buildCoverArtPrompt(worldName, worldSummary)
	result, err := h.imagegenClient.Generate(c.Request().Context(), prompt, imagegen.GenerateOptions{
		AspectRatio: "16:9",
	})
	if err != nil {
		c.Logger().Errorf("Cover art generation failed: %v", err)
		if patchErr := sse.PatchElementTempl(p.CoverArtError("Cover art generation failed. You can try again or hatch without it.", worldName, mayorName)); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return
	}

	// Save to disk.
	artPath, saveErr := savePendingCoverArt(h.dataDir, session.DiscordID, result.Data, result.MIMEType)
	if saveErr != nil {
		c.Logger().Errorf("Failed to save cover art to disk: %v", saveErr)
		if patchErr := sse.PatchElementTempl(p.CoverArtError("Cover art generation failed. You can try again or hatch without it.", worldName, mayorName)); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return
	}
	h.convMgr.SetCoverArtPath(session.DiscordID, artPath, result.MIMEType)

	// Show preview with Hatch/Regenerate buttons.
	if err := sse.PatchElementTempl(p.CoverArtPreview("/mayor/cover-preview", worldName, mayorName)); err != nil {
		c.Logger().Errorf("Failed to patch cover art preview: %v", err)
	}
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}
}

// hatchWorldWithCover creates the Discord channel and optionally includes cover art
// in the harness webhook.
func (h *Handler) hatchWorldWithCover(c echo.Context, sse *datastar.ServerSentEventGenerator, session *auth.Session, mayorName, worldName, worldSummary string, coverArtData []byte, coverArtMIME string) {
	// Silent race-condition safety net: if name was taken since page load,
	// append a roman numeral suffix.
	finalMayorName := mayorName
	if err := h.wcClient.CheckMayorNameUnique(finalMayorName); err != nil {
		suffixes := []string{"II", "III", "IV", "V"}
		for _, suffix := range suffixes {
			candidate := mayorName + " " + suffix
			if checkErr := h.wcClient.CheckMayorNameUnique(candidate); checkErr == nil {
				finalMayorName = candidate
				break
			}
		}
	}

	result, err := h.wcClient.CreateChannel(worldchannel.CreateChannelParams{
		WorldName:        worldName,
		MayorName:        finalMayorName,
		WorldSummary:     worldSummary,
		CreatorDiscordID: session.DiscordID,
	})
	if err != nil {
		c.Logger().Errorf("Channel creation failed: %v", err)
		if patchErr := sse.PatchElementTempl(p.WorldSummaryCard(worldName, finalMayorName, worldSummary)); patchErr != nil {
			c.Logger().Errorf("Failed to patch summary card: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return
	}

	// Send welcome message.
	if err := h.wcClient.SendWelcomeMessage(result.ChannelID, worldchannel.WelcomeMessageParams{
		CreatorDiscordID: session.DiscordID,
		MayorName:        finalMayorName,
		WorldName:        worldName,
	}); err != nil {
		c.Logger().Errorf("Failed to send welcome message: %v", err)
	}

	// Persist onboarding conversation for future OpenClaw agent bootstrap.
	h.pinOnboardingConversation(c, result.ChannelID, session, finalMayorName, worldName, worldSummary)

	// Notify harness (with cover art if available).
	go h.notifyHarnessWorldHatchedWithCover(result.ChannelID, worldName, finalMayorName, session.DiscordID, session.DiscordUsername, coverArtData, coverArtMIME)

	// Clear world-ready state (also removes pending cover art file).
	h.convMgr.ClearWorldReady(session.DiscordID)

	// Show hatched card with Discord link and optional harness link.
	channelURL := fmt.Sprintf("https://discord.com/channels/%s/%s",
		h.wcClient.Config().GuildID, result.ChannelID)
	if err := sse.PatchElementTempl(p.WorldHatched(
		worldName, finalMayorName, channelURL,
		session.DiscordUsername, session.DiscordAvatar,
		h.HarnessURL,
	)); err != nil {
		c.Logger().Errorf("Failed to patch hatched card: %v", err)
	}
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}
}

// HandleCoverPreview serves the pending cover art image from disk.
// GET /mayor/cover-preview
func (h *Handler) HandleCoverPreview(c echo.Context) error {
	session, ok := c.Get("session").(*auth.Session)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	artPath, _, ok := h.convMgr.GetCoverArtPath(session.DiscordID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "no pending cover art")
	}

	return c.File(artPath)
}

// HandleGenerateCover regenerates cover art via SSE.
// POST /mayor/generate-cover
func (h *Handler) HandleGenerateCover(c echo.Context) error {
	session, ok := c.Get("session").(*auth.Session)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	mayorName, worldName, worldSummary, ready := h.convMgr.GetWorldReady(session.DiscordID)
	if !ready {
		return echo.NewHTTPError(http.StatusBadRequest, "no world ready for cover art generation")
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	if h.imagegenClient == nil {
		// Shouldn't happen (button wouldn't be shown), but handle gracefully.
		h.hatchWorldWithCover(c, sse, session, mayorName, worldName, worldSummary, nil, "")
		return nil
	}

	// Show loading spinner.
	if err := sse.PatchElementTempl(p.CoverArtGenerating(worldName, mayorName)); err != nil {
		c.Logger().Errorf("Failed to patch cover art loading: %v", err)
	}
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Generate cover art.
	prompt := buildCoverArtPrompt(worldName, worldSummary)
	result, err := h.imagegenClient.Generate(c.Request().Context(), prompt, imagegen.GenerateOptions{
		AspectRatio: "16:9",
	})
	if err != nil {
		c.Logger().Errorf("Cover art regeneration failed: %v", err)
		if patchErr := sse.PatchElementTempl(p.CoverArtError("Cover art generation failed. You can try again or hatch without it.", worldName, mayorName)); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}

	// Save to disk (overwrites previous).
	artPath, saveErr := savePendingCoverArt(h.dataDir, session.DiscordID, result.Data, result.MIMEType)
	if saveErr != nil {
		c.Logger().Errorf("Failed to save regenerated cover art: %v", saveErr)
		if patchErr := sse.PatchElementTempl(p.CoverArtError("Cover art generation failed. You can try again or hatch without it.", worldName, mayorName)); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}
	h.convMgr.SetCoverArtPath(session.DiscordID, artPath, result.MIMEType)

	// Show preview with cache-busting query param.
	previewURL := fmt.Sprintf("/mayor/cover-preview?t=%d", time.Now().UnixMilli())
	if err := sse.PatchElementTempl(p.CoverArtPreview(previewURL, worldName, mayorName)); err != nil {
		c.Logger().Errorf("Failed to patch cover art preview: %v", err)
	}
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	return nil
}

// HandleHatch reads pending cover art from disk and hatches the world.
// POST /mayor/hatch
func (h *Handler) HandleHatch(c echo.Context) error {
	session, ok := c.Get("session").(*auth.Session)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	mayorName, worldName, worldSummary, ready := h.convMgr.GetWorldReady(session.DiscordID)
	if !ready {
		return echo.NewHTTPError(http.StatusBadRequest, "no world ready to hatch")
	}

	// Prevent duplicate hatch attempts from concurrent requests.
	if !h.convMgr.SetHatched(session.DiscordID) {
		h.logger.Warn("duplicate hatch attempt blocked (HandleHatch)", "user", session.DiscordID)
		return echo.NewHTTPError(http.StatusConflict, "world is already being hatched")
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Read cover art from disk (if available).
	var coverData []byte
	var coverMIME string
	if artPath, mime, ok := h.convMgr.GetCoverArtPath(session.DiscordID); ok {
		data, err := os.ReadFile(artPath)
		if err != nil {
			h.logger.Warn("failed to read pending cover art, hatching without", "error", err)
		} else {
			coverData = data
			coverMIME = mime
		}
	}

	h.hatchWorldWithCover(c, sse, session, mayorName, worldName, worldSummary, coverData, coverMIME)
	return nil
}

// notifyHarnessWorldHatchedWithCover sends the world-hatched webhook with
// optional cover art as base64. Non-blocking — errors are logged.
func (h *Handler) notifyHarnessWorldHatchedWithCover(channelID, worldName, mayorName, creatorDiscordID, creatorUsername string, coverData []byte, coverMIME string) {
	harnessURL := h.HarnessURL
	if harnessURL == "" {
		return
	}

	hookSecret := os.Getenv("CM_HOOK_SECRET")

	payloadMap := map[string]string{
		"discord_channel_id": channelID,
		"world_name":         worldName,
		"mayor_name":         mayorName,
		"creator_discord_id": creatorDiscordID,
		"creator_username":   creatorUsername,
	}
	if len(coverData) > 0 {
		payloadMap["cover_image_base64"] = base64.StdEncoding.EncodeToString(coverData)
		payloadMap["cover_image_mime"] = coverMIME
	}

	payload, _ := json.Marshal(payloadMap)

	req, err := http.NewRequest(http.MethodPost, harnessURL+"/api/world-hatched", bytes.NewReader(payload))
	if err != nil {
		h.logger.Error("failed to create world-hatched request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if hookSecret != "" {
		req.Header.Set("X-Hook-Secret", hookSecret)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		h.logger.Error("world-hatched webhook failed", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		h.logger.Error("world-hatched webhook returned unexpected status", "status", resp.StatusCode)
	}
}

// pinOnboardingConversation persists the full onboarding conversation to Discord
// as a pinned message for later OpenClaw agent bootstrap.
func (h *Handler) pinOnboardingConversation(c echo.Context, channelID string, session *auth.Session, mayorName, worldName, worldSummary string) {
	messages := h.convMgr.GetMessages(session.DiscordID)
	onboardingMessages := make([]worldchannel.OnboardingMessage, len(messages))
	for i, m := range messages {
		onboardingMessages[i] = worldchannel.OnboardingMessage{
			Role: m.Role, Content: m.Content,
		}
	}
	if err := h.wcClient.PinOnboardingData(channelID, worldchannel.OnboardingData{
		Version: 1,
		Creator: worldchannel.OnboardingCreator{
			DiscordID: session.DiscordID,
			Username:  session.DiscordUsername,
		},
		World: worldchannel.OnboardingWorld{
			Name: worldName, Summary: worldSummary,
		},
		Mayor:    worldchannel.OnboardingMayor{Name: mayorName},
		Messages: onboardingMessages,
	}); err != nil {
		c.Logger().Errorf("Failed to pin onboarding data: %v", err)
	}
}

// isBillingOrOverloadError checks if an error is an Anthropic API error
// with a status code indicating billing issues (402), rate limiting (429),
// or overload (529).
func isBillingOrOverloadError(err error) bool {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 402, 429, 529:
			return true
		}
	}
	return false
}
