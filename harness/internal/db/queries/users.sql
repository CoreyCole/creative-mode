-- name: UpsertUser :exec
INSERT INTO users (id, github_id, github_username, avatar_url, role)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(github_id) DO UPDATE SET
    github_username = excluded.github_username,
    avatar_url = excluded.avatar_url,
    last_seen_at = CURRENT_TIMESTAMP;

-- name: GetUserByID :one
SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users WHERE id = ?;

-- name: GetUserByGitHubID :one
SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users WHERE github_id = ?;

-- name: UpdateUserRole :execresult
UPDATE users SET role = ? WHERE id = ?;

-- name: ListUsers :many
SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users ORDER BY created_at ASC;

-- name: ListPendingUsers :many
SELECT id, github_id, github_username, avatar_url, role, created_at, last_seen_at
FROM users WHERE role = 'pending' ORDER BY created_at ASC;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: UpdateLastSeen :exec
UPDATE users SET last_seen_at = CURRENT_TIMESTAMP WHERE id = ?;
