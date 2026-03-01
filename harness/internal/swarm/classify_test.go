package swarm

import "testing"

func TestClassifyTicket_ExplicitFooter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		title       string
		description string
		want        WorkflowType
	}{
		{
			name:  "research footer",
			title: "Implement dark mode",
			description: `Add dark mode to the dashboard.

---
swarm_type: research
---`,
			want: WorkflowTypeResearch,
		},
		{
			name:  "project footer",
			title: "Some ticket",
			description: `Description here.

swarm_type: project`,
			want: WorkflowTypeProject,
		},
		{
			name:  "code footer",
			title: "Fix bug",
			description: `Fix the login bug.

swarm_type: code`,
			want: WorkflowTypeCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyTicket(tt.title, tt.description)
			if got != tt.want {
				t.Errorf("ClassifyTicket() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyTicket_KeywordRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		desc  string
		want  WorkflowType
	}{
		{
			name:  "research keyword in title",
			title: "Investigate auth performance",
			desc:  "",
			want:  WorkflowTypeResearch,
		},
		{
			name:  "explore keyword in description",
			title: "Auth spike",
			desc:  "Explore different OAuth providers",
			want:  WorkflowTypeResearch,
		},
		{
			name:  "spike keyword",
			title: "Spike: evaluate caching strategies",
			desc:  "",
			want:  WorkflowTypeResearch,
		},
		{
			name:  "project keyword in title",
			title: "Project: redesign dashboard",
			desc:  "",
			want:  WorkflowTypeProject,
		},
		{
			name:  "multi-ticket keyword",
			title: "Dashboard redesign",
			desc:  "This is a multi-ticket effort",
			want:  WorkflowTypeProject,
		},
		{
			name:  "decompose keyword",
			title: "Auth overhaul",
			desc:  "decompose into smaller tickets",
			want:  WorkflowTypeProject,
		},
		{
			name:  "default to code",
			title: "Fix login button styling",
			desc:  "The button is misaligned on mobile",
			want:  WorkflowTypeCode,
		},
		{
			name:  "empty strings default to code",
			title: "",
			desc:  "",
			want:  WorkflowTypeCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyTicket(tt.title, tt.desc)
			if got != tt.want {
				t.Errorf("ClassifyTicket() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyTicket_FooterOverridesKeywords(t *testing.T) {
	t.Parallel()

	// Title suggests research, but footer explicitly says code.
	got := ClassifyTicket(
		"Research authentication methods",
		"Look into OAuth options.\n\nswarm_type: code",
	)
	if got != WorkflowTypeCode {
		t.Errorf("expected footer to override keywords, got %q", got)
	}
}

func TestClassifyTicket_ProjectBeforeResearch(t *testing.T) {
	t.Parallel()

	// Both project and research keywords present — project wins.
	got := ClassifyTicket(
		"Project: research and implement caching",
		"",
	)
	if got != WorkflowTypeProject {
		t.Errorf("expected project to take priority, got %q", got)
	}
}

func TestParseYAMLFooterType_InvalidType(t *testing.T) {
	t.Parallel()

	got := parseYAMLFooterType("swarm_type: invalid_type")
	if got != "" {
		t.Errorf("expected empty for invalid type, got %q", got)
	}
}
