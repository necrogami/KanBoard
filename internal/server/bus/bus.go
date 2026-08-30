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
	allow   func(events.Event) bool
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

// Subscribe registers interest in every event in one workspace.
//
// Workspace membership does not imply project membership, and events
// carry project and board ids and payloads that include card titles, so
// a raw subscriber must filter what it forwards to a reader. Prefer
// service.Subscribe, which applies the actor's policy for you.
func (b *Bus) Subscribe(workspaceID string) *Sub {
	return b.SubscribeFunc(workspaceID, nil)
}

// SubscribeFunc is Subscribe with a per-event filter: only events for
// which allow returns true reach C. A nil allow accepts everything.
// allow runs on the publishing goroutine while the bus lock is held, so
// it must be cheap and must not block or call back into the bus.
func (b *Bus) SubscribeFunc(workspaceID string, allow func(events.Event) bool) *Sub {
	c := make(chan events.Event, b.buffer)
	s := &Sub{C: c, c: c, ws: workspaceID, allow: allow, bus: b}
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
		if s.allow != nil && !s.allow(ev) {
			continue
		}
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
