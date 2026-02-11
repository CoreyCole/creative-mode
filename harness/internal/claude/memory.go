package claude

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// updateMemory appends the latest prompt as context to the checkpoint's
// MEMORY.md before starting a Claude Code session. This gives Claude context
// about what the user asked for.
func updateMemory(checkpointDir, prompt string) {
	memoryPath := filepath.Join(checkpointDir, "MEMORY.md")
	//nolint:gosec // G304: internal checkpoint path
	content, err := os.ReadFile(memoryPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to read MEMORY.md",
			"path", memoryPath, "error", err)
	}

	addition := fmt.Sprintf("\n\n## Latest Prompt\n%s\n", prompt)
	content = append(content, []byte(addition)...)
	if writeErr := os.WriteFile(memoryPath, content, 0o600); writeErr != nil {
		slog.Error("failed to write MEMORY.md",
			"path", memoryPath, "error", writeErr)
	}
}
