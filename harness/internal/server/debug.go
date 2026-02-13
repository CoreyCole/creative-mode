package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"creative-mode/harness/internal/events"
)

// pendingClientQuery holds a channel waiting for a client debug response.
type pendingClientQuery struct {
	ch chan json.RawMessage
}

var (
	pendingQueries = make(
		map[string]*pendingClientQuery,
	) //nolint:gochecknoglobals // intentional singleton
	pendingQueriesMu sync.Mutex //nolint:gochecknoglobals // guards pendingQueries
)

// handleClientDebug sends a debug query to a connected browser via SSE,
// waits for the browser to execute it in the WASM iframe and POST back.
func (s *Server) handleClientDebug(c echo.Context) error {
	worldID := c.Param("worldID")

	var query json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&query); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}

	// Generate query ID and register pending response channel.
	queryID := uuid.New().String()[:8]
	pending := &pendingClientQuery{ch: make(chan json.RawMessage, 1)}

	pendingQueriesMu.Lock()
	pendingQueries[queryID] = pending
	pendingQueriesMu.Unlock()

	defer func() {
		pendingQueriesMu.Lock()
		delete(pendingQueries, queryID)
		pendingQueriesMu.Unlock()
	}()

	// Build JS that the browser will execute:
	// 1. postMessage the query to the game iframe
	// 2. Listen for the response
	// 3. POST it back to the harness
	js := fmt.Sprintf(`
(function() {
  var frame = document.getElementById('game-frame');
  if (!frame) return;
  var id = %q;

  function onMessage(event) {
    if (!event.data || event.data.type !== 'debug-response' || event.data.id !== id) return;
    window.removeEventListener('message', onMessage);
    fetch('/world/%s/client-debug-response?id=' + id, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(event.data.response),
    });
  }
  window.addEventListener('message', onMessage);

  frame.contentWindow.postMessage({
    type: 'debug-query',
    id: id,
    query: %s,
  }, '*');
})();
`, queryID, worldID, string(query))

	// Publish ExecuteScript event to all SSE connections for this world.
	if s.EventBus != nil {
		s.EventBus.Publish(worldID, map[string]any{
			"event":  events.EventExecuteScript,
			"script": js,
		})
	}

	// Wait for response with timeout.
	select {
	case result := <-pending.ch:
		return c.JSONBlob(http.StatusOK, result)
	case <-time.After(debugProxyTimeout):
		return echo.NewHTTPError(
			http.StatusGatewayTimeout,
			"client debug query timed out",
		)
	case <-c.Request().Context().Done():
		return nil
	}
}

// handleClientDebugResponse receives the debug query result POSTed back
// by the browser JS after querying the WASM iframe.
func (s *Server) handleClientDebugResponse(c echo.Context) error {
	queryID := c.QueryParam("id")
	if queryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing query id")
	}

	var result json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&result); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}

	pendingQueriesMu.Lock()
	pending, ok := pendingQueries[queryID]
	pendingQueriesMu.Unlock()

	if !ok {
		return c.NoContent(http.StatusGone) // query already timed out
	}

	// Non-blocking send (buffer of 1).
	select {
	case pending.ch <- result:
	default:
	}

	return c.NoContent(http.StatusOK)
}
