package mayor

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// GatewayStatus returns the OpenClaw gateway health status.
func (m *Manager) GatewayStatus() map[string]any {
	healthy := m.IsGatewayHealthy()
	return map[string]any{
		"healthy":   healthy,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
}

// AgentStatus returns the OpenClaw agent status for a given agent ID.
func (m *Manager) AgentStatus(agentID string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.openclawBin, "agents", "get", agentID)
	cmd.Env = append(cmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return map[string]any{"raw": string(output)}, nil
	}

	return result, nil
}

// ListAgents returns all registered OpenClaw agents.
func (m *Manager) ListAgents() ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.openclawBin, "agents", "list")
	cmd.Env = append(cmd.Environ(), "OPENCLAW_HOME="+m.openclawHome)

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}

	return result, nil
}
