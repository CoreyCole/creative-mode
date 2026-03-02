package swarmorch

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"creative-mode/harness/internal/swarm"
)

const (
	alertDedupWindow = time.Hour
)

// DiscordSender is the interface for sending messages to Discord.
// Satisfied by *worldchannel.Client.
type DiscordSender interface {
	SendMessage(channelID, content string) (string, error)
}

// AlertManager sends Discord alerts for swarm operational events with
// deduplication to avoid spam. All alert methods are fire-and-forget.
// Operational alerts go to channelID; integration errors go to errChannelID.
type AlertManager struct {
	discord      DiscordSender
	channelID    string
	errChannelID string
	logger       *slog.Logger
	mu           sync.Mutex
	dedup        map[string]time.Time // alertKey → lastFired
}

// NewAlertManager creates a new AlertManager. If discord is nil or channelID
// is empty, alerts are logged but not sent.
func NewAlertManager(
	discord DiscordSender,
	channelID string,
	errChannelID string,
	logger *slog.Logger,
) *AlertManager {
	return &AlertManager{
		discord:      discord,
		channelID:    channelID,
		errChannelID: errChannelID,
		logger:       logger,
		dedup:        make(map[string]time.Time),
	}
}

// FireTerminalFailure alerts when a workflow reaches terminal failure.
func (a *AlertManager) FireTerminalFailure(ticketID, reason string) {
	key := "terminal:" + ticketID
	msg := fmt.Sprintf(
		"**[SWARM ALERT] Terminal Failure**\nTicket: `%s`\nReason: %s",
		ticketID,
		reason,
	)

	a.fireAsync(key, msg)
}

// FireCrashRecovery alerts when a session crashes and is recovered.
func (a *AlertManager) FireCrashRecovery(ticketID string, phase swarm.Phase) {
	key := fmt.Sprintf("crash:%s:%s", ticketID, phase)
	msg := fmt.Sprintf(
		"**[SWARM ALERT] Crash Recovery**\nTicket: `%s`\nPhase: `%s`\nSession crashed — recovered via tmux fallback.",
		ticketID,
		phase,
	)

	a.fireAsync(key, msg)
}

// FireStallDetected alerts when a workflow appears stalled.
func (a *AlertManager) FireStallDetected(
	ticketID string,
	phase swarm.Phase,
	minutes int,
) {
	key := "stall:" + ticketID
	msg := fmt.Sprintf(
		"**[SWARM ALERT] Stall Detected**\nTicket: `%s`\nPhase: `%s`\nNo progress for %d minutes.",
		ticketID,
		phase,
		minutes,
	)

	a.fireAsync(key, msg)
}

// FireGateReached alerts when a workflow enters a human review gate.
func (a *AlertManager) FireGateReached(ticketID string, phase swarm.Phase) {
	key := fmt.Sprintf("gate:%s:%s", ticketID, phase)
	msg := fmt.Sprintf(
		"**[SWARM] Gate Reached**\nTicket: `%s`\nPhase: `%s`\nHuman review required — approve or reject in the dashboard.",
		ticketID,
		phase,
	)

	a.fireAsync(key, msg)
}

// FireHighRetryRate alerts when a workflow has an unusually high retry rate.
func (a *AlertManager) FireHighRetryRate(
	ticketID string,
	phase swarm.Phase,
	attempt int,
) {
	key := "high-retry:" + ticketID
	msg := fmt.Sprintf(
		"**[SWARM ALERT] High Retry Rate**\nTicket: `%s`\nPhase: `%s`\nAttempt %d — consider investigating.",
		ticketID,
		phase,
		attempt,
	)

	a.fireAsync(key, msg)
}

// FireError sends an integration error to the errors channel. No dedup —
// every unique error is reported. Used for Linear API failures, session
// spawn failures, and other integration issues that should not be silent.
func (a *AlertManager) FireError(component, detail string) {
	msg := fmt.Sprintf(
		"**[SWARM ERROR] %s**\n%s",
		component,
		detail,
	)

	a.logger.Error("swarm error", "component", component, "detail", detail)

	chID := a.errChannelID
	if chID == "" {
		chID = a.channelID // fall back to main alerts channel
	}

	if a.discord == nil || chID == "" {
		return
	}

	go func() {
		if _, err := a.discord.SendMessage(chID, msg); err != nil {
			a.logger.Error("failed to send swarm error alert",
				"component", component, "error", err)
		}
	}()
}

// fireAsync sends a Discord alert in a goroutine with dedup.
func (a *AlertManager) fireAsync(key, msg string) {
	if !a.shouldFire(key) {
		return
	}

	a.logger.Warn("swarm alert", "key", key, "message", msg)

	if a.discord == nil || a.channelID == "" {
		return
	}

	go func() {
		if _, err := a.discord.SendMessage(a.channelID, msg); err != nil {
			a.logger.Error("failed to send swarm alert", "key", key, "error", err)
		}
	}()
}

// shouldFire checks dedup and records the alert if allowed.
func (a *AlertManager) shouldFire(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()

	// Clean expired entries while we're here.
	for k, t := range a.dedup {
		if now.Sub(t) > alertDedupWindow {
			delete(a.dedup, k)
		}
	}

	if lastFired, ok := a.dedup[key]; ok {
		if now.Sub(lastFired) < alertDedupWindow {
			return false
		}
	}

	a.dedup[key] = now

	return true
}
