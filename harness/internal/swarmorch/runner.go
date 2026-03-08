package swarmorch

import (
	"context"
	"os/exec"
	"path/filepath"
)

// AgentRunner builds exec.Cmd instances for spawning JS agent scripts.
type AgentRunner interface {
	BuildCommand(ctx context.Context, script string, env []string) *exec.Cmd
}

// DirectRunner spawns Node.js agent scripts via exec.CommandContext.
type DirectRunner struct {
	NodePath  string
	AgentsDir string
}

// BuildCommand creates an exec.Cmd that runs the given JS script with Node.
func (r *DirectRunner) BuildCommand(
	ctx context.Context,
	script string,
	env []string,
) *exec.Cmd {
	scriptPath := filepath.Join(r.AgentsDir, script)
	cmd := exec.CommandContext(
		ctx,
		r.NodePath,
		scriptPath,
	)
	cmd.Env = env
	cmd.Dir = r.AgentsDir
	return cmd
}
