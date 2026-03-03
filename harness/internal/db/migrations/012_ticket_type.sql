-- Add ticket_type column to swarm_tickets so buildProjectGraph can determine
-- workflow type from stored data instead of fragile title heuristics.
ALTER TABLE swarm_tickets ADD COLUMN ticket_type TEXT NOT NULL DEFAULT 'code';
