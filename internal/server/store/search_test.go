package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/filter"
	"github.com/necrogami/kanboard/internal/core/id"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

func TestSearchCards(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		q := st.Q()
		ws, _ := q.CreateWorkspace(ctx, sqlitegen.CreateWorkspaceParams{ID: id.New(), Name: "W", Slug: "s", CreatedAt: 1, UpdatedAt: 1})
		u, _ := q.CreateUser(ctx, sqlitegen.CreateUserParams{ID: id.New(), WorkspaceID: ws.ID, Email: sql.NullString{String: "a@b.c", Valid: true}, Name: "A", Kind: "human", Locale: "en", CreatedAt: 1, UpdatedAt: 1})
		p, _ := q.CreateProject(ctx, sqlitegen.CreateProjectParams{ID: id.New(), WorkspaceID: ws.ID, Key: "SRCH", Name: "S", CreatedAt: 1, UpdatedAt: 1})
		b, _ := q.CreateBoard(ctx, sqlitegen.CreateBoardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "B", Position: "V", CreatedAt: 1, UpdatedAt: 1})
		c1, _ := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "A", Position: "F", Category: "todo", CreatedAt: 1, UpdatedAt: 1})
		c2, _ := q.CreateColumn(ctx, sqlitegen.CreateColumnParams{ID: id.New(), WorkspaceID: ws.ID, BoardID: b.ID, Name: "B", Position: "V", Category: "done", CreatedAt: 1, UpdatedAt: 1})
		lbl, _ := q.CreateLabel(ctx, sqlitegen.CreateLabelParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, Name: "bug", Color: "#FF0000", CreatedAt: 1, UpdatedAt: 1})
		mk := func(n int64, col, title string, updated int64) sqlitegen.Card {
			c, err := q.CreateCard(ctx, sqlitegen.CreateCardParams{ID: id.New(), WorkspaceID: ws.ID, ProjectID: p.ID, BoardID: b.ID, ColumnID: col, Number: n, Title: title, Description: "", Position: "V" + string(rune('a'+n)), CreatedBy: u.ID, CreatedAt: updated, UpdatedAt: updated})
			if err != nil {
				t.Fatal(err)
			}
			return c
		}
		a := mk(1, c1.ID, "Fix login 100% broken", 10)
		mk(2, c1.ID, "Write docs", 20)
		d := mk(3, c2.ID, "Deploy", 30)
		_ = q.AddCardLabel(ctx, sqlitegen.AddCardLabelParams{WorkspaceID: ws.ID, CardID: a.ID, LabelID: lbl.ID})
		if _, err := q.SetCardArchived(ctx, sqlitegen.SetCardArchivedParams{ID: d.ID, Version: d.Version, ArchivedAt: sqlNull(40), UpdatedAt: 40}); err != nil {
			t.Fatal(err)
		}

		run := func(f filter.Filter) []sqlitegen.Card {
			t.Helper()
			if err := f.Normalize(); err != nil {
				t.Fatal(err)
			}
			rows, _, err := store.SearchCards(ctx, st.DB, st.Dialect, p.ID, f)
			if err != nil {
				t.Fatal(err)
			}
			return rows
		}
		if got := run(filter.Filter{}); len(got) != 2 {
			t.Fatalf("default excludes archived: got %d", len(got))
		}
		arch := true
		if got := run(filter.Filter{Archived: &arch}); len(got) != 1 || got[0].ID != d.ID {
			t.Fatalf("archived only: %+v", got)
		}
		if got := run(filter.Filter{Text: "100%"}); len(got) != 1 || got[0].ID != a.ID {
			t.Fatalf("text with wildcard char: %d", len(got))
		}
		if got := run(filter.Filter{Text: "fix LOGIN"}); len(got) != 1 || got[0].ID != a.ID {
			t.Fatalf("case-insensitive text: %d", len(got))
		}
		if got := run(filter.Filter{LabelIDs: []string{lbl.ID}}); len(got) != 1 || got[0].ID != a.ID {
			t.Fatalf("label: %d", len(got))
		}
		if got := run(filter.Filter{ColumnIDs: []string{c2.ID}}); len(got) != 0 {
			t.Fatalf("column c2 active: %d", len(got))
		}
		ua := clockAt(15)
		if got := run(filter.Filter{UpdatedAfter: &ua}); len(got) != 1 {
			t.Fatalf("updated after: %d", len(got))
		}
		// Pagination: limit 1 yields a cursor, second page yields the rest.
		f := filter.Filter{Limit: 1}
		_ = f.Normalize()
		page1, cursor, err := store.SearchCards(ctx, st.DB, st.Dialect, p.ID, f)
		if err != nil || len(page1) != 1 || cursor == "" {
			t.Fatalf("page1 = %d, cursor %q, %v", len(page1), cursor, err)
		}
		f.Cursor = cursor
		page2, cursor2, err := store.SearchCards(ctx, st.DB, st.Dialect, p.ID, f)
		if err != nil || len(page2) != 1 || cursor2 != "" || page2[0].ID == page1[0].ID {
			t.Fatalf("page2 = %d, cursor %q, %v", len(page2), cursor2, err)
		}
	})
}

func sqlNull(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

func clockAt(ms int64) time.Time { return clock.FromMillis(ms) }
