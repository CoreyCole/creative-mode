-- Prompt version tracking for swarm sessions.
CREATE TABLE IF NOT EXISTS swarm_prompt_versions (
    id TEXT PRIMARY KEY,
    phase TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(phase, content_hash)
);

-- Extended token tracking on sessions.
ALTER TABLE swarm_sessions ADD COLUMN input_tokens INTEGER DEFAULT 0;
ALTER TABLE swarm_sessions ADD COLUMN output_tokens INTEGER DEFAULT 0;
ALTER TABLE swarm_sessions ADD COLUMN cache_read_tokens INTEGER DEFAULT 0;
ALTER TABLE swarm_sessions ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0;
ALTER TABLE swarm_sessions ADD COLUMN model_used TEXT;
ALTER TABLE swarm_sessions ADD COLUMN estimated_cost_usd REAL;
ALTER TABLE swarm_sessions ADD COLUMN prompt_version_id TEXT REFERENCES swarm_prompt_versions(id);
