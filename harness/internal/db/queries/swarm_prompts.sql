-- name: UpsertSwarmPromptVersion :one
INSERT INTO swarm_prompt_versions (id, phase, content_hash)
VALUES (?, ?, ?)
ON CONFLICT(phase, content_hash) DO UPDATE SET id = swarm_prompt_versions.id
RETURNING id;

-- name: GetSwarmPromptVersion :one
SELECT id, phase, content_hash, created_at
FROM swarm_prompt_versions WHERE id = ?;
