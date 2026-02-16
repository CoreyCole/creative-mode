package worldchannel

import (
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// Config holds the Discord bot configuration for world channel management.
type Config struct {
	BotToken          string
	GuildID           string
	WorldsCategoryID  string
}

// Client wraps a discordgo.Session for world channel operations.
// It uses REST-only calls (no gateway WebSocket).
type Client struct {
	session  *discordgo.Session
	config   Config
	botUserID string
	logger   *slog.Logger
}

// NewClient creates a new Client using the provided config.
// It opens a REST-only session and resolves the bot's user ID.
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	session, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("creating discord session: %w", err)
	}

	// Resolve bot user ID via REST (no gateway needed).
	user, err := session.User("@me")
	if err != nil {
		return nil, fmt.Errorf("resolving bot user: %w", err)
	}

	return &Client{
		session:   session,
		config:    cfg,
		botUserID: user.ID,
		logger:    logger,
	}, nil
}

// Config returns the client's configuration.
func (c *Client) Config() Config {
	return c.config
}

// BotUserID returns the bot's Discord user ID.
func (c *Client) BotUserID() string {
	return c.botUserID
}

// SendMessage sends a text message to a Discord channel.
func (c *Client) SendMessage(channelID, content string) (string, error) {
	msg, err := c.session.ChannelMessageSend(channelID, content)
	if err != nil {
		return "", fmt.Errorf("sending message to channel %s: %w", channelID, err)
	}
	return msg.ID, nil
}

// SendComplexMessage sends a message with optional file attachments.
func (c *Client) SendComplexMessage(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
	m, err := c.session.ChannelMessageSendComplex(channelID, msg)
	if err != nil {
		return nil, fmt.Errorf("sending complex message to channel %s: %w", channelID, err)
	}
	return m, nil
}

// GuildMemberCount returns the approximate member count for the configured guild.
func (c *Client) GuildMemberCount() (int, error) {
	guild, err := c.session.GuildWithCounts(c.config.GuildID)
	if err != nil {
		return 0, fmt.Errorf("fetching guild with counts: %w", err)
	}
	return guild.ApproximateMemberCount, nil
}

// Close cleans up the Discord session.
func (c *Client) Close() error {
	return c.session.Close()
}
