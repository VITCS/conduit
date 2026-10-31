package store

import (
	"sync"

	"conduit/internal/model"
)

// Broker is a minimal fan-out pub/sub for run events. Each subscriber gets its
// own buffered channel; slow subscribers drop events rather than block writers.
type Broker struct {
	mu   sync.Mutex
	next int
	subs map[int]chan model.Event
}

func newBroker() *Broker {
	return &Broker{subs: map[int]chan model.Event{}}
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

// Publish delivers an event to every subscriber without blocking.
func (b *Broker) Publish(e model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default: // subscriber is behind; drop to keep the engine moving
		}
	}
}
