package swarm

import (
	"regexp"
	"strings"
)

// ClassifyTicket determines workflow type from ticket metadata.
// Priority:
// 1. Explicit swarm_type in YAML footer (e.g., "swarm_type: research")
// 2. Keyword-based rules on title and description
// 3. Default: code
func ClassifyTicket(title, description string) WorkflowType {
	// 1. Check YAML footer for explicit swarm_type.
	if wfType := parseYAMLFooterType(description); wfType != "" {
		return wfType
	}

	// 2. Keyword-based classification.
	combined := strings.ToLower(title + " " + description)

	if matchesProjectKeywords(combined) {
		return WorkflowTypeProject
	}

	if matchesResearchKeywords(combined) {
		return WorkflowTypeResearch
	}

	// 3. Default to code.
	return WorkflowTypeCode
}

// yamlFooterRe matches swarm_type in a YAML footer block (--- delimited).
var yamlFooterRe = regexp.MustCompile(
	`(?m)^swarm_type:\s*(research|code|project)\s*$`,
)

// parseYAMLFooterType extracts swarm_type from a YAML footer in the
// description. The footer may be delimited by --- or appear at the end.
func parseYAMLFooterType(description string) WorkflowType {
	match := yamlFooterRe.FindStringSubmatch(description)
	if match == nil {
		return ""
	}

	wfType := WorkflowType(strings.TrimSpace(match[1]))
	if wfType.Valid() {
		return wfType
	}

	return ""
}

// researchKeywords triggers research classification.
var researchKeywords = []string{
	"research",
	"investigate",
	"explore",
	"analyze",
	"study",
	"evaluate",
	"assess",
	"survey",
	"spike",
}

// projectKeywords triggers project classification.
var projectKeywords = []string{
	"project:",
	"epic:",
	"initiative:",
	"multi-ticket",
	"decompose into",
	"break down into",
	"parent ticket",
}

func matchesResearchKeywords(text string) bool {
	for _, kw := range researchKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}

	return false
}

func matchesProjectKeywords(text string) bool {
	for _, kw := range projectKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}

	return false
}
