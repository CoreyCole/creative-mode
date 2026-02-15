package mayor

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/google/uuid"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
)

// Manager handles mayor agent lifecycle: provisioning, workspace generation,
// OpenClaw CLI integration, and Discord communication.
type Manager struct {
	openclawHome string
	openclawBin  string // path to openclaw CLI binary
	harnessURL   string
	wcClient     *worldchannel.Client
	db           *db.DB
	logger       *slog.Logger
}

// NewManager creates a new mayor Manager.
func NewManager(
	openclawHome, openclawBin, harnessURL string,
	wcClient *worldchannel.Client,
	database *db.DB,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		openclawHome: openclawHome,
		openclawBin:  openclawBin,
		harnessURL:   harnessURL,
		wcClient:     wcClient,
		db:           database,
		logger:       logger,
	}
}

// ProvisionFromWebhook handles the full provisioning flow triggered by the
// world-hatched webhook. It reads onboarding data from Discord, creates a
// world in the harness DB, generates workspace files, and registers the agent.
func (m *Manager) ProvisionFromWebhook(
	discordChannelID, worldName, mayorName, creatorDiscordID, creatorUsername string,
) error {
	ctx := context.Background()

	// Read onboarding data from Discord pinned messages.
	onboarding, err := m.wcClient.ReadOnboardingData(discordChannelID)
	if err != nil {
		return fmt.Errorf("reading onboarding data: %w", err)
	}
	if onboarding == nil {
		m.logger.Warn("no onboarding data found, provisioning with defaults",
			"channel_id", discordChannelID)
	}

	// Look up or create the world.
	// First check if a world already exists for this channel.
	existingWorld, err := m.db.GetWorldByDiscordChannel(ctx, sql.NullString{
		String: discordChannelID,
		Valid:  true,
	})
	if err == nil {
		m.logger.Info("world already exists for channel, skipping provisioning",
			"world_id", existingWorld.ID,
			"channel_id", discordChannelID,
		)
		return nil
	}

	// Look up creator user.
	creatorID := ""
	users, _ := m.db.ListUsers(ctx)
	for _, u := range users {
		if u.DiscordID == creatorDiscordID {
			creatorID = u.ID
			break
		}
	}

	// Create the world.
	worldID := uuid.New().String()[:8]
	if err := m.db.CreateWorld(ctx, sqlc.CreateWorldParams{
		ID:           worldID,
		Name:         worldName,
		Description:  sql.NullString{String: "Created via Meet the Mayor", Valid: true},
		CreatedBy:    sql.NullString{String: creatorID, Valid: creatorID != ""},
		TemplateType: "2d", // Default to 2D for mayor-managed worlds
	}); err != nil {
		return fmt.Errorf("creating world: %w", err)
	}

	// Generate mayor secret.
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("generating mayor secret: %w", err)
	}
	mayorSecret := hex.EncodeToString(secretBytes)

	// Provision the OpenClaw agent.
	agentID := fmt.Sprintf("world-%s", worldID)
	if err := m.provisionAgent(agentID, worldID, worldName, mayorName, mayorSecret, onboarding); err != nil {
		return fmt.Errorf("provisioning agent: %w", err)
	}

	// Update the world record with mayor info.
	if err := m.db.UpdateWorldMayor(ctx, sqlc.UpdateWorldMayorParams{
		MayorName:        sql.NullString{String: mayorName, Valid: true},
		MayorSecret:      sql.NullString{String: mayorSecret, Valid: true},
		DiscordChannelID: sql.NullString{String: discordChannelID, Valid: true},
		OpenClawAgentID:  sql.NullString{String: agentID, Valid: true},
		ID:               worldID,
	}); err != nil {
		return fmt.Errorf("updating world mayor: %w", err)
	}

	// Log the activity.
	if err := m.db.CreateMayorActivity(ctx, sqlc.CreateMayorActivityParams{
		ID:           uuid.New().String()[:8],
		WorldID:      worldID,
		ActivityType: "agent_provisioned",
		Detail: sql.NullString{
			String: fmt.Sprintf("Mayor %q provisioned for world %q", mayorName, worldName),
			Valid:  true,
		},
	}); err != nil {
		m.logger.Warn("failed to log provisioning activity", "error", err)
	}

	m.logger.Info("mayor agent provisioned",
		"world_id", worldID,
		"agent_id", agentID,
		"mayor_name", mayorName,
		"channel_id", discordChannelID,
	)

	return nil
}

// PostToDiscord sends a message to a Discord channel via the worldchannel client.
func (m *Manager) PostToDiscord(channelID, content string) {
	if m.wcClient == nil {
		return
	}
	if _, err := m.wcClient.SendMessage(channelID, content); err != nil {
		m.logger.Error("failed to post to discord",
			"channel_id", channelID,
			"error", err,
		)
	}
}

// IsGatewayHealthy checks if the OpenClaw gateway is responsive.
func (m *Manager) IsGatewayHealthy() bool {
	return checkGatewayHealth()
}
