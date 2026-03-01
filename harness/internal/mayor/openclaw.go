package mayor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	gatewayHealthTimeout = 2 * time.Second
	gatewayHealthURL     = "http://localhost:18789/health"
	openclawCLITimeout   = 30 * time.Second
)

// provisionAgent creates the OpenClaw agent, writes workspace files, and binds
// it to a Discord channel.
func (m *Manager) provisionAgent(
	agentID, worldID, worldName, mayorName, mayorSecret string,
	onboarding any, // *worldchannel.OnboardingData or nil
) error {
	workspaceDir := filepath.Join(m.openclawHome, "workspaces", agentID)

	// Write workspace files.
	if err := writeWorkspaceFiles(
		workspaceDir,
		worldID,
		worldName,
		mayorName,
		mayorSecret,
		m.harnessURL,
		onboarding,
	); err != nil {
		return fmt.Errorf("writing workspace files: %w", err)
	}

	// Create agent via CLI.
	if err := m.createAgentViaCLI(agentID, workspaceDir); err != nil {
		return fmt.Errorf("creating agent: %w", err)
	}

	return nil
}

// createAgentViaCLI registers a new agent with OpenClaw.
func (m *Manager) createAgentViaCLI(agentID, workspaceDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), openclawCLITimeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		m.openclawBin, "agents",
		"add",
		"--id",
		agentID,
		"--workspace",
		workspaceDir,
	)
	cmd.Env = append(cmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("openclaw agents add: %s: %w", string(output), err)
	}

	m.logger.Info("created openclaw agent", "agent_id", agentID)
	return nil
}

// BindAgentToDiscord binds an agent to a Discord channel by reading existing
// bindings, appending the new one, and writing back (config set does FULL REPLACE).
func (m *Manager) BindAgentToDiscord(agentID, channelID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), openclawCLITimeout)
	defer cancel()

	// Read existing bindings.
	getCmd := exec.CommandContext(
		ctx,
		m.openclawBin,
		"config",
		"get",
		"bindings",
	)
	getCmd.Env = append(getCmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)
	existingJSON, err := getCmd.Output()
	if err != nil {
		existingJSON = []byte("[]")
	}

	bindings := make([]map[string]any, 0, 1)
	if unmarshalErr := json.Unmarshal(existingJSON, &bindings); unmarshalErr != nil {
		bindings = []map[string]any{}
	}

	// Append new binding.
	bindings = append(bindings, map[string]any{
		"agent":   agentID,
		"channel": "discord:" + channelID,
	})

	bindingsJSON, err := json.Marshal(bindings)
	if err != nil {
		return fmt.Errorf("marshaling bindings: %w", err)
	}

	// Write back.
	setCmd := exec.CommandContext(ctx,
		m.openclawBin,
		"config",
		"set",
		"bindings",
		string(bindingsJSON),
	)
	setCmd.Env = append(setCmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)
	if output, setErr := setCmd.CombinedOutput(); setErr != nil {
		return fmt.Errorf("openclaw config set bindings: %s: %w", string(output), setErr)
	}

	m.logger.Info("bound agent to discord channel",
		"agent_id", agentID,
		"channel_id", channelID,
	)
	return nil
}

// DeleteAgent removes an agent from OpenClaw.
func (m *Manager) DeleteAgent(agentID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), openclawCLITimeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		m.openclawBin,
		"agents",
		"delete",
		"--id",
		agentID,
	)
	cmd.Env = append(cmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("openclaw agents delete: %s: %w", string(output), err)
	}

	return nil
}

// checkGatewayHealth pings the OpenClaw gateway health endpoint.
func checkGatewayHealth() bool {
	ctx, cancel := context.WithTimeout(context.Background(), gatewayHealthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		gatewayHealthURL,
		http.NoBody,
	)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	// 503 = Control UI assets not built (gateway is still functional).
	return resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusServiceUnavailable
}
