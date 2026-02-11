package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"creative-mode/harness/views/chat"
)

const (
	sseHeartbeatInterval = 30 * time.Second
	recentMessageLimit   = 50
)

// sseLogErr logs an SSE error to the browser console, falling
// back to slog if the console message itself fails.
func sseLogErr(
	sse *datastar.ServerSentEventGenerator,
	err error,
	msg string,
) {
	if cErr := sse.ConsoleError(err); cErr != nil {
		slog.Error(msg, "err", err)
	}
}

// ssePatchChat appends a templ component to #chat-log via SSE.
func ssePatchChat(
	sse *datastar.ServerSentEventGenerator,
	component templ.Component,
	errMsg string,
) {
	err := sse.PatchElementTempl(
		component,
		datastar.WithSelectorID("chat-log"),
		datastar.WithModeAppend(),
	)
	if err != nil {
		sseLogErr(sse, err, errMsg)
	}
}

// ssePatchSignals sends a signal update via SSE.
func ssePatchSignals(
	sse *datastar.ServerSentEventGenerator,
	signals map[string]any,
) {
	if err := sse.MarshalAndPatchSignals(signals); err != nil {
		sseLogErr(sse, err, "SSE signal error")
	}
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
			"event":    "player.joined",
			"username": user.GitHubUsername,
			"worldID":  worldID,
		})
	}

	for {
		select {
		case event := <-globalCh:
			s.handleGlobalEvent(sse, event)
		case event := <-worldCh:
			s.handleWorldEvent(sse, event)
		case <-heartbeat.C:
			if err := sse.MarshalAndPatchSignals(map[string]any{}); err != nil {
				return nil
			}
		case <-ctx.Done():
			if user != nil {
				s.EventBus.PublishGlobal(map[string]any{
					"event":    "player.left",
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
			s.handleGlobalEvent(sse, event)
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
	recentMsgs, _ := s.DB.GetRecentMessagesWithUser(
		ctx,
		recentMessageLimit,
	)

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
func (s *Server) handleGlobalEvent(
	sse *datastar.ServerSentEventGenerator,
	event any,
) {
	e, ok := event.(map[string]any)
	if !ok {
		return
	}

	eventType, _ := e["event"].(string)

	switch eventType {
	case "chat.message":
		username, _ := e["username"].(string)
		avatar, _ := e["avatar"].(string)
		content, _ := e["content"].(string)
		ts, _ := e["ts"].(string)
		ssePatchChat(
			sse,
			chat.Message(username, avatar, content, ts),
			"SSE chat error",
		)
	case "player.joined":
		username, _ := e["username"].(string)
		ssePatchChat(
			sse,
			chat.SystemNotification(username+" joined"),
			"SSE notification error",
		)
	case "player.left":
		username, _ := e["username"].(string)
		ssePatchChat(
			sse,
			chat.SystemNotification(username+" left"),
			"SSE notification error",
		)
	}
}

// handleWorldEvent processes a world-specific event and sends SSE patches.
func (s *Server) handleWorldEvent(
	sse *datastar.ServerSentEventGenerator,
	event any,
) {
	e, ok := event.(map[string]any)
	if !ok {
		return
	}

	eventType, _ := e["event"].(string)

	switch eventType {
	case "claude.tool_use.pre":
		ssePatchSignals(
			sse,
			map[string]any{"build_status": "editing"},
		)
	case "claude.session_stopped":
		ssePatchSignals(
			sse,
			map[string]any{"build_status": "compiling"},
		)
	case "build.completed":
		ssePatchSignals(
			sse,
			map[string]any{"build_status": "ready"},
		)

		worldID, _ := e["worldID"].(string)
		cpID, _ := e["cpID"].(string)
		worldName, _ := e["worldName"].(string)
		ssePatchChat(
			sse,
			chat.BuildReadyNotification(
				worldID,
				cpID,
				worldName,
			),
			"SSE build notification error",
		)
	case "build.failed":
		ssePatchSignals(
			sse,
			map[string]any{"build_status": "failed"},
		)

		errMsg, _ := e["error"].(string)
		ssePatchChat(
			sse,
			chat.SystemNotification("Build failed: "+errMsg),
			"SSE build error notification",
		)
	case "claude.rate_limited":
		ssePatchSignals(
			sse,
			map[string]any{"build_status": "rate_limited"},
		)
	}
}
