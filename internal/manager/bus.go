package manager

import (
	"sync"

	"github.com/vishwa/dm/internal/model"
)

// Bus fans manager events out to subscribers. Publishing never blocks: a slow
// subscriber drops events instead of stalling the manager.
type Bus struct {
	mu   sync.Mutex
	subs map[int]chan model.Event
	next int
}

func newBus() *Bus {
	return &Bus{subs: make(map[int]chan model.Event)}
}

// subscribe returns a channel of events and a function that releases it.
func (b *Bus) subscribe() (<-chan model.Event, func()) {
	b.mu.Lock()
	id := b.next
	b.next++
	ch := make(chan model.Event, 64)
	b.subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			close(ch)
			b.mu.Unlock()
		})
	}
}

func (b *Bus) publish(ev model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
