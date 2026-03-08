package swarmorch

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// Size limits for JSON truncation and context gathering.
const (
	maxJSONLen         = 4096
	maxFileContentLen  = 4096
	maxFilesPerKeyword = 5
	maxKeywords        = 3
	maxSlugLen         = 40
)

// truncateJSON marshals v to JSON and truncates to maxJSONLen bytes.
func truncateJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	if len(b) > maxJSONLen {
		return string(b[:maxJSONLen]) + "..."
	}
	return string(b)
}

// marshal is a convenience wrapper for json.Marshal that returns
// a json.RawMessage or nil on error.
func marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// toNullString converts a Go string to a sql.NullString.
func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// filePathRe matches file paths like "internal/server/server.go".
var filePathRe = regexp.MustCompile(`[\w\-./]+\.\w+`)

// camelCaseRe matches CamelCase identifiers like "EventBus", "SwarmManager".
var camelCaseRe = regexp.MustCompile(`[A-Z][a-z]+[A-Z]\w+`)

// backtickRe matches backtick-quoted terms like `EventBus`.
var backtickRe = regexp.MustCompile("`([^`]+)`")

// extractKeywords pulls searchable terms from a question text:
// file paths, CamelCase identifiers, and backtick-quoted terms.
func extractKeywords(text string) []string {
	seen := make(map[string]bool)
	var keywords []string

	addKeyword := func(kw string) {
		kw = strings.TrimSpace(kw)
		if kw == "" || seen[kw] {
			return
		}
		seen[kw] = true
		keywords = append(keywords, kw)
	}

	for _, m := range filePathRe.FindAllString(text, -1) {
		addKeyword(m)
	}
	for _, m := range camelCaseRe.FindAllString(text, -1) {
		addKeyword(m)
	}
	for _, m := range backtickRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			addKeyword(m[1])
		}
	}

	if len(keywords) > maxKeywords {
		keywords = keywords[:maxKeywords]
	}
	return keywords
}

// truncate shortens s to maxLen runes, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// sanitizeSlug converts text to a kebab-case slug suitable for file names.
func sanitizeSlug(text string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		case !prevDash && b.Len() > 0:
			b.WriteByte('-')
			prevDash = true
		}
	}

	s := strings.TrimRight(b.String(), "-")
	if len(s) > maxSlugLen {
		s = s[:maxSlugLen]
		s = strings.TrimRight(s, "-")
	}
	return s
}
