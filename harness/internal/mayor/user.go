package mayor

import "fmt"

// generateUSER creates the USER.md content for a mayor agent.
func generateUSER(creatorUsername string) string {
	if creatorUsername == "" {
		creatorUsername = "the world creator"
	}
	return fmt.Sprintf(`# User

The primary user is **%s**, the world's creator.

Other users may be invited to the world channel and can also interact with you.

## Expectations

- Users want to build things in their world
- They may be vague — help them refine ideas
- They appreciate seeing progress quickly
- They want to understand what was built and why
`, creatorUsername)
}
