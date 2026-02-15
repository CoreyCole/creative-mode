-- name: CreateMayorMessage :exec
INSERT INTO mayor_messages (id, world_id, discord_message_id, author_type, author_name, content)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetMayorMessages :many
SELECT id, world_id, discord_message_id, author_type, author_name, content, created_at
FROM mayor_messages WHERE world_id = ? ORDER BY created_at ASC;

-- name: GetRecentMayorMessages :many
SELECT id, world_id, discord_message_id, author_type, author_name, content, created_at
FROM mayor_messages WHERE world_id = ? ORDER BY created_at DESC LIMIT ?;

-- name: GetMayorMessageByDiscordID :one
SELECT id, world_id, discord_message_id, author_type, author_name, content, created_at
FROM mayor_messages WHERE discord_message_id = ?;
