-- Self-directed project creation: add description to tickets, fix phase constraints.

-- 1. Add description column to swarm_tickets.
ALTER TABLE swarm_tickets ADD COLUMN description TEXT;

-- 2. Recreate swarm_workflows to add project_decompose to phase CHECK constraint.
CREATE TABLE IF NOT EXISTS swarm_workflows_new (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL,
    workflow_type        TEXT NOT NULL CHECK(workflow_type IN ('research', 'code', 'project')),
    phase                TEXT NOT NULL CHECK(phase IN (
        'research', 'code_plan', 'plan_review', 'implement', 'verify', 'pr',
        'project_decompose', 'project_plan', 'project_review', 'project_verify',
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

INSERT OR IGNORE INTO swarm_workflows_new (id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, gate_phase, review_feedback, created_at, updated_at)
SELECT id, ticket_id, workflow_type, phase, status, attempt, previous_workflow_id, branch_name, gate_phase, review_feedback, created_at, updated_at
FROM swarm_workflows;

DROP TABLE IF EXISTS swarm_workflows;
ALTER TABLE swarm_workflows_new RENAME TO swarm_workflows;

CREATE INDEX IF NOT EXISTS idx_swarm_workflows_ticket ON swarm_workflows(ticket_id);
CREATE INDEX IF NOT EXISTS idx_swarm_workflows_status ON swarm_workflows(status);

-- 3. Recreate swarm_sessions to add project_decompose to phase CHECK constraint.
CREATE TABLE IF NOT EXISTS swarm_sessions_new (
    id            TEXT PRIMARY KEY,
    workflow_id   TEXT NOT NULL REFERENCES swarm_workflows(id),
    session_name  TEXT NOT NULL,
    skill         TEXT NOT NULL,
    phase         TEXT NOT NULL CHECK(phase IN (
        'research', 'code_plan', 'plan_review', 'implement', 'verify', 'pr',
        'project_decompose', 'project_plan', 'project_review', 'project_verify'
    )),
    result        TEXT CHECK(result IN ('success', 'logic_failure', 'infra_failure', 'timeout', 'context_limit')),
    detail        TEXT,
    duration_sec  INTEGER,
    total_tokens  INTEGER,
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at  TEXT
);

INSERT OR IGNORE INTO swarm_sessions_new (id, workflow_id, session_name, skill, phase, result, detail, duration_sec, total_tokens, started_at, completed_at)
SELECT id, workflow_id, session_name, skill, phase, result, detail, duration_sec, total_tokens, started_at, completed_at
FROM swarm_sessions;

DROP TABLE IF EXISTS swarm_sessions;
ALTER TABLE swarm_sessions_new RENAME TO swarm_sessions;

CREATE INDEX IF NOT EXISTS idx_swarm_sessions_workflow ON swarm_sessions(workflow_id);
