package worldchannel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	// OnboardingDataVersion is the current version for newly created onboarding data.
	OnboardingDataVersion = 2

	// onboardingMarker identifies onboarding data messages in Discord.
	onboardingMarker = "\U0001F95A Onboarding Conversation"
	// onboardingContinuation identifies continuation messages.
	onboardingContinuation = "\U0001F95A Onboarding (continued)"
	// discordMaxMessageLen is Discord's message character limit.
	discordMaxMessageLen = 2000
)

// OnboardingData holds the full onboarding conversation for later agent bootstrap.
type OnboardingData struct {
	Version  int                 `json:"version"`
	Creator  OnboardingCreator   `json:"creator"`
	World    OnboardingWorld     `json:"world"`
	Mayor    OnboardingMayor     `json:"mayor"`
	Messages []OnboardingMessage `json:"conversation"`
}

// OnboardingCreator identifies the world creator.
type OnboardingCreator struct {
	DiscordID string `json:"discord_id"`
	Username  string `json:"username"`
}

// OnboardingWorld holds the world's name and summary.
type OnboardingWorld struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// OnboardingMayor holds the mayor's name and optional personality traits.
type OnboardingMayor struct {
	Name     string `json:"name"`
	Creature string `json:"creature,omitempty"`
	Vibe     string `json:"vibe,omitempty"`
	Emoji    string `json:"emoji,omitempty"`
}

// OnboardingMessage is a single message in the onboarding conversation.
type OnboardingMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// PinOnboardingData posts a human-friendly summary message with the full
// onboarding conversation as a JSON file attachment, then pins it.
func (c *Client) PinOnboardingData(channelID string, data OnboardingData) error {
	fullJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling onboarding data: %w", err)
	}

	// Human-friendly message content.
	summary := data.World.Summary
	if runes := []rune(summary); len(runes) > 200 {
		summary = string(runes[:200]) + "..."
	}
	content := fmt.Sprintf("%s\n\n**World**: %s\n**Mayor**: %s\n**Creator**: <@%s>\n\n> %s",
		onboardingMarker, data.World.Name, data.Mayor.Name, data.Creator.DiscordID, summary)

	msg := &discordgo.MessageSend{
		Content: content,
		Files: []*discordgo.File{{
			Name:        "onboarding.json",
			ContentType: "application/json",
			Reader:      bytes.NewReader(fullJSON),
		}},
	}

	sent, err := c.SendComplexMessage(channelID, msg)
	if err != nil {
		return fmt.Errorf("sending onboarding message: %w", err)
	}
	if err := c.session.ChannelMessagePin(channelID, sent.ID); err != nil {
		c.logger.Warn("failed to pin onboarding message", "message_id", sent.ID, "error", err)
	}
	return nil
}

// ReadOnboardingData reads pinned messages from a channel and reassembles
// the onboarding conversation. Checks for file attachments first (new format),
// then falls back to legacy code-block format. Returns nil if no data is found.
func (c *Client) ReadOnboardingData(channelID string) (*OnboardingData, error) {
	pins, err := c.session.ChannelMessagesPinned(channelID)
	if err != nil {
		return nil, fmt.Errorf("fetching pinned messages: %w", err)
	}

	// Try new format first: file attachment on message with marker.
	for _, pin := range pins {
		if !strings.HasPrefix(pin.Content, onboardingMarker) {
			continue
		}
		for _, att := range pin.Attachments {
			if att.Filename == "onboarding.json" {
				return c.downloadOnboardingJSON(att.URL)
			}
		}
	}

	// Fall back to legacy code-block format for existing channels.
	var mainMsg string
	var continuationMsgs []string
	for i := len(pins) - 1; i >= 0; i-- {
		content := pins[i].Content
		if j := extractJSON(content, onboardingMarker); j != "" {
			mainMsg = j
		} else if j := extractJSON(content, onboardingContinuation); j != "" {
			continuationMsgs = append(continuationMsgs, j)
		}
	}

	if mainMsg == "" {
		return nil, nil
	}

	var data OnboardingData
	if err := json.Unmarshal([]byte(mainMsg), &data); err != nil {
		return nil, fmt.Errorf("parsing onboarding data: %w", err)
	}
	for _, contJSON := range continuationMsgs {
		var msgs []OnboardingMessage
		if err := json.Unmarshal([]byte(contJSON), &msgs); err != nil {
			c.logger.Warn("skipping malformed onboarding continuation", "error", err)
			continue
		}
		data.Messages = append(data.Messages, msgs...)
	}
	return &data, nil
}

// downloadOnboardingJSON fetches and decodes a JSON file attachment.
func (c *Client) downloadOnboardingJSON(url string) (*OnboardingData, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("downloading onboarding attachment: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, 1<<20) // 1MB max
	var data OnboardingData
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, fmt.Errorf("decoding onboarding attachment: %w", err)
	}
	return &data, nil
}

// extractJSON extracts the JSON string from a formatted onboarding message.
// Used for legacy code-block format compatibility.
func extractJSON(content, marker string) string {
	if !strings.HasPrefix(content, marker) {
		return ""
	}
	rest := content[len(marker):]
	start := strings.Index(rest, "```json\n")
	if start == -1 {
		return ""
	}
	rest = rest[start+len("```json\n"):]
	end := strings.Index(rest, "\n```")
	if end == -1 {
		return ""
	}
	return rest[:end]
}

// splitConversation splits messages into chunks where each chunk's JSON
// fits within a Discord message. Retained for legacy format compatibility.
func splitConversation(messages []OnboardingMessage) [][]OnboardingMessage {
	if len(messages) == 0 {
		return nil
	}

	// Overhead: marker + code block fencing.
	overhead := len(onboardingContinuation) + len("\n```json\n") + len("\n```")
	maxPayload := discordMaxMessageLen - overhead

	var chunks [][]OnboardingMessage
	var current []OnboardingMessage

	for _, msg := range messages {
		candidate := append(current, msg)
		candidateJSON, _ := json.Marshal(candidate)
		if len(candidateJSON) > maxPayload && len(current) > 0 {
			chunks = append(chunks, current)
			current = []OnboardingMessage{msg}
		} else {
			current = candidate
		}
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}

	return chunks
}
