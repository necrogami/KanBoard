package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/necrogami/kanboard/internal/core/id"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

func TestWorkspaceSeqAndReceipt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		ws, err := st.Q().CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "w", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		for want := int64(1); want <= 3; want++ {
			got, err := st.Q().NextSeq(ctx, ws.ID)
			if err != nil || got != want {
				t.Fatalf("NextSeq = %d, %v; want %d", got, err, want)
			}
		}
		_, err = st.Q().GetReceipt(ctx, sqlitegen.GetReceiptParams{IdempotencyKey: "k", ActorID: "a"})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetReceipt on empty = %v", err)
		}
		if err := st.Q().InsertReceipt(ctx, sqlitegen.InsertReceiptParams{IdempotencyKey: "k", ActorID: "a", Result: "{}", CreatedAt: 5}); err != nil {
			t.Fatal(err)
		}
		r, err := st.Q().GetReceipt(ctx, sqlitegen.GetReceiptParams{IdempotencyKey: "k", ActorID: "a"})
		if err != nil || r.Result != "{}" {
			t.Fatalf("GetReceipt = %+v, %v", r, err)
		}
		n, err := st.Q().DeleteReceiptsBefore(ctx, 10)
		if err != nil || n != 1 {
			t.Fatalf("DeleteReceiptsBefore = %d, %v", n, err)
		}
	})
}

func TestWithTxRollsBackOnError(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		boom := errors.New("boom")
		err := st.WithTx(ctx, func(q store.Querier) error {
			if _, err := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "tx", CreatedAt: 1, UpdatedAt: 1}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
		if _, err := st.Q().GetWorkspaceBySlug(ctx, "tx"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("workspace survived rollback: %v", err)
		}
	})
}

func TestCardPositions(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "pos", CreatedAt: 1, UpdatedAt: 1})
		u, _ := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "a@b.c", Valid: true}, Name: "A", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "POS", Name: "P", CreatedAt: 1, UpdatedAt: 1})
		b, _ := q.CreateBoard(ctx, sqlitegen.CreateBoardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "B", Position: "V", CreatedAt: 1, UpdatedAt: 1})
		c, _ := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "Todo", Position: "V", Category: "todo", CreatedAt: 1, UpdatedAt: 1})
		if _, err := q.FirstPositionInColumn(ctx, c.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("empty column first = %v", err)
		}
		for i, pos := range []string{"F", "V", "k"} {
			n, err := q.NextCardNumber(ctx, sqlitegen.NextCardNumberParams{ID: p.ID, UpdatedAt: 1})
			if err != nil || n != int64(i+1) {
				t.Fatalf("NextCardNumber = %d, %v", n, err)
			}
			if _, err := q.CreateCard(ctx, sqlitegen.CreateCardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, BoardID: b.ID, ColumnID: c.ID, Number: n, Title: "t", Description: "", Position: pos, CreatedBy: u.ID, CreatedAt: 1, UpdatedAt: 1}); err != nil {
				t.Fatal(err)
			}
		}
		first, _ := q.FirstPositionInColumn(ctx, c.ID)
		last, _ := q.LastPositionInColumn(ctx, c.ID)
		next, _ := q.NextPositionAfter(ctx, sqlitegen.NextPositionAfterParams{ColumnID: c.ID, Position: "F"})
		if first != "F" || last != "k" || next != "V" {
			t.Fatalf("first=%q last=%q next=%q", first, last, next)
		}
		cnt, _ := q.CountCardsInColumn(ctx, c.ID)
		if cnt != 3 {
			t.Fatalf("count = %d", cnt)
		}
		prev, _ := q.PrevPositionBefore(ctx, sqlitegen.PrevPositionBeforeParams{ColumnID: c.ID, Position: "k"})
		if prev != "V" {
			t.Fatalf("prev = %q", prev)
		}
		byCol, _ := q.ListCardsByColumn(ctx, c.ID)
		if len(byCol) != 3 || byCol[0].Position != "F" {
			t.Fatalf("ListCardsByColumn = %d rows", len(byCol))
		}
	})
}

// TestEveryQueryRunsOnBothEngines exercises the queries no other test
// reaches, so a dialect problem surfaces here rather than in plan 3.
func TestEveryQueryRunsOnBothEngines(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "all", CreatedAt: 1, UpdatedAt: 1})
		u, err := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "u@b.c", Valid: true}, Name: "U", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetUserByEmail(ctx, sqlitegen.GetUserByEmailParams{WorkspaceID: ws.ID, Email: sql.NullString{String: "u@b.c", Valid: true}}); err != nil || got.ID != u.ID {
			t.Fatalf("GetUserByEmail: %v", err)
		}
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "ALL", Name: "All", CreatedAt: 1, UpdatedAt: 1})
		if ps, err := q.ListProjects(ctx, ws.ID); err != nil || len(ps) != 1 {
			t.Fatalf("ListProjects: %d %v", len(ps), err)
		}
		if _, err := q.CreateLabel(ctx, sqlitegen.CreateLabelParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "bug", Color: "#FF0000", CreatedAt: 1, UpdatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if ls, err := q.ListLabels(ctx, p.ID); err != nil || len(ls) != 1 {
			t.Fatalf("ListLabels: %d %v", len(ls), err)
		}
		for i := int64(1); i <= 3; i++ {
			if err := q.InsertEvent(ctx, sqlitegen.InsertEventParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: sql.NullString{String: p.ID, Valid: true}, Seq: i, ActorKind: "system", Kind: "project.updated", Payload: "{}", OccurredAt: i}); err != nil {
				t.Fatal(err)
			}
		}
		if evs, err := q.ListEventsSince(ctx, sqlitegen.ListEventsSinceParams{WorkspaceID: ws.ID, Seq: 1, Lim: 10}); err != nil || len(evs) != 2 || evs[0].Seq != 2 {
			t.Fatalf("ListEventsSince: %d %v", len(evs), err)
		}
		if evs, err := q.ListEventsByCard(ctx, sqlitegen.ListEventsByCardParams{CardID: sql.NullString{String: "none", Valid: true}, Lim: 10}); err != nil || len(evs) != 0 {
			t.Fatalf("ListEventsByCard: %d %v", len(evs), err)
		}
		if err := q.InsertJob(ctx, sqlitegen.InsertJobParams{ID: "job1", Kind: "k", Payload: "{}", MaxAttempts: 1, RunAt: 1, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if j, err := q.GetJob(ctx, "job1"); err != nil || j.State != "queued" {
			t.Fatalf("GetJob: %+v %v", j, err)
		}
	})
}

func TestForeignKeysAndUniqueViolation(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		if _, err := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: "missing", Key: "FK", Name: "x", CreatedAt: 1, UpdatedAt: 1}); err == nil {
			t.Fatal("foreign key not enforced")
		}
		if _, err := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "dup", CreatedAt: 1, UpdatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		_, err := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W2", Slug: "dup", CreatedAt: 1, UpdatedAt: 1})
		if !store.IsUniqueViolation(err) {
			t.Fatalf("IsUniqueViolation(%v) = false", err)
		}
		if store.IsUniqueViolation(nil) || store.IsUniqueViolation(errors.New("x")) {
			t.Fatal("false positive")
		}
	})
}

func TestStatusOnBothEngines(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		cur, latest, err := st.Status(context.Background())
		if err != nil || cur != latest || cur < 1 {
			t.Fatalf("status = %d/%d %v", cur, latest, err)
		}
	})
}
