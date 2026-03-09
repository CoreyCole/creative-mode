-- Swarm Tasks

-- name: CreateSwarmTask :exec
INSERT INTO swarm_tasks (id, primitive_type, request_text, status, workflow_id, linear_issue_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSwarmTaskStatus :exec
UPDATE swarm_tasks SET status = ?, updated_at = ? WHERE id = ?;

-- name: UpdateSwarmTaskWorkflowID :exec
UPDATE swarm_tasks SET workflow_id = ?, updated_at = ? WHERE id = ?;

-- name: GetSwarmTask :one
SELECT id, primitive_type, request_text, status, workflow_id, linear_issue_id, created_at, updated_at
FROM swarm_tasks WHERE id = ?;

-- name: ListSwarmTasks :many
SELECT id, primitive_type, request_text, status, workflow_id, linear_issue_id, created_at, updated_at
FROM swarm_tasks ORDER BY created_at DESC LIMIT ?;

-- name: ListSwarmTasksByStatus :many
SELECT id, primitive_type, request_text, status, workflow_id, linear_issue_id, created_at, updated_at
FROM swarm_tasks WHERE status = ? ORDER BY created_at DESC LIMIT ?;

-- Swarm Research Questions

-- name: CreateSwarmResearchQuestion :exec
INSERT INTO swarm_research_questions (id, task_id, question_text, agent_index, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSwarmResearchQuestion :exec
UPDATE swarm_research_questions SET status = ?, result_summary = ?, updated_at = ? WHERE id = ?;

-- name: GetSwarmResearchQuestionsByTask :many
SELECT id, task_id, question_text, agent_index, status, result_summary, created_at, updated_at
FROM swarm_research_questions WHERE task_id = ? ORDER BY agent_index;

-- Swarm Artifacts

-- name: CreateSwarmArtifact :exec
INSERT INTO swarm_artifacts (id, task_id, artifact_type, file_path, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetSwarmArtifact :one
SELECT id, task_id, artifact_type, file_path, created_at
FROM swarm_artifacts WHERE id = ?;

-- name: GetSwarmArtifactsByTask :many
SELECT id, task_id, artifact_type, file_path, created_at
FROM swarm_artifacts WHERE task_id = ? ORDER BY created_at;

-- Swarm Spans

-- name: CreateSwarmSpan :exec
INSERT INTO swarm_spans (id, task_id, parent_span_id, span_type, name, status, input_json, started_at, metadata_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CompleteSwarmSpan :exec
UPDATE swarm_spans
SET status = 'completed', output_json = ?, ended_at = ?, duration_ms = ?
WHERE id = ?;

-- name: FailSwarmSpan :exec
UPDATE swarm_spans
SET status = 'failed', error_message = ?, ended_at = ?, duration_ms = ?
WHERE id = ?;

-- name: CompleteSwarmSpanWithMetadata :exec
UPDATE swarm_spans
SET status = 'completed', output_json = ?, ended_at = ?, duration_ms = ?, metadata_json = ?
WHERE id = ?;

-- name: FailSwarmSpanWithMetadata :exec
UPDATE swarm_spans
SET status = 'failed', error_message = ?, ended_at = ?, duration_ms = ?, metadata_json = ?
WHERE id = ?;

-- name: FailRunningSpansByTask :exec
UPDATE swarm_spans
SET status = 'failed', error_message = ?, ended_at = datetime('now'), duration_ms = 0
WHERE task_id = ? AND status = 'running';

-- name: GetSwarmSpan :one
SELECT id, task_id, parent_span_id, span_type, name, status, input_json, output_json,
       error_message, started_at, ended_at, duration_ms, metadata_json
FROM swarm_spans WHERE id = ?;

-- name: GetSwarmSpansByTask :many
SELECT id, task_id, parent_span_id, span_type, name, status, input_json, output_json,
       error_message, started_at, ended_at, duration_ms, metadata_json
FROM swarm_spans WHERE task_id = ? ORDER BY started_at;

-- name: GetSwarmSpanTree :many
-- Recursive CTE to get the full span tree for a task, ordered for tree rendering.
-- Returns spans depth-first: parent before children, siblings by start time.
WITH RECURSIVE span_tree AS (
    -- Root spans (no parent)
    SELECT s.id, s.task_id, s.parent_span_id, s.span_type, s.name, s.status, s.input_json, s.output_json,
           s.error_message, s.started_at, s.ended_at, s.duration_ms, s.metadata_json, 0 AS depth
    FROM swarm_spans s
    WHERE s.task_id = ? AND s.parent_span_id IS NULL
    UNION ALL
    -- Child spans
    SELECT c.id, c.task_id, c.parent_span_id, c.span_type, c.name, c.status, c.input_json, c.output_json,
           c.error_message, c.started_at, c.ended_at, c.duration_ms, c.metadata_json, st.depth + 1
    FROM swarm_spans c
    JOIN span_tree st ON c.parent_span_id = st.id
)
SELECT id, task_id, parent_span_id, span_type, name, status, input_json, output_json,
       error_message, started_at, ended_at, duration_ms, metadata_json, depth
FROM span_tree ORDER BY started_at;

-- Swarm Task Messages

-- name: CreateSwarmTaskMessage :exec
INSERT INTO swarm_task_messages (id, task_id, role, content, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetSwarmTaskMessages :many
SELECT id, task_id, role, content, created_at
FROM swarm_task_messages WHERE task_id = ? ORDER BY created_at;

-- name: CleanupOrphanedSpans :exec
-- Mark stale running spans as failed (e.g. after crash recovery).
UPDATE swarm_spans
SET status = 'failed', error_message = 'orphaned: stale after restart',
    ended_at = datetime('now'), duration_ms = 0
WHERE status = 'running' AND ended_at IS NULL
  AND started_at < datetime('now', '-15 minutes');
