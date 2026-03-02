package swarmorch

import "testing"

func TestParseDecomposeOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    int // expected number of topics
	}{
		{
			name: "valid decompose output",
			content: `# Research Decomposition

## Research Topics

| # | Topic | Description |
|---|-------|-------------|
| 1 | State machine routing | How does the current state machine handle project workflows? |
| 2 | Skill architecture | What patterns exist for swarm skills? |
| 3 | Test infrastructure | How are swarm tests structured? |
`,
			want: 3,
		},
		{
			name:    "empty content",
			content: "",
			want:    0,
		},
		{
			name: "no table",
			content: `# Research Decomposition

Some text without a table.
`,
			want: 0,
		},
		{
			name: "table with header only",
			content: `## Research Topics

| # | Topic | Description |
|---|-------|-------------|
`,
			want: 0,
		},
		{
			name: "single topic",
			content: `| # | Topic | Description |
|---|-------|-------------|
| 1 | Single topic | Just one research area |
`,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			topics := ParseDecomposeOutput(tt.content)
			if len(topics) != tt.want {
				t.Errorf(
					"ParseDecomposeOutput() returned %d topics, want %d",
					len(topics),
					tt.want,
				)
			}
		})
	}
}

func TestParseDecomposeOutputFields(t *testing.T) {
	t.Parallel()

	content := `| # | Topic | Description |
|---|-------|-------------|
| 1 | State machine routing | How does the current state machine handle project workflows? |
| 2 | Skill architecture | What patterns exist for swarm skills? |
`

	topics := ParseDecomposeOutput(content)
	if len(topics) != 2 {
		t.Fatalf("expected 2 topics, got %d", len(topics))
	}

	if topics[0].Num != 1 {
		t.Errorf("topic[0].Num = %d, want 1", topics[0].Num)
	}

	if topics[0].Title != "State machine routing" {
		t.Errorf("topic[0].Title = %q, want %q", topics[0].Title, "State machine routing")
	}

	if topics[0].Description != "How does the current state machine handle project workflows?" {
		t.Errorf("topic[0].Description = %q", topics[0].Description)
	}

	if topics[1].Num != 2 {
		t.Errorf("topic[1].Num = %d, want 2", topics[1].Num)
	}

	if topics[1].Title != "Skill architecture" {
		t.Errorf("topic[1].Title = %q, want %q", topics[1].Title, "Skill architecture")
	}
}
