-- name: CreateWorldInvite :exec
INSERT OR IGNORE INTO world_invites (world_id, user_id, invited_by)
VALUES (?, ?, ?);

-- name: GetWorldInvites :many
SELECT world_id, user_id, invited_by, created_at
FROM world_invites WHERE world_id = ?;

-- name: DeleteWorldInvite :exec
DELETE FROM world_invites WHERE world_id = ? AND user_id = ?;
