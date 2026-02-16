package monitor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/labstack/echo/v4"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/starfederation/datastar-go/datastar"

	l "github.com/coreycole/creative-mode/site/layouts"
	p "github.com/coreycole/creative-mode/site/pages"
)

// SystemSignals holds the scalar system metrics pushed via MarshalAndPatchSignals.
type SystemSignals struct {
	MemTotal       string `json:"memTotal,omitempty"`
	MemUsed        string `json:"memUsed,omitempty"`
	MemUsedPercent string `json:"memUsedPercent,omitempty"`
	CpuPercent     string `json:"cpuPercent,omitempty"`
	Uptime         string `json:"uptime,omitempty"`
}

// Handler serves the /status page and SSE events.
type Handler struct {
	db              *sql.DB
	logger          *slog.Logger
	harnessURL      string // empty = skip harness section
	presidentSecret string // empty = skip worlds section
	commit          string
	startedAt       time.Time
}

// NewHandler creates a new monitor handler.
func NewHandler(db *sql.DB, logger *slog.Logger, harnessURL, presidentSecret, commit string) *Handler {
	return &Handler{
		db:              db,
		logger:          logger,
		harnessURL:      harnessURL,
		presidentSecret: presidentSecret,
		commit:          commit,
		startedAt:       time.Now(),
	}
}

// HandlePage renders the status page.
func (h *Handler) HandlePage(c echo.Context) error {
	rootArgs := l.RootArgs{
		Title:       "Status - Creative Mode",
		CurrentPath: c.Request().URL.Path,
		Commit:      h.commit,
	}
	return p.StatusPage(rootArgs, h.harnessURL != "", h.presidentSecret != "").
		Render(c.Request().Context(), c.Response().Writer)
}

// HandleEvents streams SSE events for system metrics, DB health, harness health, and world status.
func (h *Handler) HandleEvents(c echo.Context) error {
	systemTicker := time.NewTicker(2 * time.Second)
	defer systemTicker.Stop()

	dbTicker := time.NewTicker(10 * time.Second)
	defer dbTicker.Stop()

	var harnessTicker *time.Ticker
	if h.harnessURL != "" {
		harnessTicker = time.NewTicker(10 * time.Second)
		defer harnessTicker.Stop()
	}

	var worldsTicker *time.Ticker
	if h.presidentSecret != "" {
		worldsTicker = time.NewTicker(30 * time.Second)
		defer worldsTicker.Stop()
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Fire all immediately on connection.
	h.sendSystemMetrics(sse)
	h.sendDBHealth(sse)
	if h.harnessURL != "" {
		h.sendHarnessHealth(sse)
	}
	if h.presidentSecret != "" {
		h.sendWorldStatus(sse)
	}

	for {
		select {
		case <-c.Request().Context().Done():
			h.logger.Debug("status SSE client disconnected")
			return nil

		case <-systemTicker.C:
			h.sendSystemMetrics(sse)

		case <-dbTicker.C:
			h.sendDBHealth(sse)

		default:
		}

		// Check optional tickers separately to avoid nil channel selects.
		if harnessTicker != nil {
			select {
			case <-harnessTicker.C:
				h.sendHarnessHealth(sse)
			default:
			}
		}
		if worldsTicker != nil {
			select {
			case <-worldsTicker.C:
				h.sendWorldStatus(sse)
			default:
			}
		}

		// Small sleep to prevent busy-waiting from the default cases.
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *Handler) sendSystemMetrics(sse *datastar.ServerSentEventGenerator) {
	signals := SystemSignals{
		Uptime: formatDuration(time.Since(h.startedAt)),
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		h.logger.Error("failed to get memory stats", "error", err)
	} else {
		signals.MemTotal = humanize.Bytes(vm.Total)
		signals.MemUsed = humanize.Bytes(vm.Used)
		signals.MemUsedPercent = fmt.Sprintf("%.1f%%", vm.UsedPercent)
	}

	cpuPct, err := cpu.Percent(0, false)
	if err != nil {
		h.logger.Error("failed to get cpu stats", "error", err)
	} else if len(cpuPct) > 0 {
		signals.CpuPercent = fmt.Sprintf("%.1f%%", cpuPct[0])
	}

	if err := sse.MarshalAndPatchSignals(signals); err != nil {
		h.logger.Error("failed to patch system signals", "error", err)
	}
}

func (h *Handler) sendDBHealth(sse *datastar.ServerSentEventGenerator) {
	start := time.Now()
	err := h.db.Ping()
	latency := time.Since(start)

	if err != nil {
		h.logger.Error("db ping failed", "error", err)
		if patchErr := sse.PatchElementTempl(
			p.DBHealthCard("error", "", 0),
			datastar.WithSelectorID("db-health"),
		); patchErr != nil {
			h.logger.Error("failed to patch db health", "error", patchErr)
		}
		return
	}

	var sessionCount int
	row := h.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE expires_at > CURRENT_TIMESTAMP")
	if scanErr := row.Scan(&sessionCount); scanErr != nil {
		h.logger.Error("failed to count sessions", "error", scanErr)
	}

	if patchErr := sse.PatchElementTempl(
		p.DBHealthCard("ok", latency.Round(time.Microsecond).String(), sessionCount),
		datastar.WithSelectorID("db-health"),
	); patchErr != nil {
		h.logger.Error("failed to patch db health", "error", patchErr)
	}
}

func (h *Handler) sendHarnessHealth(sse *datastar.ServerSentEventGenerator) {
	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	resp, err := client.Get(h.harnessURL + "/health")
	latency := time.Since(start)

	if err != nil {
		h.logger.Warn("harness health check failed", "error", err)
		if patchErr := sse.PatchElementTempl(
			p.HarnessHealthCard("error", "", h.harnessURL),
			datastar.WithSelectorID("harness-health"),
		); patchErr != nil {
			h.logger.Error("failed to patch harness health", "error", patchErr)
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()

	status := "ok"
	if resp.StatusCode != http.StatusOK {
		status = "error"
	}

	if patchErr := sse.PatchElementTempl(
		p.HarnessHealthCard(status, latency.Round(time.Millisecond).String(), h.harnessURL),
		datastar.WithSelectorID("harness-health"),
	); patchErr != nil {
		h.logger.Error("failed to patch harness health", "error", patchErr)
	}
}

// mayorStatusResponse matches the JSON from GET /api/president/mayor-status.
type mayorStatusResponse struct {
	Worlds    []mayorWorldStatus `json:"worlds"`
	Timestamp string             `json:"timestamp"`
}

type mayorWorldStatus struct {
	WorldID      string `json:"world_id"`
	WorldName    string `json:"world_name"`
	MayorName    string `json:"mayor_name"`
	TemplateType string `json:"template_type"`
	Checkpoints  int    `json:"checkpoint_count"`
	LatestStatus string `json:"latest_status"`
	GameRunning  bool   `json:"game_server_running"`
	RecentBuilds int    `json:"recent_builds"`
}

func (h *Handler) sendWorldStatus(sse *datastar.ServerSentEventGenerator) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, h.harnessURL+"/api/president/mayor-status", nil)
	if err != nil {
		h.logger.Error("failed to create mayor-status request", "error", err)
		return
	}
	req.Header.Set("X-President-Secret", h.presidentSecret)

	resp, err := client.Do(req)
	if err != nil {
		h.logger.Warn("mayor-status request failed", "error", err)
		if patchErr := sse.PatchElementTempl(
			p.WorldStatusError("Harness unreachable"),
			datastar.WithSelectorID("world-status"),
		); patchErr != nil {
			h.logger.Error("failed to patch world status error", "error", patchErr)
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		h.logger.Warn("mayor-status returned non-200", "status", resp.StatusCode)
		if patchErr := sse.PatchElementTempl(
			p.WorldStatusError(fmt.Sprintf("HTTP %d", resp.StatusCode)),
			datastar.WithSelectorID("world-status"),
		); patchErr != nil {
			h.logger.Error("failed to patch world status error", "error", patchErr)
		}
		return
	}

	var statusResp mayorStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		h.logger.Error("failed to decode mayor-status response", "error", err)
		if patchErr := sse.PatchElementTempl(
			p.WorldStatusError("Invalid response"),
			datastar.WithSelectorID("world-status"),
		); patchErr != nil {
			h.logger.Error("failed to patch world status error", "error", patchErr)
		}
		return
	}

	worlds := make([]p.WorldInfo, len(statusResp.Worlds))
	for i, w := range statusResp.Worlds {
		worlds[i] = p.WorldInfo{
			WorldID:      w.WorldID,
			WorldName:    w.WorldName,
			MayorName:    w.MayorName,
			TemplateType: w.TemplateType,
			Checkpoints:  w.Checkpoints,
			LatestStatus: w.LatestStatus,
			GameRunning:  w.GameRunning,
			RecentBuilds: w.RecentBuilds,
		}
	}

	if patchErr := sse.PatchElementTempl(
		p.WorldStatusTable(worlds, statusResp.Timestamp),
		datastar.WithSelectorID("world-status"),
	); patchErr != nil {
		h.logger.Error("failed to patch world status", "error", patchErr)
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
