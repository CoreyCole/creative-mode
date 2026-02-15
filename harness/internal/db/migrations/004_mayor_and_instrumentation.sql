-- Mayor identity (world columns)
ALTER TABLE worlds ADD COLUMN mayor_name TEXT;
ALTER TABLE worlds ADD COLUMN mayor_personality TEXT;
ALTER TABLE worlds ADD COLUMN mayor_secret TEXT;
ALTER TABLE worlds ADD COLUMN discord_channel_id TEXT;
ALTER TABLE worlds ADD COLUMN openclaw_agent_id TEXT;

-- Discord message mirror
CREATE TABLE IF NOT EXISTS mayor_messages (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    discord_message_id TEXT UNIQUE,
    author_type TEXT NOT NULL,  -- 'user', 'mayor', 'system'
    author_name TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mayor_messages_world ON mayor_messages(world_id, created_at);

-- Activity log
CREATE TABLE IF NOT EXISTS mayor_activity (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    activity_type TEXT NOT NULL,
    detail TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mayor_activity_world ON mayor_activity(world_id, created_at);

-- Build delegations
CREATE TABLE IF NOT EXISTS mayor_builds (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    checkpoint_id TEXT,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'building',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    duration_seconds INTEGER,
    error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_mayor_builds_world ON mayor_builds(world_id, started_at);

-- Session tracking
CREATE TABLE IF NOT EXISTS mayor_sessions (
    id TEXT PRIMARY KEY,
    world_id TEXT NOT NULL REFERENCES worlds(id),
    session_key TEXT NOT NULL,
    message_count INTEGER DEFAULT 0,
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- World invites
CREATE TABLE IF NOT EXISTS world_invites (
    world_id TEXT NOT NULL REFERENCES worlds(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    invited_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (world_id, user_id)
);
