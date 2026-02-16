package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/coreycole/creative-mode/pkg/worldchannel"
	"github.com/dustin/go-humanize"
	"github.com/labstack/echo/v4"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/starfederation/datastar-go/datastar"

	l "github.com/coreycole/creative-mode/site/layouts"
	p "github.com/coreycole/creative-mode/site/pages"
)

// SystemSignals holds the scalar system metrics pushed via MarshalAndPatchSignals.
type SystemSignals struct {
	MemTotal        string `json:"memTotal,omitempty"`
	MemUsed         string `json:"memUsed,omitempty"`
	MemUsedPercent  string `json:"memUsedPercent,omitempty"`
	CpuPercent      string `json:"cpuPercent,omitempty"`
	Uptime          string `json:"uptime,omitempty"`
	DiskTotal       string `json:"diskTotal,omitempty"`
	DiskUsed        string `json:"diskUsed,omitempty"`
	DiskUsedPercent string `json:"diskUsedPercent,omitempty"`
}

// Handler serves the /status page and SSE events.
type Handler struct {
	db              *sql.DB
	logger          *slog.Logger
	harnessURL      string // empty = skip harness section
	presidentSecret string // empty = skip worlds section
	commit          string
	startedAt       time.Time

	wcClient       *worldchannel.Client
	mu             sync.Mutex
	discordMembers int
	worldCount     int

	cancel context.CancelFunc
}

// NewHandler creates a new monitor handler and starts the background snapshot writer.
func NewHandler(db *sql.DB, logger *slog.Logger, harnessURL, presidentSecret, commit string, wcClient *worldchannel.Client) *Handler {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{
		db:              db,
		logger:          logger,
		harnessURL:      harnessURL,
		presidentSecret: presidentSecret,
		commit:          commit,
		startedAt:       time.Now(),
		wcClient:        wcClient,
		cancel:          cancel,
	}
	go h.runSnapshotWriter(ctx)
	go h.runDiscordPoller(ctx)
	go h.runRetentionCleanup(ctx)
	return h
}

// Stop shuts down the background snapshot writer.
func (h *Handler) Stop() {
	h.cancel()
}

// runSnapshotWriter inserts a metrics_snapshots row every 30 seconds.
func (h *Handler) runSnapshotWriter(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Write one snapshot immediately on startup.
	h.writeSnapshot()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.writeSnapshot()
		}
	}
}

func (h *Handler) writeSnapshot() {
	var memPercent float64
	var memBytes uint64
	vm, err := mem.VirtualMemory()
	if err != nil {
		h.logger.Error("snapshot: failed to get memory stats", "error", err)
	} else {
		memPercent = vm.UsedPercent
		memBytes = vm.Used
	}

	var totalVisits int64
	row := h.db.QueryRow("SELECT COUNT(*) FROM page_views")
	if scanErr := row.Scan(&totalVisits); scanErr != nil {
		h.logger.Error("snapshot: failed to count page views", "error", scanErr)
	}

	var uniqueVisitors int64
	row = h.db.QueryRow("SELECT COUNT(DISTINCT visitor_hash) FROM page_views WHERE visitor_hash != ''")
	if scanErr := row.Scan(&uniqueVisitors); scanErr != nil {
		h.logger.Error("snapshot: failed to count unique visitors", "error", scanErr)
	}

	h.mu.Lock()
	discordMembers := h.discordMembers
	worldCount := h.worldCount
	h.mu.Unlock()

	_, err = h.db.Exec(
		"INSERT INTO metrics_snapshots (mem_used_percent, mem_used_bytes, total_visits, unique_visitors, discord_members, worlds_hatched) VALUES (?, ?, ?, ?, ?, ?)",
		memPercent, memBytes, totalVisits, uniqueVisitors, discordMembers, worldCount,
	)
	if err != nil {
		h.logger.Error("snapshot: failed to insert", "error", err)
	}
}

// runDiscordPoller updates cached Discord member count and world count periodically.
func (h *Handler) runDiscordPoller(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Fetch immediately on startup.
	h.updateDiscordStats()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.updateDiscordStats()
		}
	}
}

func (h *Handler) updateDiscordStats() {
	if h.wcClient == nil {
		return
	}

	members, err := h.wcClient.GuildMemberCount()
	if err != nil {
		h.logger.Warn("failed to fetch guild member count", "error", err)
	}

	worlds, err := h.wcClient.ListExistingMayors()
	if err != nil {
		h.logger.Warn("failed to list existing mayors", "error", err)
	}

	h.mu.Lock()
	if err == nil {
		h.worldCount = len(worlds)
	}
	if members > 0 {
		h.discordMembers = members
	}
	h.mu.Unlock()
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

	dbTicker := time.NewTicker(2 * time.Second)
	defer dbTicker.Stop()

	statsTicker := time.NewTicker(2 * time.Second)
	defer statsTicker.Stop()

	var harnessTicker *time.Ticker
	if h.harnessURL != "" {
		harnessTicker = time.NewTicker(10 * time.Second)
		defer harnessTicker.Stop()
	}

	var worldsTicker *time.Ticker
	if h.presidentSecret != "" {
		worldsTicker = time.NewTicker(2 * time.Second)
		defer worldsTicker.Stop()
	}

	sse := datastar.NewSSE(c.Response().Writer, c.Request())

	// Fire all immediately on connection.
	h.sendSystemMetrics(sse)
	h.sendDBHealth(sse)
	h.sendStatsOverview(sse)
	h.sendMetricsGraph(sse)
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

		case <-statsTicker.C:
			h.sendStatsOverview(sse)

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

	du, err := disk.Usage("/")
	if err != nil {
		h.logger.Error("failed to get disk stats", "error", err)
	} else {
		signals.DiskTotal = humanize.Bytes(du.Total)
		signals.DiskUsed = humanize.Bytes(du.Used)
		signals.DiskUsedPercent = fmt.Sprintf("%.1f%%", du.UsedPercent)
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
			datastar.WithModeInner(),
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
		datastar.WithModeInner(),
	); patchErr != nil {
		h.logger.Error("failed to patch db health", "error", patchErr)
	}
}

func (h *Handler) sendStatsOverview(sse *datastar.ServerSentEventGenerator) {
	var totalVisits int64
	row := h.db.QueryRow("SELECT COUNT(*) FROM page_views")
	if err := row.Scan(&totalVisits); err != nil {
		h.logger.Error("failed to count page views", "error", err)
	}

	var uniqueVisitors int64
	row = h.db.QueryRow("SELECT COUNT(DISTINCT visitor_hash) FROM page_views WHERE visitor_hash != ''")
	if err := row.Scan(&uniqueVisitors); err != nil {
		h.logger.Error("failed to count unique visitors", "error", err)
	}

	h.mu.Lock()
	discordMembers := h.discordMembers
	worldCount := h.worldCount
	h.mu.Unlock()

	if patchErr := sse.PatchElementTempl(
		p.StatsOverview(totalVisits, uniqueVisitors, discordMembers, worldCount),
		datastar.WithSelectorID("stats-overview"),
		datastar.WithModeInner(),
	); patchErr != nil {
		h.logger.Error("failed to patch stats overview", "error", patchErr)
	}
}

func (h *Handler) sendMetricsGraph(sse *datastar.ServerSentEventGenerator) {
	graphData := h.queryGraphData("-1 hour", 1*time.Hour)
	graphData.TimeLabel = "Last Hour"

	if patchErr := sse.PatchElementTempl(
		p.MetricsGraph(graphData),
		datastar.WithSelectorID("metrics-graph"),
		datastar.WithModeInner(),
	); patchErr != nil {
		h.logger.Error("failed to patch metrics graph", "error", patchErr)
	}
}

// graphRangeSignals is the signal struct read from POST /status/graph.
// The select component namespaces signals as {graphRange: {value: "1h", ...}}.
type graphRangeSignals struct {
	GraphRange struct {
		Value string `json:"value"`
	} `json:"graphRange"`
}

// graphRangeMap maps signal values to SQLite datetime offsets, display labels, and durations.
var graphRangeMap = map[string]struct {
	offset   string
	label    string
	duration time.Duration
}{
	"1h":  {"-1 hour", "Last Hour", 1 * time.Hour},
	"6h":  {"-6 hours", "Last 6 Hours", 6 * time.Hour},
	"24h": {"-24 hours", "Last 24 Hours", 24 * time.Hour},
	"7d":  {"-7 days", "Last 7 Days", 7 * 24 * time.Hour},
}

// HandleGraphUpdate handles POST /status/graph — reads graphRange signal, returns updated graph.
func (h *Handler) HandleGraphUpdate(c echo.Context) error {
	var signals graphRangeSignals
	if err := datastar.ReadSignals(c.Request(), &signals); err != nil {
		h.logger.Error("failed to read graph range signals", "error", err)
		return echo.NewHTTPError(400, "invalid signals")
	}

	entry, ok := graphRangeMap[signals.GraphRange.Value]
	if !ok {
		entry = graphRangeMap["1h"]
	}

	graphData := h.queryGraphData(entry.offset, entry.duration)
	graphData.TimeLabel = entry.label

	sse := datastar.NewSSE(c.Response().Writer, c.Request())
	if patchErr := sse.PatchElementTempl(
		p.MetricsGraph(graphData),
		datastar.WithSelectorID("metrics-graph"),
		datastar.WithModeInner(),
	); patchErr != nil {
		h.logger.Error("failed to patch metrics graph", "error", patchErr)
	}
	return nil
}

type snapshotRow struct {
	memBytes    uint64
	totalVisits int64
	createdAt   time.Time
}

func (h *Handler) queryGraphData(timeOffset string, rangeDuration time.Duration) p.GraphData {
	now := time.Now()
	rangeStart := now.Add(-rangeDuration)

	rows, err := h.db.Query(
		"SELECT mem_used_bytes, total_visits, created_at FROM metrics_snapshots WHERE created_at >= datetime('now', ?) ORDER BY created_at ASC",
		timeOffset,
	)
	if err != nil {
		h.logger.Error("failed to query metrics snapshots", "error", err)
		return p.GraphData{}
	}
	defer func() { _ = rows.Close() }()

	var snapshots []snapshotRow
	for rows.Next() {
		var s snapshotRow
		if err := rows.Scan(&s.memBytes, &s.totalVisits, &s.createdAt); err != nil {
			h.logger.Error("failed to scan snapshot row", "error", err)
			continue
		}
		snapshots = append(snapshots, s)
	}

	if len(snapshots) == 0 {
		return p.GraphData{}
	}

	// Pad with zero at range start if data doesn't cover the full range.
	if snapshots[0].createdAt.Sub(rangeStart) > 1*time.Minute {
		snapshots = append([]snapshotRow{{createdAt: rangeStart}}, snapshots...)
	}

	return buildGraphData(snapshots, rangeStart, now)
}

func buildGraphData(snapshots []snapshotRow, rangeStart, rangeEnd time.Time) p.GraphData {
	n := len(snapshots)
	data := p.GraphData{
		PointCount: n,
		MemoryLast: humanize.Bytes(snapshots[n-1].memBytes),
		VisitsLast: fmt.Sprintf("%d", snapshots[n-1].totalVisits),
	}

	// Find memory min/max for range normalization.
	var memMin, memMax uint64
	memMin = math.MaxUint64
	for _, s := range snapshots {
		if s.memBytes < memMin {
			memMin = s.memBytes
		}
		if s.memBytes > memMax {
			memMax = s.memBytes
		}
	}
	data.MemoryMax = humanize.Bytes(memMax)
	data.MemoryMid = humanize.Bytes((memMin + memMax) / 2)
	data.MemoryMin = humanize.Bytes(memMin)

	// Find visits min/max for normalization.
	var visitsMin, visitsMax int64
	visitsMin = math.MaxInt64
	for _, s := range snapshots {
		if s.totalVisits < visitsMin {
			visitsMin = s.totalVisits
		}
		if s.totalVisits > visitsMax {
			visitsMax = s.totalVisits
		}
	}
	data.VisitsMin = visitsMin
	data.VisitsMax = visitsMax

	// Build SVG paths. X: 0-100 (timestamp-based), Y: 0-100 (inverted: 0=top, 100=bottom).
	// Both axes are range-normalized to their own min/max.
	memRange := float64(memMax - memMin)
	visitsRange := float64(visitsMax - visitsMin)
	totalSeconds := rangeEnd.Sub(rangeStart).Seconds()

	memPath := ""
	visitsPath := ""
	for i, s := range snapshots {
		x := 0.0
		if totalSeconds > 0 {
			x = s.createdAt.Sub(rangeStart).Seconds() / totalSeconds * 100
		}
		if x < 0 {
			x = 0
		} else if x > 100 {
			x = 100
		}

		var memNorm float64
		if memRange > 0 {
			memNorm = float64(s.memBytes-memMin) / memRange * 100
		} else {
			memNorm = 50
		}
		memY := 100 - memNorm

		var visitsNorm float64
		if visitsRange > 0 {
			visitsNorm = float64(s.totalVisits-visitsMin) / visitsRange * 100
		} else {
			visitsNorm = 50
		}
		visitsY := 100 - visitsNorm

		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		memPath += fmt.Sprintf("%s%.1f,%.1f ", cmd, x, memY)
		visitsPath += fmt.Sprintf("%s%.1f,%.1f ", cmd, x, visitsY)
	}

	data.MemoryPath = memPath
	data.VisitsPath = visitsPath
	return data
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
			datastar.WithModeInner(),
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
		datastar.WithModeInner(),
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
			datastar.WithModeInner(),
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
			datastar.WithModeInner(),
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
			datastar.WithModeInner(),
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
		datastar.WithModeInner(),
	); patchErr != nil {
		h.logger.Error("failed to patch world status", "error", patchErr)
	}
}

// runRetentionCleanup deletes old page_views and metrics_snapshots rows once per hour.
func (h *Handler) runRetentionCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	h.cleanOldData() // run once on startup

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.cleanOldData()
		}
	}
}

func (h *Handler) cleanOldData() {
	res, err := h.db.Exec("DELETE FROM page_views WHERE created_at < datetime('now', '-30 days')")
	if err != nil {
		h.logger.Error("retention: failed to clean page_views", "error", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		h.logger.Info("retention: cleaned page_views", "deleted", n)
	}

	res, err = h.db.Exec("DELETE FROM metrics_snapshots WHERE created_at < datetime('now', '-30 days')")
	if err != nil {
		h.logger.Error("retention: failed to clean metrics_snapshots", "error", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		h.logger.Info("retention: cleaned metrics_snapshots", "deleted", n)
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
