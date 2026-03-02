package swarmorch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteHooksConfig(t *testing.T) {
	t.Parallel()

	configDir, err := WriteHooksConfig(
		"sess-01",
		"TICK-1",
		"research",
		"http://localhost:8080",
		"secret123",
	)
	if err != nil {
		t.Fatalf("WriteHooksConfig: %v", err)
	}

	defer func() { _ = os.RemoveAll(configDir) }()

	// Verify settings.json exists and is valid JSON.
	settingsPath := filepath.Join(configDir, "settings.json")

	data, err := os.ReadFile(settingsPath) //nolint:gosec // test file
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("settings missing 'hooks' key")
	}

	// Verify all 6 hook events are present.
	expectedEvents := []string{
		"SessionStart", "PreToolUse", "PostToolUse",
		"PreCompact", "Stop", "SessionEnd",
	}
	for _, event := range expectedEvents {
		if _, ok := hooks[event]; !ok {
			t.Errorf("missing hook event %q", event)
		}
	}

	// Verify PreToolUse has Bash matcher.
	preToolUse, _ := hooks["PreToolUse"].([]any)
	if len(preToolUse) == 0 {
		t.Fatal("PreToolUse has no matcher groups")
	}

	group, _ := preToolUse[0].(map[string]any)
	if group["matcher"] != "Bash" {
		t.Errorf("PreToolUse matcher = %v; want Bash", group["matcher"])
	}

	// Verify X-Swarm-Phase is in the settings.
	if !strings.Contains(string(data), "X-Swarm-Phase") {
		t.Error("settings.json missing X-Swarm-Phase header")
	}
	if !strings.Contains(string(data), "research") {
		t.Error("settings.json missing phase value 'research'")
	}
}

func TestWriteHooksConfig_Cleanup(t *testing.T) {
	t.Parallel()

	configDir, err := WriteHooksConfig(
		"sess-cleanup",
		"T",
		"implement",
		"http://localhost:8080",
		"s",
	)
	if err != nil {
		t.Fatalf("WriteHooksConfig: %v", err)
	}

	// Verify dir exists.
	if _, statErr := os.Stat(configDir); os.IsNotExist(statErr) {
		t.Fatal("config dir does not exist after WriteHooksConfig")
	}

	CleanupHooksConfig("sess-cleanup")

	if _, statErr := os.Stat(configDir); !os.IsNotExist(statErr) {
		t.Error("config dir still exists after CleanupHooksConfig")
	}
}

func TestMatchesDenyPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cmd  string
		deny bool
	}{
		{"cargo build", true},
		{"cargo clippy --fix", true},
		{"cargo check", true},
		{"go build ./...", true},
		{"templ generate", true},
		{"just generate", true},
		{"cargo test", false},
		{"go test ./...", false},
		{"git status", false},
		{"just check", false},
		{"echo cargo build", true}, // substring match is intentional
	}

	for _, tt := range tests {
		if got := MatchesDenyPattern(tt.cmd); got != tt.deny {
			t.Errorf("MatchesDenyPattern(%q) = %v; want %v", tt.cmd, got, tt.deny)
		}
	}
}

func TestFilterToolArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		wantKeys []string
		dropKeys []string
	}{
		{
			name: "Bash keeps command and description",
			tool: "Bash",
			input: map[string]any{
				"command":     "git status",
				"description": "check status",
				"timeout":     120000,
			},
			wantKeys: []string{"command", "description"},
			dropKeys: []string{"timeout"},
		},
		{
			name: "Read keeps file_path, offset, limit",
			tool: "Read",
			input: map[string]any{
				"file_path": "/foo/bar.go",
				"offset":    10,
				"limit":     50,
			},
			wantKeys: []string{"file_path", "offset", "limit"},
		},
		{
			name: "Write keeps only file_path",
			tool: "Write",
			input: map[string]any{
				"file_path": "/foo/bar.go",
				"content":   "package main\n// lots of content",
			},
			wantKeys: []string{"file_path"},
			dropKeys: []string{"content"},
		},
		{
			name: "Edit keeps only file_path",
			tool: "Edit",
			input: map[string]any{
				"file_path":  "/foo/bar.go",
				"old_string": "old code",
				"new_string": "new code",
			},
			wantKeys: []string{"file_path"},
			dropKeys: []string{"old_string", "new_string"},
		},
		{
			name: "Grep keeps pattern, path, glob, type",
			tool: "Grep",
			input: map[string]any{
				"pattern": "func.*Test",
				"path":    "/src",
				"glob":    "*.go",
				"type":    "go",
			},
			wantKeys: []string{"pattern", "path", "glob", "type"},
		},
		{
			name: "Agent keeps description and subagent_type",
			tool: "Agent",
			input: map[string]any{
				"description":   "find files",
				"subagent_type": "Explore",
				"prompt":        "very long prompt content",
			},
			wantKeys: []string{"description", "subagent_type"},
			dropKeys: []string{"prompt"},
		},
		{
			name: "Unknown tool keeps defaults",
			tool: "WebFetch",
			input: map[string]any{
				"file_path": "/foo",
				"pattern":   "bar",
				"url":       "https://example.com",
			},
			wantKeys: []string{"file_path", "pattern"},
			dropKeys: []string{"url"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FilterToolArgs(tt.tool, tt.input)

			for _, k := range tt.wantKeys {
				if _, ok := got[k]; !ok {
					t.Errorf("expected key %q to be present", k)
				}
			}

			for _, k := range tt.dropKeys {
				if _, ok := got[k]; ok {
					t.Errorf("expected key %q to be dropped", k)
				}
			}
		})
	}
}

func TestFilterToolArgs_BashTruncation(t *testing.T) {
	t.Parallel()

	longCmd := make([]byte, 200)
	for i := range longCmd {
		longCmd[i] = 'x'
	}

	got := FilterToolArgs("Bash", map[string]any{
		"command": string(longCmd),
	})

	cmd, _ := got["command"].(string)
	if len(cmd) != 123 { // 120 + "..."
		t.Errorf("truncated command length = %d; want 123", len(cmd))
	}
	if cmd[120:] != "..." {
		t.Errorf("truncated command should end with '...'; got %q", cmd[120:])
	}
}

func TestFilterToolArgs_NilInput(t *testing.T) {
	t.Parallel()

	got := FilterToolArgs("Bash", nil)
	if got != nil {
		t.Errorf("FilterToolArgs(nil) = %v; want nil", got)
	}
}

func TestContextPressure_IncrementAndGet(t *testing.T) {
	t.Parallel()

	cp := NewContextPressure()

	if got := cp.Get("sess-01"); got != 0 {
		t.Errorf("initial count = %d; want 0", got)
	}

	if got := cp.Increment("sess-01"); got != 1 {
		t.Errorf("first increment = %d; want 1", got)
	}

	if got := cp.Increment("sess-01"); got != 2 {
		t.Errorf("second increment = %d; want 2", got)
	}

	// Sentinel file should exist after second compact.
	sentinelPath := filepath.Join(os.TempDir(), "swarm-context-pressure-sess-01")
	if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
		t.Error("sentinel file not created after second compact")
	}

	// Clean up.
	cp.Remove("sess-01")

	if got := cp.Get("sess-01"); got != 0 {
		t.Errorf("count after remove = %d; want 0", got)
	}

	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Error("sentinel file not removed after Remove()")
	}
}

func TestContextPressure_NoSentinelBeforeSecond(t *testing.T) {
	t.Parallel()

	cp := NewContextPressure()
	cp.Increment("sess-02")

	sentinelPath := filepath.Join(os.TempDir(), "swarm-context-pressure-sess-02")
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Error("sentinel file should not exist after only one compact")
	}

	cp.Remove("sess-02")
}
