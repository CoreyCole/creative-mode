package president

import (
	"fmt"
	"os"
	"path/filepath"
)

// writePresidentSkills creates the skills directory and files for the president.
func writePresidentSkills(workspaceDir, presidentSecret, harnessURL string) error {
	skills := map[string]string{
		"mayor-status": fmt.Sprintf(`---
name: mayor-status
description: Check status of all mayor agents and their worlds
metadata: |
  {"skillKey": "mayor-status"}
---
# Mayor Status

Returns status of all worlds with mayors, including recent activity, builds, and sessions.

## Usage

%s
`, "```bash\ncurl -s \""+harnessURL+"/api/president/mayor-status\" \\\n  -H \"X-President-Secret: "+presidentSecret+"\"\n```"),

		"repo-build": fmt.Sprintf(`---
name: repo-build
description: Run the repo build check (just check)
metadata: |
  {"skillKey": "repo-build"}
---
# Repo Build

Spawns a tmux session that runs the full build check at the repo root.

## Usage

%s
`, "```bash\ncurl -s -X POST \""+harnessURL+"/api/president/repo-build\" \\\n  -H \"X-President-Secret: "+presidentSecret+"\"\n```"),

		"template-update": fmt.Sprintf(`---
name: template-update
description: Spawn a Claude Code session to update templates
metadata: |
  {"skillKey": "template-update"}
---
# Template Update

Spawns a Claude Code session at the repo root with the given prompt.

## Usage

%s
`, "```bash\ncurl -s -X POST \""+harnessURL+"/api/president/template-update\" \\\n  -H \"X-President-Secret: "+presidentSecret+"\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"prompt\": \"YOUR PROMPT HERE\"}'\n```"),

		"deploy": fmt.Sprintf(`---
name: deploy
description: Build and restart the harness service
metadata: |
  {"skillKey": "deploy"}
---
# Deploy

Runs the build pipeline and restarts the harness systemd service.

## Usage

%s
`, "```bash\ncurl -s -X POST \""+harnessURL+"/api/president/deploy\" \\\n  -H \"X-President-Secret: "+presidentSecret+"\"\n```"),
	}

	for name, content := range skills {
		dir := filepath.Join(workspaceDir, "skills", name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(
			filepath.Join(dir, "SKILL.md"),
			[]byte(content),
			0o600,
		); err != nil {
			return err
		}
	}

	return nil
}
