package president

import (
	"fmt"
	"os"
	"path/filepath"
)

// writePresidentSkills creates the skills directory and files for the president.
func writePresidentSkills(
	workspaceDir, presidentSecret, hookSecret, harnessURL string,
) error {
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

		"swarm-learnings": swarmLearningsSkill(hookSecret, harnessURL),
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

func swarmLearningsSkill(hookSecret, harnessURL string) string {
	return `---
name: swarm-learnings
description: Query swarm learnings, health, metrics, and digests
metadata: |
  {"skillKey": "swarm-learnings"}
---
# Swarm Learnings

Query the swarm agent system for learnings, health status, aggregate metrics, and daily digests.
Use this to understand what the swarm has learned, identify recurring issues, and monitor system health.

## Endpoints

### Recent Learnings

Returns recent learnings, optionally filtered by ticket or phase.

` + "```bash\n# All recent learnings\ncurl -s \"" + harnessURL + "/api/swarm/learnings\" \\\n  -H \"X-Hook-Secret: " + hookSecret + "\"\n\n# Filter by ticket\ncurl -s \"" + harnessURL + "/api/swarm/learnings?ticket=ENG-1234\" \\\n  -H \"X-Hook-Secret: " + hookSecret + "\"\n\n# Filter by phase\ncurl -s \"" + harnessURL + "/api/swarm/learnings?phase=verify\" \\\n  -H \"X-Hook-Secret: " + hookSecret + "\"\n```" + `

### System Health

Returns health status (healthy/degraded/unhealthy), active sessions, capacity, stalled workflows, and recent completions.

` + "```bash\ncurl -s \"" + harnessURL + "/api/swarm/health\" \\\n  -H \"X-Hook-Secret: " + hookSecret + "\"\n```" + `

### Aggregate Metrics

Returns workflow completion rates, phase durations, retry rates, and learning counts. Period: 24h, 7d, 30d, or all.

` + "```bash\ncurl -s \"" + harnessURL + "/api/swarm/metrics?period=24h\" \\\n  -H \"X-Hook-Secret: " + hookSecret + "\"\n```" + `

### Latest Digest

Returns the most recent daily learning digest with pattern-detected action items.

` + "```bash\ncurl -s \"" + harnessURL + "/api/swarm/learnings/digest/latest\" \\\n  -H \"X-Hook-Secret: " + hookSecret + "\"\n```" + `
`
}
