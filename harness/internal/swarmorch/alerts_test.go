package swarmorch

import (
	"sync"
	"testing"
	"time"

	"creative-mode/harness/internal/swarm"
)

// mockDiscordSender records messages for testing.
type mockDiscordSender struct {
	mu       sync.Mutex
	messages []string
}

func (m *mockDiscordSender) SendMessage(_, content string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, content)

	return "msg-1", nil
}

func (m *mockDiscordSender) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.messages)
}

func TestAlertManagerDedup(t *testing.T) {
	t.Parallel()

	sender := &mockDiscordSender{}
	mgr := NewAlertManager(sender, "ch-1", "ch-err-1", testLogger())

	// First fire should succeed.
	mgr.FireTerminalFailure("TKT-1", "out of retries")

	// Wait for goroutine to fire.
	time.Sleep(50 * time.Millisecond)

	if sender.count() != 1 {
		t.Errorf("expected 1 message, got %d", sender.count())
	}

	// Same alert within window should be deduped.
	mgr.FireTerminalFailure("TKT-1", "out of retries again")
	time.Sleep(50 * time.Millisecond)

	if sender.count() != 1 {
		t.Errorf("expected still 1 message after dedup, got %d", sender.count())
	}

	// Different ticket should fire.
	mgr.FireTerminalFailure("TKT-2", "different ticket")
	time.Sleep(50 * time.Millisecond)

	if sender.count() != 2 {
		t.Errorf("expected 2 messages, got %d", sender.count())
	}
}

func TestAlertManagerDifferentTypes(t *testing.T) {
	t.Parallel()

	sender := &mockDiscordSender{}
	mgr := NewAlertManager(sender, "ch-1", "ch-err-1", testLogger())

	mgr.FireTerminalFailure("TKT-1", "failed")
	mgr.FireCrashRecovery("TKT-1", swarm.PhaseImplement)
	mgr.FireStallDetected("TKT-1", swarm.PhaseResearch, 45)

	time.Sleep(100 * time.Millisecond)

	if sender.count() != 3 {
		t.Errorf("expected 3 messages for different alert types, got %d", sender.count())
	}
}

func TestAlertManagerNilDiscord(t *testing.T) {
	t.Parallel()

	// Should not panic with nil discord.
	mgr := NewAlertManager(nil, "", "", testLogger())
	mgr.FireTerminalFailure("TKT-1", "test")
	// Just verify no panic.
}

func TestAlertManagerDedupExpiry(t *testing.T) {
	t.Parallel()

	mgr := NewAlertManager(nil, "", "", testLogger())

	// Fire once.
	if !mgr.shouldFire("test-key") {
		t.Error("first fire should succeed")
	}

	// Should be deduped.
	if mgr.shouldFire("test-key") {
		t.Error("second fire within window should be deduped")
	}

	// Manually expire the entry.
	mgr.mu.Lock()
	mgr.dedup["test-key"] = time.Now().Add(-2 * alertDedupWindow)
	mgr.mu.Unlock()

	// Should fire again after expiry.
	if !mgr.shouldFire("test-key") {
		t.Error("fire after expiry should succeed")
	}
}
