package swarmorch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/swarm"
)

const (
	sessionPollInterval  = 15 * time.Second
	startTimeout         = 30 * time.Second
	tmuxFallbackInterval = 30 * time.Second
	resultFilePrefix     = "swarm-result-"
	learningFilePrefix   = "swarm-learning-"
	promptFilePrefix     = "swarm-prompt-"
)

// Manager orchestrates swarm workflows: starting workflows, spawning Claude
// Code sessions per-phase, detecting completion, parsing results, advancing
// through phases, and capturing learnings.
type Manager struct {
	db         *db.DB
	logger     *slog.Logger
	eventBus   *events.EventBus
	baseDir    string // project root for cwd + handoff resolution
	logsDir    string // data/logs for session logs
	harnessURL string
	mu         sync.Mutex // serializes workflow advancement

	// Hook-driven completion: registries replace tmux polling as the primary
	// mechanism. Tmux checks remain as a fallback.
	completionReg *CompletionRegistry
	startReg      *StartRegistry
	ctxPressure   *ContextPressure

	// Metrics cache with 60s TTL.
	metricsCache *metricsCache

	// JSONL log writers keyed by sessionID; closed on session completion.
	jsonlMu      sync.RWMutex
	jsonlWriters map[string]*JSONLWriter
}

// NewManager creates a new swarm orchestrator.
func NewManager(
	database *db.DB,
	logger *slog.Logger,
	eventBus *events.EventBus,
	baseDir, logsDir, harnessURL string,
) *Manager {
	return &Manager{
		db:            database,
		logger:        logger,
		eventBus:      eventBus,
		baseDir:       baseDir,
		logsDir:       logsDir,
		harnessURL:    harnessURL,
		completionReg: NewCompletionRegistry(),
		startReg:      NewStartRegistry(),
		ctxPressure:   NewContextPressure(),
		metricsCache:  newMetricsCache(),
		jsonlWriters:  make(map[string]*JSONLWriter),
	}
}

// StartWorkflow creates a new workflow record, emits events, and spawns
// the first session. Returns the workflow ID.
func (m *Manager) StartWorkflow(
	ctx context.Context,
	ticketID string,
	workflowType swarm.WorkflowType,
	ticketURL string,
) (string, error) {
	if !workflowType.Valid() {
		return "", fmt.Errorf("invalid workflow type: %q", workflowType)
	}

	wfID := uuid.New().String()[:8]

	if err := m.db.CreateSwarmWorkflow(ctx, sqlc.CreateSwarmWorkflowParams{
		ID:           wfID,
		TicketID:     ticketID,
		WorkflowType: workflowType,
		Phase:        swarm.PhaseResearch,
		Status:       swarm.StatusRunning,
		Attempt:      1,
	}); err != nil {
		return "", fmt.Errorf("create workflow: %w", err)
	}

	// Upsert ticket record with URL.
	_ = m.db.UpsertSwarmTicket(ctx, sqlc.UpsertSwarmTicketParams{
		ID:         ticketID,
		Identifier: ticketID,
		Title:      ticketID,
		Status:     "In Progress",
		Url:        ticketURL,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	})

	m.emitEvent(
		ctx,
		wfID,
		"",
		ticketID,
		swarm.EventWorkflowStarted,
		swarm.PhaseResearch,
		"",
	)

	if m.eventBus != nil {
		m.eventBus.PublishGlobal(map[string]any{
			"event":         events.EventSwarmWorkflowStarted,
			"workflow_id":   wfID,
			"ticket_id":     ticketID,
			"workflow_type": string(workflowType),
		})
	}

	wf, err := m.db.GetSwarmWorkflow(ctx, wfID)
	if err != nil {
		return "", fmt.Errorf("get workflow: %w", err)
	}

	if err := m.spawnSession(ctx, wf); err != nil {
		return wfID, fmt.Errorf("spawn first session: %w", err)
	}

	return wfID, nil
}

// CancelWorkflow kills the active tmux session and marks the workflow canceled.
func (m *Manager) CancelWorkflow(ctx context.Context, workflowID string) error {
	wf, err := m.db.GetSwarmWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}

	if wf.Status != swarm.StatusRunning && wf.Status != swarm.StatusPending {
		return fmt.Errorf("workflow %s is not active (status: %s)", workflowID, wf.Status)
	}

	// Kill the active tmux session if any.
	session, sessionErr := m.db.GetLatestSwarmSession(ctx, workflowID)
	if sessionErr == nil && !session.CompletedAt.Valid {
		_ = exec.CommandContext(ctx, "tmux", "kill-session", "-t", session.SessionName).
			Run()
	}

	if err := m.db.UpdateSwarmWorkflowStatus(ctx, sqlc.UpdateSwarmWorkflowStatusParams{
		Status: swarm.StatusCanceled,
		ID:     workflowID,
	}); err != nil {
		return fmt.Errorf("update workflow status: %w", err)
	}

	m.emitEvent(
		ctx,
		workflowID,
		"",
		wf.TicketID,
		swarm.EventWorkflowCanceled,
		wf.Phase,
		"",
	)

	return nil
}

// RecoverWorkflows finds running workflows with dead tmux sessions and
// re-advances them. Called on startup to handle unclean restarts.
func (m *Manager) RecoverWorkflows(ctx context.Context) error {
	workflows, err := m.db.ListRunningSwarmWorkflows(ctx)
	if err != nil {
		return fmt.Errorf("list running workflows: %w", err)
	}

	for _, wf := range workflows {
		session, sessionErr := m.db.GetLatestSwarmSession(ctx, wf.ID)
		if sessionErr != nil {
			m.logger.Warn("recovery: no session for workflow", "workflow_id", wf.ID)

			continue
		}

		// Skip already-completed sessions.
		if session.CompletedAt.Valid {
			continue
		}

		// Check if tmux session is still alive.
		if isTmuxSessionAlive(session.SessionName) {
			// Re-attach watcher with fresh registry channels (hooks may not
			// fire for recovered sessions, but tmux fallback will catch them).
			m.logger.Info("recovery: re-watching live session",
				"session", session.SessionName, "workflow_id", wf.ID)
			startCh := m.startReg.Register(session.ID)
			completionCh := m.completionReg.Register(session.ID)
			go m.watchSession(session.ID, session.SessionName, startCh, completionCh)

			continue
		}

		// Session is dead — handle completion.
		m.logger.Info("recovery: handling dead session",
			"session", session.SessionName, "workflow_id", wf.ID)
		m.handleSessionComplete(ctx, session.ID)
	}

	return nil
}

// spawnSession creates a session DB record, builds env vars, creates a tmux
// session, sends the skill prompt, and starts a watcher goroutine.
func (m *Manager) spawnSession(ctx context.Context, wf sqlc.SwarmWorkflow) error {
	sessionID := uuid.New().String()[:8]
	skill := swarm.SkillForPhase(wf.Phase)

	if skill == "" {
		return fmt.Errorf("no skill for phase %q", wf.Phase)
	}

	sessionName := fmt.Sprintf("cm-swarm-%s-%s", wf.TicketID, wf.Phase)

	if err := m.db.CreateSwarmSession(ctx, sqlc.CreateSwarmSessionParams{
		ID:          sessionID,
		WorkflowID:  wf.ID,
		SessionName: sessionName,
		Skill:       skill,
		Phase:       wf.Phase,
	}); err != nil {
		return fmt.Errorf("create session record: %w", err)
	}

	env, cleanupFn := m.buildEnv(ctx, wf, sessionID)

	// Generate Claude Code hooks config pointing back to harness.
	hookSecret := os.Getenv("CM_HOOK_SECRET")
	hooksConfigDir, hooksErr := WriteHooksConfig(
		sessionID,
		wf.TicketID,
		m.harnessURL,
		hookSecret,
	)
	if hooksErr != nil {
		m.logger.Warn("failed to create hooks config", "error", hooksErr)
	} else {
		env["CLAUDE_CONFIG_DIR"] = hooksConfigDir
	}

	// Register in start and completion registries before spawning.
	startCh := m.startReg.Register(sessionID)
	completionCh := m.completionReg.Register(sessionID)

	// Create per-session JSONL writer.
	jw, jwErr := NewJSONLWriter(m.logsDir, wf.TicketID, sessionID)
	if jwErr != nil {
		m.logger.Warn("failed to create JSONL writer", "error", jwErr)
	} else {
		m.jsonlMu.Lock()
		m.jsonlWriters[sessionID] = jw
		m.jsonlMu.Unlock()

		_ = jw.Write(map[string]any{
			"event":       "session_spawned",
			"session_id":  sessionID,
			"workflow_id": wf.ID,
			"ticket_id":   wf.TicketID,
			"phase":       string(wf.Phase),
			"skill":       skill,
		})
	}

	sessLog := NewSessionLog(m.logger, wf.TicketID, wf.ID, sessionID, wf.Phase)

	if err := m.createTmuxSession(ctx, sessionName, m.baseDir, env); err != nil {
		cleanupFn()

		return fmt.Errorf("create tmux session: %w", err)
	}

	if err := m.sendSkillPrompt(ctx, sessionName, skill, sessionID); err != nil {
		cleanupFn()

		return fmt.Errorf("send skill prompt: %w", err)
	}

	sessLog.Info("session spawned", "session_name", sessionName, "skill", skill)

	m.emitEvent(
		ctx,
		wf.ID,
		sessionID,
		wf.TicketID,
		swarm.EventSessionSpawned,
		wf.Phase,
		skill,
	)

	if m.eventBus != nil {
		m.eventBus.PublishGlobal(map[string]any{
			"event":       events.EventSwarmSessionSpawned,
			"workflow_id": wf.ID,
			"session_id":  sessionID,
			"ticket_id":   wf.TicketID,
			"phase":       string(wf.Phase),
			"skill":       skill,
		})
	}

	go m.watchSession(sessionID, sessionName, startCh, completionCh)

	return nil
}

// watchSession waits for hook-driven completion signals, falling back to tmux
// health checks if hooks don't fire.
func (m *Manager) watchSession(
	sessionID, sessionName string,
	startCh chan struct{},
	completionCh chan SessionResult,
) {
	startedAt := time.Now()

	defer func() {
		m.startReg.Unregister(sessionID)
		m.completionReg.Unregister(sessionID)
		m.ctxPressure.Remove(sessionID)
		CleanupHooksConfig(sessionID)
	}()

	// Phase 1: Wait for SessionStart hook (30s timeout).
	select {
	case <-startCh:
		m.logger.Info("session started (hook)", "session", sessionName)
	case <-time.After(startTimeout):
		// Timeout — check if tmux is alive as fallback.
		if !isTmuxSessionAlive(sessionName) {
			m.logger.Warn("session never started and tmux is dead",
				"session", sessionName)
			m.handleSessionComplete(context.Background(), sessionID)

			return
		}

		m.logger.Info("session start hook timed out, tmux alive — continuing",
			"session", sessionName)
	}

	// Phase 2: Wait for completion signal from Stop/SessionEnd hooks,
	// with periodic tmux health checks as fallback.
	tmuxTicker := time.NewTicker(tmuxFallbackInterval)
	defer tmuxTicker.Stop()

	for {
		select {
		case <-completionCh:
			m.logger.Info("session completed (hook)",
				"session", sessionName,
				"duration", time.Since(startedAt).Round(time.Second))
			m.handleSessionComplete(context.Background(), sessionID)

			return

		case <-tmuxTicker.C:
			if isTmuxSessionAlive(sessionName) {
				continue
			}

			// Tmux died without hooks firing — handle completion.
			m.logger.Info("session ended (tmux fallback)",
				"session", sessionName,
				"duration", time.Since(startedAt).Round(time.Second))
			m.handleSessionComplete(context.Background(), sessionID)

			return
		}
	}
}

// handleSessionComplete reads the result file, completes the session record,
// captures learnings, and advances the workflow.
func (m *Manager) handleSessionComplete(ctx context.Context, sessionID string) {
	session, err := m.db.GetSwarmSession(ctx, sessionID)
	if err != nil {
		m.logger.Error(
			"get session for completion",
			"session_id",
			sessionID,
			"error",
			err,
		)

		return
	}

	// Already completed (double-fire guard).
	if session.CompletedAt.Valid {
		return
	}

	resultPath := ResultFilePath(sessionID)
	result, _ := swarm.ParseResultFile(resultPath)

	// Compute duration from started_at.
	var durationSec int64
	if startedAt, parseErr := time.Parse(
		"2006-01-02 15:04:05",
		session.StartedAt,
	); parseErr == nil {
		durationSec = int64(time.Since(startedAt).Seconds())
	}

	if completeErr := m.db.CompleteSwarmSession(ctx, sqlc.CompleteSwarmSessionParams{
		Result:      result.Result,
		Detail:      sql.NullString{String: result.Summary, Valid: result.Summary != ""},
		DurationSec: sql.NullInt64{Int64: durationSec, Valid: durationSec > 0},
		ID:          sessionID,
	}); completeErr != nil {
		m.logger.Error(
			"complete session record",
			"session_id",
			sessionID,
			"error",
			completeErr,
		)
	}

	m.emitEvent(
		ctx,
		session.WorkflowID,
		sessionID,
		"",
		swarm.EventSessionComplete,
		session.Phase,
		fmt.Sprintf("%s: %s", result.Result, result.Summary),
	)

	if m.eventBus != nil {
		m.eventBus.PublishGlobal(map[string]any{
			"event":       events.EventSwarmSessionComplete,
			"workflow_id": session.WorkflowID,
			"session_id":  sessionID,
			"phase":       string(session.Phase),
			"result":      string(result.Result),
			"summary":     result.Summary,
		})
	}

	wf, wfErr := m.db.GetSwarmWorkflow(ctx, session.WorkflowID)
	if wfErr != nil {
		m.logger.Error(
			"get workflow for advancement",
			"workflow_id",
			session.WorkflowID,
			"error",
			wfErr,
		)

		return
	}

	m.captureLearnings(ctx, wf, session, result)
	m.advanceWorkflow(ctx, wf, result)

	// Close JSONL writer for this session.
	m.closeJSONLWriter(sessionID)

	// Clean up temp files and hooks config.
	_ = os.Remove(resultPath)
	_ = os.Remove(LearningFilePath(sessionID))
	CleanupHooksConfig(sessionID)
}

// advanceWorkflow uses the state machine to determine the next phase and
// either spawns a new session or marks the workflow terminal.
func (m *Manager) advanceWorkflow(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
	result *swarm.SessionResultData,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-read workflow under lock to avoid stale state.
	wf, err := m.db.GetSwarmWorkflow(ctx, wf.ID)
	if err != nil {
		m.logger.Error("re-read workflow", "workflow_id", wf.ID, "error", err)

		return
	}

	// Skip if workflow is no longer running.
	if wf.Status != swarm.StatusRunning {
		return
	}

	config := m.loadConfig(ctx)

	transition := swarm.DetermineNextPhase(
		wf.WorkflowType,
		wf.Phase,
		int(wf.Attempt),
		result.Result,
		config,
	)

	m.logger.Info("workflow transition",
		"workflow_id", wf.ID,
		"from_phase", wf.Phase,
		"to_phase", transition.NextPhase,
		"retry", transition.Retry,
		"failed", transition.Failed,
		"result", result.Result,
	)

	// Terminal: done.
	if transition.NextPhase == swarm.PhaseDone {
		if statusErr := m.db.UpdateSwarmWorkflowStatus(
			ctx,
			sqlc.UpdateSwarmWorkflowStatusParams{
				Status: swarm.StatusComplete,
				ID:     wf.ID,
			},
		); statusErr != nil {
			m.logger.Error("mark workflow complete", "error", statusErr)
		}

		m.emitEvent(
			ctx,
			wf.ID,
			"",
			wf.TicketID,
			swarm.EventWorkflowComplete,
			swarm.PhaseDone,
			result.Summary,
		)

		if m.eventBus != nil {
			m.eventBus.PublishGlobal(map[string]any{
				"event":       events.EventSwarmWorkflowComplete,
				"workflow_id": wf.ID,
				"ticket_id":   wf.TicketID,
			})
		}

		// Capture success pattern.
		hadRetries := wf.Attempt > 1
		_ = swarm.CaptureSuccessPattern(ctx, m.db.SQLDB(), wf.ID, wf.TicketID, hadRetries)

		return
	}

	// Terminal: failed.
	if transition.Failed {
		if statusErr := m.db.UpdateSwarmWorkflowStatus(
			ctx,
			sqlc.UpdateSwarmWorkflowStatusParams{
				Status: swarm.StatusFailed,
				ID:     wf.ID,
			},
		); statusErr != nil {
			m.logger.Error("mark workflow failed", "error", statusErr)
		}

		m.emitEvent(
			ctx,
			wf.ID,
			"",
			wf.TicketID,
			swarm.EventWorkflowFailed,
			wf.Phase,
			result.Summary,
		)

		if m.eventBus != nil {
			m.eventBus.PublishGlobal(map[string]any{
				"event":       events.EventSwarmWorkflowFailed,
				"workflow_id": wf.ID,
				"ticket_id":   wf.TicketID,
				"phase":       string(wf.Phase),
				"reason":      result.Summary,
			})
		}

		// Capture terminal failure learning.
		_ = swarm.CaptureTerminalFailure(
			ctx,
			m.db.SQLDB(),
			wf.ID,
			"",
			wf.TicketID,
			wf.Phase,
			result.Summary,
		)

		return
	}

	// Non-terminal: advance to next phase.
	newAttempt := wf.Attempt
	if transition.Retry {
		newAttempt++
		m.emitEvent(
			ctx,
			wf.ID,
			"",
			wf.TicketID,
			swarm.EventRetryTriggered,
			transition.NextPhase,
			fmt.Sprintf("attempt %d", newAttempt),
		)
	}

	if phaseErr := m.db.UpdateSwarmWorkflowPhase(ctx, sqlc.UpdateSwarmWorkflowPhaseParams{
		Phase:   transition.NextPhase,
		Attempt: newAttempt,
		ID:      wf.ID,
	}); phaseErr != nil {
		m.logger.Error("update workflow phase", "error", phaseErr)

		return
	}

	m.emitEvent(ctx, wf.ID, "", wf.TicketID, swarm.EventPhaseComplete, wf.Phase, "")
	m.emitEvent(
		ctx,
		wf.ID,
		"",
		wf.TicketID,
		swarm.EventPhaseStarted,
		transition.NextPhase,
		"",
	)

	// Refresh workflow and spawn next session.
	updatedWf, getErr := m.db.GetSwarmWorkflow(ctx, wf.ID)
	if getErr != nil {
		m.logger.Error("get updated workflow", "error", getErr)

		return
	}

	if spawnErr := m.spawnSession(ctx, updatedWf); spawnErr != nil {
		m.logger.Error(
			"spawn next session",
			"workflow_id",
			wf.ID,
			"phase",
			transition.NextPhase,
			"error",
			spawnErr,
		)
	}
}

// captureLearnings routes to the appropriate learning capture function based
// on the session result and phase.
func (m *Manager) captureLearnings(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
	session sqlc.SwarmSession,
	result *swarm.SessionResultData,
) {
	rawDB := m.db.SQLDB()

	switch {
	case result.Result == swarm.ResultLogicFailure && session.Phase == swarm.PhasePlanReview:
		_ = swarm.CapturePlanIssue(
			ctx,
			rawDB,
			wf.ID,
			session.ID,
			wf.TicketID,
			result.Summary,
		)

	case result.Result == swarm.ResultLogicFailure && session.Phase == swarm.PhaseVerify:
		_ = swarm.CaptureCodeBug(
			ctx,
			rawDB,
			wf.ID,
			session.ID,
			wf.TicketID,
			result.Summary,
		)

	case result.Result == swarm.ResultInfraFailure || result.Result == swarm.ResultTimeout:
		_ = swarm.CaptureTerminalFailure(
			ctx,
			rawDB,
			wf.ID,
			session.ID,
			wf.TicketID,
			session.Phase,
			result.Summary,
		)
	}
}

// buildEnv assembles the CM_SWARM_* environment variable map for a session.
// Returns the env map and a cleanup function to remove temp files.
func (m *Manager) buildEnv(
	ctx context.Context,
	wf sqlc.SwarmWorkflow,
	sessionID string,
) (map[string]string, func()) {
	var cleanups []string
	cleanup := func() {
		for _, f := range cleanups {
			_ = os.Remove(f)
		}
	}

	env := map[string]string{
		"CM_SWARM_TICKET_ID":   wf.TicketID,
		"CM_SWARM_WORKFLOW_ID": wf.ID,
		"CM_SWARM_SESSION_ID":  sessionID,
		"CM_SWARM_PHASE":       string(wf.Phase),
		"CM_SWARM_ATTEMPT":     strconv.FormatInt(wf.Attempt, 10),
		"CM_SWARM_RESULT_PATH": ResultFilePath(sessionID),
		"CM_HARNESS_URL":       m.harnessURL,
	}

	if wf.BranchName.Valid {
		env["CM_SWARM_BRANCH"] = wf.BranchName.String
	}

	// Look up ticket URL.
	ticket, ticketErr := m.db.GetSwarmTicket(ctx, wf.TicketID)
	if ticketErr == nil && ticket.Url != "" {
		env["CM_SWARM_TICKET_URL"] = ticket.Url
	}

	if hookSecret := os.Getenv("CM_HOOK_SECRET"); hookSecret != "" {
		env["CM_HOOK_SECRET"] = hookSecret
	}

	if os.Getenv("CM_SWARM_DRY_RUN") == "true" {
		env["CM_SWARM_DRY_RUN"] = "true"
	}

	// Resolve handoff path from previous phase.
	handoffPath, handoffErr := swarm.ResolveHandoffPath(m.baseDir, wf.TicketID)
	if handoffErr == nil && handoffPath != "" {
		env["CM_SWARM_HANDOFF_PATH"] = handoffPath
	}

	// Build learning context and write to temp file.
	learningCtx, learningErr := swarm.GetLearningContext(
		ctx,
		m.db.SQLDB(),
		wf.TicketID,
		wf.Phase,
	)
	if learningErr == nil && learningCtx != "" {
		learningPath := LearningFilePath(sessionID)
		if writeErr := os.WriteFile(
			learningPath,
			[]byte(learningCtx),
			0o600,
		); writeErr == nil {
			env["CM_SWARM_LEARNING_CONTEXT_PATH"] = learningPath
			cleanups = append(cleanups, learningPath)
		}
	}

	return env, cleanup
}

// createTmuxSession creates a new tmux session with the given name, working
// directory, and environment variables.
func (m *Manager) createTmuxSession(
	ctx context.Context,
	name, workDir string,
	env map[string]string,
) error {
	baseArgs := []string{"new-session", "-d", "-s", name, "-c", workDir}
	args := make([]string, 0, len(baseArgs)+2*len(env))
	args = append(args, baseArgs...)

	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux new-session: %w: %s", err, out)
	}

	return nil
}

// sendSkillPrompt writes the skill invocation to a temp file and launches
// Claude Code in the tmux session with --input-file.
func (m *Manager) sendSkillPrompt(
	ctx context.Context,
	sessionName, skill, sessionID string,
) error {
	promptContent := "/" + skill

	promptPath := filepath.Join(os.TempDir(), promptFilePrefix+sessionID+".txt")
	if err := os.WriteFile(promptPath, []byte(promptContent), 0o600); err != nil {
		return fmt.Errorf("write prompt file: %w", err)
	}

	claudeCmd := fmt.Sprintf(
		"claude --dangerously-skip-permissions --input-file %s ; exit",
		promptPath,
	)

	cmd := exec.CommandContext(
		ctx,
		"tmux",
		"send-keys",
		"-t",
		sessionName,
		claudeCmd,
		"Enter",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys: %w: %s", err, out)
	}

	return nil
}

// isTmuxSessionAlive checks if a tmux session exists.
func isTmuxSessionAlive(name string) bool {
	return exec.CommandContext(context.Background(), "tmux", "has-session", "-t", name).
		Run() ==
		nil
}

// loadConfig reads the swarm config from DB, merging with DefaultConfig
// so that unset fields fall back to sensible defaults.
func (m *Manager) loadConfig(ctx context.Context) swarm.SwarmConfig {
	dbConfig, err := m.db.GetSwarmConfig(ctx)
	if err != nil {
		return swarm.DefaultConfig
	}

	config := swarm.DefaultConfig // start with defaults
	if jsonErr := json.Unmarshal([]byte(dbConfig.Config), &config); jsonErr != nil {
		return swarm.DefaultConfig
	}

	return config
}

// emitEvent inserts a swarm event record into the database.
func (m *Manager) emitEvent(
	ctx context.Context,
	workflowID, sessionID, ticketID string,
	eventType swarm.EventType,
	phase swarm.Phase,
	detail string,
) {
	// If ticketID is empty, try to resolve from workflow.
	if ticketID == "" && workflowID != "" {
		if wf, wfErr := m.db.GetSwarmWorkflow(ctx, workflowID); wfErr == nil {
			ticketID = wf.TicketID
		}
	}

	if err := m.db.CreateSwarmEvent(ctx, sqlc.CreateSwarmEventParams{
		ID:         uuid.New().String()[:8],
		WorkflowID: sql.NullString{String: workflowID, Valid: workflowID != ""},
		SessionID:  sql.NullString{String: sessionID, Valid: sessionID != ""},
		TicketID:   ticketID,
		EventType:  eventType,
		Phase:      sql.NullString{String: string(phase), Valid: phase != ""},
		Detail:     sql.NullString{String: detail, Valid: detail != ""},
	}); err != nil {
		m.logger.Error("emit swarm event", "type", eventType, "error", err)
	}
}

// closeJSONLWriter closes and removes the JSONL writer for a session.
func (m *Manager) closeJSONLWriter(sessionID string) {
	m.jsonlMu.Lock()
	defer m.jsonlMu.Unlock()

	if jw, ok := m.jsonlWriters[sessionID]; ok {
		_ = jw.Close()
		delete(m.jsonlWriters, sessionID)
	}
}

// WriteJSONLEvent writes an event to the JSONL log for a session.
func (m *Manager) WriteJSONLEvent(sessionID string, event map[string]any) {
	m.jsonlMu.RLock()
	jw, ok := m.jsonlWriters[sessionID]
	m.jsonlMu.RUnlock()

	if ok {
		_ = jw.Write(event)
	}
}

// SignalStart notifies the watcher that a session's Claude Code process has started.
func (m *Manager) SignalStart(sessionID string) {
	m.startReg.Signal(sessionID)
}

// SignalCompletion notifies the watcher that a session has completed.
func (m *Manager) SignalCompletion(sessionID string, result SessionResult) {
	m.completionReg.Signal(sessionID, result)
}

// IncrementContextPressure records a compaction event for the session and
// returns the updated count.
func (m *Manager) IncrementContextPressure(sessionID string) int {
	return m.ctxPressure.Increment(sessionID)
}

// GetContextPressure returns the current compact count for a session.
func (m *Manager) GetContextPressure(sessionID string) int {
	return m.ctxPressure.Get(sessionID)
}

// LogsDir returns the logs directory path.
func (m *Manager) LogsDir() string {
	return m.logsDir
}

// ResultFilePath returns the temp file path for a session's RESULT output.
func ResultFilePath(sessionID string) string {
	return filepath.Join(os.TempDir(), resultFilePrefix+sessionID+".txt")
}

// LearningFilePath returns the temp file path for learning context.
func LearningFilePath(sessionID string) string {
	return filepath.Join(os.TempDir(), learningFilePrefix+sessionID+".md")
}

// GetWorkflow returns a workflow and its latest session for status queries.
func (m *Manager) GetWorkflow(
	ctx context.Context,
	workflowID string,
) (sqlc.SwarmWorkflow, *sqlc.SwarmSession, error) {
	wf, err := m.db.GetSwarmWorkflow(ctx, workflowID)
	if err != nil {
		return sqlc.SwarmWorkflow{}, nil, fmt.Errorf("get workflow: %w", err)
	}

	session, sessionErr := m.db.GetLatestSwarmSession(ctx, workflowID)
	if sessionErr != nil {
		if errors.Is(sessionErr, sql.ErrNoRows) {
			return wf, nil, nil
		}

		return wf, nil, fmt.Errorf("get latest session: %w", sessionErr)
	}

	return wf, &session, nil
}

// SessionName returns the tmux session name for a ticket and phase.
func SessionName(ticketID string, phase swarm.Phase) string {
	return fmt.Sprintf("cm-swarm-%s-%s", ticketID, phase)
}

// ListActiveSessions returns tmux session names matching cm-swarm-*.
func ListActiveSessions(ctx context.Context) []string {
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}").
		Output()
	if err != nil {
		return nil
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, "cm-swarm-") {
			sessions = append(sessions, line)
		}
	}

	return sessions
}
