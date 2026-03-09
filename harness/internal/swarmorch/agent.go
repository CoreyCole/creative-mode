package swarmorch

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
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
	script         string
	taskID         string
	parentSpanID   string
	input          any
	systemPrompt   string
	projectContext string // deterministic project docs prepended to system prompt
	repoRoot       string
	outputPath     string // file path where agent writes its output
	runner         AgentRunner
	database       *db.DB
	eventBus       *events.EventBus
	logger         *slog.Logger
	heartbeat      func(details any) // Temporal heartbeat callback
	toolCallLimit  int               // 0 means use default (100)
	agentConfig    *AgentConfig      // optional config sent to agent
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
	createSpan(ctx, p.database, p.eventBus, p.logger, SpanParams{
		ID:           agentSpanID,
		TaskID:       p.taskID,
		ParentSpanID: p.parentSpanID,
		SpanType:     sqlc.SpanTypeAgent,
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
			p.logger,
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
		failSpan(ctx, p.database, p.eventBus, p.logger, agentSpanID, now, err)
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		failSpan(ctx, p.database, p.eventBus, p.logger, agentSpanID, now, err)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Capture stderr for diagnostics.
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if startErr := cmd.Start(); startErr != nil {
		failSpan(ctx, p.database, p.eventBus, p.logger, agentSpanID, now, startErr)
		return nil, fmt.Errorf("start agent %s: %w", p.script, startErr)
	}

	// Send the start message.
	startMsg := StartMessage{
		Type:           "start",
		Task:           inputJSON,
		SystemPrompt:   p.systemPrompt,
		ProjectContext: p.projectContext,
		Config:         p.agentConfig,
	}
	if writeErr := writeJSONLine(stdin, startMsg); writeErr != nil {
		failSpan(ctx, p.database, p.eventBus, p.logger, agentSpanID, now, writeErr)
		return nil, fmt.Errorf("write start message: %w", writeErr)
	}

	// Resolve tool call limit (0 means default).
	toolLimit := p.toolCallLimit
	if toolLimit <= 0 {
		toolLimit = toolCallKillThreshold
	}
	warnAt := toolLimit / 2 //nolint:mnd // warn at half the limit

	// Read JSONL from stdout.
	loopParams := agentLoopParams{
		stdout:        stdout,
		stdin:         stdin,
		agentSpanID:   agentSpanID,
		taskID:        p.taskID,
		repoRoot:      p.repoRoot,
		database:      p.database,
		eventBus:      p.eventBus,
		logger:        p.logger,
		heartbeat:     p.heartbeat,
		startedAt:     now,
		cmd:           cmd,
		toolCallLimit: toolLimit,
		toolCallWarn:  warnAt,
	}
	loopErr := readAgentLoop(ctx, &loopParams)

	// Wait for process exit.
	waitErr := cmd.Wait()
	stderr := strings.TrimSpace(stderrBuf.String())

	if loopErr != nil {
		failSpanWithMetadata(
			ctx,
			p.database,
			p.eventBus,
			p.logger,
			agentSpanID,
			now,
			loopErr,
			&loopParams,
			stderr,
		)
		failOrphanedChildSpans(
			ctx,
			p.database,
			p.eventBus,
			p.logger,
			p.taskID,
			agentSpanID,
		)
		return nil, fmt.Errorf("agent %s loop: %w", p.script, loopErr)
	}

	if waitErr != nil {
		errStr := truncate(stderr, maxJSONLen)
		err := fmt.Errorf("agent %s exited: %w (stderr: %s)", p.script, waitErr, errStr)
		failSpanWithMetadata(
			ctx,
			p.database,
			p.eventBus,
			p.logger,
			agentSpanID,
			now,
			err,
			&loopParams,
			stderr,
		)
		failOrphanedChildSpans(
			ctx,
			p.database,
			p.eventBus,
			p.logger,
			p.taskID,
			agentSpanID,
		)
		return nil, err
	}

	// Read output file written by the agent.
	outputData, readErr := os.ReadFile(p.outputPath)
	if readErr != nil {
		err := fmt.Errorf(
			"agent %s output file missing (%s): %w",
			p.script,
			p.outputPath,
			readErr,
		)
		failSpanWithMetadata(ctx, p.database, p.eventBus, p.logger, agentSpanID, now,
			err, &loopParams, stderr)
		failOrphanedChildSpans(
			ctx,
			p.database,
			p.eventBus,
			p.logger,
			p.taskID,
			agentSpanID,
		)
		return nil, err
	}

	result := &runAgentResult{ArtifactJSON: outputData}

	// Complete the agent span with aggregate metadata.
	agentMeta := SpanMetadata{
		TotalInputTokens:  loopParams.totalInputTokens,
		TotalOutputTokens: loopParams.totalOutputTokens,
		TotalCost:         loopParams.totalCost,
		ToolCallCount:     loopParams.toolCallCount,
		LLMCallCount:      loopParams.llmCallCount,
	}
	completeSpanWithMetadata(ctx, p.database, p.eventBus, p.logger, agentSpanID, now,
		truncate(string(outputData), maxJSONLen), &agentMeta)

	return result, nil
}

// agentLoopParams holds the parameters for the main agent read loop.
type agentLoopParams struct {
	stdout        io.Reader
	stdin         io.WriteCloser
	agentSpanID   string
	taskID        string
	repoRoot      string
	database      *db.DB
	eventBus      *events.EventBus
	logger        *slog.Logger
	heartbeat     func(details any)
	startedAt     time.Time
	cmd           *exec.Cmd
	toolCallLimit int // kill threshold
	toolCallWarn  int // warning threshold
	// Running totals for agent-level aggregate metadata.
	totalInputTokens  int
	totalOutputTokens int
	totalCost         float64
	toolCallCount     int
	llmCallCount      int
}

// readAgentLoop reads JSONL messages from the agent's stdout and dispatches
// them to the appropriate handler. EOF is treated as normal — the agent
// writes its output to a file, so we read it after the process exits.
func readAgentLoop(
	ctx context.Context,
	p *agentLoopParams,
) error {
	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, scannerBufferSize), scannerBufferSize)

	toolCallCount := 0
	// Track open tool call spans for completion on tool_execution_end.
	openToolSpans := make(map[string]toolSpanInfo)
	// Track the current inference span (one at a time).
	var inferenceSpan *toolSpanInfo

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
			toolCallCount, inferenceSpan, loopErr = handleToolEvent(
				ctx, p, msg, toolCallCount, openToolSpans, inferenceSpan,
			)

		case "question":
			loopErr = handleQuestion(ctx, *p, msg)

		case "heartbeat":
			// No-op — already heartbeated above on line read.
		}
		if loopErr != nil {
			return loopErr
		}
	}

	// Close any open inference span after EOF.
	if inferenceSpan != nil {
		completeSpan(ctx, p.database, p.eventBus, p.logger, inferenceSpan.spanID,
			inferenceSpan.startedAt, "")
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil // EOF is normal — agent wrote output to file
}

// toolSpanInfo tracks an open tool call span.
type toolSpanInfo struct {
	spanID    string
	startedAt time.Time
}

// handleToolEvent processes tool_execution_start/end and inference_start/end events.
func handleToolEvent(
	ctx context.Context,
	p *agentLoopParams,
	msg AgentMessage,
	toolCallCount int,
	openToolSpans map[string]toolSpanInfo,
	inferenceSpan *toolSpanInfo,
) (int, *toolSpanInfo, error) {
	switch msg.Event {
	case AgentEventToolStart:
		toolCallCount++
		p.toolCallCount++

		if toolCallCount == p.toolCallWarn {
			p.logger.Warn("agent approaching tool call limit",
				"count", toolCallCount,
				"limit", p.toolCallLimit,
				"taskID", p.taskID)
		}
		if toolCallCount >= p.toolCallLimit {
			return toolCallCount, inferenceSpan, fmt.Errorf(
				"agent exceeded %d tool calls", p.toolCallLimit)
		}

		spanID := uuid.NewString()
		now := time.Now().UTC()
		openToolSpans[msg.ToolCallID] = toolSpanInfo{
			spanID:    spanID,
			startedAt: now,
		}
		createSpan(ctx, p.database, p.eventBus, p.logger, SpanParams{
			ID:           spanID,
			TaskID:       p.taskID,
			ParentSpanID: p.agentSpanID,
			SpanType:     sqlc.SpanTypeToolCall,
			Name:         msg.Tool,
			InputJSON:    msg.Data,
			StartedAt:    now.Format(time.RFC3339),
		})

	case AgentEventToolEnd:
		if info, ok := openToolSpans[msg.ToolCallID]; ok {
			completeSpan(ctx, p.database, p.eventBus, p.logger, info.spanID,
				info.startedAt, truncateJSON(msg.Data))
			delete(openToolSpans, msg.ToolCallID)
		}

	case AgentEventInferenceStart:
		// Close any previous inference span that wasn't ended.
		if inferenceSpan != nil {
			completeSpan(ctx, p.database, p.eventBus, p.logger, inferenceSpan.spanID,
				inferenceSpan.startedAt, "")
		}
		spanID := uuid.NewString()
		now := time.Now().UTC()
		inferenceSpan = &toolSpanInfo{spanID: spanID, startedAt: now}

		// Extract model from inference_start data.
		name := "llm"
		if len(msg.Data) > 0 {
			var meta SpanMetadata
			if json.Unmarshal(msg.Data, &meta) == nil && meta.Model != "" {
				name = meta.Model
			}
		}

		createSpan(ctx, p.database, p.eventBus, p.logger, SpanParams{
			ID:           spanID,
			TaskID:       p.taskID,
			ParentSpanID: p.agentSpanID,
			SpanType:     sqlc.SpanTypeLLMCall,
			Name:         name,
			InputJSON:    msg.Data,
			StartedAt:    now.Format(time.RFC3339),
		})

	case AgentEventInferenceEnd:
		if inferenceSpan == nil {
			break
		}
		p.llmCallCount++

		// Parse metadata from inference_end data for token tracking.
		var meta *SpanMetadata
		if len(msg.Data) > 0 {
			var m SpanMetadata
			if json.Unmarshal(msg.Data, &m) == nil {
				meta = &m
				if m.Usage != nil {
					p.totalInputTokens += m.Usage.Input
					p.totalOutputTokens += m.Usage.Output
					p.totalCost += m.Usage.Cost.Total
				}
			}
		}

		if meta != nil {
			completeSpanWithMetadata(ctx, p.database, p.eventBus, p.logger,
				inferenceSpan.spanID, inferenceSpan.startedAt,
				truncateJSON(msg.Data), meta)
		} else {
			completeSpan(ctx, p.database, p.eventBus, p.logger, inferenceSpan.spanID,
				inferenceSpan.startedAt, truncateJSON(msg.Data))
		}
		inferenceSpan = nil
	default:
		// Unknown event type — ignore.
	}

	return toolCallCount, inferenceSpan, nil
}

// handleQuestion processes a question from the agent and sends an answer.
func handleQuestion(
	ctx context.Context,
	p agentLoopParams,
	msg AgentMessage,
) error {
	spanID := uuid.NewString()
	now := time.Now().UTC()
	createSpan(ctx, p.database, p.eventBus, p.logger, SpanParams{
		ID:           spanID,
		TaskID:       p.taskID,
		ParentSpanID: p.agentSpanID,
		SpanType:     sqlc.SpanTypeQuestion,
		Name:         truncate(msg.Text, truncateSpanNameLen),
		InputJSON:    marshal(map[string]string{"question": msg.Text}),
		StartedAt:    now.Format(time.RFC3339),
	})

	answer := answerQuestion(p.repoRoot, msg.Text, p.logger)

	completeSpan(
		ctx,
		p.database,
		p.eventBus,
		p.logger,
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
	templatesDir := filepath.Join(repoRoot, "templates")

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

		// Search harness and templates source for truncated matches.
		for _, dir := range []string{harnessDir, templatesDir} {
			srcFiles := grepFiles(dir, kw, maxFilesPerKeyword)
			for _, f := range srcFiles {
				if seen[f] {
					continue
				}
				// Skip non-source files.
				ext := filepath.Ext(f)
				if ext != ".go" && ext != ".sql" && ext != ".templ" && ext != ".rs" {
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
		"--include=*.rs",
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
	logger *slog.Logger,
	p SpanParams,
) {
	if err := database.CreateSwarmSpan(ctx, sqlc.CreateSwarmSpanParams{
		ID:           p.ID,
		TaskID:       p.TaskID,
		ParentSpanID: toNullString(p.ParentSpanID),
		SpanType:     p.SpanType,
		Name:         p.Name,
		Status:       sqlc.SpanStatusRunning,
		InputJSON:    sqlNullJSON(p.InputJSON),
		StartedAt:    p.StartedAt,
	}); err != nil {
		logger.Warn("failed to create span", "spanID", p.ID, "name", p.Name, "error", err)
		return
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
	logger *slog.Logger,
	spanID string,
	startedAt time.Time,
	outputJSON string,
) {
	now := time.Now().UTC()
	durationMs := now.Sub(startedAt).Milliseconds()

	if err := database.CompleteSwarmSpan(ctx, sqlc.CompleteSwarmSpanParams{
		OutputJSON: toNullString(outputJSON),
		EndedAt:    toNullString(now.Format(time.RFC3339)),
		DurationMs: sql.NullInt64{Int64: durationMs, Valid: true},
		ID:         spanID,
	}); err != nil {
		logger.Warn("failed to complete span", "spanID", spanID, "error", err)
		return
	}

	if eventBus != nil {
		eventBus.Publish("swarm", map[string]any{
			"event":      events.EventSpanCompleted,
			"spanID":     spanID,
			"durationMs": durationMs,
		})
	}
}

// completeSpanWithMetadata marks a span as completed with metadata_json.
func completeSpanWithMetadata(
	ctx context.Context,
	database *db.DB,
	eventBus *events.EventBus,
	logger *slog.Logger,
	spanID string,
	startedAt time.Time,
	outputJSON string,
	meta *SpanMetadata,
) {
	now := time.Now().UTC()
	durationMs := now.Sub(startedAt).Milliseconds()

	metaJSON := ""
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			metaJSON = string(b)
		}
	}

	if err := database.CompleteSwarmSpanWithMetadata(
		ctx,
		sqlc.CompleteSwarmSpanWithMetadataParams{
			OutputJSON:   toNullString(outputJSON),
			EndedAt:      toNullString(now.Format(time.RFC3339)),
			DurationMs:   sql.NullInt64{Int64: durationMs, Valid: true},
			MetadataJSON: toNullString(metaJSON),
			ID:           spanID,
		},
	); err != nil {
		logger.Warn(
			"failed to complete span with metadata",
			"spanID",
			spanID,
			"error",
			err,
		)
		return
	}

	if eventBus != nil {
		eventBus.Publish("swarm", map[string]any{
			"event":      events.EventSpanCompleted,
			"spanID":     spanID,
			"durationMs": durationMs,
		})
	}
}

// failSpanWithMetadata marks a span as failed and writes aggregate metadata + stderr.
func failSpanWithMetadata(
	ctx context.Context,
	database *db.DB,
	eventBus *events.EventBus,
	logger *slog.Logger,
	spanID string,
	startedAt time.Time,
	spanErr error,
	lp *agentLoopParams,
	stderr string,
) {
	meta := SpanMetadata{
		TotalInputTokens:  lp.totalInputTokens,
		TotalOutputTokens: lp.totalOutputTokens,
		TotalCost:         lp.totalCost,
		ToolCallCount:     lp.toolCallCount,
		LLMCallCount:      lp.llmCallCount,
		Stderr:            truncate(stderr, maxJSONLen),
	}

	now := time.Now().UTC()
	durationMs := now.Sub(startedAt).Milliseconds()

	metaJSON := ""
	if b, err := json.Marshal(meta); err == nil {
		metaJSON = string(b)
	}

	if err := database.FailSwarmSpanWithMetadata(
		ctx,
		sqlc.FailSwarmSpanWithMetadataParams{
			ErrorMessage: toNullString(spanErr.Error()),
			EndedAt:      toNullString(now.Format(time.RFC3339)),
			DurationMs:   sql.NullInt64{Int64: durationMs, Valid: true},
			MetadataJSON: toNullString(metaJSON),
			ID:           spanID,
		},
	); err != nil {
		logger.Warn("failed to fail span with metadata", "spanID", spanID, "error", err)
		return
	}

	if eventBus != nil {
		eventBus.Publish("swarm", map[string]any{
			"event":  events.EventSpanFailed,
			"spanID": spanID,
			"error":  spanErr.Error(),
		})
	}
}

// failSpan marks a span as failed in the DB and publishes an event.
func failSpan(
	ctx context.Context,
	database *db.DB,
	eventBus *events.EventBus,
	logger *slog.Logger,
	spanID string,
	startedAt time.Time,
	spanErr error,
) {
	now := time.Now().UTC()
	durationMs := now.Sub(startedAt).Milliseconds()

	if err := database.FailSwarmSpan(ctx, sqlc.FailSwarmSpanParams{
		ErrorMessage: toNullString(spanErr.Error()),
		EndedAt:      toNullString(now.Format(time.RFC3339)),
		DurationMs:   sql.NullInt64{Int64: durationMs, Valid: true},
		ID:           spanID,
	}); err != nil {
		logger.Warn("failed to fail span", "spanID", spanID, "error", err)
		return
	}

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
	logger *slog.Logger,
	taskID string,
	parentSpanID string,
) {
	spans, err := database.GetSwarmSpansByTask(ctx, taskID)
	if err != nil {
		logger.Warn(
			"failed to query spans for orphan cleanup",
			"taskID",
			taskID,
			"error",
			err,
		)
		return
	}

	now := time.Now().UTC()
	for _, s := range spans {
		if s.ParentSpanID.Valid && s.ParentSpanID.String == parentSpanID &&
			s.Status == sqlc.SpanStatusRunning {
			failSpan(ctx, database, eventBus, logger, s.ID, now,
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
