// Package bus is the in-process, per-workspace publish/subscribe channel
// that carries committed events to SSE handlers and other listeners.
package bus

import (
	"sync"

	"github.com/necrogami/kanboard/internal/core/events"
)

// Bus fans events out per workspace.
type Bus struct {
	mu     sync.Mutex
	buffer int
	subs   map[string]map[*Sub]struct{}
}

// Sub is one subscription. Receive from C; call Close when done. C is
// closed by the bus if the subscriber falls behind (slow consumer).
type Sub struct {
	C       <-chan events.Event
	c       chan events.Event
	ws      string
	bus     *Bus
	once    sync.Once
	evicted bool
}

// Evicted reports whether the bus closed this subscription because it
// fell behind. False after the subscriber's own Close.
func (s *Sub) Evicted() bool {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	return s.evicted
}

// New returns a bus whose subscriptions buffer up to buffer events.
func New(buffer int) *Bus {
	return &Bus{buffer: buffer, subs: map[string]map[*Sub]struct{}{}}
}

// Subscribe registers interest in one workspace.
func (b *Bus) Subscribe(workspaceID string) *Sub {
	c := make(chan events.Event, b.buffer)
	s := &Sub{C: c, c: c, ws: workspaceID, bus: b}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[workspaceID] == nil {
		b.subs[workspaceID] = map[*Sub]struct{}{}
	}
	b.subs[workspaceID][s] = struct{}{}
	return s
}

// Close unsubscribes; safe to call more than once.
func (s *Sub) Close() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs[s.ws], s)
		s.bus.mu.Unlock()
		close(s.c)
	})
}

// Publish delivers ev to every subscriber of its workspace without
// blocking. A subscriber whose buffer is full is closed and dropped.
func (b *Bus) Publish(ev events.Event) {
	b.mu.Lock()
	var slow []*Sub
	for s := range b.subs[ev.WorkspaceID] {
		select {
		case s.c <- ev:
		default:
			s.evicted = true
			slow = append(slow, s)
		}
	}
	b.mu.Unlock()
	for _, s := range slow {
		s.Close()
	}
}
