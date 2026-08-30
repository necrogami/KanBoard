package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/bus"
	"github.com/necrogami/kanboard/internal/server/service"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

// countRows counts a table through the raw connection, so the assertion
// does not depend on a query that might itself be scoped.
func countRows(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var n int
	if err := st.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestOutboxJobRollsBackWithTheCommand is the defining property of a
// transactional outbox: a job row enqueued for an event must not survive
// a command that does not commit. The enqueue happens in the first hook
// and the second hook then fails, which is the only way to fail after an
// outbox write inside the same transaction.
func TestOutboxJobRollsBackWithTheCommand(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		boom := errors.New("hook failed")
		enqueue := func(ctx context.Context, q store.Querier, ev events.Event) error {
			return q.InsertJob(ctx, sqlitegen.InsertJobParams{
				ID: "job-" + string(ev.Kind), WorkspaceID: sql.NullString{String: ev.WorkspaceID, Valid: true},
				Kind: "test.echo", Payload: "{}", MaxAttempts: 1, RunAt: 1, CreatedAt: 1,
			})
		}
		fail := func(context.Context, store.Querier, events.Event) error { return boom }
		svc := service.New(st, service.WithClock(fake), service.WithBus(bus.New(8)),
			service.WithOutboxHook(enqueue), service.WithOutboxHook(fail))

		_, err := svc.CreateWorkspace(ctx, commands.CreateWorkspace{Name: "W", Slug: "w", AdminEmail: "a@b.c", AdminName: "A"})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the hook error", err)
		}
		if n := countRows(t, st, "job"); n != 0 {
			t.Fatalf("job rows after rollback = %d", n)
		}
		if n := countRows(t, st, "event"); n != 0 {
			t.Fatalf("event rows after rollback = %d", n)
		}
		if n := countRows(t, st, "workspace"); n != 0 {
			t.Fatalf("workspace rows after rollback = %d", n)
		}
	})
}

// TestPolicyDenialLeavesNoEventAndNoReceipt: a forbidden command must
// not be recorded anywhere, and in particular must not write an
// idempotency receipt, which would make the denial replayable as a
// stored result (spec 12.2, service row).
func TestPolicyDenialLeavesNoEventAndNoReceipt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		p, _, cards := projectWithCards(t, h, "DENY", 1)

		before := countRows(t, st, "event")
		viewer := policy.Actor{UserID: "viewer", WorkspaceID: h.ws.ID, WorkspaceRole: policy.WorkspaceMember,
			ProjectRoles: map[string]policy.ProjectRole{p.ID: policy.ProjectViewer}}
		meta := commands.Meta{IdempotencyKey: "denied-1"}

		title := "nope"
		_, err := h.svc.UpdateCard(ctx, viewer, commands.UpdateCard{Meta: meta, CardKey: cards[0].Key, Title: &title})
		_ = code(t, err, service.CodeForbidden)

		if n := countRows(t, st, "event"); n != before {
			t.Fatalf("event rows = %d, want %d", n, before)
		}
		if n := countRows(t, st, "command_receipt"); n != 0 {
			t.Fatalf("command_receipt rows after denial = %d", n)
		}
	})
}
