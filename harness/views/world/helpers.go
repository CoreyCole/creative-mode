package world

import (
	"fmt"
	"time"
)

const hoursPerDay = 24

// TruncateStr truncates s to maxLen characters, appending "..." if needed.
func TruncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen-3] + "..."
}

// TimeAgo returns a human-readable time-ago string.
func TimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < hoursPerDay*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf(
			"%dd ago",
			int(d.Hours()/hoursPerDay),
		)
	}
}
