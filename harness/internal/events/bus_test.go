package events

import "testing"

func TestEventBus_PublishGlobalSequencing(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()

	bus.PublishGlobal("event-1")
	bus.PublishGlobal("event-2")
	bus.PublishGlobal("event-3")

	seq := bus.CurrentSeq()
	if seq != 3 {
		t.Errorf("CurrentSeq() = %d; want 3", seq)
	}

	// Verify monotonic: events since 0 should have seq 1, 2, 3.
	events := bus.EventsSince(0)
	if len(events) != 3 {
		t.Fatalf("EventsSince(0) returned %d events; want 3", len(events))
	}

	for i, ne := range events {
		expected := int64(i + 1)
		if ne.Seq != expected {
			t.Errorf("events[%d].Seq = %d; want %d", i, ne.Seq, expected)
		}
	}
}

func TestEventBus_EventsSince(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()

	for i := range 10 {
		bus.PublishGlobal(i)
	}

	events := bus.EventsSince(5)
	if len(events) != 5 {
		t.Fatalf("EventsSince(5) returned %d events; want 5", len(events))
	}

	// Should be seq 6, 7, 8, 9, 10.
	for i, ne := range events {
		expected := int64(i + 6)
		if ne.Seq != expected {
			t.Errorf("events[%d].Seq = %d; want %d", i, ne.Seq, expected)
		}
	}
}

func TestEventBus_RingBufferWraparound(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()

	// Publish more than ringSize events.
	total := ringSize + 100
	for i := range total {
		bus.PublishGlobal(i)
	}

	// Only last ringSize events should be in the buffer.
	events := bus.EventsSince(0)
	if len(events) != ringSize {
		t.Fatalf(
			"EventsSince(0) after wraparound returned %d events; want %d",
			len(events),
			ringSize,
		)
	}

	// First should be seq total-ringSize+1, last should be seq total.
	firstExpected := int64(total - ringSize + 1)
	if events[0].Seq != firstExpected {
		t.Errorf("first event Seq = %d; want %d", events[0].Seq, firstExpected)
	}

	lastExpected := int64(total)
	if events[len(events)-1].Seq != lastExpected {
		t.Errorf("last event Seq = %d; want %d", events[len(events)-1].Seq, lastExpected)
	}
}

func TestEventBus_EventsSinceOrdering(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()

	for i := range 50 {
		bus.PublishGlobal(i)
	}

	events := bus.EventsSince(20)

	// Verify sorted by Seq.
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Errorf("events not sorted: events[%d].Seq=%d <= events[%d].Seq=%d",
				i, events[i].Seq, i-1, events[i-1].Seq)
		}
	}
}
