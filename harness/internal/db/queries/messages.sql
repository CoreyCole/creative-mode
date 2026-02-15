-- name: CreateMessage :exec
INSERT INTO messages (id, type, user_id, world_id, checkpoint_id, content)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetRecentMessagesByWorld :many
SELECT id, type, user_id, world_id, checkpoint_id, content, created_at
FROM messages WHERE world_id = ? ORDER BY created_at DESC LIMIT ?;

-- name: GetRecentMessagesWithUser :many
SELECT m.id, m.type, m.user_id, m.world_id, m.checkpoint_id, m.content, m.created_at,
       u.discord_username, u.avatar_url
FROM messages m
LEFT JOIN users u ON m.user_id = u.id
ORDER BY m.created_at DESC LIMIT ?;

-- name: DeleteMessagesByUserID :exec
DELETE FROM messages WHERE user_id = ?;
