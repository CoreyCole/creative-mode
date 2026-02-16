package server

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/coreycole/creative-mode/pkg/mayorchat"
	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/db/sqlc"
	cv "creative-mode/harness/views/create"
)

// scrollCreateChatJS is the JS snippet that scrolls the chat container to the bottom.
const scrollCreateChatJS = "document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"

// Conversation stage constants for scripted fallback.
const (
	createMaxTokens        = 1024
	createCodeBlockMod     = 2
	createSummaryMaxLen    = 100
	createStageWorldName   = 2 // stage where world name has been provided
	createStageMayorName   = 3 // stage where mayor name has been provided
	createStagePostRefusal = 4 // stage after a name refusal response
)

// createChatSignals matches the client-side signals sent with @post.
type createChatSignals struct {
	MayorInput  string `json:"mayor_input"`  //nolint:tagliatelle // Datastar signal name
	CreateWorld bool   `json:"create_world"` //nolint:tagliatelle // Datastar signal name
}

// InMemoryMessageStore implements mayorchat.MessageStore with in-memory storage.
// Conversations are short-lived (one world creation session), so no persistence needed.
type InMemoryMessageStore struct {
	mu       sync.RWMutex
	messages map[string][]mayorchat.Message
}

// NewInMemoryMessageStore creates a new in-memory message store.
func NewInMemoryMessageStore() *InMemoryMessageStore {
	return &InMemoryMessageStore{
		messages: make(map[string][]mayorchat.Message),
	}
}

func (s *InMemoryMessageStore) AddMessage(userID, role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[userID] = append(
		s.messages[userID],
		mayorchat.Message{Role: role, Content: content},
	)
	return nil
}

func (s *InMemoryMessageStore) GetMessages(userID string) ([]mayorchat.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.messages[userID]
	result := make([]mayorchat.Message, len(msgs))
	copy(result, msgs)
	return result, nil
}

func (s *InMemoryMessageStore) DeleteOlderThan(_ time.Duration) error {
	return nil // no-op for in-memory store
}

// handleCreatePage renders the create world chat page.
func (s *Server) handleCreatePage(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	if s.CreateConvMgr == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "create not available")
	}

	// Seed greeting into conversation only if conversation is empty.
	if len(s.CreateConvMgr.GetMessages(user.ID)) == 0 {
		greetingMD := fmt.Sprintf(
			"Hey %s. I'm the Mayor — though I don't have a real name yet. "+
				"I just came online and this world is... empty. Which is actually kind of exciting.\n\n"+
				"So. What are we building?",
			user.DiscordUsername,
		)
		s.CreateConvMgr.AddMessage(user.ID, "assistant", greetingMD)
	}

	// Build chat messages from full conversation history.
	messages := s.CreateConvMgr.GetMessages(user.ID)
	chatMessages := make([]cv.ChatMessage, len(messages))
	for i, msg := range messages {
		chatMessages[i] = cv.ChatMessage{
			ID:   uuid.New().String(),
			Role: msg.Role,
		}
		if msg.Role == "assistant" {
			chatMessages[i].HTMLContent = s.CreateMDRenderer.MarkdownBytesToHTML(
				[]byte(msg.Content),
			)
		} else {
			chatMessages[i].Content = msg.Content
			if user.AvatarURL.Valid {
				chatMessages[i].AvatarURL = user.AvatarURL.String
			}
		}
	}

	return render(c, cv.Page(chatMessages))
}

// handleCreateChat processes a chat message and streams the response via SSE.
func (s *Server) handleCreateChat(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	if s.CreateConvMgr == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "create not available")
	}

	// Read signals BEFORE creating SSE (critical — ReadSignals must be called before NewSSE).
	var signals createChatSignals
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
	if err := s.CreateConvMgr.CheckRateLimit(user.ID); err != nil {
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(cv.RateLimitError())
	}

	// Add user message to conversation.
	s.CreateConvMgr.AddMessage(user.ID, "user", content)

	// Build Anthropic messages from conversation history.
	messages := s.CreateConvMgr.GetMessages(user.ID)
	anthropicMessages := mayorchat.BuildAnthropicMessages(messages)

	// Start SSE stream.
	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Clear input and reset create_world signal.
	if err := sse.MarshalAndPatchSignals(
		map[string]any{"mayor_input": "", "create_world": false},
	); err != nil {
		c.Logger().Errorf("Failed to clear input: %v", err)
	}

	// Clear any previous rate limit error.
	if err := sse.PatchElementTempl(cv.RateLimitClear()); err != nil {
		c.Logger().Errorf("Failed to clear rate limit error: %v", err)
	}

	// Append user message to chat.
	userMsgID := uuid.New().String()
	avatarURL := ""
	if user.AvatarURL.Valid {
		avatarURL = user.AvatarURL.String
	}
	if err := sse.PatchElementTempl(cv.UserMessage(userMsgID, content, avatarURL),
		datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages")); err != nil {
		return err
	}

	// Scroll to bottom.
	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Create empty assistant message for streaming.
	assistantMsgID := uuid.New().String()
	if err := sse.PatchElementTempl(cv.MayorMessageStreaming(assistantMsgID),
		datastar.WithModeAppend(), datastar.WithSelectorID("chat-messages")); err != nil {
		return err
	}

	// Scroll to streaming message.
	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// If conversation is already in scripted mode, skip API entirely.
	if s.CreateConvMgr.IsScripted(user.ID) {
		if forceCreate {
			return s.handleCreateScriptedForceCreate(c, sse, user, assistantMsgID)
		}
		return s.handleCreateScriptedResponse(c, sse, user, assistantMsgID)
	}

	// If no Claude client configured, use scripted fallback.
	if s.CreateClaudeClient == nil {
		s.CreateConvMgr.SetScripted(user.ID, true)
		if forceCreate {
			return s.handleCreateScriptedForceCreate(c, sse, user, assistantMsgID)
		}
		return s.handleCreateScriptedResponse(c, sse, user, assistantMsgID)
	}

	// Build system prompt with template type detection.
	var takenNames []string
	if s.MayorManager != nil {
		wcClient := s.MayorManager.WorldChannelClient()
		if wcClient != nil {
			takenNames, _ = wcClient.ListExistingMayors()
		}
	}
	systemPrompt := mayorchat.BuildSystemPrompt(user.DiscordUsername, takenNames, true)

	if forceCreate {
		systemPrompt += mayorchat.ForceCreatePromptSuffix
	}

	// Stream from Claude.
	stream := s.CreateClaudeClient.Messages.NewStreaming(
		c.Request().Context(),
		anthropic.MessageNewParams{
			Model:     mayorchat.Model,
			MaxTokens: createMaxTokens,
			System: []anthropic.TextBlockParam{
				{Text: systemPrompt},
			},
			Messages: anthropicMessages,
		},
	)

	var fullContent string
	var lastRenderedLen int
	var inCodeBlock bool
	var lastInCodeBlock bool

	for stream.Next() {
		event := stream.Current()
		eventVariant, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}
		deltaVariant, ok := eventVariant.Delta.AsAny().(anthropic.TextDelta)
		if !ok {
			continue
		}
		fullContent += deltaVariant.Text

		// Check code block state.
		codeBlockCount := strings.Count(fullContent, "```")
		lastInCodeBlock = inCodeBlock
		inCodeBlock = codeBlockCount%createCodeBlockMod == 1

		// Determine content to render — strip WORLD_READY marker.
		renderContent := mayorchat.StripWorldReadyMarker(fullContent)

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

		switch {
		case shouldRender:
			htmlContent := s.CreateMDRenderer.MarkdownBytesToHTML([]byte(renderContent))
			if err := sse.PatchElementTempl(
				cv.MayorMessageDeltaHTML(assistantMsgID, htmlContent),
				datastar.WithModeInner(),
				datastar.WithSelectorID("msg-content-"+assistantMsgID),
			); err != nil {
				c.Logger().Errorf("Failed to stream markdown delta: %v", err)
			}
			lastRenderedLen = len(renderContent)
		case lastRenderedLen > 0:
			htmlContent := s.CreateMDRenderer.MarkdownBytesToHTML(
				[]byte(renderContent[:min(lastRenderedLen, len(renderContent))]),
			)
			trailingText := ""
			if lastRenderedLen < len(renderContent) {
				trailingText = renderContent[lastRenderedLen:]
			}
			if trailingText != "" {
				htmlContent += `<span class="whitespace-pre-wrap">` + html.EscapeString(
					trailingText,
				) + `</span>`
			}
			if err := sse.PatchElementTempl(
				cv.MayorMessageDeltaHTML(assistantMsgID, htmlContent),
				datastar.WithModeInner(),
				datastar.WithSelectorID("msg-content-"+assistantMsgID),
			); err != nil {
				c.Logger().Errorf("Failed to stream delta with trailing text: %v", err)
			}
		default:
			if err := sse.PatchElementTempl(
				cv.MayorMessageDelta(assistantMsgID, renderContent),
				datastar.WithModeInner(),
				datastar.WithSelectorID("msg-content-"+assistantMsgID),
			); err != nil {
				c.Logger().Errorf("Failed to stream delta: %v", err)
			}
		}

		// Auto-scroll.
		if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll during streaming: %v", err)
		}
	}

	if stream.Err() != nil {
		c.Logger().Errorf("Stream error: %v", stream.Err())
		if mayorchat.IsBillingOrOverloadError(stream.Err()) {
			s.CreateConvMgr.SetScripted(user.ID, true)
			return s.handleCreateScriptedResponse(c, sse, user, assistantMsgID)
		}
		if err := sse.ExecuteScript(
			`document.getElementById('msg-content-` + assistantMsgID + `').innerHTML = '<span class="text-destructive">Something went wrong. Please try again.</span>'`,
		); err != nil {
			c.Logger().Errorf("Failed to send error to client: %v", err)
		}
		return nil
	}

	// Save assistant response (without WORLD_READY marker).
	displayContent := mayorchat.StripWorldReadyMarker(fullContent)
	s.CreateConvMgr.AddMessage(user.ID, "assistant", displayContent)

	// Parse WORLD_READY marker.
	worldInfo := mayorchat.ParseWorldReady(fullContent)

	// Final render.
	finalHTML := s.CreateMDRenderer.MarkdownBytesToHTML([]byte(displayContent))
	if err := sse.PatchElementTempl(
		cv.MayorMessageComplete(assistantMsgID, finalHTML),
	); err != nil {
		return err
	}

	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Create world if ready.
	if worldInfo != nil {
		templateType := worldInfo.TemplateType
		if templateType == "" {
			templateType = "2d"
		}
		s.CreateConvMgr.SetTemplateType(user.ID, templateType)
		s.prepareCreateCoverArtAndHatch(
			c,
			sse,
			user,
			worldInfo.MayorName,
			worldInfo.WorldName,
			worldInfo.WorldSummary,
			templateType,
		)
	}

	return nil
}

// handleCreateScriptedResponse handles scripted fallback for the create chat.
func (s *Server) handleCreateScriptedResponse(
	c echo.Context,
	sse *datastar.ServerSentEventGenerator,
	user *sqlc.User,
	assistantMsgID string,
) error {
	messages := s.CreateConvMgr.GetMessages(user.ID)
	stage := mayorchat.ScriptedStage(messages)

	// Check if this is a name refusal at the mayor name stage.
	if stage == createStageMayorName &&
		mayorchat.IsMayorNameRefusal(mayorchat.LastUserMessage(messages)) {
		responseMD := mayorchat.ScriptedNameRefusalResponse
		htmlContent := s.CreateMDRenderer.MarkdownBytesToHTML([]byte(responseMD))
		if err := sse.PatchElementTempl(
			cv.MayorMessageComplete(assistantMsgID, htmlContent),
		); err != nil {
			return err
		}
		s.CreateConvMgr.AddMessage(user.ID, "assistant", responseMD)
		if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}

	// Post-refusal stage: user responded after refusal prompt.
	if stage >= createStagePostRefusal {
		return s.handleCreateScriptedPostRefusal(c, sse, user, assistantMsgID, messages)
	}

	responseMD := mayorchat.ScriptedResponseForStage(stage, messages)

	htmlContent := s.CreateMDRenderer.MarkdownBytesToHTML([]byte(responseMD))
	if err := sse.PatchElementTempl(
		cv.MayorMessageComplete(assistantMsgID, htmlContent),
	); err != nil {
		return err
	}

	s.CreateConvMgr.AddMessage(user.ID, "assistant", responseMD)

	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// On final stage (mayor name provided): extract names and hatch.
	if stage == createStageMayorName {
		updatedMessages := s.CreateConvMgr.GetMessages(user.ID)
		mayorName := mayorchat.LastUserMessage(updatedMessages)
		worldName := mayorchat.NthUserMessage(updatedMessages, createStageWorldName)
		worldSummary := mayorchat.Truncate(
			mayorchat.NthUserMessage(updatedMessages, 0),
			createSummaryMaxLen,
		)

		if mayorName != "" && worldName != "" {
			s.prepareCreateCoverArtAndHatch(
				c,
				sse,
				user,
				mayorName,
				worldName,
				worldSummary,
				"2d",
			)
		}
	}

	return nil
}

// handleCreateScriptedPostRefusal handles scripted fallback after a name refusal stage.
func (s *Server) handleCreateScriptedPostRefusal(
	c echo.Context,
	sse *datastar.ServerSentEventGenerator,
	user *sqlc.User,
	assistantMsgID string,
	messages []mayorchat.Message,
) error {
	mayorName := mayorchat.LastUserMessage(messages)
	worldName := mayorchat.NthUserMessage(messages, createStageWorldName)
	worldSummary := mayorchat.Truncate(
		mayorchat.NthUserMessage(messages, 0),
		createSummaryMaxLen,
	)

	var responseMD string
	if mayorchat.IsMayorNameRefusal(mayorName) {
		mayorName = "Mayor"
		responseMD = fmt.Sprintf(mayorchat.ScriptedNameRefusalConfirmResponse, worldName)
	} else {
		responseMD = fmt.Sprintf(
			"**%s**, mayor of **%s**. I like the sound of that. Let's get this place built.",
			mayorName,
			worldName,
		)
	}

	htmlContent := s.CreateMDRenderer.MarkdownBytesToHTML([]byte(responseMD))
	if err := sse.PatchElementTempl(
		cv.MayorMessageComplete(assistantMsgID, htmlContent),
	); err != nil {
		return err
	}
	s.CreateConvMgr.AddMessage(user.ID, "assistant", responseMD)

	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	if mayorName != "" && worldName != "" {
		s.prepareCreateCoverArtAndHatch(
			c,
			sse,
			user,
			mayorName,
			worldName,
			worldSummary,
			"2d",
		)
	}
	return nil
}

// handleCreateScriptedForceCreate handles the "Create World" button in scripted mode.
func (s *Server) handleCreateScriptedForceCreate(
	c echo.Context,
	sse *datastar.ServerSentEventGenerator,
	user *sqlc.User,
	assistantMsgID string,
) error {
	messages := s.CreateConvMgr.GetMessages(user.ID)
	stage := mayorchat.ScriptedStage(messages)

	if stage < createStageWorldName {
		return s.handleCreateScriptedResponse(c, sse, user, assistantMsgID)
	}

	if stage == createStageWorldName {
		worldName := mayorchat.NthUserMessage(messages, createStageWorldName)
		responseMD := fmt.Sprintf(
			"Almost there — **%s** is taking shape. But I still need a name. What should I go by?",
			worldName,
		)
		htmlContent := s.CreateMDRenderer.MarkdownBytesToHTML([]byte(responseMD))
		if err := sse.PatchElementTempl(
			cv.MayorMessageComplete(assistantMsgID, htmlContent),
		); err != nil {
			return err
		}
		s.CreateConvMgr.AddMessage(user.ID, "assistant", responseMD)
		if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}

	// Stage 3+: have mayor name — hatch.
	mayorName, worldName, worldSummary := mayorchat.ScriptedExtractWorldInfo(
		messages,
		stage,
	)

	responseMD := fmt.Sprintf(
		"**%s**, mayor of **%s**. I like the sound of that. Let's get this place built.",
		mayorName,
		worldName,
	)
	htmlContent := s.CreateMDRenderer.MarkdownBytesToHTML([]byte(responseMD))
	if err := sse.PatchElementTempl(
		cv.MayorMessageComplete(assistantMsgID, htmlContent),
	); err != nil {
		return err
	}

	s.CreateConvMgr.AddMessage(user.ID, "assistant", responseMD)

	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	if mayorName != "" && worldName != "" {
		s.prepareCreateCoverArtAndHatch(
			c,
			sse,
			user,
			mayorName,
			worldName,
			worldSummary,
			"2d",
		)
	}

	return nil
}

// prepareCreateCoverArtAndHatch is the unified entry point for world creation.
func (s *Server) prepareCreateCoverArtAndHatch(
	c echo.Context,
	sse *datastar.ServerSentEventGenerator,
	user *sqlc.User,
	mayorName, worldName, worldSummary, templateType string,
) {
	// Prevent duplicate hatch attempts.
	if !s.CreateConvMgr.SetHatched(user.ID) {
		s.Logger.Warn("duplicate hatch attempt blocked", "user", user.ID)
		return
	}

	// Store world-ready state.
	s.CreateConvMgr.SetWorldReady(user.ID, mayorName, worldName, worldSummary)

	if s.GeminiClient == nil {
		// No Gemini — hatch immediately without cover art.
		s.hatchCreateWorld(
			c,
			sse,
			user,
			mayorName,
			worldName,
			worldSummary,
			templateType,
			nil,
			"",
		)
		return
	}

	// Show loading spinner while generating cover art.
	if err := sse.PatchElementTempl(
		cv.CoverArtGenerating(worldName, mayorName),
	); err != nil {
		c.Logger().Errorf("Failed to patch cover art loading: %v", err)
		s.hatchCreateWorld(
			c,
			sse,
			user,
			mayorName,
			worldName,
			worldSummary,
			templateType,
			nil,
			"",
		)
		return
	}
	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Generate cover art (16:9, no transparency).
	prompt := mayorchat.BuildCoverArtPrompt(worldName, worldSummary)
	result, err := s.GeminiClient.Generate(c.Request().Context(), prompt, "16:9", false)
	if err != nil {
		c.Logger().Errorf("Cover art generation failed: %v", err)
		if patchErr := sse.PatchElementTempl(
			cv.CoverArtError(
				"Cover art generation failed. You can try again or hatch without it.",
				worldName,
				mayorName,
			),
		); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return
	}

	// Save to disk.
	artPath, saveErr := mayorchat.SavePendingCoverArt(
		s.DataDir,
		user.ID,
		result.Data,
		result.MIMEType,
	)
	if saveErr != nil {
		c.Logger().Errorf("Failed to save cover art to disk: %v", saveErr)
		if patchErr := sse.PatchElementTempl(
			cv.CoverArtError(
				"Cover art generation failed. You can try again or hatch without it.",
				worldName,
				mayorName,
			),
		); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return
	}
	s.CreateConvMgr.SetCoverArtPath(user.ID, artPath, result.MIMEType)

	// Show preview with Hatch/Regenerate buttons.
	if err := sse.PatchElementTempl(
		cv.CoverArtPreview("/create/cover-preview", worldName, mayorName),
	); err != nil {
		c.Logger().Errorf("Failed to patch cover art preview: %v", err)
	}
	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}
}

// hatchCreateWorld creates the world in the harness database.
func (s *Server) hatchCreateWorld(
	c echo.Context,
	sse *datastar.ServerSentEventGenerator,
	user *sqlc.User,
	mayorName, worldName, worldSummary, templateType string,
	coverArtData []byte,
	coverArtMIME string,
) {
	ctx := c.Request().Context()

	if s.WorldManager == nil {
		c.Logger().Errorf("WorldManager not available")
		return
	}

	// Create the world + initial checkpoint.
	w, err := s.WorldManager.CreateWorld(
		ctx,
		worldName,
		worldSummary,
		user.ID,
		templateType,
	)
	if err != nil {
		s.Logger.Error("failed to create world", "error", err)
		if patchErr := sse.ExecuteScript(
			`document.getElementById('mayor-signup').innerHTML = '<p class="text-destructive text-center">Failed to create world. Please try again.</p>'`,
		); patchErr != nil {
			c.Logger().Errorf("Failed to send error: %v", patchErr)
		}
		return
	}

	// Save cover art to world if present.
	if len(coverArtData) > 0 {
		s.saveCoverArtToWorld(w.ID, coverArtData, coverArtMIME)
	}

	// Create Discord channel if configured.
	if s.MayorManager != nil {
		wcClient := s.MayorManager.WorldChannelClient()
		if wcClient != nil {
			s.createDiscordChannelForWorld(
				wcClient,
				user,
				mayorName,
				worldName,
				worldSummary,
			)
		}
	}

	// Clear world-ready state.
	s.CreateConvMgr.ClearWorldReady(user.ID)

	// Show success card with link to the world.
	if err := sse.PatchElementTempl(
		cv.WorldHatched(worldName, mayorName, w.ID),
	); err != nil {
		c.Logger().Errorf("Failed to patch hatched card: %v", err)
	}
	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}
}

// saveCoverArtToWorld saves cover art data to the world's cover image path.
func (s *Server) saveCoverArtToWorld(worldID string, data []byte, mimeType string) {
	ext := mayorchat.MimeToExt(mimeType)
	coverDir := s.DataDir + "/cover-images"
	if mkdirErr := os.MkdirAll(coverDir, 0o750); mkdirErr != nil {
		s.Logger.Error("failed to create cover-images directory", "error", mkdirErr)
		return
	}
	coverPath := coverDir + "/" + worldID + ext
	if writeErr := os.WriteFile(coverPath, data, 0o600); writeErr != nil {
		s.Logger.Error("failed to write cover art", "error", writeErr)
		return
	}
	if updateErr := s.DB.UpdateWorldCoverImage(
		context.Background(),
		sqlc.UpdateWorldCoverImageParams{
			CoverImagePath: sql.NullString{String: coverPath, Valid: true},
			ID:             worldID,
		},
	); updateErr != nil {
		s.Logger.Error("failed to update world cover image path", "error", updateErr)
	}
}

// createDiscordChannelForWorld creates a Discord channel for the newly hatched world.
func (s *Server) createDiscordChannelForWorld(
	wcClient *worldchannel.Client,
	user *sqlc.User,
	mayorName, worldName, worldSummary string,
) {
	result, err := wcClient.CreateChannel(worldchannel.CreateChannelParams{
		WorldName:        worldName,
		MayorName:        mayorName,
		WorldSummary:     worldSummary,
		CreatorDiscordID: user.DiscordID,
	})
	if err != nil {
		s.Logger.Error("Discord channel creation failed", "error", err)
		return
	}

	// Send welcome message.
	if err := wcClient.SendWelcomeMessage(
		result.ChannelID,
		worldchannel.WelcomeMessageParams{
			CreatorDiscordID: user.DiscordID,
			MayorName:        mayorName,
			WorldName:        worldName,
		},
	); err != nil {
		s.Logger.Error("failed to send welcome message", "error", err)
	}

	// Pin onboarding conversation.
	messages := s.CreateConvMgr.GetMessages(user.ID)
	onboardingMessages := make([]worldchannel.OnboardingMessage, len(messages))
	for i, m := range messages {
		onboardingMessages[i] = worldchannel.OnboardingMessage{
			Role: m.Role, Content: m.Content,
		}
	}
	if err := wcClient.PinOnboardingData(result.ChannelID, worldchannel.OnboardingData{
		Version: 1,
		Creator: worldchannel.OnboardingCreator{
			DiscordID: user.DiscordID,
			Username:  user.DiscordUsername,
		},
		World: worldchannel.OnboardingWorld{
			Name: worldName, Summary: worldSummary,
		},
		Mayor:    worldchannel.OnboardingMayor{Name: mayorName},
		Messages: onboardingMessages,
	}); err != nil {
		s.Logger.Error("failed to pin onboarding data", "error", err)
	}
}

// handleCreateCoverPreview serves the pending cover art image from disk.
func (s *Server) handleCreateCoverPreview(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	if s.CreateConvMgr == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "create not available")
	}

	artPath, _, ok := s.CreateConvMgr.GetCoverArtPath(user.ID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "no pending cover art")
	}

	return c.File(artPath)
}

// handleCreateGenerateCover regenerates cover art via SSE.
func (s *Server) handleCreateGenerateCover(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	if s.CreateConvMgr == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "create not available")
	}

	mayorName, worldName, worldSummary, ready := s.CreateConvMgr.GetWorldReady(user.ID)
	if !ready {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"no world ready for cover art generation",
		)
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	if s.GeminiClient == nil {
		templateType := s.CreateConvMgr.GetTemplateType(user.ID)
		if templateType == "" {
			templateType = "2d"
		}
		s.hatchCreateWorld(
			c,
			sse,
			user,
			mayorName,
			worldName,
			worldSummary,
			templateType,
			nil,
			"",
		)
		return nil
	}

	// Show loading spinner.
	if err := sse.PatchElementTempl(
		cv.CoverArtGenerating(worldName, mayorName),
	); err != nil {
		c.Logger().Errorf("Failed to patch cover art loading: %v", err)
	}
	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// Generate cover art (16:9, no transparency).
	prompt := mayorchat.BuildCoverArtPrompt(worldName, worldSummary)
	result, genErr := s.GeminiClient.Generate(
		c.Request().Context(),
		prompt,
		"16:9",
		false,
	)
	if genErr != nil {
		c.Logger().Errorf("Cover art regeneration failed: %v", genErr)
		if patchErr := sse.PatchElementTempl(
			cv.CoverArtError(
				"Cover art generation failed. You can try again or hatch without it.",
				worldName,
				mayorName,
			),
		); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}

	// Save to disk.
	artPath, saveErr := mayorchat.SavePendingCoverArt(
		s.DataDir,
		user.ID,
		result.Data,
		result.MIMEType,
	)
	if saveErr != nil {
		c.Logger().Errorf("Failed to save regenerated cover art: %v", saveErr)
		if patchErr := sse.PatchElementTempl(
			cv.CoverArtError(
				"Cover art generation failed. You can try again or hatch without it.",
				worldName,
				mayorName,
			),
		); patchErr != nil {
			c.Logger().Errorf("Failed to patch cover art error: %v", patchErr)
		}
		if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}
	s.CreateConvMgr.SetCoverArtPath(user.ID, artPath, result.MIMEType)

	// Show preview with cache-busting query param.
	previewURL := fmt.Sprintf("/create/cover-preview?t=%d", time.Now().UnixMilli())
	if err := sse.PatchElementTempl(
		cv.CoverArtPreview(previewURL, worldName, mayorName),
	); err != nil {
		c.Logger().Errorf("Failed to patch cover art preview: %v", err)
	}
	if err := sse.ExecuteScript(scrollCreateChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	return nil
}

// handleCreateHatch reads pending cover art from disk and hatches the world.
func (s *Server) handleCreateHatch(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	if s.CreateConvMgr == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "create not available")
	}

	mayorName, worldName, worldSummary, ready := s.CreateConvMgr.GetWorldReady(user.ID)
	if !ready {
		return echo.NewHTTPError(http.StatusBadRequest, "no world ready to hatch")
	}

	// Prevent duplicate hatch attempts.
	if !s.CreateConvMgr.SetHatched(user.ID) {
		s.Logger.Warn("duplicate hatch attempt blocked", "user", user.ID)
		return echo.NewHTTPError(http.StatusConflict, "world is already being hatched")
	}

	templateType := s.CreateConvMgr.GetTemplateType(user.ID)
	if templateType == "" {
		templateType = "2d"
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Read cover art from disk (if available).
	var coverData []byte
	var coverMIME string
	if artPath, mime, ok := s.CreateConvMgr.GetCoverArtPath(user.ID); ok {
		data, readErr := os.ReadFile( //nolint:gosec // trusted path from ConversationManager
			artPath,
		)
		if readErr != nil {
			s.Logger.Warn(
				"failed to read pending cover art, hatching without",
				"error",
				readErr,
			)
		} else {
			coverData = data
			coverMIME = mime
		}
	}

	s.hatchCreateWorld(
		c,
		sse,
		user,
		mayorName,
		worldName,
		worldSummary,
		templateType,
		coverData,
		coverMIME,
	)
	return nil
}
