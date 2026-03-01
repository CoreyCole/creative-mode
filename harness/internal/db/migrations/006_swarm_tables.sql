-- Swarm agent orchestration tables (v4 base + v5 learning layer)

CREATE TABLE IF NOT EXISTS swarm_config (
    id     TEXT PRIMARY KEY DEFAULT 'default',
    config TEXT NOT NULL DEFAULT '{}'
);

INSERT OR IGNORE INTO swarm_config (id, config) VALUES ('default', '{"maxSessions":4,"heartbeatSeconds":120,"stallMinutes":45,"maxPlanRevisions":3,"maxVerifyRetries":3,"retryBackoffSecs":30}');

CREATE TABLE IF NOT EXISTS swarm_workflows (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL,
    workflow_type        TEXT NOT NULL CHECK(workflow_type IN ('research', 'code', 'project')),
    phase                TEXT NOT NULL CHECK(phase IN (
        'research', 'code_plan', 'plan_review', 'implement', 'verify', 'pr',
        'project_plan', 'project_review', 'project_verify',
        'done', 'failed'
    )),
    status               TEXT NOT NULL CHECK(status IN ('pending', 'running', 'completed', 'failed', 'canceled')),
    attempt              INTEGER NOT NULL DEFAULT 1,
    previous_workflow_id TEXT,
    branch_name          TEXT,
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_swarm_workflows_ticket ON swarm_workflows(ticket_id);
CREATE INDEX IF NOT EXISTS idx_swarm_workflows_status ON swarm_workflows(status);

CREATE TABLE IF NOT EXISTS swarm_sessions (
    id            TEXT PRIMARY KEY,
    workflow_id   TEXT NOT NULL REFERENCES swarm_workflows(id),
    session_name  TEXT NOT NULL,
    skill         TEXT NOT NULL,
    phase         TEXT NOT NULL CHECK(phase IN (
        'research', 'code_plan', 'plan_review', 'implement', 'verify', 'pr',
        'project_plan', 'project_review', 'project_verify'
    )),
    result        TEXT CHECK(result IN ('success', 'logic_failure', 'infra_failure', 'timeout', 'context_limit')),
    detail        TEXT,
    duration_sec  INTEGER,
    total_tokens  INTEGER,
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at  TEXT
);

CREATE INDEX IF NOT EXISTS idx_swarm_sessions_workflow ON swarm_sessions(workflow_id);

CREATE TABLE IF NOT EXISTS swarm_events (
    id          TEXT PRIMARY KEY,
    workflow_id TEXT REFERENCES swarm_workflows(id),
    session_id  TEXT REFERENCES swarm_sessions(id),
    ticket_id   TEXT NOT NULL,
    event_type  TEXT NOT NULL CHECK(event_type IN (
        'workflow_started', 'workflow_completed', 'workflow_failed', 'workflow_canceled',
        'phase_started', 'phase_completed',
        'session_spawned', 'session_completed',
        'plan_review_verdict', 'verify_result',
        'milestone_passed', 'milestone_failed',
        'retry_triggered',
        'stall_detected', 'session_reaped',
        'ticket_synced',
        'terminal_failure'
    )),
    phase       TEXT,
    detail      TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_swarm_events_workflow ON swarm_events(workflow_id);
CREATE INDEX IF NOT EXISTS idx_swarm_events_type ON swarm_events(event_type);

CREATE TABLE IF NOT EXISTS swarm_project_milestones (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES swarm_workflows(id),
    project_id   TEXT,
    name         TEXT NOT NULL,
    criteria     TEXT NOT NULL,
    status       TEXT NOT NULL CHECK(status IN ('pending', 'passed', 'failed')),
    verified_at  TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS swarm_tickets (
    id           TEXT PRIMARY KEY,
    identifier   TEXT NOT NULL,
    title        TEXT NOT NULL,
    status       TEXT NOT NULL,
    priority     INTEGER,
    assignee     TEXT,
    labels       TEXT,
    parent_id    TEXT,
    project_id   TEXT,
    url          TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    synced_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_swarm_tickets_identifier ON swarm_tickets(identifier);

CREATE TABLE IF NOT EXISTS swarm_ticket_comments (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT NOT NULL REFERENCES swarm_tickets(id),
    body       TEXT NOT NULL,
    author     TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    synced_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_swarm_ticket_comments_ticket ON swarm_ticket_comments(ticket_id);

-- Learning tables (v5)

CREATE TABLE IF NOT EXISTS swarm_learnings (
    id                 TEXT PRIMARY KEY,
    source_workflow_id TEXT REFERENCES swarm_workflows(id),
    source_session_id  TEXT REFERENCES swarm_sessions(id),
    ticket_id          TEXT NOT NULL,
    category           TEXT NOT NULL CHECK(category IN (
        'plan_issue', 'code_bug', 'pattern', 'post_mortem', 'convention'
    )),
    phase              TEXT,
    severity           TEXT NOT NULL DEFAULT 'info' CHECK(severity IN (
        'critical', 'warning', 'info'
    )),
    title              TEXT NOT NULL,
    content            TEXT NOT NULL,
    doc_path           TEXT,
    tags               TEXT,
    relevance_score    REAL NOT NULL DEFAULT 1.0,
    referenced_count   INTEGER NOT NULL DEFAULT 0,
    archived_at        TEXT,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_swarm_learnings_category ON swarm_learnings(category, archived_at);
CREATE INDEX IF NOT EXISTS idx_swarm_learnings_ticket ON swarm_learnings(ticket_id);
CREATE INDEX IF NOT EXISTS idx_swarm_learnings_relevance ON swarm_learnings(relevance_score DESC)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS swarm_learning_digests (
    id              TEXT PRIMARY KEY,
    digest_type     TEXT NOT NULL CHECK(digest_type IN ('daily', 'weekly', 'ad_hoc')),
    period_start    TEXT NOT NULL,
    period_end      TEXT NOT NULL,
    learning_count  INTEGER NOT NULL,
    summary         TEXT NOT NULL,
    action_items    TEXT,
    doc_path        TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
