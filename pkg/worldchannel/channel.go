package worldchannel

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// CreateChannelParams holds the parameters for creating a world channel.
type CreateChannelParams struct {
	WorldName        string
	MayorName        string
	WorldSummary     string
	CreatorDiscordID string
	MayorBotID       string // Optional: separate mayor bot user ID
}

// CreateChannelResult holds the result of channel creation.
type CreateChannelResult struct {
	ChannelID   string
	ChannelName string
}

// CreateChannel creates a private text channel in the Worlds category with
// permission overwrites for the creator and bots.
func (c *Client) CreateChannel(params CreateChannelParams) (*CreateChannelResult, error) {
	channelName := SanitizeChannelName(params.WorldName)
	topic := FormatChannelTopic(params.MayorName, params.WorldSummary)

	// Permission bit constants.
	var viewChannel int64 = discordgo.PermissionViewChannel
	var sendMessages int64 = discordgo.PermissionSendMessages
	var readHistory int64 = discordgo.PermissionReadMessageHistory
	var manageMessages int64 = discordgo.PermissionManageMessages

	userPerms := viewChannel | sendMessages | readHistory
	botPerms := viewChannel | sendMessages | readHistory | manageMessages

	overwrites := []*discordgo.PermissionOverwrite{
		// Deny @everyone (role ID = guild ID).
		{
			ID:   c.config.GuildID,
			Type: discordgo.PermissionOverwriteTypeRole,
			Deny: viewChannel | sendMessages | readHistory,
		},
		// Allow creator.
		{
			ID:    params.CreatorDiscordID,
			Type:  discordgo.PermissionOverwriteTypeMember,
			Allow: userPerms,
		},
		// Allow harness bot.
		{
			ID:    c.botUserID,
			Type:  discordgo.PermissionOverwriteTypeMember,
			Allow: botPerms,
		},
	}

	// Allow mayor bot if separate from harness bot.
	if params.MayorBotID != "" && params.MayorBotID != c.botUserID {
		overwrites = append(overwrites, &discordgo.PermissionOverwrite{
			ID:    params.MayorBotID,
			Type:  discordgo.PermissionOverwriteTypeMember,
			Allow: userPerms,
		})
	}

	ch, err := c.session.GuildChannelCreateComplex(c.config.GuildID, discordgo.GuildChannelCreateData{
		Name:                 channelName,
		Type:                 discordgo.ChannelTypeGuildText,
		Topic:                topic,
		ParentID:             c.config.WorldsCategoryID,
		PermissionOverwrites: overwrites,
	})
	if err != nil {
		return nil, fmt.Errorf("creating channel %q: %w", channelName, err)
	}

	c.logger.Info("created world channel",
		"channel_id", ch.ID,
		"channel_name", channelName,
		"world_name", params.WorldName,
		"mayor_name", params.MayorName,
	)

	return &CreateChannelResult{
		ChannelID:   ch.ID,
		ChannelName: channelName,
	}, nil
}

// GrantAccess gives a user view/send/read permissions on a world channel.
func (c *Client) GrantAccess(channelID, userDiscordID string) error {
	var viewChannel int64 = discordgo.PermissionViewChannel
	var sendMessages int64 = discordgo.PermissionSendMessages
	var readHistory int64 = discordgo.PermissionReadMessageHistory

	return c.session.ChannelPermissionSet(channelID, userDiscordID,
		discordgo.PermissionOverwriteTypeMember,
		viewChannel|sendMessages|readHistory, 0)
}

// RevokeAccess removes a user's permission overwrite from a world channel.
func (c *Client) RevokeAccess(channelID, userDiscordID string) error {
	return c.session.ChannelPermissionDelete(channelID, userDiscordID)
}
