-- Dependency edges between swarm tickets within a project.
CREATE TABLE IF NOT EXISTS swarm_ticket_dependencies (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL,
    depends_on_ticket_id TEXT NOT NULL,
    project_id           TEXT NOT NULL,
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(ticket_id, depends_on_ticket_id)
);
CREATE INDEX IF NOT EXISTS idx_swarm_deps_ticket ON swarm_ticket_dependencies(ticket_id);
CREATE INDEX IF NOT EXISTS idx_swarm_deps_project ON swarm_ticket_dependencies(project_id);
