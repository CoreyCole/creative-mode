package events

import "sync"

const eventChannelBuffer = 100

// EventBus supports both global (all players) and per-world pub/sub.
// SSE handlers subscribe to both global and world-specific channels.
type EventBus struct {
	mu         sync.RWMutex
	worldSubs  map[string][]chan any // worldID -> subscriber channels
	globalSubs []chan any            // all-player subscribers
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

// PublishGlobal sends an event to all global subscribers.
func (b *EventBus) PublishGlobal(event any) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.globalSubs {
		select {
		case ch <- event:
		default: // drop if subscriber is slow
		}
	}
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
