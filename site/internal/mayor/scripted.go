package mayor

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/coreycole/creative-mode/site/internal/auth"
	p "github.com/coreycole/creative-mode/site/pages"
)

// Scripted responses for each stage when the API is unavailable.
// Stage is determined by counting user messages (0-indexed).
var scriptedResponses = []string{
	// Stage 0: user described their world idea
	"Okay, I can work with that. What would people actually *do* there — " +
		"chill hang-out-and-explore, or more of a quest-and-fight-things situation?",
	// Stage 1: user described mood/gameplay — suggest world names
	"That's starting to feel like a real place. What do you want to call it? " +
		"A few ideas off the top of my head: **Duskhollow**, **Ember Reach**, **Wanderveil** — " +
		"or something totally different. Your call.",
	// Stage 2: user provided world name — confirm it, then ask for mayor name
	"**%s** — yeah, that works. Now — I need a name too. " +
		"I'm the mayor of this place, so pick something that fits. What should I go by?",
	// Stage 3: user provided mayor name (response uses placeholders, triggers hatchWorld)
	"**%s**, mayor of **%s**. I like the sound of that. Let's get this place built.",
}

// scriptedNameRefusalResponse is shown when the user declines to name the mayor.
const scriptedNameRefusalResponse = "Just \"Mayor\"? That's... functional. " +
	"A bit like naming your dog \"Dog.\" But hey, it works — you can rename me any time. " +
	"What should I go by for real, or should I just roll with **Mayor**?"

// scriptedNameRefusalConfirmResponse is shown after the user confirms "Mayor" a second time.
const scriptedNameRefusalConfirmResponse = "**Mayor** of **%s**. Fine, I'll own it. Let's get this place built."

// isMayorNameRefusal returns true if the user's input looks like a refusal to
// name the mayor (empty, vague, or literally "mayor").
func isMayorNameRefusal(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return true
	}
	refusals := []string{
		"mayor", "just mayor", "idk", "i don't know", "i dont know",
		"no", "nah", "no idea", "dunno", "whatever", "skip",
		"no name", "none", "pass", "default",
	}
	for _, r := range refusals {
		if normalized == r {
			return true
		}
	}
	return false
}

// handleScriptedResponse fills the streaming placeholder with a pre-written
// response. It follows the same PatchElementTempl pattern as the real API path
// so the frontend doesn't know or care where the text came from.
func (h *Handler) handleScriptedResponse(c echo.Context, sse *datastar.ServerSentEventGenerator, session *auth.Session, assistantMsgID string) error {
	messages := h.convMgr.GetMessages(session.DiscordID)
	userMsgCount := countUserMessages(messages)
	stage := userMsgCount - 1 // 0-indexed: first user message = stage 0
	if stage < 0 {
		stage = 0
	}

	// Check if this is a name refusal at stage 3.
	if stage == 3 && isMayorNameRefusal(lastUserMessage(messages)) {
		// Show self-deprecating response asking them to reconsider or confirm.
		responseMD := scriptedNameRefusalResponse
		htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
		if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
			return err
		}
		h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)
		if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}

	// Stage 4+: user responded after refusal prompt — check if they gave a real
	// name or confirmed the default again.
	if stage >= 4 {
		mayorName := lastUserMessage(messages)
		worldName := nthUserMessage(messages, 2)
		worldSummary := truncate(nthUserMessage(messages, 0), 100)

		if isMayorNameRefusal(mayorName) {
			mayorName = "Mayor"
			responseMD := fmt.Sprintf(scriptedNameRefusalConfirmResponse, worldName)
			htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
			if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
				return err
			}
			h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)
		} else {
			responseMD := fmt.Sprintf("**%s**, mayor of **%s**. I like the sound of that. Let's get this place built.", mayorName, worldName)
			htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
			if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
				return err
			}
			h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)
		}

		if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}

		if mayorName != "" && worldName != "" {
			h.prepareCoverArtAndHatch(c, sse, session, mayorName, worldName, worldSummary)
		}
		return nil
	}

	responseMD := scriptedResponseForStage(stage, messages)

	// Render markdown and patch into the streaming placeholder.
	htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
	if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
		return err
	}

	// Save to conversation history.
	h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)

	// Scroll to bottom.
	if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// On final stage (stage 3 with a real name): extract names and hatch.
	if stage == 3 {
		updatedMessages := h.convMgr.GetMessages(session.DiscordID)
		mayorName := lastUserMessage(updatedMessages)
		worldName := nthUserMessage(updatedMessages, 2)
		worldSummary := truncate(nthUserMessage(updatedMessages, 0), 100)

		if mayorName != "" && worldName != "" {
			h.prepareCoverArtAndHatch(c, sse, session, mayorName, worldName, worldSummary)
		}
	}

	return nil
}

// handleScriptedForceCreate handles the "Create World" button in scripted mode.
// If the conversation has enough data (stage >= 3) and a valid mayor name, it
// skips to hatching. If the mayor name is missing (stage < 3) or was refused,
// it prompts for the name instead of auto-generating one.
func (h *Handler) handleScriptedForceCreate(c echo.Context, sse *datastar.ServerSentEventGenerator, session *auth.Session, assistantMsgID string) error {
	messages := h.convMgr.GetMessages(session.DiscordID)
	userMsgCount := countUserMessages(messages)
	stage := userMsgCount - 1

	if stage < 2 {
		// Not enough info yet — fall through to normal scripted flow.
		return h.handleScriptedResponse(c, sse, session, assistantMsgID)
	}

	// Stage 2: have world name but no mayor name yet — ask for it.
	if stage == 2 {
		worldName := nthUserMessage(messages, 2)
		responseMD := fmt.Sprintf("Almost there — **%s** is taking shape. But I still need a name. What should I go by?", worldName)
		htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
		if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
			return err
		}
		h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)
		if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}

	// Stage 3+: have mayor name — hatch.
	mayorName := lastUserMessage(messages)
	worldName := nthUserMessage(messages, 2)
	worldSummary := truncate(nthUserMessage(messages, 0), 100)

	// If the mayor name was a refusal, default to "Mayor".
	if isMayorNameRefusal(mayorName) {
		mayorName = "Mayor"
	}

	responseMD := fmt.Sprintf("**%s**, mayor of **%s**. I like the sound of that. Let's get this place built.", mayorName, worldName)

	htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
	if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
		return err
	}

	h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)

	if err := sse.ExecuteScript("document.getElementById('chat-messages').scrollTop = document.getElementById('chat-messages').scrollHeight"); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	if mayorName != "" && worldName != "" {
		h.prepareCoverArtAndHatch(c, sse, session, mayorName, worldName, worldSummary)
	}

	return nil
}

// scriptedResponseForStage returns the pre-written response for the given stage,
// substituting names from conversation history where needed.
func scriptedResponseForStage(stage int, messages []Message) string {
	// Clamp to last stage.
	if stage >= len(scriptedResponses) {
		stage = len(scriptedResponses) - 1
	}

	switch stage {
	case 2:
		// Insert world name (user's message at this stage).
		worldName := nthUserMessage(messages, 2)
		return fmt.Sprintf(scriptedResponses[stage], worldName)
	case 3:
		// Insert mayor name and world name.
		mayorName := lastUserMessage(messages)
		worldName := nthUserMessage(messages, 2)
		return fmt.Sprintf(scriptedResponses[stage], mayorName, worldName)
	default:
		return scriptedResponses[stage]
	}
}

// countUserMessages returns the number of user messages in the conversation.
func countUserMessages(messages []Message) int {
	count := 0
	for _, m := range messages {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// nthUserMessage returns the nth user message (0-indexed) or empty string.
func nthUserMessage(messages []Message, n int) string {
	count := 0
	for _, m := range messages {
		if m.Role == "user" {
			if count == n {
				return strings.TrimSpace(m.Content)
			}
			count++
		}
	}
	return ""
}

// lastUserMessage returns the most recent user message or empty string.
func lastUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

// truncate returns s truncated to maxLen characters with "..." appended if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
