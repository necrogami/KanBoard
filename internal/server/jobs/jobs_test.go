package jobs_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/server/jobs"
	"github.com/necrogami/kanboard/internal/server/service"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

type recorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *recorder) handle(_ context.Context, j jobs.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, j.Payload)
	return nil
}

func TestBackoff(t *testing.T) {
	if d := jobs.Backoff(1, 0); d != 30*time.Second {
		t.Fatalf("attempt 1 = %s", d)
	}
	if d := jobs.Backoff(2, 0); d != 60*time.Second {
		t.Fatalf("attempt 2 = %s", d)
	}
	if d := jobs.Backoff(20, 0); d != 4*time.Hour {
		t.Fatalf("attempt 20 = %s (cap)", d)
	}
	if d := jobs.Backoff(1, 1); d != 30*time.Second+7500*time.Millisecond {
		t.Fatalf("attempt 1 max jitter = %s", d)
	}
}

func TestOutboxJobsRunAfterCommit(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		hook := func(ctx context.Context, q store.Querier, ev events.Event) error {
			_, err := jobs.Enqueue(ctx, q, fake.Now(), jobs.Spec{Kind: "test.echo", WorkspaceID: ev.WorkspaceID, Payload: string(ev.Kind)})
			return err
		}
		svc := service.New(st, service.WithClock(fake), service.WithOutboxHook(hook))
		rec := &recorder{}
		r := jobs.New(st, jobs.WithClock(fake), jobs.WithJitter(func() float64 { return 0 }))
		r.Register("test.echo", rec.handle)

		ws, err := svc.CreateWorkspace(ctx, commands.CreateWorkspace{Name: "W", Slug: "w", AdminEmail: "a@b.c", AdminName: "A"})
		if err != nil {
			t.Fatal(err)
		}
		n, err := r.RunOnce(ctx)
		if err != nil || n != 2 {
			t.Fatalf("RunOnce = %d, %v (workspace.created and member.added expected)", n, err)
		}
		// Postgres's UPDATE ... RETURNING does not carry the subquery's
		// ORDER BY through to the returned rows, so RunOnce sorts the
		// leased batch by (run_at, created_at, id) before running it. Both
		// jobs share a millisecond, so the UUIDv7 id is the tie-break and
		// the enqueue order holds on both engines.
		if len(rec.seen) != 2 || rec.seen[0] != "workspace.created" || rec.seen[1] != "member.added" {
			t.Fatalf("seen = %v, want [workspace.created member.added]", rec.seen)
		}
		rows, _ := st.Q().ListJobsByState(ctx, sqlitegen.ListJobsByStateParams{State: "done", Lim: 10})
		if len(rows) != 2 || rows[0].WorkspaceID.String != ws.ID {
			t.Fatalf("done jobs = %+v", rows)
		}
	})
}

func TestFailedJobRetriesWithBackoffThenDies(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		r := jobs.New(st, jobs.WithClock(fake), jobs.WithJitter(func() float64 { return 0 }))
		calls := 0
		r.Register("test.fail", func(context.Context, jobs.Job) error { calls++; return errors.New("nope") })
		if _, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.fail", Payload: "x", MaxAttempts: 2}); err != nil {
			t.Fatal(err)
		}
		if n, _ := r.RunOnce(ctx); n != 1 || calls != 1 {
			t.Fatalf("first run n=%d calls=%d", n, calls)
		}
		if n, _ := r.RunOnce(ctx); n != 0 {
			t.Fatalf("ran again before backoff elapsed: n=%d", n)
		}
		fake.Advance(31 * time.Second)
		if n, _ := r.RunOnce(ctx); n != 1 || calls != 2 {
			t.Fatalf("second run n=%d calls=%d", n, calls)
		}
		dead, _ := st.Q().ListJobsByState(ctx, sqlitegen.ListJobsByStateParams{State: "dead", Lim: 10})
		if len(dead) != 1 || dead[0].LastError.String != "nope" || dead[0].Attempts != 2 {
			t.Fatalf("dead = %+v", dead)
		}
		fake.Advance(time.Hour)
		if n, _ := r.RunOnce(ctx); n != 0 {
			t.Fatal("dead job ran again")
		}
	})
}

func TestExpiredLeaseIsReclaimedOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		if _, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.echo", Payload: "p"}); err != nil {
			t.Fatal(err)
		}
		// A crashed worker leased it and never finished.
		leased, err := st.Q().LeaseJobs(ctx, sqlitegen.LeaseJobsParams{
			Owner:      sql.NullString{String: "crashed", Valid: true},
			LeaseUntil: sql.NullInt64{Int64: clock.Millis(fake.Now().Add(time.Minute)), Valid: true},
			NowQueued:  clock.Millis(fake.Now()),
			NowLeased:  sql.NullInt64{Int64: clock.Millis(fake.Now()), Valid: true},
			Batch:      10,
		})
		if err != nil || len(leased) != 1 {
			t.Fatalf("lease = %d, %v", len(leased), err)
		}
		rec := &recorder{}
		r := jobs.New(st, jobs.WithClock(fake), jobs.WithJitter(func() float64 { return 0 }))
		r.Register("test.echo", rec.handle)
		if n, _ := r.RunOnce(ctx); n != 0 {
			t.Fatal("stole a live lease")
		}
		fake.Advance(2 * time.Minute)
		if n, _ := r.RunOnce(ctx); n != 1 || len(rec.seen) != 1 {
			t.Fatalf("reclaim n=%d seen=%v", n, rec.seen)
		}
		if n, _ := r.RunOnce(ctx); n != 0 {
			t.Fatal("ran twice")
		}
	})
}

func TestPanicAndUnknownKindGoDead(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		r := jobs.New(st, jobs.WithClock(fake), jobs.WithJitter(func() float64 { return 0 }))
		r.Register("test.panic", func(context.Context, jobs.Job) error { panic("boom") })
		if _, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.panic", Payload: "p", MaxAttempts: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.unknown", Payload: "p"}); err != nil {
			t.Fatal(err)
		}
		if n, err := r.RunOnce(ctx); err != nil || n != 2 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		dead, _ := st.Q().ListJobsByState(ctx, sqlitegen.ListJobsByStateParams{State: "dead", Lim: 10})
		if len(dead) != 2 {
			t.Fatalf("dead = %d", len(dead))
		}
	})
}

func TestWakeRunsImmediately(t *testing.T) {
	st := storetest.OpenSQLite(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
	r := jobs.New(st, jobs.WithClock(fake), jobs.WithPoll(time.Hour), jobs.WithJitter(func() float64 { return 0 }))
	done := make(chan string, 1)
	r.Register("test.echo", func(_ context.Context, j jobs.Job) error { done <- j.Payload; return nil })
	stopped := make(chan struct{})
	go func() { _ = r.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	if _, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.echo", Payload: "woken"}); err != nil {
		t.Fatal(err)
	}
	r.Wake()
	select {
	case p := <-done:
		if p != "woken" {
			t.Fatalf("payload = %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job did not run after Wake")
	}
}

// TestStaleLeaseOwnerCannotClobberReclaimedJob drives two runners over one
// job: the first overruns its lease, the second reclaims the job and fails
// it into a retry, and the first then reports its own outcome late. The
// late write must not land, because the lease no longer belongs to it.
func TestStaleLeaseOwnerCannotClobberReclaimedJob(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		zero := jobs.WithJitter(func() float64 { return 0 })
		slow := jobs.New(st, jobs.WithClock(fake), jobs.WithLease(time.Minute), zero)
		fast := jobs.New(st, jobs.WithClock(fake), jobs.WithLease(time.Minute), zero)
		fast.Register("test.slow", func(context.Context, jobs.Job) error { return errors.New("reclaimed and failed") })
		slow.Register("test.slow", func(ctx context.Context, _ jobs.Job) error {
			// While this handler runs, its lease expires and the second
			// runner reclaims the job and schedules a retry.
			fake.Advance(2 * time.Minute)
			if n, err := fast.RunOnce(ctx); err != nil || n != 1 {
				t.Errorf("reclaim RunOnce = %d, %v", n, err)
			}
			return nil
		})

		jid, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.slow", Payload: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if n, err := slow.RunOnce(ctx); err != nil || n != 1 {
			t.Fatalf("first RunOnce = %d, %v", n, err)
		}

		got, err := st.Q().GetJob(ctx, jid)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != "queued" {
			t.Fatalf("state = %q, want queued (the stale owner completed a job it no longer held)", got.State)
		}
		if got.LastError.String != "reclaimed and failed" {
			t.Fatalf("last_error = %q", got.LastError.String)
		}
		if got.CompletedAt.Valid {
			t.Fatalf("completed_at = %v, want null", got.CompletedAt)
		}
	})
}

// TestCancelledContextReleasesTheJob covers both halves of shutdown
// bookkeeping: the state-transition write runs on a context the
// cancellation does not reach, and the job goes back to the queue with
// the run_at and attempt count it had before the lease, rather than
// burning an attempt or sitting leased until the lease expires.
func TestCancelledContextReleasesTheJob(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		runAt := fake.Now().Add(-time.Minute)
		r := jobs.New(st, jobs.WithClock(fake), jobs.WithJitter(func() float64 { return 0 }))
		r.Register("test.slow", func(context.Context, jobs.Job) error {
			cancel() // the process is shutting down mid-handler
			return errors.New("interrupted")
		})
		jid, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.slow", Payload: "p", RunAt: runAt})
		if err != nil {
			t.Fatal(err)
		}
		if n, err := r.RunOnce(ctx); err != nil || n != 1 {
			t.Fatalf("RunOnce = %d, %v", n, err)
		}
		got, err := st.Q().GetJob(context.Background(), jid)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != "queued" {
			t.Fatalf("state = %q, want queued", got.State)
		}
		if got.Attempts != 0 {
			t.Fatalf("attempts = %d, want 0 (a shutdown must not burn one)", got.Attempts)
		}
		if got.RunAt != clock.Millis(runAt) {
			t.Fatalf("run_at = %d, want %d (unchanged)", got.RunAt, clock.Millis(runAt))
		}
		if got.LeaseOwner.Valid || got.LeaseExpiresAt.Valid {
			t.Fatalf("lease not released: %+v", got)
		}
		if got.LastError.Valid {
			t.Fatalf("last_error = %q, want none", got.LastError.String)
		}
	})
}
