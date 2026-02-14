package worldchannel

import (
	"regexp"
	"strings"
)

var nonAlphanumericHyphen = regexp.MustCompile(`[^a-z0-9-]`)
var multipleHyphens = regexp.MustCompile(`-{2,}`)

// SanitizeChannelName converts a world name into a valid Discord channel name.
// Discord channel names must be lowercase, alphanumeric + hyphens, max 100 chars.
func SanitizeChannelName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = nonAlphanumericHyphen.ReplaceAllString(s, "")
	s = multipleHyphens.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 100 {
		s = s[:100]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "world"
	}
	return s
}
