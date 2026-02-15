package president

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"creative-mode/harness/internal/db"
)

// Manager handles the president agent lifecycle.
type Manager struct {
	openclawHome    string
	openclawBin     string
	harnessURL      string
	presidentSecret string
	channelID       string // Discord channel ID for #creative-mode-dev
	db              *db.DB
	logger          *slog.Logger
}

// NewManager creates a new president Manager.
func NewManager(
	openclawHome, openclawBin, harnessURL, presidentSecret, channelID string,
	database *db.DB,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		openclawHome:    openclawHome,
		openclawBin:     openclawBin,
		harnessURL:      harnessURL,
		presidentSecret: presidentSecret,
		channelID:       channelID,
		db:              database,
		logger:          logger,
	}
}

const presidentAgentID = "president"

// IsProvisioned checks if the president agent workspace exists.
func (m *Manager) IsProvisioned() bool {
	soulPath := filepath.Join(m.openclawHome, "workspaces", presidentAgentID, "SOUL.md")
	_, err := os.Stat(soulPath)
	return err == nil
}

// Provision creates the president agent in OpenClaw with all workspace files.
func (m *Manager) Provision() error {
	if m.IsProvisioned() {
		m.logger.Info("president agent already provisioned")
		return nil
	}

	workspaceDir := filepath.Join(m.openclawHome, "workspaces", presidentAgentID)
	if err := os.MkdirAll(workspaceDir, 0o750); err != nil {
		return fmt.Errorf("creating workspace dir: %w", err)
	}

	// Write workspace files.
	files := map[string]string{
		"SOUL.md":      generatePresidentSOUL(),
		"AGENTS.md":    generatePresidentAGENTS(),
		"IDENTITY.md":  generatePresidentIDENTITY(),
		"USER.md":      generatePresidentUSER(),
		"HEARTBEAT.md": generatePresidentHEARTBEAT(),
		"MEMORY.md":    "# Memory\n\nNo observations yet.\n",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workspaceDir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}

	// Write skills.
	if err := writePresidentSkills(workspaceDir, m.presidentSecret, m.harnessURL); err != nil {
		return fmt.Errorf("writing skills: %w", err)
	}

	// Register agent via CLI.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.openclawBin,
		"agents", "add",
		"--id", presidentAgentID,
		"--workspace", workspaceDir,
	)
	cmd.Env = append(cmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("openclaw agents add: %s: %w", string(output), err)
	}

	// Bind to Discord channel.
	if m.channelID != "" {
		if err := m.bindToChannel(); err != nil {
			m.logger.Warn("failed to bind president to channel", "error", err)
		}
	}

	m.logger.Info("president agent provisioned",
		"workspace", workspaceDir,
		"channel_id", m.channelID,
	)
	return nil
}

// bindToChannel binds the president agent to its Discord channel.
func (m *Manager) bindToChannel() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Read existing bindings.
	getCmd := exec.CommandContext(ctx, m.openclawBin, "config", "get", "bindings")
	getCmd.Env = append(getCmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)
	existingJSON, err := getCmd.Output()
	if err != nil {
		existingJSON = []byte("[]")
	}

	var bindings []map[string]any
	if err := json.Unmarshal(existingJSON, &bindings); err != nil {
		bindings = []map[string]any{}
	}

	bindings = append(bindings, map[string]any{
		"agent":   presidentAgentID,
		"channel": fmt.Sprintf("discord:%s", m.channelID),
	})

	bindingsJSON, err := json.Marshal(bindings)
	if err != nil {
		return err
	}

	setCmd := exec.CommandContext(ctx, m.openclawBin, "config", "set", "bindings", string(bindingsJSON))
	setCmd.Env = append(setCmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)
	if output, err := setCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("openclaw config set bindings: %s: %w", string(output), err)
	}

	return nil
}
