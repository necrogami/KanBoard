package service_test

import (
	"context"
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

type harness struct {
	st    *store.Store
	svc   *service.Service
	clock *clock.Fake
	bus   *bus.Bus
	ws    service.Workspace
	admin policy.Actor
}

func newHarness(t *testing.T, st *store.Store) *harness {
	t.Helper()
	ctx := context.Background()
	h := &harness{st: st, clock: clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)), bus: bus.New(64)}
	h.svc = service.New(st, service.WithClock(h.clock), service.WithBus(h.bus))
	ws, err := h.svc.CreateWorkspace(ctx, commands.CreateWorkspace{Name: "Acme", Slug: "acme", AdminEmail: "admin@acme.test", AdminName: "Admin"})
	if err != nil {
		t.Fatal(err)
	}
	h.ws = ws
	h.admin, err = h.svc.LoadActor(ctx, ws.ID, ws.AdminUserID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func code(t *testing.T, err error, want service.Code) *service.Error {
	t.Helper()
	var se *service.Error
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want service.Error %s", err, want)
	}
	if se.Code != want {
		t.Fatalf("code = %s, want %s", se.Code, want)
	}
	return se
}

func TestCreateWorkspaceBootstrapsAdmin(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		if h.ws.Slug != "acme" || h.ws.AdminUserID == "" {
			t.Fatalf("workspace = %+v", h.ws)
		}
		if h.admin.WorkspaceRole != policy.WorkspaceAdminRole {
			t.Fatalf("admin role = %q", h.admin.WorkspaceRole)
		}
		_, err := h.svc.CreateWorkspace(context.Background(), commands.CreateWorkspace{Name: "Second", Slug: "second", AdminEmail: "b@acme.test", AdminName: "B"})
		_ = code(t, err, service.CodeConflict)
	})
}

func TestCreateProjectSeedsBoardAndColumns(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		sub := h.bus.Subscribe(h.ws.ID)
		defer sub.Close()

		p, err := h.svc.CreateProject(ctx, h.admin, commands.CreateProject{WorkspaceID: h.ws.ID, Key: "ACME", Name: "Acme Board"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Key != "ACME" || p.BoardID == "" {
			t.Fatalf("project = %+v", p)
		}
		b, _, err := h.svc.GetBoard(ctx, h.admin, "ACME")
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Columns) != 3 || b.Columns[0].Category != "todo" || b.Columns[2].Category != "done" {
			t.Fatalf("columns = %+v", b.Columns)
		}
		if b.Columns[0].Position >= b.Columns[1].Position {
			t.Fatal("columns not ordered")
		}
		kinds := map[events.Kind]int{}
		for i := 0; i < 5; i++ {
			select {
			case ev := <-sub.C:
				kinds[ev.Kind]++
				if ev.Seq == 0 || ev.ActorUserID != h.ws.AdminUserID {
					t.Fatalf("event %+v", ev)
				}
			case <-time.After(time.Second):
				t.Fatalf("only %d events published", i)
			}
		}
		if kinds[events.ProjectCreated] != 1 || kinds[events.BoardCreated] != 1 || kinds[events.ColumnCreated] != 3 {
			t.Fatalf("kinds = %v", kinds)
		}
	})
}

func TestCreateProjectDuplicateKeyConflicts(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		if _, err := h.svc.CreateProject(ctx, h.admin, commands.CreateProject{WorkspaceID: h.ws.ID, Key: "DUP", Name: "One"}); err != nil {
			t.Fatal(err)
		}
		_, err := h.svc.CreateProject(ctx, h.admin, commands.CreateProject{WorkspaceID: h.ws.ID, Key: "DUP", Name: "Two"})
		_ = code(t, err, service.CodeConflict)
	})
}

func TestCreateProjectIdempotentReplay(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		cmd := commands.CreateProject{Meta: commands.Meta{IdempotencyKey: "idem-1"}, WorkspaceID: h.ws.ID, Key: "IDEM", Name: "Once"}
		first, err := h.svc.CreateProject(ctx, h.admin, cmd)
		if err != nil {
			t.Fatal(err)
		}
		second, err := h.svc.CreateProject(ctx, h.admin, cmd)
		if err != nil {
			t.Fatal("replay:", err)
		}
		if first.ID != second.ID {
			t.Fatalf("replay created a new project: %s vs %s", first.ID, second.ID)
		}
		var n int
		if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM project WHERE key = 'IDEM'").Scan(&n); err != nil || n != 1 {
			t.Fatalf("projects = %d, %v", n, err)
		}
	})
}

func TestLoadActorPaths(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		if _, err := h.svc.LoadActor(ctx, h.ws.ID, "no-such-user", nil); err == nil {
			t.Fatal("unknown user loaded")
		}
		if _, err := h.svc.LoadActor(ctx, "other-workspace", h.ws.AdminUserID, nil); err == nil {
			t.Fatal("wrong workspace loaded")
		}
		tok := &service.TokenInfo{ID: "t1", Scopes: []policy.Scope{policy.ScopeRead}, BoardIDs: []string{"b1"}}
		a, err := h.svc.LoadActor(ctx, h.ws.ID, h.ws.AdminUserID, tok)
		if err != nil || a.Kind != policy.KindAgent || a.Token == nil || a.Token.ID != "t1" || len(a.Token.BoardIDs) != 1 {
			t.Fatalf("token actor = %+v, %v", a, err)
		}
		if _, err := st.DB.ExecContext(ctx, "UPDATE app_user SET disabled_at = 1 WHERE id = ?", h.ws.AdminUserID); err != nil {
			if _, err2 := st.DB.ExecContext(ctx, "UPDATE app_user SET disabled_at = 1 WHERE id = $1", h.ws.AdminUserID); err2 != nil {
				t.Fatal(err, err2)
			}
		}
		_, err = h.svc.LoadActor(ctx, h.ws.ID, h.ws.AdminUserID, nil)
		_ = code(t, err, service.CodeForbidden)
	})
}

func TestIdempotencyKeyReusedForDifferentCommandConflicts(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		if err := st.Q().InsertReceipt(ctx, sqlitegen.InsertReceiptParams{IdempotencyKey: "reused", ActorID: h.ws.AdminUserID, CommandKind: "Other", Result: "{}", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		_, err := h.svc.CreateProject(ctx, h.admin, commands.CreateProject{Meta: commands.Meta{IdempotencyKey: "reused"}, WorkspaceID: h.ws.ID, Key: "REUSE", Name: "x"})
		_ = code(t, err, service.CodeConflict)
	})
}

func TestCreateProjectForbiddenForOutsider(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		outsider := policy.Actor{UserID: "nobody", WorkspaceID: h.ws.ID, Kind: policy.KindHuman}
		_, err := h.svc.CreateProject(context.Background(), outsider, commands.CreateProject{WorkspaceID: h.ws.ID, Key: "NOPE", Name: "x"})
		_ = code(t, err, service.CodeForbidden)
		var n int
		if err := st.DB.QueryRow("SELECT count(*) FROM event WHERE kind = 'project.created'").Scan(&n); err != nil || n != 0 {
			t.Fatalf("events after forbidden = %d", n)
		}
	})
}
