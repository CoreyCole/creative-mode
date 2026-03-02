-- Add revision_target column to gate reviews for routing rejections.
ALTER TABLE swarm_gate_reviews ADD COLUMN revision_target TEXT;
