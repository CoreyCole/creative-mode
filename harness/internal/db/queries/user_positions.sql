-- name: GetUserPosition :one
SELECT checkpoint_id FROM user_positions
WHERE user_id = ? AND world_id = ?;

-- name: SetUserPosition :exec
INSERT INTO user_positions (user_id, world_id, checkpoint_id, last_accessed_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(user_id, world_id) DO UPDATE SET
    checkpoint_id = excluded.checkpoint_id,
    last_accessed_at = CURRENT_TIMESTAMP;

-- name: DeleteUserPositionsByUserID :exec
DELETE FROM user_positions WHERE user_id = ?;
