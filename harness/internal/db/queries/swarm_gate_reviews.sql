-- name: CreateSwarmGateReview :exec
INSERT INTO swarm_gate_reviews (id, workflow_id, gate_phase, action, feedback, reviewer)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListSwarmGateReviewsByWorkflow :many
SELECT id, workflow_id, gate_phase, action, feedback, reviewer, created_at
FROM swarm_gate_reviews WHERE workflow_id = ? ORDER BY created_at DESC;
