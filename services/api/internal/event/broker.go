package event

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// Broker is a thread-safe in-memory SSE pub/sub hub.
// Each subscriber gets a buffered channel. Slow subscribers drop messages.
type Broker struct {
	mu           sync.RWMutex
	subscribers  map[string]chan []byte
	droppedTotal atomic.Int64
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[string]chan []byte),
	}
}

func (b *Broker) Subscribe(id string) chan []byte {
	ch := make(chan []byte, 16)
	b.mu.Lock()
	b.subscribers[id] = ch
	b.mu.Unlock()
	return ch
}

func (b *Broker) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
	}
}

// Publish fans out msg to all subscribers. Drops for any whose buffer is full.
func (b *Broker) Publish(msg []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for id, ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			b.droppedTotal.Add(1)
			slog.Warn("sse event dropped", "subscriber_id", id)
		}
	}
}

// DroppedTotal returns the total number of dropped SSE events since startup.
func (b *Broker) DroppedTotal() int64 {
	return b.droppedTotal.Load()
}
