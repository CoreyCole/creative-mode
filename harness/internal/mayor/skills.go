package mayor

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeSkills creates the skills directory and skill files for a mayor agent.
func writeSkills(workspaceDir, worldID, mayorSecret, harnessURL string) error {
	skillsDir := filepath.Join(workspaceDir, "skills")

	// world-build skill
	buildDir := filepath.Join(skillsDir, "world-build")
	if err := os.MkdirAll(buildDir, 0o750); err != nil {
		return err
	}
	buildSkill := fmt.Sprintf(`---
name: world-build
description: Trigger a build to modify the world
metadata: |
  {"skillKey": "world-build"}
---
# World Build

Triggers the build pipeline for your world. The build will fork the current
checkpoint, run Claude Code with your prompt, compile the result, and deploy.

## Usage

%s

Replace the prompt with a description of what to build or change.

## Notes

- Builds take 2-5 minutes depending on complexity
- Check build status with the world-status skill
- Only one build can run at a time per world
`, "```bash\ncurl -s -X POST \""+harnessURL+"/api/mayor/build\" \\\n  -H \"X-Mayor-Secret: "+mayorSecret+"\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"prompt\": \"YOUR BUILD PROMPT HERE\"}'\n```")

	if err := os.WriteFile(filepath.Join(buildDir, "SKILL.md"), []byte(buildSkill), 0o600); err != nil {
		return err
	}

	// world-status skill
	statusDir := filepath.Join(skillsDir, "world-status")
	if err := os.MkdirAll(statusDir, 0o750); err != nil {
		return err
	}
	statusSkill := fmt.Sprintf(`---
name: world-status
description: Check the current status of your world
metadata: |
  {"skillKey": "world-status"}
---
# World Status

Returns the current checkpoint, build status, and game server state for your world.

## Usage

%s

## Response

JSON with world_id, checkpoint_id, build_status, and game_server info.
`, "```bash\ncurl -s \""+harnessURL+"/api/mayor/status\" \\\n  -H \"X-Mayor-Secret: "+mayorSecret+"\"\n```")

	if err := os.WriteFile(filepath.Join(statusDir, "SKILL.md"), []byte(statusSkill), 0o600); err != nil {
		return err
	}

	return nil
}
