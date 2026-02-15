package mayor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/coreycole/creative-mode/site/internal/auth"
	"github.com/coreycole/creative-mode/site/internal/markdown"
	p "github.com/coreycole/creative-mode/site/pages"
)

// ChatSignals matches the client-side signals sent with @post.
type ChatSignals struct {
	MayorInput string `json:"mayor_input"`
}

// Handler handles the mayor chat SSE endpoint.
type Handler struct {
	client     anthropic.Client
	convMgr    *ConversationManager
	mdRenderer *markdown.Renderer
	wcClient   *worldchannel.Client // nil if no bot token configured
	HarnessURL string               // optional — shown on world hatch card
}

// NewHandler creates a new mayor chat handler.
func NewHandler(client anthropic.Client, convMgr *ConversationManager, mdRenderer *markdown.Renderer, wcClient *worldchannel.Client) *Handler {
	return &Handler{
		client:     client,
		convMgr:    convMgr,
		mdRenderer: mdRenderer,
		wcClient:   wcClient,
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

	content := strings.TrimSpace(signals.MayorInput)
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

	// Clear input.
	if err := sse.MarshalAndPatchSignals(map[string]any{"mayor_input": ""}); err != nil {
		c.Logger().Errorf("Failed to clear input: %v", err)
	}

	// Append user message to chat.
	userMsgID := uuid.New().String()
	if err := sse.PatchElementTempl(p.UserMessage(userMsgID, content),
		datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages")); err != nil {
		return err
	}

	// Scroll to bottom.
	if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Create empty assistant message for streaming.
	assistantMsgID := uuid.New().String()
	if err := sse.PatchElementTempl(p.MayorMessageStreaming(assistantMsgID),
		datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages")); err != nil {
		return err
	}

	// Scroll to streaming message.
	if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// If conversation is already in scripted mode, skip API entirely.
	if h.convMgr.IsScripted(session.DiscordID) {
		return h.handleScriptedResponse(c, sse, session, assistantMsgID)
	}

	// Get the system prompt stored on the session (built at page load).
	systemPrompt := session.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = BuildSystemPrompt(session.DiscordUsername, nil)
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
				if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
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
		if len(parts) == 3 {
			mayorName = strings.TrimSpace(parts[0])
			worldName = strings.TrimSpace(parts[1])
			worldSummary = strings.TrimSpace(parts[2])
		}
	}

	// Final render (without marker).
	finalHTML := h.mdRenderer.MarkdownBytesToHTML([]byte(displayContent))
	if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, finalHTML)); err != nil {
		return err
	}

	// Scroll to bottom after final render.
	if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Create Discord channel if world is ready.
	if mayorName != "" && worldName != "" {
		if h.wcClient != nil {
			h.hatchWorld(c, sse, session, mayorName, worldName, worldSummary)
		} else {
			// No bot token — show summary card without Discord link.
			if err := sse.PatchElementTempl(p.WorldSummaryCard(worldName, mayorName, worldSummary)); err != nil {
				c.Logger().Errorf("Failed to patch summary card: %v", err)
			}
		}
	}

	return nil
}

// hatchWorld creates the Discord channel and sends the welcome message.
func (h *Handler) hatchWorld(c echo.Context, sse *datastar.ServerSentEventGenerator, session *auth.Session, mayorName, worldName, worldSummary string) {
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
		// Fall back to summary card without Discord link.
		if patchErr := sse.PatchElementTempl(p.WorldSummaryCard(worldName, finalMayorName, worldSummary)); patchErr != nil {
			c.Logger().Errorf("Failed to patch summary card: %v", patchErr)
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

	// Notify harness that a world was hatched (fire and forget).
	go h.notifyHarnessWorldHatched(result.ChannelID, worldName, finalMayorName, session.DiscordID, session.DiscordUsername)

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

// notifyHarnessWorldHatched sends a webhook to the harness server to trigger
// mayor agent provisioning. Non-blocking — errors are logged but don't fail
// the user flow.
func (h *Handler) notifyHarnessWorldHatched(channelID, worldName, mayorName, creatorDiscordID, creatorUsername string) {
	harnessURL := h.HarnessURL
	if harnessURL == "" {
		return
	}

	hookSecret := os.Getenv("CM_HOOK_SECRET")

	payload, _ := json.Marshal(map[string]string{
		"discord_channel_id": channelID,
		"world_name":         worldName,
		"mayor_name":         mayorName,
		"creator_discord_id": creatorDiscordID,
		"creator_username":   creatorUsername,
	})

	req, err := http.NewRequest(http.MethodPost, harnessURL+"/api/world-hatched", bytes.NewReader(payload))
	if err != nil {
		fmt.Printf("ERROR: failed to create world-hatched request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if hookSecret != "" {
		req.Header.Set("X-Hook-Secret", hookSecret)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("ERROR: world-hatched webhook failed: %v\n", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		fmt.Printf("ERROR: world-hatched webhook returned %d\n", resp.StatusCode)
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
