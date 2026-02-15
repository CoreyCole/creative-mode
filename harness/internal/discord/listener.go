package discord

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
)

// Listener connects to the Discord gateway WebSocket and mirrors messages
// from world channels to the local database + EventBus.
type Listener struct {
	session    *discordgo.Session
	botUserID  string
	db         *db.DB
	eventBus   *events.EventBus
	logger     *slog.Logger
	channelMap map[string]string // discord channel ID → world ID
	mu         sync.RWMutex
}

// NewListener creates a new Discord gateway listener.
// This is a SEPARATE session from the worldchannel REST client.
func NewListener(
	botToken string,
	database *db.DB,
	eventBus *events.EventBus,
	logger *slog.Logger,
) (*Listener, error) {
	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, err
	}

	// Only subscribe to message events.
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	user, err := session.User("@me")
	if err != nil {
		return nil, err
	}

	l := &Listener{
		session:    session,
		botUserID:  user.ID,
		db:         database,
		eventBus:   eventBus,
		logger:     logger,
		channelMap: make(map[string]string),
	}

	session.AddHandler(l.onMessageCreate)
	return l, nil
}

// Start opens the gateway WebSocket and begins receiving events.
func (l *Listener) Start() error {
	// Load channel map from DB.
	if err := l.refreshChannelMap(); err != nil {
		l.logger.Warn("failed to load channel map on startup", "error", err)
	}

	return l.session.Open()
}

// Stop closes the gateway connection.
func (l *Listener) Stop() error {
	return l.session.Close()
}

// RegisterChannel adds a channel→world mapping for live message mirroring.
func (l *Listener) RegisterChannel(channelID, worldID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.channelMap[channelID] = worldID
}

// refreshChannelMap reloads the channel→world map from the database.
func (l *Listener) refreshChannelMap() error {
	ctx := context.Background()
	worlds, err := l.db.GetWorldsWithDiscordChannels(ctx)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.channelMap = make(map[string]string, len(worlds))
	for _, w := range worlds {
		if w.DiscordChannelID.Valid {
			l.channelMap[w.DiscordChannelID.String] = w.ID
		}
	}

	l.logger.Info("loaded discord channel map", "count", len(l.channelMap))
	return nil
}

// onMessageCreate handles incoming Discord messages.
func (l *Listener) onMessageCreate(_ *discordgo.Session, m *discordgo.MessageCreate) {
	// Look up world for this channel.
	l.mu.RLock()
	worldID, ok := l.channelMap[m.ChannelID]
	l.mu.RUnlock()

	if !ok {
		return // Not a world channel we care about.
	}

	// Classify message.
	authorType := classifyMessage(m, l.botUserID)
	authorName := m.Author.Username

	// Insert into DB.
	ctx := context.Background()
	msgID := uuid.New().String()[:8]
	if err := l.db.CreateMayorMessage(ctx, sqlc.CreateMayorMessageParams{
		ID:               msgID,
		WorldID:          worldID,
		DiscordMessageID: sql.NullString{String: m.ID, Valid: true},
		AuthorType:       authorType,
		AuthorName:       authorName,
		Content:          m.Content,
	}); err != nil {
		l.logger.Error("failed to insert mayor message",
			"world_id", worldID,
			"discord_msg_id", m.ID,
			"error", err,
		)
		return
	}

	// Publish to EventBus for live SSE updates.
	if l.eventBus != nil {
		l.eventBus.Publish(worldID, map[string]any{
			"event":       events.EventMayorMessage,
			"worldID":     worldID,
			"author_type": authorType,
			"author_name": authorName,
			"content":     m.Content,
			"message_id":  msgID,
		})
	}
}

// classifyMessage determines the author type based on the message source.
func classifyMessage(m *discordgo.MessageCreate, botUserID string) string {
	if m.Author.ID != botUserID {
		return "user"
	}
	// Bot messages: check for system prefixes.
	if strings.HasPrefix(m.Content, "[BUILD") || strings.HasPrefix(m.Content, "[SYSTEM") {
		return "system"
	}
	return "mayor"
}
