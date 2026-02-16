package mayorchat

import (
	"fmt"
	"os"
	"path/filepath"
)

// BuildCoverArtPrompt generates a Gemini prompt from the world name and summary.
func BuildCoverArtPrompt(worldName, summary string) string {
	return fmt.Sprintf(
		"Cover art for a multiplayer game world called %q. %s. "+
			"Digital art style, vibrant colors, wide landscape composition, "+
			"game cover aesthetic. No text or logos.",
		worldName, summary,
	)
}

// SavePendingCoverArt writes cover art to a temp file on disk.
// Returns the file path. Caller is responsible for cleanup.
func SavePendingCoverArt(dataDir, userID string, data []byte, mimeType string) (string, error) {
	dir := filepath.Join(dataDir, "cover-art-pending")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create cover-art-pending dir: %w", err)
	}

	// Remove any previous cover art for this user (may have different extension).
	matches, _ := filepath.Glob(filepath.Join(dir, userID+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}

	ext := MimeToExt(mimeType)
	path := filepath.Join(dir, userID+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write cover art: %w", err)
	}

	return path, nil
}

// MimeToExt returns a file extension for the given MIME type.
func MimeToExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}
