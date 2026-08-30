// Package jobs is the database-backed background queue. Rows are
// enqueued inside the mutation transaction (transactional outbox) and
// leased by a single in-process runner with backoff and a dead-letter
// state. No Redis, ever.
package jobs

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/id"
	"github.com/necrogami/kanboard/internal/server/jobkind"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

const (
	defaultMaxAttempts = jobkind.DefaultMaxAttempts
	backoffBase        = 30 * time.Second
	backoffCap         = 4 * time.Hour
	jitterFraction     = 0.25

	// bookkeepingTimeout bounds the state-transition write that records a
	// job's outcome. It runs on a context detached from the caller's, so
	// it needs a deadline of its own.
	bookkeepingTimeout = 5 * time.Second

	// KindRankRebalance is an alias of jobkind.RankRebalance, kept so a
	// handler registration reads in terms of this package. The constant
	// itself lives in jobkind because the service enqueues it.
	KindRankRebalance = jobkind.RankRebalance
)

// Spec describes a job to enqueue.
type Spec struct {
	Kind        string
	WorkspaceID string
	Payload     string
	MaxAttempts int64     // 0 means the default of 8
	RunAt       time.Time // zero means now
}

// Job is what a Handler receives.
type Job struct {
	ID          string
	WorkspaceID string
	Kind        string
	Payload     string
	Attempts    int64
	MaxAttempts int64
}

// Handler processes one job. Returning an error schedules a retry.
type Handler func(ctx context.Context, job Job) error

// Enqueue inserts a job row using q, which should be the mutation's
// transaction so the job commits atomically with the change.
func Enqueue(ctx context.Context, q store.Querier, now time.Time, spec Spec) (string, error) {
	if spec.Kind == "" {
		return "", fmt.Errorf("jobs: kind is required")
	}
	max := spec.MaxAttempts
	if max <= 0 {
		max = defaultMaxAttempts
	}
	runAt := spec.RunAt
	if runAt.IsZero() {
		runAt = now
	}
	jid := id.New()
	err := q.InsertJob(ctx, sqlitegen.InsertJobParams{
		ID: jid, WorkspaceID: sql.NullString{String: spec.WorkspaceID, Valid: spec.WorkspaceID != ""},
		Kind: spec.Kind, Payload: spec.Payload, MaxAttempts: max,
		RunAt: clock.Millis(runAt), CreatedAt: clock.Millis(now),
	})
	return jid, err
}

// Backoff returns the delay before the next attempt: 30s doubling per
// attempt, capped at 4h, plus up to 25 percent jitter (jitter in [0,1]).
func Backoff(attempts int64, jitter float64) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := float64(backoffBase) * math.Pow(2, float64(attempts-1))
	if d > float64(backoffCap) {
		d = float64(backoffCap)
	}
	return time.Duration(d * (1 + jitter*jitterFraction))
}

// Runner leases and executes jobs.
type Runner struct {
	st    *store.Store
	clock clock.Clock
	// handlers is guarded because Run reads it on its own goroutine.
	// Register-then-Run remains the documented order; the lock is what
	// makes a late registration safe rather than silently racy.
	mu       sync.RWMutex
	handlers map[string]Handler
	worker   string
	wake     chan struct{}
	poll     time.Duration
	lease    time.Duration
	batch    int64
	jitter   func() float64
	log      *slog.Logger
}

// Option configures a Runner.
type Option func(*Runner)

func WithClock(c clock.Clock) Option     { return func(r *Runner) { r.clock = c } }
func WithPoll(d time.Duration) Option    { return func(r *Runner) { r.poll = d } }
func WithLease(d time.Duration) Option   { return func(r *Runner) { r.lease = d } }
func WithBatch(n int64) Option           { return func(r *Runner) { r.batch = n } }
func WithJitter(f func() float64) Option { return func(r *Runner) { r.jitter = f } }
func WithLogger(l *slog.Logger) Option   { return func(r *Runner) { r.log = l } }

// New builds a Runner; call Register for each kind, then Run.
func New(st *store.Store, opts ...Option) *Runner {
	host, _ := os.Hostname()
	r := &Runner{
		st: st, clock: clock.Real{}, handlers: map[string]Handler{},
		worker: fmt.Sprintf("%s-%d-%s", host, os.Getpid(), id.New()[:8]),
		wake:   make(chan struct{}, 1), poll: 5 * time.Second, lease: 5 * time.Minute, batch: 10,
		jitter: rand.Float64, log: slog.Default(),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Register sets the handler for a kind. Unknown kinds go straight to
// dead, so register every kind before Run.
func (r *Runner) Register(kind string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[kind] = h
}

// handler returns the handler for kind, if any.
func (r *Runner) handler(kind string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[kind]
	return h, ok
}

// Wake nudges Run to lease immediately instead of waiting for the poll.
func (r *Runner) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run loops until ctx is done.
func (r *Runner) Run(ctx context.Context) error {
	for {
		n, err := r.RunOnce(ctx)
		if err != nil {
			r.log.Error("jobs: lease", "err", err)
		}
		if n > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.wake:
		case <-time.After(r.poll):
		}
	}
}

// RunOnce leases up to batch due jobs and runs them, returning how many
// it processed.
func (r *Runner) RunOnce(ctx context.Context) (int, error) {
	now := r.clock.Now()
	// lease_owner, lease_expires_at and completed_at are nullable columns,
	// so sqlc types the parameters compared with or assigned to them as
	// sql.NullString / sql.NullInt64.
	// One lease deadline for the whole batch, which then runs serially;
	// runOne re-stamps each job's deadline before calling its handler, so
	// a slow job at the head does not eat into the leases behind it.
	rows, err := r.st.Q().LeaseJobs(ctx, sqlitegen.LeaseJobsParams{
		Owner: r.owner(), LeaseUntil: nullMs(now.Add(r.lease)),
		NowQueued: clock.Millis(now), NowLeased: nullMs(now), Batch: r.batch,
	})
	if err != nil {
		return 0, err
	}
	// Postgres does not carry the subquery's ORDER BY through UPDATE ...
	// RETURNING, so the leased batch comes back in an arbitrary order on
	// that engine. Restore the queue order here, where both engines agree:
	// run_at first, then created_at, then the UUIDv7 id, which is
	// time-ordered and so breaks a same-millisecond tie by enqueue order.
	slices.SortFunc(rows, func(a, b sqlitegen.Job) int {
		if c := cmp.Compare(a.RunAt, b.RunAt); c != 0 {
			return c
		}
		if c := cmp.Compare(a.CreatedAt, b.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	for _, row := range rows {
		r.runOne(ctx, row)
	}
	return len(rows), nil
}

func (r *Runner) runOne(ctx context.Context, row sqlitegen.Job) {
	// LeaseJobs has already incremented attempts, so a bookkeeping write
	// that fails leaves the row leased for the rest of the lease with the
	// attempt spent. Record the outcome on a context that a shutdown does
	// not cancel, with its own short deadline.
	book, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()

	job := Job{ID: row.ID, WorkspaceID: row.WorkspaceID.String, Kind: row.Kind, Payload: row.Payload, Attempts: row.Attempts, MaxAttempts: row.MaxAttempts}
	h, ok := r.handler(row.Kind)
	var err error
	if !ok {
		err = fmt.Errorf("no handler registered for kind %q", row.Kind)
		r.dead(book, row.ID, err)
		return
	}
	// The batch carries one deadline for jobs that run serially, so give
	// this job a lease that starts now rather than when the batch was
	// leased. Zero rows means another runner reclaimed it while the jobs
	// ahead of it were running, and it is no longer ours to run or to
	// report on.
	held, lerr := r.st.Q().UpdateJobLease(book, sqlitegen.UpdateJobLeaseParams{
		ID: row.ID, LeaseUntil: nullMs(r.clock.Now().Add(r.lease)), Owner: r.owner(),
	})
	if lerr != nil {
		r.log.Error("jobs: extend lease", "id", row.ID, "err", lerr)
		return
	}
	if held == 0 {
		r.log.Warn("jobs: lease lost before the handler ran, skipping", "id", row.ID, "kind", row.Kind)
		return
	}
	err = r.safeCall(ctx, h, job)
	if ctx.Err() != nil {
		// The runner is shutting down. Whatever the handler returned, that
		// is not the job failing: put it back exactly as it was found so
		// the next runner picks it up immediately.
		if e := r.st.Q().ReleaseJob(book, sqlitegen.ReleaseJobParams{ID: row.ID, RunAt: row.RunAt, Attempts: row.Attempts - 1, Owner: r.owner()}); e != nil {
			r.log.Error("jobs: release", "id", row.ID, "err", e)
		}
		return
	}
	now := nullMs(r.clock.Now())
	if err == nil {
		if e := r.st.Q().CompleteJob(book, sqlitegen.CompleteJobParams{ID: row.ID, CompletedAt: now, Owner: r.owner()}); e != nil {
			r.log.Error("jobs: complete", "id", row.ID, "err", e)
		}
		return
	}
	if row.Attempts >= row.MaxAttempts {
		r.dead(book, row.ID, err)
		return
	}
	next := r.clock.Now().Add(Backoff(row.Attempts, r.jitter()))
	if e := r.st.Q().RetryJob(book, sqlitegen.RetryJobParams{ID: row.ID, RunAt: clock.Millis(next), LastError: sql.NullString{String: err.Error(), Valid: true}, Owner: r.owner()}); e != nil {
		r.log.Error("jobs: retry", "id", row.ID, "err", e)
	}
	r.log.Warn("jobs: failed, will retry", "id", row.ID, "kind", row.Kind, "attempt", row.Attempts, "err", err)
}

func (r *Runner) dead(ctx context.Context, jid string, cause error) {
	now := nullMs(r.clock.Now())
	if e := r.st.Q().DeadJob(ctx, sqlitegen.DeadJobParams{ID: jid, CompletedAt: now, LastError: sql.NullString{String: cause.Error(), Valid: true}, Owner: r.owner()}); e != nil {
		r.log.Error("jobs: dead", "id", jid, "err", e)
	}
	r.log.Error("jobs: dead letter", "id", jid, "err", cause)
}

// owner is this runner's lease-owner value, as the release queries expect
// it: lease_owner is nullable, so sqlc types the comparison parameter as
// sql.NullString.
func (r *Runner) owner() sql.NullString {
	return sql.NullString{String: r.worker, Valid: true}
}

func nullMs(t time.Time) sql.NullInt64 {
	return sql.NullInt64{Int64: clock.Millis(t), Valid: true}
}

func (r *Runner) safeCall(ctx context.Context, h Handler, job Job) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return h(ctx, job)
}
