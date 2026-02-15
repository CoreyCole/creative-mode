-- name: UpsertMayorSession :exec
INSERT INTO mayor_sessions (id, world_id, session_key, message_count, first_seen_at, last_active_at)
VALUES (?, ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(id) DO UPDATE SET
    message_count = mayor_sessions.message_count + 1,
    last_active_at = CURRENT_TIMESTAMP;

-- name: GetMayorSessions :many
SELECT id, world_id, session_key, message_count, first_seen_at, last_active_at
FROM mayor_sessions WHERE world_id = ? ORDER BY last_active_at DESC LIMIT ?;
