-- name: UpsertUser :exec
INSERT INTO users (id, discord_id, discord_username, avatar_url, role)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(discord_id) DO UPDATE SET
    discord_username = excluded.discord_username,
    avatar_url = excluded.avatar_url,
    last_seen_at = CURRENT_TIMESTAMP;

-- name: GetUserByID :one
SELECT id, discord_id, discord_username, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users WHERE id = ?;

-- name: GetUserByDiscordID :one
SELECT id, discord_id, discord_username, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users WHERE discord_id = ?;

-- name: UpdateUserRole :execresult
UPDATE users SET role = ? WHERE id = ?;

-- name: ListUsers :many
SELECT id, discord_id, discord_username, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users ORDER BY created_at ASC;

-- name: ListPendingUsers :many
SELECT id, discord_id, discord_username, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users WHERE role = 'pending' ORDER BY created_at ASC;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: UpdateLastSeen :exec
UPDATE users SET last_seen_at = CURRENT_TIMESTAMP WHERE id = ?;
