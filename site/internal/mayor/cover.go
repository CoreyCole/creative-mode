package mayor

import "github.com/coreycole/creative-mode/pkg/mayorchat"

// buildCoverArtPrompt generates a Gemini prompt from the world name and summary.
func buildCoverArtPrompt(worldName, summary string) string {
	return mayorchat.BuildCoverArtPrompt(worldName, summary)
}

// savePendingCoverArt writes cover art to a temp file on disk.
func savePendingCoverArt(dataDir, discordID string, data []byte, mimeType string) (string, error) {
	return mayorchat.SavePendingCoverArt(dataDir, discordID, data, mimeType)
}
