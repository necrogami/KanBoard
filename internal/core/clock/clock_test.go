package clock_test

import (
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
)

func TestFakeAdvances(t *testing.T) {
	f := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
	a := f.Now()
	f.Advance(90 * time.Second)
	if f.Now().Sub(a) != 90*time.Second {
		t.Fatal("Advance did not move the clock")
	}
}

func TestMillisRoundTrip(t *testing.T) {
	ts := time.Date(2026, 8, 29, 12, 0, 0, 123456789, time.UTC)
	ms := clock.Millis(ts)
	if ms != 1788004800123 {
		t.Fatalf("Millis = %d", ms)
	}
	if back := clock.FromMillis(ms); !back.Equal(ts.Truncate(time.Millisecond)) {
		t.Fatalf("FromMillis = %v", back)
	}
}
