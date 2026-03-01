-- swarm_config

-- name: GetSwarmConfig :one
SELECT id, config FROM swarm_config WHERE id = 'default';

-- name: UpdateSwarmConfig :exec
UPDATE swarm_config SET config = ? WHERE id = 'default';

-- swarm_workflows

-- name: CreateSwarmWorkflow :exec
INSERT INTO swarm_workflows (id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSwarmWorkflow :one
SELECT id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, created_at, updated_at
FROM swarm_workflows WHERE id = ?;

-- name: GetSwarmWorkflowsByTicket :many
SELECT id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, created_at, updated_at
FROM swarm_workflows WHERE ticket_id = ? ORDER BY created_at DESC;

-- name: ListRunningSwarmWorkflows :many
SELECT id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, created_at, updated_at
FROM swarm_workflows WHERE status = 'running' ORDER BY created_at ASC;

-- name: ListActiveSwarmWorkflows :many
SELECT id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, created_at, updated_at
FROM swarm_workflows WHERE status IN ('pending', 'running') ORDER BY created_at ASC;

-- name: UpdateSwarmWorkflowPhase :exec
UPDATE swarm_workflows SET phase = ?, attempt = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateSwarmWorkflowStatus :exec
UPDATE swarm_workflows SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateSwarmWorkflowBranch :exec
UPDATE swarm_workflows SET branch_name = ?, updated_at = datetime('now') WHERE id = ?;

-- name: GetSwarmWorkflowByPrevious :one
SELECT id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, created_at, updated_at
FROM swarm_workflows WHERE previous_workflow_id = ?;

-- name: CountActiveSwarmSessions :one
SELECT COUNT(*) FROM swarm_sessions
WHERE completed_at IS NULL;

-- swarm_sessions

-- name: CreateSwarmSession :exec
INSERT INTO swarm_sessions (id, workflow_id, session_name, skill, phase)
VALUES (?, ?, ?, ?, ?);

-- name: CompleteSwarmSession :exec
UPDATE swarm_sessions
SET result = ?, detail = ?, duration_sec = ?, total_tokens = ?, completed_at = datetime('now')
WHERE id = ?;

-- name: GetSwarmSession :one
SELECT id, workflow_id, session_name, skill, phase, COALESCE(result, '') AS result, detail, duration_sec, total_tokens, started_at, completed_at
FROM swarm_sessions WHERE id = ?;

-- name: ListSwarmSessionsByWorkflow :many
SELECT id, workflow_id, session_name, skill, phase, COALESCE(result, '') AS result, detail, duration_sec, total_tokens, started_at, completed_at
FROM swarm_sessions WHERE workflow_id = ? ORDER BY started_at DESC;

-- name: GetLatestSwarmSession :one
SELECT id, workflow_id, session_name, skill, phase, COALESCE(result, '') AS result, detail, duration_sec, total_tokens, started_at, completed_at
FROM swarm_sessions WHERE workflow_id = ? ORDER BY started_at DESC LIMIT 1;

-- swarm_events

-- name: CreateSwarmEvent :exec
INSERT INTO swarm_events (id, workflow_id, session_id, ticket_id, event_type, phase, detail)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListSwarmEventsByWorkflow :many
SELECT id, workflow_id, session_id, ticket_id, event_type, phase, detail, created_at
FROM swarm_events WHERE workflow_id = ? ORDER BY created_at DESC;

-- name: ListSwarmEventsByType :many
SELECT id, workflow_id, session_id, ticket_id, event_type, phase, detail, created_at
FROM swarm_events WHERE event_type = ? ORDER BY created_at DESC LIMIT ?;

-- name: ListRecentSwarmEvents :many
SELECT id, workflow_id, session_id, ticket_id, event_type, phase, detail, created_at
FROM swarm_events ORDER BY created_at DESC LIMIT ?;

-- swarm_project_milestones

-- name: CreateSwarmMilestone :exec
INSERT INTO swarm_project_milestones (id, workflow_id, project_id, name, criteria, status)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSwarmMilestone :one
SELECT id, workflow_id, project_id, name, criteria, status, verified_at, created_at
FROM swarm_project_milestones WHERE id = ?;

-- name: ListSwarmMilestonesByWorkflow :many
SELECT id, workflow_id, project_id, name, criteria, status, verified_at, created_at
FROM swarm_project_milestones WHERE workflow_id = ? ORDER BY created_at ASC;

-- name: UpdateSwarmMilestoneStatus :exec
UPDATE swarm_project_milestones SET status = ?, verified_at = ? WHERE id = ?;

-- swarm_tickets

-- name: UpsertSwarmTicket :exec
INSERT INTO swarm_tickets (id, identifier, title, status, priority, assignee, labels, parent_id, project_id, url, created_at, updated_at, synced_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    status = excluded.status,
    priority = excluded.priority,
    assignee = excluded.assignee,
    labels = excluded.labels,
    parent_id = excluded.parent_id,
    project_id = excluded.project_id,
    url = excluded.url,
    updated_at = excluded.updated_at,
    synced_at = datetime('now');

-- name: GetSwarmTicket :one
SELECT id, identifier, title, status, priority, assignee, labels, parent_id, project_id, url, created_at, updated_at, synced_at
FROM swarm_tickets WHERE id = ?;

-- name: GetSwarmTicketByIdentifier :one
SELECT id, identifier, title, status, priority, assignee, labels, parent_id, project_id, url, created_at, updated_at, synced_at
FROM swarm_tickets WHERE identifier = ?;

-- name: ListSwarmTickets :many
SELECT id, identifier, title, status, priority, assignee, labels, parent_id, project_id, url, created_at, updated_at, synced_at
FROM swarm_tickets ORDER BY updated_at DESC;

-- swarm_ticket_comments

-- name: UpsertSwarmTicketComment :exec
INSERT INTO swarm_ticket_comments (id, ticket_id, body, author, created_at, updated_at, synced_at)
VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(id) DO UPDATE SET
    body = excluded.body,
    author = excluded.author,
    updated_at = excluded.updated_at,
    synced_at = datetime('now');

-- name: ListSwarmTicketComments :many
SELECT id, ticket_id, body, author, created_at, updated_at, synced_at
FROM swarm_ticket_comments WHERE ticket_id = ? ORDER BY created_at ASC;

-- name: GetLatestSwarmResultComment :one
SELECT id, ticket_id, body, author, created_at, updated_at, synced_at
FROM swarm_ticket_comments
WHERE ticket_id = ? AND body LIKE 'RESULT:%'
ORDER BY created_at DESC LIMIT 1;

-- Dashboard queries

-- name: ListSwarmWorkflowsWithLatestSession :many
SELECT w.id, w.ticket_id, w.workflow_type, w.phase, w.status, w.attempt, w.created_at, w.updated_at,
       s.id AS session_id, s.skill, s.phase AS session_phase, COALESCE(s.result, '') AS result, s.started_at AS session_started_at
FROM swarm_workflows w
LEFT JOIN swarm_sessions s ON s.id = (
    SELECT id FROM swarm_sessions WHERE workflow_id = w.id ORDER BY started_at DESC LIMIT 1
)
WHERE w.status IN ('pending', 'running')
ORDER BY w.created_at ASC;
