package mayor

import (
	"fmt"
	"os"
	"path/filepath"
)

// buildCoverArtPrompt generates a Gemini prompt from the world name and summary.
func buildCoverArtPrompt(worldName, summary string) string {
	return fmt.Sprintf(
		"Cover art for a multiplayer game world called %q. %s. "+
			"Digital art style, vibrant colors, wide landscape composition, "+
			"game cover aesthetic. No text or logos.",
		worldName, summary,
	)
}

// savePendingCoverArt writes cover art to a temp file on disk.
// Returns the file path. Caller is responsible for cleanup.
func savePendingCoverArt(dataDir, discordID string, data []byte, mimeType string) (string, error) {
	dir := filepath.Join(dataDir, "cover-art-pending")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create cover-art-pending dir: %w", err)
	}

	// Remove any previous cover art for this user (may have different extension).
	matches, _ := filepath.Glob(filepath.Join(dir, discordID+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}

	ext := mimeToExt(mimeType)
	path := filepath.Join(dir, discordID+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write cover art: %w", err)
	}

	return path, nil
}

// mimeToExt returns a file extension for the given MIME type.
func mimeToExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
