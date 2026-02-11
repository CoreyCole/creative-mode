-- name: CreatePromptHistory :exec
INSERT INTO prompt_history (id, checkpoint_id, world_id, user_id, prompt_text)
VALUES (?, ?, ?, ?, ?);

-- name: DeletePromptHistoryByUserID :exec
DELETE FROM prompt_history WHERE user_id = ?;
