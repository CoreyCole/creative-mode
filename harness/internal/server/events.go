package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/internal/events"
	"creative-mode/harness/views/chat"
)

const (
	sseHeartbeatInterval = 30 * time.Second
	recentMessageLimit   = 50
)

// sseLogErr logs an SSE error server-side and attempts to forward it
// to the browser console.
func sseLogErr(
	sse *datastar.ServerSentEventGenerator,
	err error,
	msg string,
) {
	slog.Warn(msg, "err", err)
	_ = sse.ConsoleError(err)
}

// ssePatchChat appends a templ component to #chat-log via SSE.
func ssePatchChat(
	sse *datastar.ServerSentEventGenerator,
	component templ.Component,
) error {
	return sse.PatchElementTempl(
		component,
		datastar.WithSelectorID("chat-log"),
		datastar.WithModeAppend(),
	)
}

// ssePatchSignals sends a signal update via SSE.
func ssePatchSignals(
	sse *datastar.ServerSentEventGenerator,
	signals map[string]any,
) error {
	return sse.MarshalAndPatchSignals(signals)
}

// handleWorldSSE streams global + world-specific events to the overlay.
func (s *Server) handleWorldSSE(c echo.Context) error {
	w := c.Response().Writer
	r := c.Request()
	sse := datastar.NewSSE(w, r)
	worldID := c.Param("worldID")
	user, _ := requireUser(c)

	globalCh := s.EventBus.SubscribeGlobal()
	defer s.EventBus.UnsubscribeGlobal(globalCh)

	worldCh := s.EventBus.Subscribe(worldID)
	defer s.EventBus.Unsubscribe(worldID, worldCh)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	// Send recent message history with usernames from joined query.
	ctx := r.Context()

	if err := s.sendChatHistory(sse, ctx); err != nil {
		return nil
	}

	// Announce player joined.
	if user != nil {
		s.EventBus.PublishGlobal(map[string]any{
			"event":    events.EventPlayerJoined,
			"username": user.GitHubUsername,
			"worldID":  worldID,
		})
	}

	for {
		select {
		case event := <-globalCh:
			if err := s.handleGlobalEvent(sse, event); err != nil {
				return nil // Connection broken.
			}
		case event := <-worldCh:
			if err := s.handleWorldEvent(sse, event); err != nil {
				return nil // Connection broken.
			}
		case <-heartbeat.C:
			if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
				return nil
			}
		case <-ctx.Done():
			if user != nil {
				s.EventBus.PublishGlobal(map[string]any{
					"event":    events.EventPlayerLeft,
					"username": user.GitHubUsername,
					"worldID":  worldID,
				})
			}

			return nil
		}
	}
}

// handleGlobalSSE streams global-only events for the lobby.
func (s *Server) handleGlobalSSE(c echo.Context) error {
	w := c.Response().Writer
	r := c.Request()
	sse := datastar.NewSSE(w, r)

	globalCh := s.EventBus.SubscribeGlobal()
	defer s.EventBus.UnsubscribeGlobal(globalCh)

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()

	if err := s.sendChatHistory(sse, ctx); err != nil {
		return nil
	}

	for {
		select {
		case event := <-globalCh:
			if err := s.handleGlobalEvent(sse, event); err != nil {
				return nil // Connection broken.
			}
		case <-heartbeat.C:
			if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// sendChatHistory sends the most recent messages to an SSE connection.
func (s *Server) sendChatHistory(
	sse *datastar.ServerSentEventGenerator,
	ctx context.Context,
) error {
	recentMsgs, err := s.DB.GetRecentMessagesWithUser(
		ctx,
		recentMessageLimit,
	)
	if err != nil {
		slog.Error("failed to load chat history", "error", err)
	}

	for i := len(recentMsgs) - 1; i >= 0; i-- {
		msg := recentMsgs[i]

		username := ""
		if msg.GitHubUsername.Valid {
			username = msg.GitHubUsername.String
		}

		avatarURL := ""
		if msg.AvatarURL.Valid {
			avatarURL = msg.AvatarURL.String
		}

		if err := sse.PatchElementTempl(
			chat.Message(
				username,
				avatarURL,
				msg.Content,
				msg.CreatedAt.Format("15:04"),
			),
			datastar.WithSelectorID("chat-log"),
			datastar.WithModeAppend(),
		); err != nil {
			sseLogErr(sse, err, "SSE history error")

			return err
		}
	}

	return nil
}

// handleGlobalEvent processes a global event and sends SSE patches.
// Returns an error if the SSE connection is broken.
func (s *Server) handleGlobalEvent(
	sse *datastar.ServerSentEventGenerator,
	event any,
) error {
	e, ok := event.(map[string]any)
	if !ok {
		return nil
	}

	eventType, _ := e["event"].(string)

	switch eventType {
	case events.EventChatMessage:
		username, _ := e["username"].(string)
		avatar, _ := e["avatar"].(string)
		content, _ := e["content"].(string)
		ts, _ := e["ts"].(string)
		return ssePatchChat(
			sse,
			chat.Message(username, avatar, content, ts),
		)
	case events.EventPlayerJoined:
		username, _ := e["username"].(string)
		return ssePatchChat(
			sse,
			chat.SystemNotification(username+" joined"),
		)
	case events.EventPlayerLeft:
		username, _ := e["username"].(string)
		return ssePatchChat(
			sse,
			chat.SystemNotification(username+" left"),
		)
	}

	return nil
}

// handleWorldEvent processes a world-specific event and sends SSE patches.
// Returns an error if the SSE connection is broken.
func (s *Server) handleWorldEvent(
	sse *datastar.ServerSentEventGenerator,
	event any,
) error {
	e, ok := event.(map[string]any)
	if !ok {
		return nil
	}

	eventType, _ := e["event"].(string)

	switch eventType {
	case events.EventClaudeToolUsePre:
		return ssePatchSignals(
			sse,
			map[string]any{"build_status": "editing"},
		)
	case events.EventClaudeSessionStop:
		return ssePatchSignals(
			sse,
			map[string]any{"build_status": "compiling"},
		)
	case events.EventBuildCompleted:
		if err := ssePatchSignals(
			sse,
			map[string]any{"build_status": "ready"},
		); err != nil {
			return err
		}

		worldID, _ := e["worldID"].(string)
		cpID, _ := e["cpID"].(string)
		worldName, _ := e["worldName"].(string)
		return ssePatchChat(
			sse,
			chat.BuildReadyNotification(
				worldID,
				cpID,
				worldName,
			),
		)
	case events.EventBuildFailed:
		if err := ssePatchSignals(
			sse,
			map[string]any{"build_status": "failed"},
		); err != nil {
			return err
		}

		errMsg, _ := e["error"].(string)
		return ssePatchChat(
			sse,
			chat.SystemNotification("Build failed: "+errMsg),
		)
	case events.EventClaudeRateLimited:
		return ssePatchSignals(
			sse,
			map[string]any{"build_status": "rate_limited"},
		)
	}

	return nil
}
