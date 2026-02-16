package mayor

import (
	"fmt"

	"github.com/coreycole/creative-mode/pkg/mayorchat"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/coreycole/creative-mode/site/internal/auth"
	p "github.com/coreycole/creative-mode/site/pages"
)

// handleScriptedResponse fills the streaming placeholder with a pre-written
// response. It follows the same PatchElementTempl pattern as the real API path
// so the frontend doesn't know or care where the text came from.
func (h *Handler) handleScriptedResponse(c echo.Context, sse *datastar.ServerSentEventGenerator, session *auth.Session, assistantMsgID string) error {
	messages := h.convMgr.GetMessages(session.DiscordID)
	stage := mayorchat.ScriptedStage(messages)

	// Check if this is a name refusal at stage 3.
	if stage == 3 && mayorchat.IsMayorNameRefusal(mayorchat.LastUserMessage(messages)) {
		responseMD := mayorchat.ScriptedNameRefusalResponse
		htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
		if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
			return err
		}
		h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		return nil
	}

	// Stage 4+: user responded after refusal prompt — check if they gave a real
	// name or confirmed the default again.
	if stage >= 4 {
		mayorName := mayorchat.LastUserMessage(messages)
		worldName := mayorchat.NthUserMessage(messages, 2)
		worldSummary := mayorchat.Truncate(mayorchat.NthUserMessage(messages, 0), 100)

		if mayorchat.IsMayorNameRefusal(mayorName) {
			mayorName = "Mayor"
			responseMD := fmt.Sprintf(mayorchat.ScriptedNameRefusalConfirmResponse, worldName)
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

		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}

		if mayorName != "" && worldName != "" {
			h.prepareCoverArtAndHatch(c, sse, session, &mayorchat.WorldReadyInfo{
				MayorName: mayorName, WorldName: worldName, WorldSummary: worldSummary,
			})
		}
		return nil
	}

	responseMD := mayorchat.ScriptedResponseForStage(stage, messages)

	// Render markdown and patch into the streaming placeholder.
	htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
	if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
		return err
	}

	// Save to conversation history.
	h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)

	// Scroll to bottom.
	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	// On final stage (stage 3 with a real name): extract names and hatch.
	if stage == 3 {
		updatedMessages := h.convMgr.GetMessages(session.DiscordID)
		mayorName := mayorchat.LastUserMessage(updatedMessages)
		worldName := mayorchat.NthUserMessage(updatedMessages, 2)
		worldSummary := mayorchat.Truncate(mayorchat.NthUserMessage(updatedMessages, 0), 100)

		if mayorName != "" && worldName != "" {
			h.prepareCoverArtAndHatch(c, sse, session, &mayorchat.WorldReadyInfo{
				MayorName: mayorName, WorldName: worldName, WorldSummary: worldSummary,
			})
		}
	}

	return nil
}

// handleScriptedForceCreate handles the "Create World" button in scripted mode.
func (h *Handler) handleScriptedForceCreate(c echo.Context, sse *datastar.ServerSentEventGenerator, session *auth.Session, assistantMsgID string) error {
	messages := h.convMgr.GetMessages(session.DiscordID)
	stage := mayorchat.ScriptedStage(messages)

	if stage < 2 {
		// Not enough info to create — re-enable the button.
		if err := sse.MarshalAndPatchSignals(map[string]any{"world_creating": false}); err != nil {
			c.Logger().Errorf("Failed to patch world_creating signal: %v", err)
		}
		return h.handleScriptedResponse(c, sse, session, assistantMsgID)
	}

	// Stage 2: have world name but no mayor name yet — ask for it.
	if stage == 2 {
		worldName := mayorchat.NthUserMessage(messages, 2)
		responseMD := fmt.Sprintf("Almost there — **%s** is taking shape. But I still need a name. What should I go by?", worldName)
		htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
		if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
			return err
		}
		h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)
		if err := sse.ExecuteScript(scrollChatJS); err != nil {
			c.Logger().Errorf("Failed to scroll: %v", err)
		}
		// Not ready yet — re-enable the button.
		if err := sse.MarshalAndPatchSignals(map[string]any{"world_creating": false}); err != nil {
			c.Logger().Errorf("Failed to patch world_creating signal: %v", err)
		}
		return nil
	}

	// Stage 3+: have mayor name — hatch.
	mayorName, worldName, worldSummary := mayorchat.ScriptedExtractWorldInfo(messages, stage)

	responseMD := fmt.Sprintf("**%s**, mayor of **%s**. I like the sound of that. Let's get this place built.", mayorName, worldName)
	htmlContent := h.mdRenderer.MarkdownBytesToHTML([]byte(responseMD))
	if err := sse.PatchElementTempl(p.MayorMessageComplete(assistantMsgID, htmlContent)); err != nil {
		return err
	}

	h.convMgr.AddMessage(session.DiscordID, "assistant", responseMD)

	if err := sse.ExecuteScript(scrollChatJS); err != nil {
		c.Logger().Errorf("Failed to scroll: %v", err)
	}

	if mayorName != "" && worldName != "" {
		h.prepareCoverArtAndHatch(c, sse, session, &mayorchat.WorldReadyInfo{
			MayorName: mayorName, WorldName: worldName, WorldSummary: worldSummary,
		})
	}

	return nil
}
