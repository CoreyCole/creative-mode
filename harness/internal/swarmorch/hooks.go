package swarmorch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"creative-mode/harness/internal/swarm"
)

const (
	hookTimeoutDefault   = 10  // seconds for most hooks
	hookTimeoutStop      = 30  // seconds for Stop hook (needs to read result file)
	pressureThreshold    = 2   // compact events before writing sentinel
	bashCmdTruncateLimit = 120 // max chars for Bash command in filtered tool args
)

// swarmDenyPatterns are commands that swarm sessions should never run.
// These match against the Bash tool_input.command field.
var swarmDenyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`cargo\s+(build|clippy|check)`),
	regexp.MustCompile(`go\s+build`),
	regexp.MustCompile(`templ\s+generate`),
	regexp.MustCompile(`just\s+generate`),
}

// MatchesDenyPattern returns true if the command matches any deny pattern.
func MatchesDenyPattern(command string) bool {
	for _, pat := range swarmDenyPatterns {
		if pat.MatchString(command) {
			return true
		}
	}

	return false
}

// hooksSettings is the settings.json structure with hooks configuration.
type hooksSettings struct {
	Hooks map[string][]matcherGroup `json:"hooks"`
}

type matcherGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookHandler `json:"hooks"`
}

type hookHandler struct {
	Type           string            `json:"type"`
	URL            string            `json:"url,omitempty"`
	Command        string            `json:"command,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	AllowedEnvVars []string          `json:"allowedEnvVars,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
}

// WriteHooksConfig generates a Claude Code settings.json with HTTP hooks
// pointing back to the harness, and returns the config directory path to use
// as CLAUDE_CONFIG_DIR.
func WriteHooksConfig(
	sessionID, ticketID, phase, harnessURL, hookSecret string,
) (string, error) {
	configDir := filepath.Join(os.TempDir(), "swarm-hooks-"+sessionID)
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return "", fmt.Errorf("create hooks config dir: %w", err)
	}

	baseURL := harnessURL + "/api/swarm/hook"

	authHeaders := map[string]string{
		"X-Hook-Secret":   "$" + swarm.EnvKey("HookSecret"),
		"X-Swarm-Session": sessionID,
		"X-Swarm-Ticket":  ticketID,
		"X-Swarm-Phase":   phase,
		"Content-Type":    "application/json",
	}
	allowedVars := []string{swarm.EnvKey("HookSecret")}

	settings := hooksSettings{
		Hooks: map[string][]matcherGroup{
			"SessionStart": {
				{
					Hooks: []hookHandler{
						{
							Type:           "http",
							URL:            baseURL + "/session-started",
							Headers:        authHeaders,
							AllowedEnvVars: allowedVars,
							Timeout:        hookTimeoutDefault,
						},
					},
				},
			},
			"PreToolUse": {
				{
					Matcher: "Bash",
					Hooks: []hookHandler{
						{
							Type:           "http",
							URL:            baseURL + "/pre-tool-use",
							Headers:        authHeaders,
							AllowedEnvVars: allowedVars,
							Timeout:        hookTimeoutDefault,
						},
					},
				},
			},
			"PostToolUse": {
				{
					Hooks: []hookHandler{
						{
							Type:           "http",
							URL:            baseURL + "/post-tool-use",
							Headers:        authHeaders,
							AllowedEnvVars: allowedVars,
							Timeout:        hookTimeoutDefault,
						},
					},
				},
			},
			"PreCompact": {
				{
					Hooks: []hookHandler{
						{
							Type:           "http",
							URL:            baseURL + "/pre-compact",
							Headers:        authHeaders,
							AllowedEnvVars: allowedVars,
							Timeout:        hookTimeoutDefault,
						},
					},
				},
			},
			"Stop": {
				{
					Hooks: []hookHandler{
						{
							Type:           "http",
							URL:            baseURL + "/session-complete",
							Headers:        authHeaders,
							AllowedEnvVars: allowedVars,
							Timeout:        hookTimeoutStop,
						},
					},
				},
			},
			"SessionEnd": {
				{
					Hooks: []hookHandler{
						{
							Type:           "http",
							URL:            baseURL + "/session-ended",
							Headers:        authHeaders,
							AllowedEnvVars: allowedVars,
							Timeout:        hookTimeoutDefault,
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal hooks settings: %w", err)
	}

	settingsPath := filepath.Join(configDir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write hooks settings: %w", err)
	}

	return configDir, nil
}

// FilterToolArgs returns a filtered copy of tool input, keeping only relevant
// keys per tool type. Large payloads (content, old_string, new_string) are dropped.
func FilterToolArgs(toolName string, input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	filtered := make(map[string]any)

	switch toolName {
	case "Bash":
		copyIfPresent(filtered, input, "description")
		if cmd, ok := input["command"].(string); ok {
			if len(cmd) > bashCmdTruncateLimit {
				cmd = cmd[:bashCmdTruncateLimit] + "..."
			}
			filtered["command"] = cmd
		}
	case "Read":
		copyIfPresent(filtered, input, "file_path", "offset", "limit")
	case "Write", "Edit":
		copyIfPresent(filtered, input, "file_path")
	case "Grep", "Glob":
		copyIfPresent(filtered, input, "pattern", "path", "glob", "type")
	case "Agent":
		copyIfPresent(filtered, input, "description", "subagent_type")
	default:
		copyIfPresent(filtered, input, "file_path", "pattern", "path")
	}

	return filtered
}

// copyIfPresent copies keys from src to dst if they exist.
func copyIfPresent(dst, src map[string]any, keys ...string) {
	for _, k := range keys {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
}

// CleanupHooksConfig removes the temporary hooks config directory for a session.
func CleanupHooksConfig(sessionID string) {
	configDir := filepath.Join(os.TempDir(), "swarm-hooks-"+sessionID)
	_ = os.RemoveAll(configDir)
}

// ContextPressure tracks compaction events per session. On the second compact,
// it writes a sentinel file that skills can check to know they're running low
// on context.
type ContextPressure struct {
	mu     sync.RWMutex
	counts map[string]int
}

// NewContextPressure creates a new ContextPressure tracker.
func NewContextPressure() *ContextPressure {
	return &ContextPressure{
		counts: make(map[string]int),
	}
}

// Increment records a compaction event and returns the new count.
// On the second compact, writes a sentinel file at /tmp/swarm-context-pressure-{sessionID}.
func (cp *ContextPressure) Increment(sessionID string) int {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cp.counts[sessionID]++
	count := cp.counts[sessionID]

	if count >= pressureThreshold {
		sentinelPath := filepath.Join(os.TempDir(), "swarm-context-pressure-"+sessionID)
		_ = os.WriteFile(sentinelPath, []byte("1"), 0o600)
	}

	return count
}

// Get returns the current compact count for a session.
func (cp *ContextPressure) Get(sessionID string) int {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	return cp.counts[sessionID]
}

// Remove cleans up tracking and the sentinel file for a session.
func (cp *ContextPressure) Remove(sessionID string) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	delete(cp.counts, sessionID)

	sentinelPath := filepath.Join(os.TempDir(), "swarm-context-pressure-"+sessionID)
	_ = os.Remove(sentinelPath)
}
