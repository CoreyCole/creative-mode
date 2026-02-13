package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"golang.org/x/net/html"
)

// devState holds dev hot-reload state. Lives on the Server struct (not
// package-level) to match the existing pattern for EventBus, etc.
type devState struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
	buildMu sync.Mutex
}

func newDevState() *devState {
	return &devState{
		clients: make(map[chan string]struct{}),
	}
}

func (d *devState) addClient(ch chan string) {
	d.mu.Lock()
	d.clients[ch] = struct{}{}
	d.mu.Unlock()
}

func (d *devState) removeClient(ch chan string) {
	d.mu.Lock()
	delete(d.clients, ch)
	d.mu.Unlock()
}

func (d *devState) broadcast(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.clients {
		select {
		case ch <- msg:
		default: // skip slow clients
		}
	}
}

// handleDevSSE serves the development hot-reload SSE endpoint.
// On (re)connection after a server restart, it fetches the current page
// and sends it as a Datastar PatchElements morph.
func (s *Server) handleDevSSE(c echo.Context) error {
	w := c.Response().Writer
	r := c.Request()
	sse := datastar.NewSSE(w, r)

	// Register this client for push events (CSS reload, etc.)
	const eventChBuffer = 8
	eventCh := make(chan string, eventChBuffer)
	s.dev.addClient(eventCh)
	defer s.dev.removeClient(eventCh)

	// On (re)connect: morph the page content
	s.devMorphPage(sse, r)

	// Listen for push events until disconnect
	for {
		select {
		case msg := <-eventCh:
			if msg == "reload-static" {
				_ = sse.ExecuteScript(
					`document.querySelectorAll('link[rel="stylesheet"]').forEach(` +
						`l=>{const u=new URL(l.href);` +
						`u.searchParams.set("v",Date.now());l.href=u.toString()})`,
				)
			}
		case <-r.Context().Done():
			return nil
		}
	}
}

// devMorphPage fetches the current page via internal HTTP request
// and sends the #page-content innerHTML as a Datastar morph.
func (s *Server) devMorphPage(
	sse *datastar.ServerSentEventGenerator,
	r *http.Request,
) {
	referer := r.Header.Get("Referer")
	if referer == "" {
		return
	}

	refURL, err := url.Parse(referer)
	if err != nil {
		return
	}

	baseURL := os.Getenv("HARNESS_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	internalURL := baseURL + refURL.Path
	if refURL.RawQuery != "" {
		internalURL += "?" + refURL.RawQuery
	}

	pageReq, err := http.NewRequestWithContext(
		r.Context(), http.MethodGet, internalURL, http.NoBody,
	)
	if err != nil {
		return
	}
	pageReq.Header.Set("Cookie", r.Header.Get("Cookie"))

	const devFetchTimeout = 5 * time.Second
	client := &http.Client{Timeout: devFetchTimeout}
	resp, err := client.Do(pageReq)
	if err != nil {
		s.Logger.Warn("dev: failed to fetch page",
			"url", internalURL, "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	content, err := extractPageContent(resp.Body)
	if err != nil {
		s.Logger.Warn("dev: failed to extract page content", "error", err)
		return
	}
	if content != "" {
		_ = sse.PatchElements(
			content,
			datastar.WithSelectorID("page-content"),
			datastar.WithModeInner(),
		)
	}
}

// handleDevRebuild builds a new binary in the background while the
// old server keeps serving. Once the build succeeds, it triggers a
// graceful restart via SIGTERM.
func (s *Server) handleDevRebuild(c echo.Context) error {
	if !s.dev.buildMu.TryLock() {
		return c.JSON(http.StatusConflict,
			map[string]string{"status": "already building"})
	}

	go func() {
		defer s.dev.buildMu.Unlock()

		start := time.Now()
		s.Logger.Info("dev: building new binary...")

		cmd := exec.CommandContext(context.Background(),
			"go", "build", "-o", "/tmp/harness-next", ".",
		)
		cmd.Dir = "/app/harness"
		cmd.Env = os.Environ()

		if output, err := cmd.CombinedOutput(); err != nil {
			s.Logger.Error("dev: build failed",
				"error", err, "output", string(output),
				"duration", time.Since(start))
			return // old server keeps running
		}

		if err := os.Rename(
			"/tmp/harness-next", "/tmp/harness",
		); err != nil {
			s.Logger.Error("dev: rename failed", "error", err)
			return
		}

		s.Logger.Info("dev: build succeeded, restarting",
			"duration", time.Since(start))

		// Trigger existing graceful shutdown in main.go
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGTERM)
	}()

	return c.JSON(http.StatusAccepted,
		map[string]string{"status": "building"})
}

// handleDevRebuildTemplate is a no-op in dev mode — cargo watch + trunk serve
// handle all template rebuilds automatically inside Docker.
func (s *Server) handleDevRebuildTemplate(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "no-op",
		"message": "template uses cargo watch + trunk serve; rebuilds are automatic",
	})
}

// handleDevReloadStatic pushes a CSS/JS cache bust to all connected
// dev SSE clients. No server restart needed.
func (s *Server) handleDevReloadStatic(c echo.Context) error {
	s.dev.broadcast("reload-static")
	return c.JSON(http.StatusOK,
		map[string]string{"status": "reloading"})
}

// extractPageContent uses golang.org/x/net/html to extract the
// innerHTML of the #page-content div.
func extractPageContent(body io.Reader) (string, error) {
	doc, err := html.Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	node := findNodeByID(doc, "page-content")
	if node == nil {
		return "", nil
	}

	// Render all children (innerHTML) into a string.
	var sb strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&sb, child); err != nil {
			return "", fmt.Errorf("render child: %w", err)
		}
	}

	return strings.TrimSpace(sb.String()), nil
}

// findNodeByID walks the HTML tree to find the element with the given id.
func findNodeByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == id {
				return n
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findNodeByID(child, id); found != nil {
			return found
		}
	}
	return nil
}
