package swarmorch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"creative-mode/harness/internal/swarm"
	"creative-mode/harness/internal/swarm/prompt"
)

// DoctorStatus represents the health state of a single check.
type DoctorStatus string

const (
	DoctorOK   DoctorStatus = "ok"
	DoctorWarn DoctorStatus = "warn"
	DoctorFail DoctorStatus = "fail"

	doctorTimeout = 5 * time.Second

	diskPctMultiplier = 100
	diskWarnPercent   = 90
	diskFailPercent   = 95
)

// DoctorCheck is the result of a single diagnostic check.
type DoctorCheck struct {
	Name   string       `json:"name"`
	Status DoctorStatus `json:"status"`
	Detail string       `json:"detail"`
}

// DoctorReport is the aggregate result of all diagnostic checks.
type DoctorReport struct {
	Overall DoctorStatus  `json:"overall"`
	Checks  []DoctorCheck `json:"checks"`
}

// RunDoctor executes all diagnostic checks and returns the report.
func (m *Manager) RunDoctor(ctx context.Context) *DoctorReport {
	checks := []DoctorCheck{
		m.checkDB(ctx),
		m.checkTmux(ctx),
		m.checkLinear(ctx),
		checkGraphite(),
		checkDiscord(),
		m.checkOrphanedSessions(ctx),
		m.checkDiskSpace(),
		checkPromptTemplates(),
	}

	overall := DoctorOK
	for _, c := range checks {
		if c.Status == DoctorFail {
			overall = DoctorFail

			break
		}
		if c.Status == DoctorWarn {
			overall = DoctorWarn
		}
	}

	return &DoctorReport{
		Overall: overall,
		Checks:  checks,
	}
}

func (m *Manager) checkDB(ctx context.Context) DoctorCheck {
	ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()

	var n int
	if err := m.db.SQLDB().QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil {
		return DoctorCheck{Name: "database", Status: DoctorFail, Detail: err.Error()}
	}

	return DoctorCheck{Name: "database", Status: DoctorOK, Detail: "connected"}
}

func (m *Manager) checkTmux(ctx context.Context) DoctorCheck {
	ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tmux", "-V").Output()
	if err != nil {
		return DoctorCheck{Name: "tmux", Status: DoctorFail, Detail: err.Error()}
	}

	return DoctorCheck{
		Name:   "tmux",
		Status: DoctorOK,
		Detail: string(out[:len(out)-1]),
	} // trim newline
}

func (m *Manager) checkLinear(ctx context.Context) DoctorCheck {
	if m.linearClient == nil {
		return DoctorCheck{
			Name:   "linear",
			Status: DoctorWarn,
			Detail: "LINEAR_API_KEY not configured",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()

	if err := m.linearClient.Ping(ctx); err != nil {
		return DoctorCheck{Name: "linear", Status: DoctorFail, Detail: err.Error()}
	}

	return DoctorCheck{Name: "linear", Status: DoctorOK, Detail: "authenticated"}
}

func checkGraphite() DoctorCheck {
	if os.Getenv("GRAPHITE_TOKEN") == "" {
		return DoctorCheck{
			Name:   "graphite",
			Status: DoctorWarn,
			Detail: "GRAPHITE_TOKEN not set",
		}
	}

	return DoctorCheck{Name: "graphite", Status: DoctorOK, Detail: "token configured"}
}

func checkDiscord() DoctorCheck {
	if os.Getenv("DISCORD_SWARM_CHANNEL_ID") == "" {
		return DoctorCheck{
			Name:   "discord",
			Status: DoctorWarn,
			Detail: "DISCORD_SWARM_CHANNEL_ID not set",
		}
	}

	return DoctorCheck{Name: "discord", Status: DoctorOK, Detail: "channel configured"}
}

func (m *Manager) checkOrphanedSessions(ctx context.Context) DoctorCheck {
	dbCount, err := m.db.CountActiveSwarmSessions(ctx)
	if err != nil {
		return DoctorCheck{
			Name:   "orphaned_sessions",
			Status: DoctorWarn,
			Detail: "db query failed: " + err.Error(),
		}
	}

	tmuxSessions := ListActiveSessions(ctx)
	tmuxCount := int64(len(tmuxSessions))

	if tmuxCount > dbCount {
		return DoctorCheck{
			Name:   "orphaned_sessions",
			Status: DoctorWarn,
			Detail: fmt.Sprintf(
				"tmux has %d sessions, db has %d active",
				tmuxCount,
				dbCount,
			),
		}
	}

	return DoctorCheck{
		Name:   "orphaned_sessions",
		Status: DoctorOK,
		Detail: fmt.Sprintf("db=%d, tmux=%d", dbCount, tmuxCount),
	}
}

func (m *Manager) checkDiskSpace() DoctorCheck {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(m.logsDir, &stat); err != nil {
		return DoctorCheck{
			Name:   "disk_space",
			Status: DoctorWarn,
			Detail: "statfs failed: " + err.Error(),
		}
	}

	totalBlocks := stat.Blocks
	freeBlocks := stat.Bfree
	if totalBlocks == 0 {
		return DoctorCheck{Name: "disk_space", Status: DoctorOK, Detail: "unknown total"}
	}

	usedPct := diskPctMultiplier * (totalBlocks - freeBlocks) / totalBlocks

	detail := fmt.Sprintf("%d%% used", usedPct)

	if usedPct >= diskFailPercent {
		return DoctorCheck{Name: "disk_space", Status: DoctorFail, Detail: detail}
	}

	if usedPct >= diskWarnPercent {
		return DoctorCheck{Name: "disk_space", Status: DoctorWarn, Detail: detail}
	}

	return DoctorCheck{Name: "disk_space", Status: DoctorOK, Detail: detail}
}

func checkPromptTemplates() DoctorCheck {
	phases := []swarm.Phase{
		swarm.PhaseResearch,
		swarm.PhaseCodePlan,
		swarm.PhasePlanReview,
		swarm.PhaseImplement,
		swarm.PhaseVerify,
		swarm.PhasePR,
	}

	dummyCtx := prompt.PromptContext{
		TicketID:   "TEST-0",
		WorkflowID: "test-wf",
		SessionID:  "test-sess",
		Phase:      "test",
		Attempt:    1,
		ResultPath: "/tmp/test-result",
	}

	for _, phase := range phases {
		dummyCtx.Phase = string(phase)
		if _, err := prompt.RenderPrompt(phase, dummyCtx); err != nil {
			return DoctorCheck{
				Name:   "prompt_templates",
				Status: DoctorFail,
				Detail: fmt.Sprintf("phase %s: %v", phase, err),
			}
		}
	}

	return DoctorCheck{
		Name:   "prompt_templates",
		Status: DoctorOK,
		Detail: "all 6 templates render",
	}
}
