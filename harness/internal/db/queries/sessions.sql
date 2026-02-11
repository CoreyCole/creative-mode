-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, expires_at)
VALUES (?, ?, ?);

-- name: GetSession :one
SELECT id, user_id, created_at, expires_at
FROM sessions WHERE id = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP;

-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions WHERE user_id = ?;
