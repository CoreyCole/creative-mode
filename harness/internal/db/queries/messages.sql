-- name: CreateMessage :exec
INSERT INTO messages (id, type, user_id, world_id, checkpoint_id, content)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetRecentMessages :many
SELECT id, type, user_id, world_id, checkpoint_id, content, created_at
FROM messages ORDER BY created_at DESC LIMIT ?;

-- name: GetRecentMessagesByWorld :many
SELECT id, type, user_id, world_id, checkpoint_id, content, created_at
FROM messages WHERE world_id = ? ORDER BY created_at DESC LIMIT ?;
