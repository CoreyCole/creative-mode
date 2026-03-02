package server

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/events"
	"creative-mode/harness/internal/swarm"
	"creative-mode/harness/internal/swarmorch"
)

const contextPressureThreshold = 2

// hookPayload is the common subset of fields Claude Code sends to all hooks.
type hookPayload struct {
	SessionID string `json:"session_id"` //nolint:tagliatelle // Claude Code hook API format
	CWD       string `json:"cwd"`
}

// preToolUsePayload extends hookPayload with tool-specific fields.
type preToolUsePayload struct {
	hookPayload
	ToolName  string         `json:"tool_name"`  //nolint:tagliatelle // Claude Code hook API format
	ToolInput map[string]any `json:"tool_input"` //nolint:tagliatelle // Claude Code hook API format
}

// postToolUsePayload extends hookPayload with tool and response fields.
type postToolUsePayload struct {
	hookPayload
	ToolName     string         `json:"tool_name"`     //nolint:tagliatelle // Claude Code hook API format
	ToolInput    map[string]any `json:"tool_input"`    //nolint:tagliatelle // Claude Code hook API format
	ToolResponse map[string]any `json:"tool_response"` //nolint:tagliatelle // Claude Code hook API format
}

// preToolUseResponse is the JSON response to deny or allow a tool call.
type preToolUseResponse struct {
	HookSpecificOutput *preToolUseOutput `json:"hookSpecificOutput,omitempty"`
}

type preToolUseOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// swarmSessionID extracts the session ID from the X-Swarm-Session header
// (set by WriteHooksConfig), falling back to the JSON payload's session_id.
func swarmSessionID(c echo.Context, payload *hookPayload) string {
	if id := c.Request().Header.Get("X-Swarm-Session"); id != "" {
		return id
	}

	return payload.SessionID
}

// handleSwarmHookSessionStarted signals the StartRegistry that a Claude Code
// session has begun.
func (s *Server) handleSwarmHookSessionStarted(c echo.Context) error {
	if s.SwarmManager == nil {
		return c.NoContent(http.StatusNoContent)
	}

	var payload hookPayload
	_ = json.NewDecoder(c.Request().Body).Decode(&payload)

	sessionID := swarmSessionID(c, &payload)
	if sessionID == "" {
		return c.NoContent(http.StatusNoContent)
	}

	s.SwarmManager.SignalStart(sessionID)
	s.SwarmManager.WriteJSONLEvent(sessionID, map[string]any{
		"event":      "session_started",
		"session_id": sessionID,
	})

	return c.NoContent(http.StatusNoContent)
}

// handleSwarmHookPreToolUse checks Bash commands against the deny list.
func (s *Server) handleSwarmHookPreToolUse(c echo.Context) error {
	if s.SwarmManager == nil {
		return c.NoContent(http.StatusNoContent)
	}

	var payload preToolUsePayload
	if err := json.NewDecoder(c.Request().Body).Decode(&payload); err != nil {
		return c.NoContent(http.StatusNoContent)
	}

	sessionID := swarmSessionID(c, &payload.hookPayload)

	// Check deny list for Bash commands.
	if payload.ToolName == "Bash" {
		command, _ := payload.ToolInput["command"].(string)
		if command != "" && swarmorch.MatchesDenyPattern(command) {
			s.SwarmManager.WriteJSONLEvent(sessionID, map[string]any{
				"event":      "tool_denied",
				"session_id": sessionID,
				"tool":       payload.ToolName,
				"command":    command,
			})

			return c.JSON(http.StatusOK, preToolUseResponse{
				HookSpecificOutput: &preToolUseOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "deny",
					PermissionDecisionReason: "Command blocked by swarm policy: " + command,
				},
			})
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// handleSwarmHookPostToolUse logs tool usage and publishes to EventBus.
func (s *Server) handleSwarmHookPostToolUse(c echo.Context) error {
	if s.SwarmManager == nil {
		return c.NoContent(http.StatusNoContent)
	}

	var payload postToolUsePayload
	if err := json.NewDecoder(c.Request().Body).Decode(&payload); err != nil {
		return c.NoContent(http.StatusNoContent)
	}

	sessionID := swarmSessionID(c, &payload.hookPayload)
	ticketID := c.Request().Header.Get("X-Swarm-Ticket")
	phase := c.Request().Header.Get("X-Swarm-Phase")

	filtered := swarmorch.FilterToolArgs(payload.ToolName, payload.ToolInput)
	file, _ := filtered["file_path"].(string)

	s.SwarmManager.WriteJSONLEvent(sessionID, map[string]any{
		"event":      "tool_use",
		"session_id": sessionID,
		"tool":       payload.ToolName,
		"phase":      phase,
		"file":       file,
		"input":      filtered,
	})

	if s.EventBus != nil {
		s.EventBus.PublishGlobal(map[string]any{
			"event":      events.EventSwarmToolUse,
			"session_id": sessionID,
			"ticket_id":  ticketID,
			"tool":       payload.ToolName,
			"phase":      phase,
			"file":       file,
			"input":      filtered,
		})
	}

	return c.NoContent(http.StatusNoContent)
}

// handleSwarmHookPreCompact tracks compaction events and writes a sentinel
// file on the second compact.
func (s *Server) handleSwarmHookPreCompact(c echo.Context) error {
	if s.SwarmManager == nil {
		return c.NoContent(http.StatusNoContent)
	}

	var payload hookPayload
	_ = json.NewDecoder(c.Request().Body).Decode(&payload)

	sessionID := swarmSessionID(c, &payload)
	if sessionID == "" {
		return c.NoContent(http.StatusNoContent)
	}

	count := s.SwarmManager.IncrementContextPressure(sessionID)

	s.SwarmManager.WriteJSONLEvent(sessionID, map[string]any{
		"event":         "pre_compact",
		"session_id":    sessionID,
		"compact_count": count,
	})

	if count >= contextPressureThreshold {
		ticketID := c.Request().Header.Get("X-Swarm-Ticket")
		if s.EventBus != nil {
			s.EventBus.PublishGlobal(map[string]any{
				"event":         events.EventSwarmContextPressure,
				"session_id":    sessionID,
				"ticket_id":     ticketID,
				"compact_count": count,
			})
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// handleSwarmHookSessionComplete is called by the Stop hook. It reads the
// result file and signals the CompletionRegistry.
func (s *Server) handleSwarmHookSessionComplete(c echo.Context) error {
	if s.SwarmManager == nil {
		return c.NoContent(http.StatusNoContent)
	}

	var payload hookPayload
	_ = json.NewDecoder(c.Request().Body).Decode(&payload)

	sessionID := swarmSessionID(c, &payload)
	if sessionID == "" {
		return c.NoContent(http.StatusNoContent)
	}

	// Read the result file written by the skill.
	resultPath := swarmorch.ResultFilePath(sessionID)
	result, _ := swarm.ParseResultFile(resultPath)

	s.SwarmManager.WriteJSONLEvent(sessionID, map[string]any{
		"event":      "session_complete",
		"session_id": sessionID,
		"result":     string(result.Result),
		"summary":    result.Summary,
	})

	s.SwarmManager.SignalCompletion(sessionID, swarmorch.SessionResult{
		Result:  result.Result,
		Summary: result.Summary,
	})

	return c.NoContent(http.StatusNoContent)
}

// handleSwarmHookSessionEnded is the crash-backup handler. If the Stop hook
// didn't fire (e.g., Claude crashed), this signals CompletionRegistry with
// infra_failure.
func (s *Server) handleSwarmHookSessionEnded(c echo.Context) error {
	if s.SwarmManager == nil {
		return c.NoContent(http.StatusNoContent)
	}

	var payload hookPayload
	_ = json.NewDecoder(c.Request().Body).Decode(&payload)

	sessionID := swarmSessionID(c, &payload)
	if sessionID == "" {
		return c.NoContent(http.StatusNoContent)
	}

	// Try to read result file first — Stop hook may have already signaled.
	resultPath := swarmorch.ResultFilePath(sessionID)
	result, _ := swarm.ParseResultFile(resultPath)

	// If no result was written, treat as infra failure.
	if result.Result == "" {
		result.Result = swarm.ResultInfraFailure
		result.Summary = "session ended without Stop hook (possible crash)"
	}

	s.SwarmManager.WriteJSONLEvent(sessionID, map[string]any{
		"event":      "session_ended",
		"session_id": sessionID,
		"result":     string(result.Result),
		"summary":    result.Summary,
	})

	// Signal — this is a no-op if Stop already signaled (buffered channel).
	s.SwarmManager.SignalCompletion(sessionID, swarmorch.SessionResult{
		Result:  result.Result,
		Summary: result.Summary,
	})

	return c.NoContent(http.StatusNoContent)
}
