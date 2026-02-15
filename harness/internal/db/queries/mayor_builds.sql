-- name: CreateMayorBuild :exec
INSERT INTO mayor_builds (id, world_id, checkpoint_id, prompt, status)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateMayorBuildStatus :exec
UPDATE mayor_builds
SET status = ?, completed_at = CURRENT_TIMESTAMP,
    duration_seconds = CAST((julianday(CURRENT_TIMESTAMP) - julianday(started_at)) * 86400 AS INTEGER),
    error_message = ?
WHERE id = ?;

-- name: GetMayorBuilds :many
SELECT id, world_id, checkpoint_id, prompt, status, started_at, completed_at, duration_seconds, error_message
FROM mayor_builds WHERE world_id = ? ORDER BY started_at DESC LIMIT ?;

-- name: GetRecentMayorBuildsAllWorlds :many
SELECT id, world_id, checkpoint_id, prompt, status, started_at, completed_at, duration_seconds, error_message
FROM mayor_builds ORDER BY started_at DESC LIMIT ?;
