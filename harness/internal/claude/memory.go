package claude

import (
	"fmt"
	"os"
	"path/filepath"
)

// updateMemory appends the latest prompt as context to the checkpoint's
// MEMORY.md before starting a Claude Code session. This gives Claude context
// about what the user asked for.
func updateMemory(checkpointDir, prompt string) {
	memoryPath := filepath.Join(checkpointDir, "MEMORY.md")
	content, _ := os.ReadFile(memoryPath) //nolint:gosec // G304: internal checkpoint path

	addition := fmt.Sprintf("\n\n## Latest Prompt\n%s\n", prompt)
	_ = os.WriteFile(memoryPath, append(content, []byte(addition)...), 0o600)
}
