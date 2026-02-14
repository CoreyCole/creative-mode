package worldchannel

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
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

// OnboardingMayor holds the mayor's name.
type OnboardingMayor struct {
	Name string `json:"name"`
}

// OnboardingMessage is a single message in the onboarding conversation.
type OnboardingMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// PinOnboardingData posts the onboarding conversation as a JSON message
// and pins it in the channel. If the JSON exceeds Discord's 2000-char limit,
// it splits across multiple messages (metadata first, then conversation chunks).
func (c *Client) PinOnboardingData(channelID string, data OnboardingData) error {
	fullJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling onboarding data: %w", err)
	}

	// Try fitting everything in one message.
	singleMsg := formatOnboardingMessage(onboardingMarker, string(fullJSON))
	if len(singleMsg) <= discordMaxMessageLen {
		return c.sendAndPin(channelID, singleMsg)
	}

	// Split: first message has metadata (no conversation), second has conversation.
	metaData := OnboardingData{
		Version:  data.Version,
		Creator:  data.Creator,
		World:    data.World,
		Mayor:    data.Mayor,
		Messages: nil,
	}
	metaJSON, err := json.Marshal(metaData)
	if err != nil {
		return fmt.Errorf("marshaling onboarding metadata: %w", err)
	}
	metaMsg := formatOnboardingMessage(onboardingMarker, string(metaJSON))
	if err := c.sendAndPin(channelID, metaMsg); err != nil {
		return err
	}

	// Split conversation into chunks that fit in Discord messages.
	convChunks := splitConversation(data.Messages)
	for _, chunk := range convChunks {
		chunkJSON, err := json.Marshal(chunk)
		if err != nil {
			return fmt.Errorf("marshaling conversation chunk: %w", err)
		}
		chunkMsg := formatOnboardingMessage(onboardingContinuation, string(chunkJSON))
		if err := c.sendAndPin(channelID, chunkMsg); err != nil {
			return err
		}
	}

	return nil
}

// ReadOnboardingData reads pinned messages from a channel and reassembles
// the onboarding conversation. Returns nil if no onboarding data is found.
func (c *Client) ReadOnboardingData(channelID string) (*OnboardingData, error) {
	pins, err := c.session.ChannelMessagesPinned(channelID)
	if err != nil {
		return nil, fmt.Errorf("fetching pinned messages: %w", err)
	}

	// Find onboarding messages (oldest first — pins are returned newest first).
	var mainMsg string
	var continuationMsgs []string
	for i := len(pins) - 1; i >= 0; i-- {
		content := pins[i].Content
		if json := extractJSON(content, onboardingMarker); json != "" {
			mainMsg = json
		} else if json := extractJSON(content, onboardingContinuation); json != "" {
			continuationMsgs = append(continuationMsgs, json)
		}
	}

	if mainMsg == "" {
		return nil, nil
	}

	var data OnboardingData
	if err := json.Unmarshal([]byte(mainMsg), &data); err != nil {
		return nil, fmt.Errorf("parsing onboarding data: %w", err)
	}

	// Reassemble conversation from continuation messages.
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

// sendAndPin sends a message and pins it.
func (c *Client) sendAndPin(channelID, content string) error {
	msg, err := c.session.ChannelMessageSend(channelID, content)
	if err != nil {
		return fmt.Errorf("sending onboarding message: %w", err)
	}
	if err := c.session.ChannelMessagePin(channelID, msg.ID); err != nil {
		c.logger.Warn("failed to pin onboarding message", "message_id", msg.ID, "error", err)
	}
	return nil
}

// formatOnboardingMessage wraps JSON in a Discord code block with a marker.
func formatOnboardingMessage(marker, jsonStr string) string {
	return marker + "\n```json\n" + jsonStr + "\n```"
}

// extractJSON extracts the JSON string from a formatted onboarding message.
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
// fits within a Discord message.
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
