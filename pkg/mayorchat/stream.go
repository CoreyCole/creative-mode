package mayorchat

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// WorldReadyInfo holds parsed WORLD_READY marker data.
type WorldReadyInfo struct {
	MayorName    string
	WorldName    string
	WorldSummary string
	TemplateType string // empty for site markers (no template detection)
	Creature     string // optional: "rogue AI", "tree spirit", etc.
	Vibe         string // optional: "snarky", "warm", "chaotic"
	Emoji        string // optional: single emoji like "🔌"
}

// ParseWorldReady extracts WORLD_READY info from content.
// Handles multiple marker formats:
//   - 3-field: mayor|world|summary (old site)
//   - 4-field: mayor|world|template|summary (old harness)
//   - 6-field: mayor|world|creature|vibe|emoji|summary (new site)
//   - 7-field: mayor|world|template|creature|vibe|emoji|summary (new harness)
//
// Returns nil if no marker found.
func ParseWorldReady(content string) *WorldReadyInfo {
	idx := strings.Index(content, "WORLD_READY|")
	if idx == -1 {
		return nil
	}

	raw := content[idx+len("WORLD_READY|"):]
	// Strip any trailing whitespace/newlines.
	raw = strings.TrimSpace(raw)

	parts := strings.SplitN(raw, "|", 8)
	info := &WorldReadyInfo{}

	switch len(parts) {
	case 7:
		// 7-field: mayor|world|template|creature|vibe|emoji|summary
		info.MayorName = strings.TrimSpace(parts[0])
		info.WorldName = strings.TrimSpace(parts[1])
		info.TemplateType = strings.TrimSpace(parts[2])
		info.Creature = strings.TrimSpace(parts[3])
		info.Vibe = strings.TrimSpace(parts[4])
		info.Emoji = strings.TrimSpace(parts[5])
		info.WorldSummary = strings.TrimSpace(parts[6])
	case 6:
		// 6-field: mayor|world|creature|vibe|emoji|summary
		info.MayorName = strings.TrimSpace(parts[0])
		info.WorldName = strings.TrimSpace(parts[1])
		info.Creature = strings.TrimSpace(parts[2])
		info.Vibe = strings.TrimSpace(parts[3])
		info.Emoji = strings.TrimSpace(parts[4])
		info.WorldSummary = strings.TrimSpace(parts[5])
	case 4:
		// 4-field: mayor|world|template|summary
		info.MayorName = strings.TrimSpace(parts[0])
		info.WorldName = strings.TrimSpace(parts[1])
		info.TemplateType = strings.TrimSpace(parts[2])
		info.WorldSummary = strings.TrimSpace(parts[3])
	case 3:
		// 3-field: mayor|world|summary
		info.MayorName = strings.TrimSpace(parts[0])
		info.WorldName = strings.TrimSpace(parts[1])
		info.WorldSummary = strings.TrimSpace(parts[2])
	case 2:
		info.MayorName = strings.TrimSpace(parts[0])
		info.WorldName = strings.TrimSpace(parts[1])
	default:
		return nil
	}

	if info.MayorName == "" || info.WorldName == "" {
		return nil
	}
	return info
}

// StripWorldReadyMarker removes the WORLD_READY marker from content.
func StripWorldReadyMarker(content string) string {
	if idx := strings.Index(content, "WORLD_READY|"); idx != -1 {
		return strings.TrimSpace(content[:idx])
	}
	return content
}

// IsBillingOrOverloadError checks if an error is an Anthropic API error
// with a status code indicating billing issues (402), rate limiting (429),
// or overload (529).
func IsBillingOrOverloadError(err error) bool {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 402, 429, 529:
			return true
		}
	}
	return false
}

// BuildAnthropicMessages converts conversation messages to Anthropic API params.
// User messages with images are sent as multi-content blocks (image + text) for vision API.
func BuildAnthropicMessages(messages []Message) []anthropic.MessageParam {
	result := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "user" {
			if len(msg.Images) > 0 {
				var blocks []anthropic.ContentBlockParamUnion
				for _, img := range msg.Images {
					data, err := os.ReadFile(img.FilePath)
					if err != nil {
						continue
					}
					b64 := base64.StdEncoding.EncodeToString(data)
					blocks = append(blocks, anthropic.NewImageBlockBase64(img.MIMEType, b64))
				}
				if msg.Content != "" {
					blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
				}
				if len(blocks) > 0 {
					result = append(result, anthropic.NewUserMessage(blocks...))
				}
			} else {
				result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
			}
		} else {
			result = append(result, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}
	return result
}
