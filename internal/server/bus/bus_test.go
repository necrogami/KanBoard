package bus_test

import (
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/server/bus"
)

func recv(t *testing.T, c <-chan events.Event) (events.Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-c:
		return ev, ok
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s")
		return events.Event{}, false
	}
}

func TestDeliversInOrderPerWorkspace(t *testing.T) {
	b := bus.New(8)
	a := b.Subscribe("w1")
	defer a.Close()
	other := b.Subscribe("w2")
	defer other.Close()
	for i := int64(1); i <= 3; i++ {
		b.Publish(events.Event{WorkspaceID: "w1", Seq: i})
	}
	for want := int64(1); want <= 3; want++ {
		ev, _ := recv(t, a.C)
		if ev.Seq != want {
			t.Fatalf("seq = %d, want %d", ev.Seq, want)
		}
	}
	select {
	case ev := <-other.C:
		t.Fatalf("w2 received %+v", ev)
	default:
	}
}

func TestSlowConsumerIsClosedNotBlocking(t *testing.T) {
	b := bus.New(2)
	slow := b.Subscribe("w1")
	fast := b.Subscribe("w1")
	b.Publish(events.Event{WorkspaceID: "w1", Seq: 1})
	b.Publish(events.Event{WorkspaceID: "w1", Seq: 2})
	// fast keeps up; slow never reads.
	recv(t, fast.C)
	recv(t, fast.C)
	b.Publish(events.Event{WorkspaceID: "w1", Seq: 3}) // overflows slow only
	ev, ok := recv(t, fast.C)
	if !ok || ev.Seq != 3 {
		t.Fatalf("fast got %+v, %v", ev, ok)
	}
	n := 0
	for {
		_, ok := recv(t, slow.C)
		if !ok {
			break
		}
		n++
	}
	if n != 2 {
		t.Fatalf("slow received %d before close, want 2", n)
	}
	fast.Close()
	slow.Close() // idempotent after the bus closed it
}

func TestCloseStopsDelivery(t *testing.T) {
	b := bus.New(2)
	s := b.Subscribe("w1")
	s.Close()
	b.Publish(events.Event{WorkspaceID: "w1", Seq: 1}) // must not panic on closed channel
	if _, ok := <-s.C; ok {
		t.Fatal("received after close")
	}
}

func TestEvictedDistinguishesOverflowFromClose(t *testing.T) {
	b := bus.New(1)
	slow := b.Subscribe("w1")
	own := b.Subscribe("w1")
	own.Close()
	if own.Evicted() {
		t.Fatal("self-closed subscription reports evicted")
	}
	b.Publish(events.Event{WorkspaceID: "w1", Seq: 1})
	b.Publish(events.Event{WorkspaceID: "w1", Seq: 2}) // overflows slow
	if _, ok := recv(t, slow.C); !ok {
		t.Fatal("first event lost")
	}
	if _, ok := recv(t, slow.C); ok {
		t.Fatal("channel not closed after overflow")
	}
	if !slow.Evicted() {
		t.Fatal("overflowed subscription does not report evicted")
	}
}
