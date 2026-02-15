-- name: CreateMayorActivity :exec
INSERT INTO mayor_activity (id, world_id, activity_type, detail)
VALUES (?, ?, ?, ?);

-- name: GetMayorActivity :many
SELECT id, world_id, activity_type, detail, created_at
FROM mayor_activity WHERE world_id = ? ORDER BY created_at DESC LIMIT ?;

-- name: GetRecentMayorActivityAllWorlds :many
SELECT id, world_id, activity_type, detail, created_at
FROM mayor_activity ORDER BY created_at DESC LIMIT ?;
