package swarmorch

import (
	"testing"
	"time"
)

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
