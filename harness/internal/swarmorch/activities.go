package swarmorch

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"creative-mode/harness/internal/swarm"
)

// Activities holds the manager reference for all Temporal activities.
type Activities struct {
	mgr *Manager
}

// RunClaudeSession spawns a tmux-based Claude Code session via the existing
// manager and polls for completion, heartbeating to Temporal.
func (a *Activities) RunClaudeSession(
	ctx context.Context,
	params SessionParams,
) (SessionWorkflowResult, error) {
	logger := activity.GetLogger(ctx)

	wf, err := a.mgr.db.GetSwarmWorkflow(ctx, params.WorkflowID)
	if err != nil {
		return SessionWorkflowResult{
			Result:  swarm.ResultInfraFailure,
			Summary: fmt.Sprintf("get workflow: %v", err),
		}, nil
	}

	// Verify workflow is still running.
	if wf.Status != swarm.StatusRunning {
		return SessionWorkflowResult{
			Result:  swarm.ResultInfraFailure,
			Summary: fmt.Sprintf("workflow not running (status: %s)", wf.Status),
		}, nil
	}

	// Spawn the session via the existing manager (creates tmux + watcher goroutine).
	if spawnErr := a.mgr.spawnSession(ctx, wf); spawnErr != nil {
		return SessionWorkflowResult{
			Result:  swarm.ResultInfraFailure,
			Summary: fmt.Sprintf("spawn session: %v", spawnErr),
		}, nil
	}

	// Get the session that was just created.
	session, sessionErr := a.mgr.db.GetLatestSwarmSession(ctx, params.WorkflowID)
	if sessionErr != nil {
		return SessionWorkflowResult{
			Result:  swarm.ResultInfraFailure,
			Summary: fmt.Sprintf("get session: %v", sessionErr),
		}, nil
	}

	logger.Info("session spawned, polling for completion",
		"session_id", session.ID, "session_name", session.SessionName)

	// Poll DB for session completion, heartbeating to Temporal.
	ticker := time.NewTicker(sessionPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return SessionWorkflowResult{
				Result:  swarm.ResultInfraFailure,
				Summary: "activity canceled",
			}, ctx.Err()

		case <-ticker.C:
			activity.RecordHeartbeat(ctx, session.ID)

			// Re-read session to check completion.
			updated, getErr := a.mgr.db.GetSwarmSession(ctx, session.ID)
			if getErr != nil {
				logger.Warn("poll session", "error", getErr)

				continue
			}

			if !updated.CompletedAt.Valid {
				continue
			}

			// Session complete — return result.
			result := swarm.ResultSuccess
			if updated.Result.Valid() {
				result = updated.Result
			}

			summary := ""
			if updated.Detail.Valid {
				summary = updated.Detail.String
			}

			return SessionWorkflowResult{
				SessionID: session.ID,
				Result:    result,
				Summary:   summary,
			}, nil
		}
	}
}

// ReadTicketQueue finds running workflows that need a session spawned.
func (a *Activities) ReadTicketQueue(ctx context.Context) ([]SpawnRequest, error) {
	workflows, err := a.mgr.db.ListRunningSwarmWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running workflows: %w", err)
	}

	var spawns []SpawnRequest

	for _, wf := range workflows {
		// Skip terminal phases.
		if wf.Phase == swarm.PhaseDone || wf.Phase == swarm.PhaseFailed {
			continue
		}

		// Skip project_verify — handled by CheckProjectProgress which waits
		// for all child tickets to complete before spawning the verify session.
		if wf.Phase == swarm.PhaseProjectVerify {
			continue
		}

		session, sessionErr := a.mgr.db.GetLatestSwarmSession(ctx, wf.ID)
		if sessionErr != nil {
			// No session yet — needs one spawned.
			spawns = append(spawns, SpawnRequest{
				WorkflowID: wf.ID,
				TicketID:   wf.TicketID,
				Phase:      wf.Phase,
				Attempt:    wf.Attempt,
			})

			continue
		}

		// If latest session is completed and we're not terminal, need a new session.
		// (The watcher/handleSessionComplete already advanced the phase; heartbeat
		// just needs to spawn the next session.)
		if session.CompletedAt.Valid {
			spawns = append(spawns, SpawnRequest{
				WorkflowID: wf.ID,
				TicketID:   wf.TicketID,
				Phase:      wf.Phase,
				Attempt:    wf.Attempt,
			})

			continue
		}

		// Session exists and is not complete — check if tmux is alive.
		if !isTmuxSessionAlive(session.SessionName) {
			// Tmux died but session not marked complete. The watcher goroutine
			// should handle this, but if it crashed we mark completion here.
			a.mgr.handleSessionComplete(ctx, session.ID)
		}
	}

	return spawns, nil
}

// SpawnPendingSessions finds running workflows that need sessions and spawns
// them directly. Replaces the fire-and-forget child workflow pattern which
// silently failed (parent completed before child start confirmation).
func (a *Activities) SpawnPendingSessions(ctx context.Context) error {
	spawns, err := a.ReadTicketQueue(ctx)
	if err != nil {
		return fmt.Errorf("read ticket queue: %w", err)
	}

	for _, sp := range spawns {
		wf, wfErr := a.mgr.db.GetSwarmWorkflow(ctx, sp.WorkflowID)
		if wfErr != nil {
			a.mgr.logger.Warn("spawn pending: get workflow",
				"workflow_id", sp.WorkflowID, "error", wfErr)

			continue
		}

		// spawnSession has an idempotency guard — safe to call even if
		// a session was already spawned by advanceWorkflow.
		spawnErr := a.mgr.spawnSession(ctx, wf)
		if spawnErr == nil {
			continue
		}

		a.mgr.logger.Warn("spawn pending: spawn session",
			"workflow_id", sp.WorkflowID,
			"phase", sp.Phase,
			"error", spawnErr)

		if a.mgr.alertMgr != nil {
			a.mgr.alertMgr.FireError("Session Spawn",
				fmt.Sprintf("Failed to spawn `%s` for `%s` (phase: %s): %v",
					sp.TicketID, sp.WorkflowID, sp.Phase, spawnErr))
		}
	}

	return nil
}

// DetectStalls checks for stalled workflows and fires alerts.
func (a *Activities) DetectStalls(ctx context.Context) error {
	a.mgr.detectAndAlertStalls(ctx)

	return nil
}

// ReapSessions kills orphaned cm-swarm-* tmux sessions with no DB match.
func (a *Activities) ReapSessions(ctx context.Context) error {
	activeSessions := ListActiveSessions(ctx)

	for _, sessionName := range activeSessions {
		// Check if any running workflow has this session.
		found := false

		workflows, err := a.mgr.db.ListRunningSwarmWorkflows(ctx)
		if err != nil {
			return fmt.Errorf("list running workflows: %w", err)
		}

		for _, wf := range workflows {
			session, sessionErr := a.mgr.db.GetLatestSwarmSession(ctx, wf.ID)
			if sessionErr != nil {
				continue
			}

			if session.SessionName == sessionName && !session.CompletedAt.Valid {
				found = true

				break
			}
		}

		if found {
			continue
		}

		a.mgr.logger.Info("reaping orphaned tmux session", "session", sessionName)

		_ = exec.CommandContext(ctx, "tmux", "kill-session", "-t", sessionName).Run()

		// Emit reap event with ticket ID extracted from session name.
		ticketID := extractTicketID(sessionName)
		a.mgr.emitEvent(ctx, "", "", ticketID,
			swarm.EventSessionReaped, "", sessionName)
	}

	return nil
}

// CheckProjectProgress checks project workflows and advances child ticket waves.
func (a *Activities) CheckProjectProgress(ctx context.Context) error {
	a.mgr.CheckProjectProgress(ctx)

	return nil
}

// AdvanceProject checks a single project's child status and advances waves
// or triggers project_verify when all children are complete.
func (a *Activities) AdvanceProject(
	ctx context.Context,
	params ProjectOrchestratorParams,
) (ProjectProgressResult, error) {
	wf, err := a.mgr.db.GetSwarmWorkflow(ctx, params.WorkflowID)
	if err != nil {
		return ProjectProgressResult{}, fmt.Errorf(
			"get workflow: %w",
			err,
		)
	}

	if wf.Status != swarm.StatusRunning {
		return ProjectProgressResult{AllComplete: true}, nil
	}

	graph := a.mgr.buildProjectGraph(ctx, params.ProjectID)
	if len(graph.Tickets) == 0 {
		return ProjectProgressResult{AllComplete: true}, nil
	}

	completed := a.mgr.completedChildTickets(ctx, graph)

	if graph.AllComplete(completed) {
		// Spawn verify session if needed.
		session, sessionErr := a.mgr.db.GetLatestSwarmSession(ctx, wf.ID)
		if sessionErr != nil || session.Phase != swarm.PhaseProjectVerify {
			if spawnErr := a.mgr.spawnSession(ctx, wf); spawnErr != nil {
				a.mgr.logger.Warn("spawn project verify",
					"workflow_id", wf.ID, "error", spawnErr)
			}
		}

		return ProjectProgressResult{
			AllComplete:    true,
			TotalTickets:   len(graph.Tickets),
			CompletedCount: len(completed),
		}, nil
	}

	// Start newly-unblocked tickets, respecting session capacity.
	ready := graph.ReadyTickets(completed)
	started := 0

	config := a.mgr.loadConfig(ctx)

	for _, rt := range ready {
		if started >= config.MaxSessions {
			break
		}

		existing, existErr := a.mgr.db.GetSwarmWorkflowsByTicket(
			ctx,
			rt.TicketID,
		)
		if existErr == nil && len(existing) > 0 {
			continue
		}

		if _, startErr := a.mgr.StartWorkflow(
			ctx, rt.TicketID, rt.WorkflowType, "", "",
		); startErr != nil {
			a.mgr.logger.Warn("start unblocked child",
				"ticket", rt.TicketID, "error", startErr)
		} else {
			started++
		}
	}

	return ProjectProgressResult{
		AllComplete:    false,
		TotalTickets:   len(graph.Tickets),
		CompletedCount: len(completed),
		StartedCount:   started,
	}, nil
}

// CheckProjectHealth checks all active project orchestrator workflows and
// returns a summary of their health. Used by LeadFDEWorkflow.
func (a *Activities) CheckProjectHealth(
	ctx context.Context,
) ([]ProjectHealthStatus, error) {
	workflows, err := a.mgr.db.ListRunningSwarmWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running workflows: %w", err)
	}

	var statuses []ProjectHealthStatus

	for _, wf := range workflows {
		if wf.WorkflowType != swarm.WorkflowTypeProject {
			continue
		}

		graph := a.mgr.buildProjectGraph(ctx, wf.ID)
		completed := a.mgr.completedChildTickets(ctx, graph)

		statuses = append(statuses, ProjectHealthStatus{
			WorkflowID:     wf.ID,
			TicketID:       wf.TicketID,
			Phase:          wf.Phase,
			TotalTickets:   len(graph.Tickets),
			CompletedCount: len(completed),
			AllComplete:    graph.AllComplete(completed),
		})
	}

	return statuses, nil
}

// PostProjectUpdate posts a status comment on the project's parent
// Linear ticket.
func (a *Activities) PostProjectUpdate(
	ctx context.Context,
	params ProjectUpdateParams,
) error {
	a.mgr.linearComment(
		params.TicketID,
		fmt.Sprintf(
			"📊 Project progress: %d/%d child tickets complete",
			params.CompletedCount,
			params.TotalTickets,
		),
	)

	return nil
}

// DecayLearnings applies relevance decay to stored learnings.
func (a *Activities) DecayLearnings(ctx context.Context) error {
	return a.mgr.decayLearningRelevance(ctx)
}

// GenerateDigest generates a learning digest if enough time has passed.
func (a *Activities) GenerateDigest(ctx context.Context) error {
	return GenerateDigest(ctx, a.mgr.db, a.mgr.baseDir)
}

// extractTicketID extracts the ticket ID from a session name like "cm-swarm-TICKET-phase".
func extractTicketID(sessionName string) string {
	// Format: cm-swarm-{ticketID}-{phase}
	parts := strings.TrimPrefix(sessionName, "cm-swarm-")
	idx := strings.LastIndex(parts, "-")
	if idx > 0 {
		return parts[:idx]
	}

	return parts
}
