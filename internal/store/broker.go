package store

import (
	"sync"

	"conduit/internal/model"
)

// Broker is a minimal fan-out pub/sub for run events. Each subscriber gets its
// own buffered channel; slow subscribers drop events rather than block writers.
//
// Publish delivers locally AND forwards to the optional relay (e.g. Postgres
// NOTIFY) so other instances see the event. Deliver is local-only and is used by
// the relay's receive side (LISTEN) to avoid an echo loop.
type Broker struct {
	mu    sync.Mutex
	next  int
	subs  map[int]chan model.Event
	relay func(model.Event) // optional external forwarder
}

func newBroker() *Broker {
	return &Broker{subs: map[int]chan model.Event{}}
}

// SetRelay installs an external forwarder invoked on Publish (not on Deliver).
func (b *Broker) SetRelay(fn func(model.Event)) {
	b.mu.Lock()
	b.relay = fn
	b.mu.Unlock()
}

// Subscribe registers a new subscriber and returns its id and channel.
func (b *Broker) Subscribe() (int, <-chan model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan model.Event, 64)
	b.subs[id] = ch
	return id, ch
}

// Unsubscribe removes and closes a subscriber channel.
func (b *Broker) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(ch)
	}
}

// Publish delivers locally and forwards to the relay (if any).
func (b *Broker) Publish(e model.Event) {
	b.mu.Lock()
	relay := b.relay
	b.mu.Unlock()
	b.Deliver(e)
	if relay != nil {
		relay(e)
	}
}

// Deliver sends an event to local subscribers only, without relaying.
func (b *Broker) Deliver(e model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default: // subscriber is behind; drop to keep the engine moving
		}
	}
}
