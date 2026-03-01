package swarmorch

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"creative-mode/harness/internal/swarm"
)

func TestCompletionRegistry_RegisterAndSignal(t *testing.T) {
	t.Parallel()

	reg := NewCompletionRegistry()
	ch := reg.Register("sess-01")

	result := SessionResult{Result: swarm.ResultSuccess, Summary: "done"}
	if !reg.Signal("sess-01", result) {
		t.Fatal("Signal returned false; want true")
	}

	select {
	case got := <-ch:
		if got.Result != swarm.ResultSuccess {
			t.Errorf("result = %v; want %v", got.Result, swarm.ResultSuccess)
		}
		if got.Summary != "done" {
			t.Errorf("summary = %q; want %q", got.Summary, "done")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestCompletionRegistry_SignalUnregistered(t *testing.T) {
	t.Parallel()

	reg := NewCompletionRegistry()
	if reg.Signal("unknown", SessionResult{}) {
		t.Fatal("Signal returned true for unregistered session")
	}
}

func TestCompletionRegistry_DoubleSignal(t *testing.T) {
	t.Parallel()

	reg := NewCompletionRegistry()
	_ = reg.Register("sess-01")

	result := SessionResult{Result: swarm.ResultSuccess}
	reg.Signal("sess-01", result)

	// Second signal should return false (buffer full).
	if reg.Signal("sess-01", result) {
		t.Fatal("second Signal returned true; want false")
	}
}

func TestCompletionRegistry_Unregister(t *testing.T) {
	t.Parallel()

	reg := NewCompletionRegistry()
	_ = reg.Register("sess-01")
	reg.Unregister("sess-01")

	if reg.Signal("sess-01", SessionResult{}) {
		t.Fatal("Signal returned true after Unregister")
	}
}

func TestCompletionRegistry_Concurrent(t *testing.T) {
	t.Parallel()

	reg := NewCompletionRegistry()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			sessID := fmt.Sprintf("sess-%d-%s", i, time.Now().Format("150405.000000000"))
			ch := reg.Register(sessID)
			reg.Signal(sessID, SessionResult{Result: swarm.ResultSuccess})
			<-ch
			reg.Unregister(sessID)
		}()
	}

	wg.Wait()
}

func TestStartRegistry_RegisterAndSignal(t *testing.T) {
	t.Parallel()

	reg := NewStartRegistry()
	ch := reg.Register("sess-01")

	if !reg.Signal("sess-01") {
		t.Fatal("Signal returned false; want true")
	}

	select {
	case <-ch:
		// OK
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestStartRegistry_SignalUnregistered(t *testing.T) {
	t.Parallel()

	reg := NewStartRegistry()
	if reg.Signal("unknown") {
		t.Fatal("Signal returned true for unregistered session")
	}
}

func TestStartRegistry_Unregister(t *testing.T) {
	t.Parallel()

	reg := NewStartRegistry()
	_ = reg.Register("sess-01")
	reg.Unregister("sess-01")

	if reg.Signal("sess-01") {
		t.Fatal("Signal returned true after Unregister")
	}
}
