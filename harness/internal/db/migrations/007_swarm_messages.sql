CREATE TABLE IF NOT EXISTS swarm_task_messages (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES swarm_tasks(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'orchestrator', 'system')),
    content TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_swarm_task_messages_task_id ON swarm_task_messages(task_id);
