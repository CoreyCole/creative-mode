-- name: CreateSwarmDependency :exec
INSERT INTO swarm_ticket_dependencies (id, ticket_id, depends_on_ticket_id, project_id)
VALUES (?, ?, ?, ?);

-- name: ListSwarmDependenciesByTicket :many
SELECT id, ticket_id, depends_on_ticket_id, project_id, created_at
FROM swarm_ticket_dependencies
WHERE ticket_id = ?
ORDER BY created_at ASC;

-- name: ListSwarmDependenciesByProject :many
SELECT id, ticket_id, depends_on_ticket_id, project_id, created_at
FROM swarm_ticket_dependencies
WHERE project_id = ?
ORDER BY created_at ASC;

-- name: ListSwarmTicketsByParent :many
SELECT id, identifier, title, status, priority, assignee, labels, parent_id, project_id, url, created_at, updated_at, synced_at, description, ticket_type
FROM swarm_tickets
WHERE parent_id = ?
ORDER BY created_at ASC;

-- name: ListSwarmTicketsByProject :many
SELECT id, identifier, title, status, priority, assignee, labels, parent_id, project_id, url, created_at, updated_at, synced_at, description, ticket_type
FROM swarm_tickets
WHERE project_id = ?
ORDER BY created_at ASC;

-- name: DeleteSwarmDependenciesByProject :exec
DELETE FROM swarm_ticket_dependencies WHERE project_id = ?;

-- name: DeleteSwarmTicketsByProject :exec
DELETE FROM swarm_tickets WHERE project_id = ?;
