package swarmorch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadProjectContext reads deterministic project documentation and assembles
// a context preamble that is injected into every agent's system prompt.
// This ensures agents have orientation docs without relying on keyword search.
func loadProjectContext(repoRoot string) string {
	var sections []string

	// 1. Root CLAUDE.md — the most valuable orientation document.
	claudeMD, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	if err == nil && len(claudeMD) > 0 {
		sections = append(sections,
			"# Project Documentation (CLAUDE.md)\n\n"+string(claudeMD))
	}

	// 2. Skills manifest — frontmatter only (name + description + tags).
	skillsDir := filepath.Join(repoRoot, "harness", "agents", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err == nil {
		var skillSummaries []string
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
				continue
			}
			fm := extractFrontmatter(filepath.Join(skillsDir, e.Name()))
			if fm != "" {
				skillSummaries = append(skillSummaries, fm)
			}
		}
		if len(skillSummaries) > 0 {
			sections = append(sections,
				"# Available Skills\n\nThe following skill documents are available "+
					"in `harness/agents/skills/`. If you have file tools, use `read` to load full content.\n\n"+
					strings.Join(skillSummaries, "\n"))
		}
	}

	return strings.Join(sections, "\n\n---\n\n")
}

// extractFrontmatter reads the YAML frontmatter block from a skill markdown
// file and returns it as a formatted string. Returns empty if no frontmatter.
func extractFrontmatter(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}

	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return ""
	}

	fm := content[4 : 4+end]
	filename := filepath.Base(path)
	return fmt.Sprintf("- **%s**:\n  ```yaml\n  %s\n  ```",
		filename, strings.ReplaceAll(fm, "\n", "\n  "))
}
