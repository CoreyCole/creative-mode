-- name: CreateWorld :exec
INSERT INTO worlds (id, name, description, created_by, template_type)
VALUES (?, ?, ?, ?, ?);

-- name: GetWorld :one
SELECT id, name, description, created_by, created_at, template_type,
       mayor_name, mayor_personality, mayor_secret, discord_channel_id, openclaw_agent_id
FROM worlds WHERE id = ?;

-- name: ListWorlds :many
SELECT id, name, description, created_by, created_at, template_type,
       mayor_name, mayor_personality, mayor_secret, discord_channel_id, openclaw_agent_id
FROM worlds ORDER BY created_at ASC;

-- name: UpdateWorldMayor :exec
UPDATE worlds
SET mayor_name = ?, mayor_secret = ?, discord_channel_id = ?, openclaw_agent_id = ?
WHERE id = ?;

-- name: GetWorldByDiscordChannel :one
SELECT id, name, description, created_by, created_at, template_type,
       mayor_name, mayor_personality, mayor_secret, discord_channel_id, openclaw_agent_id
FROM worlds WHERE discord_channel_id = ?;

-- name: GetWorldByMayorSecret :one
SELECT id, name, description, created_by, created_at, template_type,
       mayor_name, mayor_personality, mayor_secret, discord_channel_id, openclaw_agent_id
FROM worlds WHERE mayor_secret = ?;

-- name: GetWorldsWithDiscordChannels :many
SELECT id, name, description, created_by, created_at, template_type,
       mayor_name, mayor_personality, mayor_secret, discord_channel_id, openclaw_agent_id
FROM worlds WHERE discord_channel_id IS NOT NULL ORDER BY created_at ASC;
