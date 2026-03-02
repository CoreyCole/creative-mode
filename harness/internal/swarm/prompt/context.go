package prompt

// PromptContext holds all data needed to render a phase-specific prompt.
type PromptContext struct {
	TicketID        string
	WorkflowID      string
	SessionID       string
	Phase           string // human-readable phase name
	Attempt         int64
	ResultPath      string
	TicketURL       string
	BranchName      string
	DryRun          bool
	HandoffContent  string // pre-read, inlined (empty if none)
	LearningContent string // pre-read, inlined (empty if none)
}
