package events

import (
	"sort"
	"sync"
	"sync/atomic"
)

const (
	eventChannelBuffer = 100
	ringSize           = 1000
)

// NumberedEvent is an event with a monotonic sequence number.
type NumberedEvent struct {
	Seq   int64 `json:"seq"`
	Event any   `json:"event"`
}

// EventBus supports both global (all players) and per-world pub/sub.
// SSE handlers subscribe to both global and world-specific channels.
// Global events are also stored in a ring buffer for replay.
type EventBus struct {
	mu         sync.RWMutex
	worldSubs  map[string][]chan any // worldID -> subscriber channels
	globalSubs []chan any            // all-player subscribers

	// Ring buffer for global event replay.
	seq      atomic.Int64
	ringMu   sync.RWMutex
	ring     [ringSize]NumberedEvent
	ringHead int // next write position
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		worldSubs: make(map[string][]chan any),
	}
}

// SubscribeGlobal creates a channel that receives all global events
// (chat messages, build notifications visible to all players).
func (b *EventBus) SubscribeGlobal() chan any {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan any, eventChannelBuffer)
	b.globalSubs = append(b.globalSubs, ch)

	return ch
}

// UnsubscribeGlobal removes a global subscriber channel.
func (b *EventBus) UnsubscribeGlobal(ch chan any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, sub := range b.globalSubs {
		if sub != ch {
			continue
		}

		b.globalSubs = append(b.globalSubs[:i], b.globalSubs[i+1:]...)
		close(ch)

		return
	}
}

// PublishGlobal sends an event to all global subscribers and stores it in the ring buffer.
func (b *EventBus) PublishGlobal(event any) {
	seq := b.seq.Add(1)

	// Store in ring buffer.
	b.ringMu.Lock()
	b.ring[b.ringHead] = NumberedEvent{Seq: seq, Event: event}
	b.ringHead = (b.ringHead + 1) % ringSize
	b.ringMu.Unlock()

	// Fan out to subscribers.
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.globalSubs {
		select {
		case ch <- event:
		default: // drop if subscriber is slow
		}
	}
}

// EventsSince returns all buffered events with Seq > lastID, sorted by Seq.
func (b *EventBus) EventsSince(lastID int64) []NumberedEvent {
	b.ringMu.RLock()
	defer b.ringMu.RUnlock()

	var result []NumberedEvent

	for i := range ringSize {
		ne := b.ring[i]
		if ne.Seq > lastID {
			result = append(result, ne)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Seq < result[j].Seq
	})

	return result
}

// CurrentSeq returns the latest sequence number.
func (b *EventBus) CurrentSeq() int64 {
	return b.seq.Load()
}

// Subscribe creates a channel for world-specific events.
func (b *EventBus) Subscribe(worldID string) chan any {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan any, eventChannelBuffer)
	b.worldSubs[worldID] = append(b.worldSubs[worldID], ch)

	return ch
}

// Unsubscribe removes a world-specific subscriber.
func (b *EventBus) Unsubscribe(worldID string, ch chan any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.worldSubs[worldID]
	for i, sub := range subs {
		if sub != ch {
			continue
		}

		b.worldSubs[worldID] = append(subs[:i], subs[i+1:]...)
		close(ch)

		return
	}
}

// Publish sends an event to all subscribers of a specific world.
func (b *EventBus) Publish(worldID string, event any) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.worldSubs[worldID] {
		select {
		case ch <- event:
		default:
		}
	}
}
