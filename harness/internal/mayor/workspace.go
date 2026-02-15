package mayor

import (
	"os"
	"path/filepath"

	"github.com/coreycole/creative-mode/pkg/worldchannel"
)

// writeWorkspaceFiles creates all the workspace files for a mayor agent.
func writeWorkspaceFiles(
	workspaceDir, worldID, worldName, mayorName, mayorSecret, harnessURL string,
	onboardingData any,
) error {
	if err := os.MkdirAll(workspaceDir, 0o750); err != nil {
		return err
	}

	// Type assert onboarding data.
	var onboarding *worldchannel.OnboardingData
	if od, ok := onboardingData.(*worldchannel.OnboardingData); ok {
		onboarding = od
	}

	creatorUsername := ""
	if onboarding != nil {
		creatorUsername = onboarding.Creator.Username
	}

	// Generate and write each file.
	files := map[string]string{
		"SOUL.md":     generateSOUL(mayorName, worldName, onboarding),
		"AGENTS.md":   generateAGENTS(mayorName, worldName),
		"IDENTITY.md": generateIDENTITY(mayorName, worldName),
		"USER.md":     generateUSER(creatorUsername),
		"MEMORY.md":   "# Memory\n\nNo observations yet.\n",
	}

	for name, content := range files {
		path := filepath.Join(workspaceDir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}

	// Write skills.
	return writeSkills(workspaceDir, worldID, mayorSecret, harnessURL)
}
