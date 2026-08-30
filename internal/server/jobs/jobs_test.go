package jobs_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"strings"
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
		rows, err := st.Q().ListJobsByState(ctx, sqlitegen.ListJobsByStateParams{State: "done", Lim: 10})
		if err != nil || len(rows) != 2 {
			t.Fatalf("done jobs = %d, %v", len(rows), err)
		}
		// Both rows share a created_at millisecond, so neither index is
		// meaningful on its own; assert the property of the whole set.
		for _, r := range rows {
			if r.WorkspaceID.String != ws.ID {
				t.Fatalf("done job in workspace %q, want %q", r.WorkspaceID.String, ws.ID)
			}
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

func TestEnqueueRequiresAKind(t *testing.T) {
	st := storetest.OpenSQLite(t)
	if _, err := jobs.Enqueue(context.Background(), st.Q(), time.Now(), jobs.Spec{Payload: "{}"}); err == nil {
		t.Fatal("a job with no kind was enqueued")
	}
}

// TestRunnerOptionsAndFutureRunAt covers the knobs plan 3 configures
// from the environment (spec 5.9) and the RunAt a scheduled job uses:
// a job dated in the future is not leased until its time comes, a
// batch of one leases one job per pass, and the injected logger is the
// one that receives the dead-letter line.
func TestRunnerOptionsAndFutureRunAt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		var logged bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError}))
		r := jobs.New(st,
			jobs.WithClock(fake), jobs.WithJitter(func() float64 { return 0 }),
			jobs.WithBatch(1), jobs.WithLease(time.Minute), jobs.WithLogger(logger))
		rec := &recorder{}
		r.Register("test.echo", rec.handle)

		for _, p := range []string{"now-1", "now-2"} {
			if _, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.echo", Payload: p}); err != nil {
				t.Fatal(err)
			}
		}
		later, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.unknown", Payload: "later", RunAt: fake.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}

		// WithBatch(1): one job per pass, and the scheduled one is not due.
		for i := 1; i <= 2; i++ {
			if n, err := r.RunOnce(ctx); err != nil || n != 1 {
				t.Fatalf("pass %d leased %d jobs, %v", i, n, err)
			}
		}
		if n, _ := r.RunOnce(ctx); n != 0 {
			t.Fatalf("a job dated an hour ahead ran early")
		}
		if got, err := st.Q().GetJob(ctx, later); err != nil || got.State != "queued" || got.Attempts != 0 {
			t.Fatalf("scheduled job = %+v, %v", got, err)
		}

		// Its time comes: no handler is registered, so it dead-letters and
		// the injected logger is where that is reported.
		fake.Advance(2 * time.Hour)
		if n, err := r.RunOnce(ctx); err != nil || n != 1 {
			t.Fatalf("scheduled job did not run: %d, %v", n, err)
		}
		if got, err := st.Q().GetJob(ctx, later); err != nil || got.State != "dead" {
			t.Fatalf("scheduled job = %+v, %v", got, err)
		}
		if !strings.Contains(logged.String(), "dead letter") {
			t.Fatalf("injected logger saw %q", logged.String())
		}
		if len(rec.seen) != 2 {
			t.Fatalf("handler ran %d times", len(rec.seen))
		}
	})
}

// TestRegisterWhileRunning: Register-then-Run is the documented order,
// but nothing stopped a late registration, and the handler map was
// written with no synchronisation against the runner reading it. Under
// -race this test fails on the unsynchronised version.
func TestRegisterWhileRunning(t *testing.T) {
	st := storetest.OpenSQLite(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := jobs.New(st, jobs.WithPoll(time.Millisecond), jobs.WithJitter(func() float64 { return 0 }))
	rec := &recorder{}
	r.Register("test.echo", rec.handle)
	stopped := make(chan struct{})
	go func() { _ = r.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })

	for i := 0; i < 50; i++ {
		if _, err := jobs.Enqueue(ctx, st.Q(), time.Now(), jobs.Spec{Kind: "test.echo", Payload: strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
		r.Register("test.late"+strconv.Itoa(i), rec.handle)
		r.Wake()
	}
}

// TestLeaseIsReStampedPerJob: one lease deadline is stamped for the
// whole batch, which then runs serially, so a slow job at the head of
// the batch used to leave the jobs behind it holding a deadline that
// had already passed by the time they ran. Another runner could then
// reclaim a job while it was running. The runner re-stamps each job's
// lease immediately before calling its handler.
func TestLeaseIsReStampedPerJob(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		zero := jobs.WithJitter(func() float64 { return 0 })
		owner := jobs.New(st, jobs.WithClock(fake), jobs.WithLease(time.Minute), jobs.WithBatch(10), zero)
		thief := jobs.New(st, jobs.WithClock(fake), jobs.WithLease(time.Minute), zero)

		stolen := 0
		thief.Register("test.slow", func(context.Context, jobs.Job) error { stolen++; return nil })

		ran := map[string]bool{}
		owner.Register("test.slow", func(ctx context.Context, j jobs.Job) error {
			ran[j.Payload] = true
			switch j.Payload {
			case "a":
				// The head of the batch overruns the deadline that was
				// stamped for the whole batch.
				fake.Advance(2 * time.Minute)
			case "b":
				// The tail is running now, so its lease must be live now.
				n, err := thief.RunOnce(ctx)
				if err != nil {
					t.Errorf("thief RunOnce: %v", err)
				}
				if n != 0 {
					t.Errorf("a second runner reclaimed %d job(s) while they were running", n)
				}
			}
			return nil
		})

		var ids []string
		for _, payload := range []string{"a", "b"} {
			jid, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.slow", Payload: payload})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, jid)
		}

		if n, err := owner.RunOnce(ctx); err != nil || n != 2 {
			t.Fatalf("RunOnce = %d, %v", n, err)
		}
		if !ran["a"] || !ran["b"] {
			t.Fatalf("handlers ran = %v", ran)
		}
		if stolen != 0 {
			t.Fatalf("the second runner ran %d stolen job(s)", stolen)
		}
		for i, jid := range ids {
			got, err := st.Q().GetJob(ctx, jid)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != "done" {
				t.Fatalf("job %d state = %q, want done (it was completed by another owner)", i, got.State)
			}
			if got.Attempts != 1 || got.LeaseOwner.Valid {
				t.Fatalf("job %d = %+v", i, got)
			}
		}
	})
}

// TestJobIsSkippedWhenTheLeaseIsAlreadyLost covers the other half of the
// per-job re-stamp. The head of the batch overruns its own lease and
// another worker reclaims what is left of the batch. When the runner
// reaches the next job the re-stamp matches no row, so it leaves the job
// alone rather than running it behind the new owner's back and then
// reporting on a lease it no longer holds.
func TestJobIsSkippedWhenTheLeaseIsAlreadyLost(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		zero := jobs.WithJitter(func() float64 { return 0 })
		var logged bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))
		owner := jobs.New(st, jobs.WithClock(fake), jobs.WithLease(time.Minute), jobs.WithBatch(10), jobs.WithLogger(logger), zero)
		thief := jobs.New(st, jobs.WithClock(fake), jobs.WithLease(time.Hour), zero)

		thiefRan := &recorder{}
		thief.Register("test.slow", thiefRan.handle)
		ownerRan := &recorder{}
		owner.Register("test.slow", func(ctx context.Context, j jobs.Job) error {
			if err := ownerRan.handle(ctx, j); err != nil {
				return err
			}
			if j.Payload == "a" {
				// The head overruns the batch deadline and the tail is
				// reclaimed and run by another runner before we reach it.
				fake.Advance(2 * time.Minute)
				if n, err := thief.RunOnce(ctx); err != nil || n != 2 {
					t.Errorf("thief RunOnce = %d, %v (both leases have expired)", n, err)
				}
			}
			return nil
		})

		var ids []string
		for _, payload := range []string{"a", "b"} {
			jid, err := jobs.Enqueue(ctx, st.Q(), fake.Now(), jobs.Spec{Kind: "test.slow", Payload: payload})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, jid)
		}
		if n, err := owner.RunOnce(ctx); err != nil || n != 2 {
			t.Fatalf("RunOnce = %d, %v", n, err)
		}
		if len(ownerRan.seen) != 1 || ownerRan.seen[0] != "a" {
			t.Fatalf("the first runner ran %v, want only the head of the batch", ownerRan.seen)
		}
		if len(thiefRan.seen) != 2 {
			t.Fatalf("the reclaiming runner ran %v, want both jobs of the expired batch", thiefRan.seen)
		}
		if !strings.Contains(logged.String(), "lease lost before the handler ran") {
			t.Fatalf("no warning for the skipped job: %q", logged.String())
		}
		// The reclaiming runner's completion stands.
		got, err := st.Q().GetJob(ctx, ids[1])
		if err != nil {
			t.Fatal(err)
		}
		if got.State != "done" || got.LeaseOwner.Valid {
			t.Fatalf("reclaimed job = %+v", got)
		}
	})
}
