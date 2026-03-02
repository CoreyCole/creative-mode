-- swarm_learnings

-- name: CreateSwarmLearning :exec
INSERT INTO swarm_learnings (id, source_workflow_id, source_session_id, ticket_id, category, phase, severity, title, content, doc_path, tags)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSwarmLearning :one
SELECT id, source_workflow_id, source_session_id, ticket_id, category, phase, severity, title, content, doc_path, tags, relevance_score, referenced_count, archived_at, created_at, updated_at
FROM swarm_learnings WHERE id = ?;

-- name: ListSwarmLearningsByTicket :many
SELECT id, source_workflow_id, source_session_id, ticket_id, category, phase, severity, title, content, doc_path, tags, relevance_score, referenced_count, archived_at, created_at, updated_at
FROM swarm_learnings WHERE ticket_id = ? AND archived_at IS NULL ORDER BY created_at DESC;

-- name: ListTopSwarmLearningsByPhase :many
SELECT id, source_workflow_id, source_session_id, ticket_id, category, phase, severity, title, content, doc_path, tags, relevance_score, referenced_count, archived_at, created_at, updated_at
FROM swarm_learnings
WHERE phase = ? AND archived_at IS NULL
ORDER BY relevance_score DESC
LIMIT ?;

-- name: ListTopCriticalSwarmLearnings :many
SELECT id, source_workflow_id, source_session_id, ticket_id, category, phase, severity, title, content, doc_path, tags, relevance_score, referenced_count, archived_at, created_at, updated_at
FROM swarm_learnings
WHERE severity = 'critical' AND archived_at IS NULL
ORDER BY relevance_score DESC
LIMIT ?;

-- name: IncrementSwarmLearningReference :exec
UPDATE swarm_learnings
SET referenced_count = referenced_count + 1, relevance_score = relevance_score + 0.1, updated_at = datetime('now')
WHERE id = ?;

-- name: DecaySwarmLearningRelevance :exec
UPDATE swarm_learnings
SET relevance_score = relevance_score * CASE severity
    WHEN 'critical' THEN 0.98
    WHEN 'warning' THEN 0.95
    WHEN 'info' THEN 0.90
    ELSE 0.95
END, updated_at = datetime('now')
WHERE archived_at IS NULL AND relevance_score > 0.1;

-- name: ArchiveSwarmLearning :exec
UPDATE swarm_learnings SET archived_at = datetime('now'), updated_at = datetime('now') WHERE id = ?;

-- name: ArchiveOldLowRelevanceLearnings :exec
UPDATE swarm_learnings
SET archived_at = datetime('now'), updated_at = datetime('now')
WHERE archived_at IS NULL
  AND relevance_score < 0.1
  AND created_at < datetime('now', '-60 days');

-- name: ListRecentSwarmLearnings :many
SELECT id, source_workflow_id, source_session_id, ticket_id, category, phase, severity, title, content, doc_path, tags, relevance_score, referenced_count, archived_at, created_at, updated_at
FROM swarm_learnings
WHERE archived_at IS NULL AND created_at > ?
ORDER BY created_at DESC;

-- swarm_learning_digests

-- name: CreateSwarmLearningDigest :exec
INSERT INTO swarm_learning_digests (id, digest_type, period_start, period_end, learning_count, summary, action_items, doc_path)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLatestSwarmLearningDigest :one
SELECT id, digest_type, period_start, period_end, learning_count, summary, action_items, doc_path, created_at
FROM swarm_learning_digests ORDER BY created_at DESC LIMIT 1;

-- name: ListSwarmLearningDigests :many
SELECT id, digest_type, period_start, period_end, learning_count, summary, action_items, doc_path, created_at
FROM swarm_learning_digests ORDER BY created_at DESC LIMIT ?;
