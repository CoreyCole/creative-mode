package sqlc

// Typed string enums for all CHECK-constrained and role/status/type columns.
// These give compile-time safety over raw string literals.

// SpanType enumerates the kinds of spans in swarm_spans.span_type.
type SpanType string

const (
	SpanTypeWorkflow SpanType = "workflow"
	SpanTypeStage    SpanType = "stage"
	SpanTypeAgent    SpanType = "agent"
	SpanTypeToolCall SpanType = "tool_call"
	SpanTypeLLMCall  SpanType = "llm_call"
	SpanTypeQuestion SpanType = "question"
)

// SpanStatus enumerates the statuses in swarm_spans.status.
type SpanStatus string

const (
	SpanStatusRunning   SpanStatus = "running"
	SpanStatusCompleted SpanStatus = "completed"
	SpanStatusFailed    SpanStatus = "failed"
)

// TaskStatus enumerates the statuses in swarm_tasks.status.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

// PrimitiveType enumerates the task types in swarm_tasks.primitive_type.
type PrimitiveType string

const (
	PrimitiveTypeResearch       PrimitiveType = "research"
	PrimitiveTypeCodeChangePlan PrimitiveType = "code_change_plan"
)

// ArtifactType enumerates the artifact types in swarm_artifacts.artifact_type.
type ArtifactType string

const (
	ArtifactTypeResearchDoc ArtifactType = "research_doc"
	ArtifactTypePlanDoc     ArtifactType = "plan_doc"
)

// MessageRole enumerates the roles in swarm_task_messages.role.
type MessageRole string

const (
	MessageRoleUser         MessageRole = "user"
	MessageRoleOrchestrator MessageRole = "orchestrator"
	MessageRoleSystem       MessageRole = "system"
)

// QuestionStatus enumerates the statuses in swarm_research_questions.status.
type QuestionStatus string

const (
	QuestionStatusPending   QuestionStatus = "pending"
	QuestionStatusRunning   QuestionStatus = "running"
	QuestionStatusCompleted QuestionStatus = "completed"
	QuestionStatusFailed    QuestionStatus = "failed"
)

// UserRole enumerates the roles in users.role.
type UserRole string

const (
	UserRoleAdmin   UserRole = "admin"
	UserRoleUser    UserRole = "user"
	UserRolePending UserRole = "pending"
)

// TemplateType enumerates the template types in worlds.template_type.
type TemplateType string

const (
	TemplateType3D        TemplateType = "3d"
	TemplateType2D        TemplateType = "2d"
	TemplateTypeBoardgame TemplateType = "boardgame"
)

// AuthorType enumerates the author types in mayor_messages.author_type.
type AuthorType string

const (
	AuthorTypeUser   AuthorType = "user"
	AuthorTypeMayor  AuthorType = "mayor"
	AuthorTypeSystem AuthorType = "system"
)

// CheckpointStatus enumerates the statuses in checkpoints.status and mayor_builds.status.
type CheckpointStatus string

const (
	CheckpointStatusBuilding CheckpointStatus = "building"
	CheckpointStatusReady    CheckpointStatus = "ready"
	CheckpointStatusFailed   CheckpointStatus = "failed"
)
