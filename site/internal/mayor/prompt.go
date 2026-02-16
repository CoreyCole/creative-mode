package mayor

import "github.com/coreycole/creative-mode/pkg/mayorchat"

// BuildSystemPrompt constructs the system prompt. Site uses 3-field markers (no template type detection).
func BuildSystemPrompt(username string, takenNames []string) string {
	return mayorchat.BuildSystemPrompt(username, takenNames, false)
}
