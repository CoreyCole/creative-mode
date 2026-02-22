package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
)

// Handler handles GitHub webhook events and self-rebuilds the site binary.
type Handler struct {
	buildMu sync.Mutex
	logger  *slog.Logger
	repoDir string
	siteDir string
	secret  string
}

// New creates a webhook handler. If secret is empty, webhook verification is disabled
// (not recommended for production).
func New(logger *slog.Logger, secret string) *Handler {
	return &Handler{
		logger:  logger,
		repoDir: "/home/ubuntu/creative-mode",
		siteDir: "/home/ubuntu/creative-mode/site",
		secret:  secret,
	}
}

// HandleHealth returns a simple health check response.
func (h *Handler) HandleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// HandleGitHub processes GitHub webhook push events.
func (h *Handler) HandleGitHub(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		h.logger.Error("webhook: error reading body", "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "bad request"})
	}

	// Verify HMAC-SHA256 signature.
	if h.secret != "" {
		sigHeader := c.Request().Header.Get("X-Hub-Signature-256")
		if sigHeader == "" {
			h.logger.Warn("webhook: missing X-Hub-Signature-256 header")
			return c.JSON(http.StatusForbidden, map[string]string{"error": "missing signature"})
		}
		if !strings.HasPrefix(sigHeader, "sha256=") {
			h.logger.Warn("webhook: malformed signature header", "header", sigHeader)
			return c.JSON(http.StatusForbidden, map[string]string{"error": "bad signature"})
		}
		sigHex := strings.TrimPrefix(sigHeader, "sha256=")
		sig, err := hex.DecodeString(sigHex)
		if err != nil {
			h.logger.Warn("webhook: invalid hex in signature", "error", err)
			return c.JSON(http.StatusForbidden, map[string]string{"error": "bad signature"})
		}

		mac := hmac.New(sha256.New, []byte(h.secret))
		mac.Write(body)
		expected := mac.Sum(nil)
		if !hmac.Equal(sig, expected) {
			h.logger.Warn("webhook: signature mismatch")
			return c.JSON(http.StatusForbidden, map[string]string{"error": "bad signature"})
		}
	}

	// Handle ping event (GitHub sends on webhook creation).
	event := c.Request().Header.Get("X-GitHub-Event")
	if event == "ping" {
		h.logger.Info("webhook: received ping event")
		return c.JSON(http.StatusOK, map[string]string{"status": "pong"})
	}

	if event != "push" {
		h.logger.Info("webhook: ignoring event", "event", event)
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored", "reason": "not a push event"})
	}

	// Parse push payload.
	var payload struct {
		Ref     string `json:"ref"`
		Commits []struct {
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
			Removed  []string `json:"removed"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Error("webhook: error parsing payload", "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "bad payload"})
	}

	// Only deploy for pushes to main.
	if payload.Ref != "refs/heads/main" {
		h.logger.Info("webhook: ignoring push", "ref", payload.Ref)
		return c.JSON(http.StatusOK, map[string]string{
			"status": "ignored",
			"reason": fmt.Sprintf("ref is %s", payload.Ref),
		})
	}

	// Check if any changed files are in site/ or pkg/ (shared dependency).
	hasSiteChanges := false
	for _, commit := range payload.Commits {
		for _, files := range [][]string{commit.Added, commit.Modified, commit.Removed} {
			for _, f := range files {
				if strings.HasPrefix(f, "site/") || strings.HasPrefix(f, "pkg/") {
					hasSiteChanges = true
					break
				}
			}
			if hasSiteChanges {
				break
			}
		}
		if hasSiteChanges {
			break
		}
	}

	if !hasSiteChanges {
		h.logger.Info("webhook: ignoring push to main — no site/ or pkg/ changes")
		return c.JSON(http.StatusOK, map[string]string{"status": "ignored", "reason": "no site/ or pkg/ changes"})
	}

	// Trigger rebuild asynchronously.
	h.logger.Info("webhook: site/ changes detected on main — triggering rebuild")
	go h.rebuild()

	return c.JSON(http.StatusAccepted, map[string]string{"status": "rebuilding"})
}

// rebuild pulls latest code, rebuilds the binary, and SIGTERMs for systemd restart.
// Follows the harness handleDevRebuild pattern.
func (h *Handler) rebuild() {
	if !h.buildMu.TryLock() {
		h.logger.Info("webhook: rebuild already in progress, skipping")
		return
	}
	defer h.buildMu.Unlock()

	start := time.Now()
	h.logger.Info("webhook: starting rebuild...")

	// 1. git fetch + reset
	if err := h.run(h.repoDir, "git", "fetch", "origin", "main"); err != nil {
		h.logger.Error("webhook: git fetch failed", "error", err)
		return
	}
	if err := h.run(h.repoDir, "git", "reset", "--hard", "origin/main"); err != nil {
		h.logger.Error("webhook: git reset failed", "error", err)
		return
	}
	h.logger.Info("webhook: code updated")

	// 2. templ generate
	if err := h.run(h.siteDir, "templ", "generate"); err != nil {
		h.logger.Error("webhook: templ generate failed", "error", err)
		return
	}
	h.logger.Info("webhook: templ generated")

	// 3. Build tailwind CSS (call tailwindcss directly to avoid snap/systemd scope issues with `just`)
	if err := h.run(h.siteDir, "tailwindcss",
		"-i", "static/css/index.css", "-o", "static/css/out.css",
		"--content", "./pages/**/*", "--content", "./layouts/**/*",
	); err != nil {
		h.logger.Error("webhook: tailwind build failed", "error", err)
		return
	}
	if err := h.run(h.siteDir, "bash", "-c",
		`HASH=$(sha256sum static/css/out.css | cut -d' ' -f1 | head -c8) && rm -f static/css/out.*.css && cp static/css/out.css static/css/out.$HASH.css`,
	); err != nil {
		h.logger.Error("webhook: tailwind hash failed", "error", err)
		return
	}
	h.logger.Info("webhook: tailwind built")

	// 4. Build new binary
	if err := h.run(h.siteDir, "go", "build", "-o", "/tmp/site-next", "."); err != nil {
		h.logger.Error("webhook: go build failed", "error", err)
		return
	}

	// 5. Replace running binary: unlink old, then rename new into place.
	// Direct os.Rename over a running executable can fail with ETXTBSY.
	// os.Remove unlinks the directory entry while the kernel keeps the inode
	// alive for the running process, freeing the path for the rename.
	binPath := "/home/ubuntu/bin/creative-mode-site"
	if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
		h.logger.Error("webhook: remove old binary failed", "error", err)
		return
	}
	if err := os.Rename("/tmp/site-next", binPath); err != nil {
		h.logger.Error("webhook: rename failed", "error", err)
		return
	}

	h.logger.Info("webhook: build succeeded, restarting", "duration", time.Since(start))

	// 6. SIGTERM self — systemd restarts with new binary
	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(syscall.SIGTERM)
}

// run executes a command in the given directory and returns any error.
func (h *Handler) run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		h.logger.Error("webhook: command failed",
			"cmd", fmt.Sprintf("%s %s", name, strings.Join(args, " ")),
			"output", string(output),
			"error", err,
		)
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
