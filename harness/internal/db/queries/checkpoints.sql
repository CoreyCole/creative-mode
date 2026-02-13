-- name: CreateCheckpoint :exec
INSERT INTO checkpoints (id, world_id, parent_checkpoint_id, name, prompt, status, dir_path, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetCheckpoint :one
SELECT id, world_id, parent_checkpoint_id, name, prompt, status,
       build_log, work_summary, files_changed, build_duration_ms,
       dir_path, wasm_path, server_port, created_by, created_at
FROM checkpoints WHERE id = ?;

-- name: UpdateCheckpointStatus :execresult
UPDATE checkpoints SET status = ?, build_log = ? WHERE id = ?;

-- name: UpdateCheckpointSummary :execresult
UPDATE checkpoints SET work_summary = ?, files_changed = ?, build_duration_ms = ? WHERE id = ?;

-- name: UpdateCheckpointServerPort :execresult
UPDATE checkpoints SET server_port = ? WHERE id = ?;

-- name: UpdateCheckpointWasmPath :exec
UPDATE checkpoints SET wasm_path = ? WHERE id = ?;

-- name: UpdateCheckpointName :exec
UPDATE checkpoints SET name = ? WHERE id = ?;

-- name: GetCheckpointTree :many
SELECT id, world_id, parent_checkpoint_id, name, prompt, status,
       build_log, work_summary, files_changed, build_duration_ms,
       dir_path, wasm_path, server_port, created_by, created_at
FROM checkpoints WHERE world_id = ? ORDER BY created_at ASC;

-- name: UpdateCheckpointDirPath :exec
UPDATE checkpoints SET dir_path = ? WHERE id = ?;

-- name: CountActiveBuilds :one
SELECT COUNT(*) FROM checkpoints WHERE created_by = ? AND status = 'building';
