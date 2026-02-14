package worldchannel

import "fmt"

// WelcomeMessageParams holds parameters for the welcome message.
type WelcomeMessageParams struct {
	CreatorDiscordID string
	MayorName        string
	WorldName        string
}

// SendWelcomeMessage posts the intro message in a newly created world channel.
func (c *Client) SendWelcomeMessage(channelID string, params WelcomeMessageParams) error {
	msg := fmt.Sprintf(`# Welcome to %s

<@%s>, meet **%s** — the mayor of your world.

%s has been appointed to help you build, shape, and evolve **%s**.
Just type a message in this channel to start a conversation.

**Try saying:**
> Hey %s, I want to build a cozy tavern with a fireplace and a quest board.

The mayor will ask questions, make suggestions, and coordinate builds.
Everything discussed here becomes part of your world's living history.`,
		params.WorldName,
		params.CreatorDiscordID,
		params.MayorName,
		params.MayorName,
		params.WorldName,
		params.MayorName,
	)

	_, err := c.session.ChannelMessageSend(channelID, msg)
	if err != nil {
		c.logger.Error("failed to send welcome message",
			"channel_id", channelID,
			"error", err,
		)
		return fmt.Errorf("sending welcome message: %w", err)
	}

	return nil
}
