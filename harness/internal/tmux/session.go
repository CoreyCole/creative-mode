package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Session represents a tmux session for a Claude Code instance working on a
// specific checkpoint.
type Session struct {
	Name    string // cm-{worldID}-{cpID}
	WorkDir string // checkpoint directory
}

// NewSession creates a session handle. Call Create to actually start it.
func NewSession(worldID, cpID, workDir string) *Session {
	return &Session{
		Name:    fmt.Sprintf("cm-%s-%s", worldID, cpID),
		WorkDir: workDir,
	}
}

// Create starts a new tmux session with CM_* environment variables.
// Hook scripts in .claude/hooks/ use these env vars to tag their JSONL events
// and POST to the harness. Extra env vars (KEY=VALUE strings) are appended.
func (s *Session) Create(
	ctx context.Context,
	worldID, cpID, logsDir, harnessURL string,
	extraEnv ...string,
) error {
	logDir := filepath.Join(logsDir, "worlds", worldID, cpID)
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}

	args := []string{
		"new-session", "-d",
		"-s", s.Name, "-c", s.WorkDir,
		"-e", "CM_WORLD_ID=" + worldID,
		"-e", "CM_CHECKPOINT_ID=" + cpID,
		"-e", "CM_HARNESS_URL=" + harnessURL,
		"-e", "CM_LOG_DIR=" + logDir,
	}
	for _, env := range extraEnv {
		args = append(args, "-e", env)
	}

	return exec.CommandContext(
		ctx, "tmux", args...,
	).Run()
}

// SendPrompt writes the prompt to a file and launches Claude Code with
// --input-file. This avoids shell injection via tmux send-keys — the prompt
// never passes through shell interpolation.
func (s *Session) SendPrompt(ctx context.Context, prompt string) error {
	promptFile := filepath.Join(s.WorkDir, ".claude-prompt.txt")
	if err := os.WriteFile(promptFile, []byte(prompt), 0o600); err != nil {
		return fmt.Errorf("writing prompt file: %w", err)
	}

	// Use --input-file for safe prompt delivery.
	// --dangerously-skip-permissions is required for unattended operation.
	cmd := fmt.Sprintf(
		"claude --dangerously-skip-permissions --input-file %q",
		promptFile,
	)

	return exec.CommandContext(
		ctx, "tmux", "send-keys", "-t", s.Name, cmd, "Enter",
	).Run()
}

// Kill terminates the tmux session.
func (s *Session) Kill() error {
	cmd := exec.CommandContext(
		context.Background(), "tmux", "kill-session", "-t", s.Name,
	)

	return cmd.Run()
}

// IsAlive checks if the tmux session still exists.
func (s *Session) IsAlive() bool {
	cmd := exec.CommandContext(
		context.Background(), "tmux", "has-session", "-t", s.Name,
	)

	return cmd.Run() == nil
}
