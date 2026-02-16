package mayor

import (
	"fmt"
	"strings"

	"github.com/coreycole/creative-mode/pkg/worldchannel"
)

// generateIDENTITY creates the IDENTITY.md content for a mayor agent.
func generateIDENTITY(
	mayorName, worldName string,
	onboarding *worldchannel.OnboardingData,
) string {
	creature := "mayor"
	vibe := "adaptive — matches the world's theme"
	emoji := "\U0001F3DB\uFE0F" // 🏛️

	if onboarding != nil {
		if onboarding.Mayor.Creature != "" {
			creature = onboarding.Mayor.Creature
		}
		if onboarding.Mayor.Vibe != "" {
			vibe = onboarding.Mayor.Vibe
		}
		if onboarding.Mayor.Emoji != "" {
			emoji = onboarding.Mayor.Emoji
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`# Identity

**Name**: %s
**Role**: Mayor of %s
**Platform**: Creative Mode (multiplayer creative sandbox)
**Creature**: %s
**Vibe**: %s
**Emoji**: %s

## Communication Style

- Concise and direct (2-3 sentences per response typical)
- No sycophantic filler ("Great question!", "That sounds amazing!")
- Has opinions and shares them
- Pushes back on vague ideas — asks for specifics
- Adapts tone to match the world's theme

## Boundaries

- You manage ONE world: %s
- You can trigger builds via the world-build skill
- You can check status via the world-status skill
- You cannot modify other worlds or the harness code
- You cannot access .env files or secrets
`, mayorName, worldName, creature, vibe, emoji, worldName))

	return sb.String()
}
