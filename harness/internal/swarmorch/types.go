package swarmorch

import "encoding/json"

// --- JSONL Protocol Messages ---

// StartMessage is sent from Go to agent on stdin
type StartMessage struct {
	Type         string          `json:"type"` // always "start"
	Task         json.RawMessage `json:"task"`
	SystemPrompt string          `json:"systemPrompt,omitempty"`
}

// AnswerMessage is sent from Go to agent in response to a question
type AnswerMessage struct {
	Type string `json:"type"` // always "answer"
	ID   string `json:"id"`
	Text string `json:"text"`
}

// AgentMessage is a generic message received from agent on stdout
type AgentMessage struct {
	Type       string          `json:"type"`                 // "event", "question", "result"
	Event      string          `json:"event,omitempty"`      // for type=event
	Tool       string          `json:"tool,omitempty"`       // for type=event
	Data       json.RawMessage `json:"data,omitempty"`       // for type=event or type=result
	ToolCallID string          `json:"toolCallID,omitempty"` // for type=event
	ID         string          `json:"id,omitempty"`         // for type=question
	Text       string          `json:"text,omitempty"`       // for type=question
}

// --- Agent Input Types ---

type GenerateQuestionsInput struct {
	TaskID       string `json:"taskID"`
	RequestText  string `json:"requestText"`
	RepoRoot     string `json:"repoRoot"`
	MaxQuestions int    `json:"maxQuestions"`
}

type ResearchAgentInput struct {
	TaskID     string `json:"taskID"`
	Question   string `json:"question"`
	RepoRoot   string `json:"repoRoot"`
	AgentIndex int    `json:"agentIndex"`
}

type SynthesizeInput struct {
	TaskID      string            `json:"taskID"`
	RequestText string            `json:"requestText"`
	Findings    []ResearchFinding `json:"findings"`
	OutputPath  string            `json:"outputPath"`
}

type ClassifyInput struct {
	TaskID          string `json:"taskID"`
	RequestText     string `json:"requestText"`
	ResearchDocPath string `json:"researchDocPath"`
	RepoRoot        string `json:"repoRoot"`
}

type SpecialistInput struct {
	TaskID      string `json:"taskID"`
	Domain      string `json:"domain"`
	Focus       string `json:"focus"`
	RequestText string `json:"requestText"`
	ResearchDoc string `json:"researchDoc"`
	RepoRoot    string `json:"repoRoot"`
}

type PlanSynthesizeInput struct {
	TaskID             string          `json:"taskID"`
	RequestText        string          `json:"requestText"`
	ResearchDocSummary string          `json:"researchDocSummary"`
	PlannerOutputs     []PlannerOutput `json:"plannerOutputs"`
	OutputPath         string          `json:"outputPath"`
}

// --- Artifact Output Types ---

type SubQuestion struct {
	Question       string   `json:"question"`
	Rationale      string   `json:"rationale"`
	SuggestedFiles []string `json:"suggestedFiles"`
}

type QuestionArtifact struct {
	Questions []SubQuestion `json:"questions"`
}

type ResearchFinding struct {
	Question        string   `json:"question"`
	Findings        string   `json:"findings"`
	FilesReferenced []string `json:"filesReferenced"`
	Confidence      string   `json:"confidence"` // "high", "medium", "low"
}

type SynthesizeResult struct {
	Document   string `json:"document"`
	Summary    string `json:"summary"`
	OutputPath string `json:"outputPath"`
}

type PlannerSpec struct {
	Type  string `json:"type"`
	Focus string `json:"focus"`
}

type ClassifyResult struct {
	Planners []PlannerSpec `json:"planners"`
}

type PlannerOutput struct {
	Domain             string   `json:"domain"`
	PlanSection        string   `json:"planSection"`
	FilesAffected      []string `json:"filesAffected"`
	VerificationChecks []string `json:"verificationChecks"`
	Risks              []string `json:"risks"`
	Dependencies       []string `json:"dependencies"`
}

type PlanSynthesizeResult struct {
	Document   string   `json:"document"`
	Summary    string   `json:"summary"`
	PhaseOrder []string `json:"phaseOrder"`
	OutputPath string   `json:"outputPath"`
}

// --- Span Helper Types ---

type SpanParams struct {
	ID           string          `json:"id"`
	TaskID       string          `json:"taskID"`
	ParentSpanID string          `json:"parentSpanID,omitempty"`
	SpanType     string          `json:"spanType"`
	Name         string          `json:"name"`
	InputJSON    json.RawMessage `json:"inputJSON,omitempty"`
	StartedAt    string          `json:"startedAt"`
}
