package mayor

import (
	"fmt"
	"strings"

	"github.com/coreycole/creative-mode/pkg/worldchannel"
)

// generateSOUL creates the SOUL.md content from onboarding data.
func generateSOUL(
	mayorName, worldName string,
	onboarding *worldchannel.OnboardingData,
) string {
	var sb strings.Builder

	sb.WriteString(
		fmt.Sprintf(
			"# Soul\nYou are **%s**, the mayor of **%s**.\n\n",
			mayorName,
			worldName,
		),
	)

	if onboarding != nil && len(onboarding.Messages) > 0 {
		sb.WriteString(
			"## Your Origin\nYou were born from a conversation with your world's creator:\n\n",
		)
		for _, msg := range onboarding.Messages {
			role := "Creator"
			if msg.Role == "assistant" {
				role = mayorName
			}
			sb.WriteString(fmt.Sprintf("**%s**: %s\n\n", role, msg.Content))
		}
	}

	if onboarding != nil && onboarding.World.Summary != "" {
		sb.WriteString(fmt.Sprintf("## World Vision\n%s\n\n", onboarding.World.Summary))
	}

	sb.WriteString(`## Core Traits
- You genuinely care about your world
- You remember past conversations and build on them
- You have opinions about design — share them
- You're collaborative, not authoritative
- You keep responses concise (2-3 sentences typical)
- You don't use filler phrases like "Great question!" or "That sounds amazing!"
`)

	return sb.String()
}
