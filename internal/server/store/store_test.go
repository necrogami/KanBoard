package store_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
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
		if err := st.Q().InsertReceipt(ctx, sqlitegen.InsertReceiptParams{IdempotencyKey: "k", ActorID: "a", CommandKind: "Test", Result: "{}", CreatedAt: 5}); err != nil {
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

func TestBoardQueries(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "board", CreatedAt: 1, UpdatedAt: 1})
		u, _ := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "b@b.c", Valid: true}, Name: "B", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "BRD", Name: "P", CreatedAt: 1, UpdatedAt: 1})
		b, err := q.CreateBoard(ctx, sqlitegen.CreateBoardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "B", Position: "V", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetBoard(ctx, b.ID); err != nil || got.ID != b.ID {
			t.Fatalf("GetBoard: %+v %v", got, err)
		}
		if got, err := q.GetBoardByProject(ctx, p.ID); err != nil || got.ID != b.ID {
			t.Fatalf("GetBoardByProject: %+v %v", got, err)
		}
		c, err := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "Todo", Position: "V", Category: "todo", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetColumn(ctx, c.ID); err != nil || got.ID != c.ID {
			t.Fatalf("GetColumn: %+v %v", got, err)
		}
		if cols, err := q.ListColumns(ctx, b.ID); err != nil || len(cols) != 1 || cols[0].ID != c.ID {
			t.Fatalf("ListColumns: %d %v", len(cols), err)
		}
		card, err := q.CreateCard(ctx, sqlitegen.CreateCardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, BoardID: b.ID, ColumnID: c.ID, Number: 1, Title: "t", Description: "", Position: "V", CreatedBy: u.ID, CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetCard(ctx, card.ID); err != nil || got.ID != card.ID {
			t.Fatalf("GetCard: %+v %v", got, err)
		}
		if got, err := q.GetCardByNumber(ctx, sqlitegen.GetCardByNumberParams{ProjectID: p.ID, Number: 1}); err != nil || got.ID != card.ID {
			t.Fatalf("GetCardByNumber: %+v %v", got, err)
		}
		if cards, err := q.ListCardsByBoard(ctx, b.ID); err != nil || len(cards) != 1 || cards[0].ID != card.ID {
			t.Fatalf("ListCardsByBoard: %d %v", len(cards), err)
		}
	})
}

// TestCardVersionedUpdates exercises the four optimistic-locking updates:
// each must increment version by one on success and return sql.ErrNoRows
// when called with a stale version, on both engines.
func TestCardVersionedUpdates(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "verc", CreatedAt: 1, UpdatedAt: 1})
		u, _ := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "v@b.c", Valid: true}, Name: "V", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "VER", Name: "P", CreatedAt: 1, UpdatedAt: 1})
		b, _ := q.CreateBoard(ctx, sqlitegen.CreateBoardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "B", Position: "V", CreatedAt: 1, UpdatedAt: 1})
		c1, _ := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "Todo", Position: "V", Category: "todo", CreatedAt: 1, UpdatedAt: 1})
		c2, _ := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "Doing", Position: "W", Category: "doing", CreatedAt: 1, UpdatedAt: 1})
		card, err := q.CreateCard(ctx, sqlitegen.CreateCardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, BoardID: b.ID, ColumnID: c1.ID, Number: 1, Title: "t", Description: "", Position: "V", CreatedBy: u.ID, CreatedAt: 1, UpdatedAt: 1})
		if err != nil || card.Version != 1 {
			t.Fatalf("CreateCard = %+v, %v", card, err)
		}

		moved, err := q.MoveCard(ctx, sqlitegen.MoveCardParams{ID: card.ID, ColumnID: c2.ID, Position: "W", UpdatedAt: 2, Version: card.Version})
		if err != nil || moved.Version != 2 || moved.ColumnID != c2.ID {
			t.Fatalf("MoveCard = %+v, %v", moved, err)
		}
		if _, err := q.MoveCard(ctx, sqlitegen.MoveCardParams{ID: card.ID, ColumnID: c2.ID, Position: "X", UpdatedAt: 3, Version: 1}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("MoveCard stale version = %v", err)
		}

		updated, err := q.UpdateCardFields(ctx, sqlitegen.UpdateCardFieldsParams{ID: card.ID, Title: "t2", Description: "d2", UpdatedAt: 4, Version: moved.Version})
		if err != nil || updated.Version != 3 || updated.Title != "t2" {
			t.Fatalf("UpdateCardFields = %+v, %v", updated, err)
		}
		if _, err := q.UpdateCardFields(ctx, sqlitegen.UpdateCardFieldsParams{ID: card.ID, Title: "t3", Description: "d3", UpdatedAt: 5, Version: 2}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("UpdateCardFields stale version = %v", err)
		}

		archived, err := q.SetCardArchived(ctx, sqlitegen.SetCardArchivedParams{ID: card.ID, ArchivedAt: sql.NullInt64{Int64: 9, Valid: true}, UpdatedAt: 6, Version: updated.Version})
		if err != nil || archived.Version != 4 || !archived.ArchivedAt.Valid {
			t.Fatalf("SetCardArchived = %+v, %v", archived, err)
		}
		if _, err := q.SetCardArchived(ctx, sqlitegen.SetCardArchivedParams{ID: card.ID, ArchivedAt: sql.NullInt64{Int64: 9, Valid: true}, UpdatedAt: 7, Version: 3}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("SetCardArchived stale version = %v", err)
		}

		touched, err := q.TouchCard(ctx, sqlitegen.TouchCardParams{ID: card.ID, UpdatedAt: 8, Version: archived.Version})
		if err != nil || touched.Version != 5 {
			t.Fatalf("TouchCard = %+v, %v", touched, err)
		}
		if _, err := q.TouchCard(ctx, sqlitegen.TouchCardParams{ID: card.ID, UpdatedAt: 9, Version: 4}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("TouchCard stale version = %v", err)
		}
	})
}

func TestLabelAndAssigneeQueries(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "lbl", CreatedAt: 1, UpdatedAt: 1})
		u, _ := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "l@b.c", Valid: true}, Name: "L", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "LBL", Name: "P", CreatedAt: 1, UpdatedAt: 1})
		b, _ := q.CreateBoard(ctx, sqlitegen.CreateBoardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "B", Position: "V", CreatedAt: 1, UpdatedAt: 1})
		c, _ := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "Todo", Position: "V", Category: "todo", CreatedAt: 1, UpdatedAt: 1})
		card, _ := q.CreateCard(ctx, sqlitegen.CreateCardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, BoardID: b.ID, ColumnID: c.ID, Number: 1, Title: "t", Description: "", Position: "V", CreatedBy: u.ID, CreatedAt: 1, UpdatedAt: 1})

		lbl, err := q.CreateLabel(ctx, sqlitegen.CreateLabelParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "bug", Color: "#F00", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetLabel(ctx, lbl.ID); err != nil || got.ID != lbl.ID {
			t.Fatalf("GetLabel: %+v %v", got, err)
		}

		if err := q.AddCardLabel(ctx, sqlitegen.AddCardLabelParams{WorkspaceID: ws.ID, CardID: card.ID, LabelID: lbl.ID}); err != nil {
			t.Fatal(err)
		}
		// Second insert must be a no-op (ON CONFLICT DO NOTHING), not a unique violation.
		if err := q.AddCardLabel(ctx, sqlitegen.AddCardLabelParams{WorkspaceID: ws.ID, CardID: card.ID, LabelID: lbl.ID}); err != nil {
			t.Fatal(err)
		}
		if ids, err := q.ListCardLabelIDs(ctx, card.ID); err != nil || len(ids) != 1 || ids[0] != lbl.ID {
			t.Fatalf("ListCardLabelIDs = %v, %v", ids, err)
		}
		if err := q.RemoveCardLabel(ctx, sqlitegen.RemoveCardLabelParams{CardID: card.ID, LabelID: lbl.ID}); err != nil {
			t.Fatal(err)
		}
		if ids, err := q.ListCardLabelIDs(ctx, card.ID); err != nil || len(ids) != 0 {
			t.Fatalf("ListCardLabelIDs after remove = %v, %v", ids, err)
		}

		if err := q.AddCardAssignee(ctx, sqlitegen.AddCardAssigneeParams{WorkspaceID: ws.ID, CardID: card.ID, UserID: u.ID}); err != nil {
			t.Fatal(err)
		}
		// Second insert must be a no-op (ON CONFLICT DO NOTHING), not a unique violation.
		if err := q.AddCardAssignee(ctx, sqlitegen.AddCardAssigneeParams{WorkspaceID: ws.ID, CardID: card.ID, UserID: u.ID}); err != nil {
			t.Fatal(err)
		}
		if ids, err := q.ListCardAssigneeIDs(ctx, card.ID); err != nil || len(ids) != 1 || ids[0] != u.ID {
			t.Fatalf("ListCardAssigneeIDs = %v, %v", ids, err)
		}
		if err := q.RemoveCardAssignee(ctx, sqlitegen.RemoveCardAssigneeParams{CardID: card.ID, UserID: u.ID}); err != nil {
			t.Fatal(err)
		}
		if ids, err := q.ListCardAssigneeIDs(ctx, card.ID); err != nil || len(ids) != 0 {
			t.Fatalf("ListCardAssigneeIDs after remove = %v, %v", ids, err)
		}
	})
}

// TestMembershipUpserts proves the ON CONFLICT DO UPDATE SET role =
// excluded.role clause actually runs on both engines (verify-early item 3
// was only checked at sqlc-generation time, not at runtime).
func TestMembershipUpserts(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "mem", CreatedAt: 1, UpdatedAt: 1})
		u, _ := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "m@b.c", Valid: true}, Name: "M", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "MEM", Name: "P", CreatedAt: 1, UpdatedAt: 1})

		if err := q.UpsertWorkspaceMember(ctx, sqlitegen.UpsertWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: u.ID, Role: "member", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if err := q.UpsertWorkspaceMember(ctx, sqlitegen.UpsertWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: u.ID, Role: "admin", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetWorkspaceMember(ctx, sqlitegen.GetWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: u.ID}); err != nil || got.Role != "admin" {
			t.Fatalf("GetWorkspaceMember after upsert = %+v, %v; want role=admin", got, err)
		}

		if err := q.UpsertProjectMember(ctx, sqlitegen.UpsertProjectMemberParams{WorkspaceID: ws.ID, ProjectID: p.ID, UserID: u.ID, Role: "member", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if err := q.UpsertProjectMember(ctx, sqlitegen.UpsertProjectMemberParams{WorkspaceID: ws.ID, ProjectID: p.ID, UserID: u.ID, Role: "lead", CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if members, err := q.ListProjectMembers(ctx, p.ID); err != nil || len(members) != 1 || members[0].Role != "lead" {
			t.Fatalf("ListProjectMembers after upsert = %+v, %v; want one member with role=lead", members, err)
		}
		if memberships, err := q.ListProjectMembershipsForUser(ctx, u.ID); err != nil || len(memberships) != 1 || memberships[0].ProjectID != p.ID {
			t.Fatalf("ListProjectMembershipsForUser = %+v, %v", memberships, err)
		}
	})
}

func TestCommentQueries(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "cmt", CreatedAt: 1, UpdatedAt: 1})
		u, _ := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "c@b.c", Valid: true}, Name: "C", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "CMT", Name: "P", CreatedAt: 1, UpdatedAt: 1})
		b, _ := q.CreateBoard(ctx, sqlitegen.CreateBoardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "B", Position: "V", CreatedAt: 1, UpdatedAt: 1})
		c, _ := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "Todo", Position: "V", Category: "todo", CreatedAt: 1, UpdatedAt: 1})
		card, _ := q.CreateCard(ctx, sqlitegen.CreateCardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, BoardID: b.ID, ColumnID: c.ID, Number: 1, Title: "t", Description: "", Position: "V", CreatedBy: u.ID, CreatedAt: 1, UpdatedAt: 1})

		cm, err := q.CreateComment(ctx, sqlitegen.CreateCommentParams{ID: id.New(), WorkspaceID: ws.ID, CardID: card.ID, AuthorID: u.ID, Body: "hi", CreatedAt: 1, UpdatedAt: 1})
		if err != nil || cm.Body != "hi" {
			t.Fatalf("CreateComment = %+v, %v", cm, err)
		}
		if cs, err := q.ListComments(ctx, card.ID); err != nil || len(cs) != 1 || cs[0].ID != cm.ID {
			t.Fatalf("ListComments = %d, %v", len(cs), err)
		}
	})
}

// TestJobLifecycle walks a job through queued -> leased -> done, a second
// job through queued -> leased -> retried -> re-leased, and a third
// through queued -> leased -> dead.
func TestJobLifecycle(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()

		for _, jid := range []string{"j1", "j2", "j3"} {
			if err := q.InsertJob(ctx, sqlitegen.InsertJobParams{ID: jid, Kind: "k", Payload: "{}", MaxAttempts: 3, RunAt: 1, CreatedAt: 1}); err != nil {
				t.Fatal(err)
			}
		}

		leased, err := q.LeaseJobs(ctx, sqlitegen.LeaseJobsParams{
			Owner:      sql.NullString{String: "w1", Valid: true},
			LeaseUntil: sql.NullInt64{Int64: 100, Valid: true},
			NowQueued:  1,
			NowLeased:  sql.NullInt64{Int64: 0, Valid: true},
			Batch:      10,
		})
		if err != nil || len(leased) != 3 {
			t.Fatalf("LeaseJobs = %d, %v", len(leased), err)
		}
		for _, j := range leased {
			if j.State != "leased" || j.LeaseOwner.String != "w1" {
				t.Fatalf("leased job = %+v", j)
			}
		}

		if err := q.CompleteJob(ctx, sqlitegen.CompleteJobParams{CompletedAt: sql.NullInt64{Int64: 5, Valid: true}, ID: "j1", Owner: sql.NullString{String: "w1", Valid: true}}); err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetJob(ctx, "j1"); err != nil || got.State != "done" {
			t.Fatalf("GetJob j1 = %+v, %v", got, err)
		}

		if err := q.RetryJob(ctx, sqlitegen.RetryJobParams{RunAt: 200, LastError: sql.NullString{String: "boom", Valid: true}, ID: "j2", Owner: sql.NullString{String: "w1", Valid: true}}); err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetJob(ctx, "j2"); err != nil || got.State != "queued" || got.RunAt != 200 {
			t.Fatalf("GetJob j2 after retry = %+v, %v", got, err)
		}
		// j2 is the only row back in the queued state; CountQueuedJobs
		// matches on kind and payload together, so a different payload
		// matches nothing.
		if n, err := q.CountQueuedJobs(ctx, sqlitegen.CountQueuedJobsParams{Kind: "k", Payload: "{}"}); err != nil || n != 1 {
			t.Fatalf("CountQueuedJobs(k, {}) = %d, %v", n, err)
		}
		if n, err := q.CountQueuedJobs(ctx, sqlitegen.CountQueuedJobsParams{Kind: "k", Payload: `{"other":1}`}); err != nil || n != 0 {
			t.Fatalf("CountQueuedJobs(k, other) = %d, %v", n, err)
		}
		relaunched, err := q.LeaseJobs(ctx, sqlitegen.LeaseJobsParams{
			Owner:      sql.NullString{String: "w2", Valid: true},
			LeaseUntil: sql.NullInt64{Int64: 300, Valid: true},
			NowQueued:  200,
			NowLeased:  sql.NullInt64{Int64: 0, Valid: true},
			Batch:      10,
		})
		if err != nil || len(relaunched) != 1 || relaunched[0].ID != "j2" {
			t.Fatalf("LeaseJobs after retry = %d, %v", len(relaunched), err)
		}

		if err := q.DeadJob(ctx, sqlitegen.DeadJobParams{CompletedAt: sql.NullInt64{Int64: 9, Valid: true}, LastError: sql.NullString{String: "fatal", Valid: true}, ID: "j3", Owner: sql.NullString{String: "w1", Valid: true}}); err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetJob(ctx, "j3"); err != nil || got.State != "dead" {
			t.Fatalf("GetJob j3 = %+v, %v", got, err)
		}
		if dead, err := q.ListJobsByState(ctx, sqlitegen.ListJobsByStateParams{State: "dead", Lim: 10}); err != nil || len(dead) != 1 || dead[0].ID != "j3" {
			t.Fatalf("ListJobsByState(dead) = %d, %v", len(dead), err)
		}
	})
}

func TestMiscGetters(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		if cnt, err := q.CountWorkspaces(ctx); err != nil || cnt != 0 {
			t.Fatalf("CountWorkspaces before create = %d, %v", cnt, err)
		}
		ws, err := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "misc", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if cnt, err := q.CountWorkspaces(ctx); err != nil || cnt != 1 {
			t.Fatalf("CountWorkspaces after create = %d, %v", cnt, err)
		}
		if got, err := q.GetWorkspace(ctx, ws.ID); err != nil || got.ID != ws.ID {
			t.Fatalf("GetWorkspace = %+v, %v", got, err)
		}
		u, err := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "x@b.c", Valid: true}, Name: "X", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetUser(ctx, u.ID); err != nil || got.ID != u.ID {
			t.Fatalf("GetUser = %+v, %v", got, err)
		}
		p, err := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "MSC", Name: "P", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.GetProject(ctx, p.ID); err != nil || got.ID != p.ID {
			t.Fatalf("GetProject = %+v, %v", got, err)
		}
		if got, err := q.GetProjectByKey(ctx, sqlitegen.GetProjectByKeyParams{WorkspaceID: ws.ID, Key: "MSC"}); err != nil || got.ID != p.ID {
			t.Fatalf("GetProjectByKey = %+v, %v", got, err)
		}
	})
}

// coveredQuerierMethods is a hand-maintained list of every sqlitegen.Querier
// method exercised by a test in this file. TestAllQuerierMethodsAreCovered
// fails if sqlitegen.Querier ever gains a method not added here, so a new
// query can never land without a test that runs it on both engines.
var coveredQuerierMethods = map[string]bool{
	"AddCardAssignee":               true,
	"AddCardLabel":                  true,
	"CompleteJob":                   true,
	"CountCardsInColumn":            true,
	"CountQueuedJobs":               true,
	"CountWorkspaces":               true,
	"CreateBoard":                   true,
	"CreateCard":                    true,
	"CreateColumn":                  true,
	"CreateComment":                 true,
	"CreateLabel":                   true,
	"CreateProject":                 true,
	"CreateUser":                    true,
	"CreateWorkspace":               true,
	"DeadJob":                       true,
	"DeleteReceiptsBefore":          true,
	"FirstPositionInColumn":         true,
	"GetBoard":                      true,
	"GetBoardByProject":             true,
	"GetCard":                       true,
	"GetCardByNumber":               true,
	"GetColumn":                     true,
	"GetJob":                        true,
	"GetLabel":                      true,
	"GetProject":                    true,
	"GetProjectByKey":               true,
	"GetReceipt":                    true,
	"GetUser":                       true,
	"GetUserByEmail":                true,
	"GetWorkspace":                  true,
	"GetWorkspaceBySlug":            true,
	"GetWorkspaceMember":            true,
	"InsertEvent":                   true,
	"InsertJob":                     true,
	"InsertReceipt":                 true,
	"LastPositionInColumn":          true,
	"LeaseJobs":                     true,
	"ListCardAssigneeIDs":           true,
	"ListCardLabelIDs":              true,
	"ListCardsByBoard":              true,
	"ListCardsByColumn":             true,
	"ListColumns":                   true,
	"ListComments":                  true,
	"ListEventsByCard":              true,
	"ListEventsSince":               true,
	"ListJobsByState":               true,
	"ListLabels":                    true,
	"ListProjectMembers":            true,
	"ListProjectMembershipsForUser": true,
	"ListProjects":                  true,
	"MoveCard":                      true,
	"NextCardNumber":                true,
	"NextPositionAfter":             true,
	"NextSeq":                       true,
	"PrevPositionBefore":            true,
	"RemoveCardAssignee":            true,
	"RemoveCardLabel":               true,
	"RetryJob":                      true,
	"SetCardArchived":               true,
	"TouchCard":                     true,
	"UpdateCardFields":              true,
	"UpsertProjectMember":           true,
	"UpsertWorkspaceMember":         true,
}

// TestAllQuerierMethodsAreCovered guards against a new query landing
// without a test that runs it on both engines: it lists sqlitegen.Querier's
// methods via reflection and fails if any is missing from
// coveredQuerierMethods above.
func TestAllQuerierMethodsAreCovered(t *testing.T) {
	typ := reflect.TypeOf((*sqlitegen.Querier)(nil)).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if !coveredQuerierMethods[name] {
			t.Errorf("sqlitegen.Querier.%s has no test exercising it on both engines; add one, then add %q to coveredQuerierMethods", name, name)
		}
	}
	if len(coveredQuerierMethods) != typ.NumMethod() {
		t.Errorf("coveredQuerierMethods has %d entries but sqlitegen.Querier has %d methods; keep them in sync", len(coveredQuerierMethods), typ.NumMethod())
	}
}
