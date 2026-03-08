package swarmorch

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"creative-mode/harness/internal/db"
	"creative-mode/harness/internal/db/sqlc"
	"creative-mode/harness/internal/events"
)

// Agent subprocess limits and buffer sizes.
const (
	toolCallWarnThreshold = 50
	toolCallKillThreshold = 100
	scannerBufferSize     = 1 << 20 // 1 MB for large artifact JSON
	truncateLogLineLen    = 200
	truncateSpanNameLen   = 100
	truncateAnswerLen     = 500
)

// Grep search parameters.
const (
	grepMaxFileResults = 10
)

// runAgentParams holds the parameters for a single agent invocation.
type runAgentParams struct {
	script       string
	taskID       string
	parentSpanID string
	input        any
	systemPrompt string
	repoRoot     string
	runner       AgentRunner
	database     *db.DB
	eventBus     *events.EventBus
	logger       *slog.Logger
	heartbeat    func(details any) // Temporal heartbeat callback
}

// runAgentResult contains the raw artifact JSON returned by the agent.
type runAgentResult struct {
	ArtifactJSON json.RawMessage
}

// runAgent spawns a JS agent subprocess, manages the bidirectional JSONL
// protocol, creates spans for tool calls and questions, and returns the
// agent's artifact on success.
func runAgent(ctx context.Context, p runAgentParams) (*runAgentResult, error) {
	// Create the agent-level span.
	agentSpanID := uuid.NewString()
	now := time.Now().UTC()
	createSpan(ctx, p.database, p.eventBus, SpanParams{
		ID:           agentSpanID,
		TaskID:       p.taskID,
		ParentSpanID: p.parentSpanID,
		SpanType:     "agent",
		Name:         p.script,
		InputJSON:    marshal(p.input),
		StartedAt:    now.Format(time.RFC3339),
	})

	inputJSON, err := json.Marshal(p.input)
	if err != nil {
		failSpan(
			ctx,
			p.database,
			p.eventBus,
			agentSpanID,
			now,
			fmt.Errorf("marshal input: %w", err),
		)
		return nil, fmt.Errorf("marshal agent input: %w", err)
	}

	env := make([]string, 0, len(os.Environ())+1)
	env = append(env, os.Environ()...)
	env = append(env, "NODE_NO_WARNINGS=1")
	cmd := p.runner.BuildCommand(ctx, p.script, env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		failSpan(ctx, p.database, p.eventBus, agentSpanID, now, err)
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		failSpan(ctx, p.database, p.eventBus, agentSpanID, now, err)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Capture stderr for diagnostics.
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if startErr := cmd.Start(); startErr != nil {
		failSpan(ctx, p.database, p.eventBus, agentSpanID, now, startErr)
		return nil, fmt.Errorf("start agent %s: %w", p.script, startErr)
	}

	// Send the start message.
	startMsg := StartMessage{
		Type:         "start",
		Task:         inputJSON,
		SystemPrompt: p.systemPrompt,
	}
	if writeErr := writeJSONLine(stdin, startMsg); writeErr != nil {
		failSpan(ctx, p.database, p.eventBus, agentSpanID, now, writeErr)
		return nil, fmt.Errorf("write start message: %w", writeErr)
	}

	// Read JSONL from stdout.
	result, loopErr := readAgentLoop(ctx, agentLoopParams{
		stdout:      stdout,
		stdin:       stdin,
		agentSpanID: agentSpanID,
		taskID:      p.taskID,
		repoRoot:    p.repoRoot,
		database:    p.database,
		eventBus:    p.eventBus,
		logger:      p.logger,
		heartbeat:   p.heartbeat,
		startedAt:   now,
		cmd:         cmd,
	})

	// Wait for process exit.
	waitErr := cmd.Wait()

	if loopErr != nil {
		failSpan(ctx, p.database, p.eventBus, agentSpanID, now, loopErr)
		failOrphanedChildSpans(ctx, p.database, p.eventBus, p.taskID, agentSpanID)
		return nil, fmt.Errorf("agent %s loop: %w", p.script, loopErr)
	}

	if waitErr != nil && result == nil {
		stderr := truncate(stderrBuf.String(), maxJSONLen)
		err := fmt.Errorf("agent %s exited: %w (stderr: %s)", p.script, waitErr, stderr)
		failSpan(ctx, p.database, p.eventBus, agentSpanID, now, err)
		failOrphanedChildSpans(ctx, p.database, p.eventBus, p.taskID, agentSpanID)
		return nil, err
	}

	// Complete the agent span.
	completeSpan(ctx, p.database, p.eventBus, agentSpanID, now,
		truncateJSON(result.ArtifactJSON))

	return result, nil
}

// agentLoopParams holds the parameters for the main agent read loop.
type agentLoopParams struct {
	stdout      io.Reader
	stdin       io.WriteCloser
	agentSpanID string
	taskID      string
	repoRoot    string
	database    *db.DB
	eventBus    *events.EventBus
	logger      *slog.Logger
	heartbeat   func(details any)
	startedAt   time.Time
	cmd         *exec.Cmd
}

// readAgentLoop reads JSONL messages from the agent's stdout and dispatches
// them to the appropriate handler.
func readAgentLoop(
	ctx context.Context,
	p agentLoopParams,
) (*runAgentResult, error) {
	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, scannerBufferSize), scannerBufferSize)

	toolCallCount := 0
	// Track open tool call spans for completion on tool_execution_end.
	openToolSpans := make(map[string]toolSpanInfo)

	for scanner.Scan() {
		// Heartbeat on each line read.
		if p.heartbeat != nil {
			p.heartbeat("reading agent output")
		}

		var msg AgentMessage
		if unmarshalErr := json.Unmarshal(scanner.Bytes(), &msg); unmarshalErr != nil {
			p.logger.Warn("invalid agent JSONL", "error", unmarshalErr,
				"line", truncate(scanner.Text(), truncateLogLineLen))
			continue
		}

		var loopErr error
		switch msg.Type {
		case "event":
			toolCallCount, loopErr = handleToolEvent(
				ctx, p, msg, toolCallCount, openToolSpans,
			)

		case "question":
			loopErr = handleQuestion(ctx, p, msg)

		case "result":
			return &runAgentResult{ArtifactJSON: msg.Data}, nil
		}
		if loopErr != nil {
			return nil, loopErr
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return nil, errors.New("agent exited without result")
}

// toolSpanInfo tracks an open tool call span.
type toolSpanInfo struct {
	spanID    string
	startedAt time.Time
}

// handleToolEvent processes tool_execution_start and tool_execution_end events.
func handleToolEvent(
	ctx context.Context,
	p agentLoopParams,
	msg AgentMessage,
	toolCallCount int,
	openToolSpans map[string]toolSpanInfo,
) (int, error) {
	switch msg.Event {
	case "tool_execution_start":
		toolCallCount++

		if toolCallCount == toolCallWarnThreshold {
			p.logger.Warn("agent approaching tool call limit",
				"count", toolCallCount,
				"taskID", p.taskID)
		}
		if toolCallCount >= toolCallKillThreshold {
			return toolCallCount, fmt.Errorf(
				"agent exceeded %d tool calls", toolCallKillThreshold)
		}

		spanID := uuid.NewString()
		now := time.Now().UTC()
		openToolSpans[msg.ToolCallID] = toolSpanInfo{
			spanID:    spanID,
			startedAt: now,
		}
		createSpan(ctx, p.database, p.eventBus, SpanParams{
			ID:           spanID,
			TaskID:       p.taskID,
			ParentSpanID: p.agentSpanID,
			SpanType:     "tool_call",
			Name:         msg.Tool,
			InputJSON:    msg.Data,
			StartedAt:    now.Format(time.RFC3339),
		})

	case "tool_execution_end":
		if info, ok := openToolSpans[msg.ToolCallID]; ok {
			completeSpan(ctx, p.database, p.eventBus, info.spanID,
				info.startedAt, truncateJSON(msg.Data))
			delete(openToolSpans, msg.ToolCallID)
		}
	}

	return toolCallCount, nil
}

// handleQuestion processes a question from the agent and sends an answer.
func handleQuestion(
	ctx context.Context,
	p agentLoopParams,
	msg AgentMessage,
) error {
	spanID := uuid.NewString()
	now := time.Now().UTC()
	createSpan(ctx, p.database, p.eventBus, SpanParams{
		ID:           spanID,
		TaskID:       p.taskID,
		ParentSpanID: p.agentSpanID,
		SpanType:     "question",
		Name:         truncate(msg.Text, truncateSpanNameLen),
		InputJSON:    marshal(map[string]string{"question": msg.Text}),
		StartedAt:    now.Format(time.RFC3339),
	})

	answer := answerQuestion(p.repoRoot, msg.Text, p.logger)

	completeSpan(
		ctx,
		p.database,
		p.eventBus,
		spanID,
		now,
		truncateJSON(
			map[string]string{"answer": truncate(answer, truncateAnswerLen)},
		),
	)

	answerMsg := AnswerMessage{
		Type: "answer",
		ID:   msg.ID,
		Text: answer,
	}
	if err := writeJSONLine(p.stdin, answerMsg); err != nil {
		return fmt.Errorf("write answer: %w", err)
	}
	return nil
}

// answerQuestion builds context for an agent's question by grepping
// the skills directory and harness source code for relevant keywords.
func answerQuestion(repoRoot, question string, logger *slog.Logger) string {
	keywords := extractKeywords(question)
	if len(keywords) == 0 {
		logger.Debug("no keywords extracted from agent question",
			"question", truncate(question, truncateSpanNameLen))
		return "No relevant context found for your question. " +
			"Try using read_file or search_context tools to find what you need."
	}

	var sections []string
	seen := make(map[string]bool)

	skillsDir := filepath.Join(repoRoot, "harness", "agents", "skills")
	harnessDir := filepath.Join(repoRoot, "harness")

	for _, kw := range keywords {
		// Search skills directory for full matches.
		skillFiles := grepFiles(skillsDir, kw, grepMaxFileResults)
		for _, f := range skillFiles {
			if seen[f] {
				continue
			}
			seen[f] = true
			content, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			relPath, _ := filepath.Rel(repoRoot, f)
			sections = append(sections,
				fmt.Sprintf("=== %s ===\n%s", relPath, string(content)))
		}

		// Search harness source for truncated matches.
		srcFiles := grepFiles(harnessDir, kw, maxFilesPerKeyword)
		for _, f := range srcFiles {
			if seen[f] {
				continue
			}
			// Skip non-source files.
			ext := filepath.Ext(f)
			if ext != ".go" && ext != ".sql" && ext != ".templ" {
				continue
			}
			seen[f] = true
			content, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			relPath, _ := filepath.Rel(repoRoot, f)
			sections = append(sections,
				fmt.Sprintf("=== %s ===\n%s",
					relPath, truncate(string(content), maxFileContentLen)))
		}
	}

	if len(sections) == 0 {
		return "No relevant files found for keywords: " +
			strings.Join(keywords, ", ") +
			". Try using read_file or search_context tools."
	}

	return strings.Join(sections, "\n\n")
}

// grepFiles runs a case-insensitive recursive grep for pattern in dir,
// returning up to maxResults matching file paths.
func grepFiles(dir, pattern string, maxResults int) []string {
	if _, err := os.Stat(dir); err != nil {
		return nil
	}

	const grepTimeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(
		context.Background(),
		grepTimeout,
	)
	defer cancel()
	cmd := exec.CommandContext(ctx, "grep", "-ril", "--include=*.go",
		"--include=*.md", "--include=*.sql", "--include=*.templ",
		pattern, dir)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > maxResults {
		lines = lines[:maxResults]
	}

	var result []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

// writeJSONLine marshals v to JSON and writes it as a single line to w.
func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal JSONL: %w", err)
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	if err != nil {
		return fmt.Errorf("write JSONL: %w", err)
	}
	return nil
}

// --- Span helpers ---

// createSpan inserts a new span into the DB and publishes an event.
func createSpan(
	ctx context.Context,
	database *db.DB,
	eventBus *events.EventBus,
	p SpanParams,
) {
	err := database.CreateSwarmSpan(ctx, sqlc.CreateSwarmSpanParams{
		ID:           p.ID,
		TaskID:       p.TaskID,
		ParentSpanID: toNullString(p.ParentSpanID),
		SpanType:     p.SpanType,
		Name:         p.Name,
		Status:       "running",
		InputJSON:    sqlNullJSON(p.InputJSON),
		StartedAt:    p.StartedAt,
	})
	if err != nil {
		return // best-effort
	}

	if eventBus != nil {
		eventBus.Publish("swarm", map[string]any{
			"event":  events.EventSpanStarted,
			"spanID": p.ID,
			"taskID": p.TaskID,
			"type":   p.SpanType,
			"name":   p.Name,
		})
	}
}

// completeSpan marks a span as completed in the DB and publishes an event.
func completeSpan(
	ctx context.Context,
	database *db.DB,
	eventBus *events.EventBus,
	spanID string,
	startedAt time.Time,
	outputJSON string,
) {
	now := time.Now().UTC()
	durationMs := now.Sub(startedAt).Milliseconds()

	_ = database.CompleteSwarmSpan(ctx, sqlc.CompleteSwarmSpanParams{
		OutputJSON: toNullString(outputJSON),
		EndedAt:    toNullString(now.Format(time.RFC3339)),
		DurationMs: sql.NullInt64{Int64: durationMs, Valid: true},
		ID:         spanID,
	})

	if eventBus != nil {
		eventBus.Publish("swarm", map[string]any{
			"event":      events.EventSpanCompleted,
			"spanID":     spanID,
			"durationMs": durationMs,
		})
	}
}

// failSpan marks a span as failed in the DB and publishes an event.
func failSpan(
	ctx context.Context,
	database *db.DB,
	eventBus *events.EventBus,
	spanID string,
	startedAt time.Time,
	spanErr error,
) {
	now := time.Now().UTC()
	durationMs := now.Sub(startedAt).Milliseconds()

	_ = database.FailSwarmSpan(ctx, sqlc.FailSwarmSpanParams{
		ErrorMessage: toNullString(spanErr.Error()),
		EndedAt:      toNullString(now.Format(time.RFC3339)),
		DurationMs:   sql.NullInt64{Int64: durationMs, Valid: true},
		ID:           spanID,
	})

	if eventBus != nil {
		eventBus.Publish("swarm", map[string]any{
			"event":  events.EventSpanFailed,
			"spanID": spanID,
			"error":  spanErr.Error(),
		})
	}
}

// failOrphanedChildSpans fails all running child spans of a parent span.
// Called when an agent crashes to clean up incomplete tool call spans.
func failOrphanedChildSpans(
	ctx context.Context,
	database *db.DB,
	eventBus *events.EventBus,
	taskID string,
	parentSpanID string,
) {
	spans, err := database.GetSwarmSpansByTask(ctx, taskID)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	for _, s := range spans {
		if s.ParentSpanID.Valid && s.ParentSpanID.String == parentSpanID &&
			s.Status == "running" {
			failSpan(ctx, database, eventBus, s.ID, now,
				fmt.Errorf("orphaned: parent span %s failed", parentSpanID))
		}
	}
}

// sqlNullJSON converts a json.RawMessage to sql.NullString.
func sqlNullJSON(data json.RawMessage) sql.NullString {
	if len(data) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
}
