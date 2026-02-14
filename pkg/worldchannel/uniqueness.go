package worldchannel

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// FormatChannelTopic returns a channel topic string in the canonical format.
// Max 1024 chars (Discord limit).
func FormatChannelTopic(mayorName, summary string) string {
	topic := "Mayor: " + mayorName + " | " + summary
	if len(topic) > 1024 {
		topic = topic[:1024]
	}
	return topic
}

// ParseMayorName extracts the mayor name from a channel topic.
// Returns empty string if the topic doesn't match the expected format.
func ParseMayorName(topic string) string {
	if !strings.HasPrefix(topic, "Mayor: ") {
		return ""
	}
	rest := topic[len("Mayor: "):]
	idx := strings.Index(rest, " | ")
	if idx == -1 {
		return rest
	}
	return rest[:idx]
}

// CheckMayorNameUnique checks if a mayor name is already taken in the guild.
// Returns *ErrMayorNameTaken if the name is in use, nil otherwise.
func (c *Client) CheckMayorNameUnique(name string) error {
	channels, err := c.session.GuildChannels(c.config.GuildID)
	if err != nil {
		return err
	}

	nameLower := strings.ToLower(strings.TrimSpace(name))

	for _, ch := range channels {
		if ch.ParentID != c.config.WorldsCategoryID {
			continue
		}
		if ch.Type != discordgo.ChannelTypeGuildText {
			continue
		}
		existing := ParseMayorName(ch.Topic)
		if strings.ToLower(existing) == nameLower {
			return &ErrMayorNameTaken{
				Name:                name,
				ExistingChannelName: ch.Name,
			}
		}
	}

	return nil
}

// ListExistingMayors returns all mayor names currently in use in the guild.
func (c *Client) ListExistingMayors() ([]string, error) {
	channels, err := c.session.GuildChannels(c.config.GuildID)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, ch := range channels {
		if ch.ParentID != c.config.WorldsCategoryID {
			continue
		}
		if ch.Type != discordgo.ChannelTypeGuildText {
			continue
		}
		if name := ParseMayorName(ch.Topic); name != "" {
			names = append(names, name)
		}
	}

	return names, nil
}
