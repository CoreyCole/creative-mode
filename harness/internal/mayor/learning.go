package mayor

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const learningTimeout = 60 * time.Second

// ContributeLearning creates a GitHub PR with knowledge the mayor wants to
// share back to the template.
func (m *Manager) ContributeLearning(worldID, title, content, filePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), learningTimeout)
	defer cancel()

	branchName := fmt.Sprintf("mayor/%s/learning-%d", worldID, time.Now().Unix())

	// Create branch, commit change, push, create PR.
	commands := [][]string{
		{"git", "checkout", "-b", branchName},
		{"git", "add", filePath},
		{
			"git",
			"commit",
			"-m",
			fmt.Sprintf(
				"Mayor learning: %s\n\nContributed by mayor of world %s",
				title,
				worldID,
			),
		},
		{"git", "push", "-u", "origin", branchName},
		{
			"gh",
			"pr",
			"create",
			"--title",
			title,
			"--body",
			fmt.Sprintf(
				"Contributed by the mayor of world `%s`.\n\n%s",
				worldID,
				content,
			),
		},
		{"git", "checkout", "main"},
	}

	for _, args := range commands {
		cmd := exec.CommandContext(
			ctx,
			args[0],
			args[1:]...)
		if output, err := cmd.CombinedOutput(); err != nil {
			// Clean up: try to go back to main.
			_ = exec.CommandContext(ctx, "git", "checkout", "main").Run()
			return fmt.Errorf("running %v: %s: %w", args, string(output), err)
		}
	}

	m.logger.Info("created learning PR",
		"world_id", worldID,
		"branch", branchName,
		"title", title,
	)
	return nil
}
