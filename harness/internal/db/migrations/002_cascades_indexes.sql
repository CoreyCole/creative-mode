CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_checkpoints_world_id ON checkpoints(world_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_created_by_status ON checkpoints(created_by, status);
CREATE INDEX IF NOT EXISTS idx_messages_world_id_created_at ON messages(world_id, created_at);
CREATE INDEX IF NOT EXISTS idx_user_positions_world_id ON user_positions(world_id);
CREATE INDEX IF NOT EXISTS idx_prompt_history_checkpoint_id ON prompt_history(checkpoint_id);
