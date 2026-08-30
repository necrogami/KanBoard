package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

// TestVersionedZeroRows drives the branch that turns "the versioned
// UPDATE matched no row" into E_CONFLICT carrying the version now
// stored. In production that happens when another process commits
// between this transaction's read and its update, which needs two
// writers; here the version is bumped through the store first and
// versioned is handed the zero-rows result directly, which exercises
// the same code with one process.
func TestVersionedZeroRows(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		ctx := context.Background()
		fake := clock.NewFake(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC))
		svc := New(st, WithClock(fake))
		ws, err := svc.CreateWorkspace(ctx, commands.CreateWorkspace{Name: "W", Slug: "w", AdminEmail: "a@b.c", AdminName: "A"})
		if err != nil {
			t.Fatal(err)
		}
		admin, err := svc.LoadActor(ctx, ws.ID, ws.AdminUserID, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := svc.CreateProject(ctx, admin, commands.CreateProject{WorkspaceID: ws.ID, Key: "VER", Name: "V"})
		if err != nil {
			t.Fatal(err)
		}
		admin, err = svc.LoadActor(ctx, ws.ID, ws.AdminUserID, nil)
		if err != nil {
			t.Fatal(err)
		}
		card, err := svc.CreateCard(ctx, admin, commands.CreateCard{ProjectID: p.ID, Title: "c"})
		if err != nil {
			t.Fatal(err)
		}

		q := st.Q()
		if _, err := q.TouchCard(ctx, sqlitegen.TouchCardParams{ID: card.ID, WorkspaceID: ws.ID, Version: card.Version, UpdatedAt: 2}); err != nil {
			t.Fatal(err)
		}
		tx := &Tx{ctx: ctx, Q: q, s: svc, actor: admin}

		_, err = tx.versioned(card.ID, sqlitegen.Card{}, sql.ErrNoRows)
		var se *Error
		if !errors.As(err, &se) || se.Code != CodeConflict {
			t.Fatalf("stale update = %v, want E_CONFLICT", err)
		}
		if se.CurrentVersion != card.Version+1 {
			t.Fatalf("current version = %d, want %d", se.CurrentVersion, card.Version+1)
		}

		_, err = tx.versioned("no-such-card", sqlitegen.Card{}, sql.ErrNoRows)
		if !errors.As(err, &se) || se.Code != CodeNotFound {
			t.Fatalf("missing card = %v, want E_NOT_FOUND", err)
		}

		// Any other error, and the success case, pass straight through.
		row := sqlitegen.Card{ID: card.ID, Version: 7}
		if got, err := tx.versioned(card.ID, row, nil); err != nil || got.Version != 7 {
			t.Fatalf("passthrough = %+v, %v", got, err)
		}
	})
}
