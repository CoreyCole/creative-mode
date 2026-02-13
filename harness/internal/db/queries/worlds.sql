-- name: CreateWorld :exec
INSERT INTO worlds (id, name, description, created_by, template_type)
VALUES (?, ?, ?, ?, ?);

-- name: GetWorld :one
SELECT id, name, description, created_by, created_at, template_type
FROM worlds WHERE id = ?;

-- name: ListWorlds :many
SELECT id, name, description, created_by, created_at, template_type
FROM worlds ORDER BY created_at ASC;
