-- Human review gates: add awaiting_review status, human_review phase, gate columns, and audit table.
-- SQLite requires table recreation to alter CHECK constraints.

-- 1. Recreate swarm_workflows with new phase/status values and gate columns.
CREATE TABLE IF NOT EXISTS swarm_workflows_new (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL,
    workflow_type        TEXT NOT NULL CHECK(workflow_type IN ('research', 'code', 'project')),
    phase                TEXT NOT NULL CHECK(phase IN (
        'research', 'code_plan', 'plan_review', 'implement', 'verify', 'pr',
        'project_plan', 'project_review', 'project_verify',
        'human_review',
        'done', 'failed'
    )),
    status               TEXT NOT NULL CHECK(status IN ('pending', 'running', 'completed', 'failed', 'canceled', 'awaiting_review')),
    attempt              INTEGER NOT NULL DEFAULT 1,
    previous_workflow_id TEXT,
    branch_name          TEXT,
    gate_phase           TEXT,
    review_feedback      TEXT,
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO swarm_workflows_new (id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, created_at, updated_at)
SELECT id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, created_at, updated_at
FROM swarm_workflows;

DROP TABLE IF EXISTS swarm_workflows;
ALTER TABLE swarm_workflows_new RENAME TO swarm_workflows;

CREATE INDEX IF NOT EXISTS idx_swarm_workflows_ticket ON swarm_workflows(ticket_id);
CREATE INDEX IF NOT EXISTS idx_swarm_workflows_status ON swarm_workflows(status);

-- 2. Recreate swarm_events with new event types.
CREATE TABLE IF NOT EXISTS swarm_events_new (
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
        'terminal_failure',
        'gate_reached', 'gate_approved', 'gate_rejected'
    )),
    phase       TEXT,
    detail      TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO swarm_events_new (id, workflow_id, session_id, ticket_id, event_type, phase, detail, created_at)
SELECT id, workflow_id, session_id, ticket_id, event_type, phase, detail, created_at
FROM swarm_events;

DROP TABLE IF EXISTS swarm_events;
ALTER TABLE swarm_events_new RENAME TO swarm_events;

CREATE INDEX IF NOT EXISTS idx_swarm_events_workflow ON swarm_events(workflow_id);
CREATE INDEX IF NOT EXISTS idx_swarm_events_type ON swarm_events(event_type);

-- 3. Gate reviews audit table.
CREATE TABLE IF NOT EXISTS swarm_gate_reviews (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES swarm_workflows(id),
    gate_phase   TEXT NOT NULL,
    action       TEXT NOT NULL CHECK(action IN ('approve', 'reject')),
    feedback     TEXT,
    reviewer     TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_swarm_gate_reviews_workflow ON swarm_gate_reviews(workflow_id);
