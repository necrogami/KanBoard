package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/service"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

// recvEvent waits up to 2s for an event on c so a stuck bus fails the
// test instead of hanging it.
func recvEvent(t *testing.T, c <-chan events.Event) events.Event {
	t.Helper()
	select {
	case ev := <-c:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return events.Event{}
	}
}

func projectWithCards(t *testing.T, h *harness, key string, n int) (service.Project, service.Board, []service.Card) {
	t.Helper()
	ctx := context.Background()
	p, err := h.svc.CreateProject(ctx, h.admin, commands.CreateProject{WorkspaceID: h.ws.ID, Key: key, Name: key})
	if err != nil {
		t.Fatal(err)
	}
	h.admin, _ = h.svc.LoadActor(ctx, h.ws.ID, h.ws.AdminUserID, nil)
	var cards []service.Card
	for i := 0; i < n; i++ {
		c, err := h.svc.CreateCard(ctx, h.admin, commands.CreateCard{ProjectID: p.ID, Title: "card"})
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, c)
	}
	b, _, err := h.svc.GetBoard(ctx, h.admin, key)
	if err != nil {
		t.Fatal(err)
	}
	return p, b, cards
}

func TestCreateCardNumbersAndOrders(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		_, b, cards := projectWithCards(t, h, "NUM", 3)
		for i, c := range cards {
			if c.Number != int64(i+1) || c.Key != "NUM-"+string(rune('1'+i)) {
				t.Fatalf("card %d = %s number %d", i, c.Key, c.Number)
			}
			if c.ColumnID != b.Columns[0].ID {
				t.Fatalf("card not in first column")
			}
		}
		if cards[0].Position >= cards[1].Position || cards[1].Position >= cards[2].Position {
			t.Fatalf("positions %q %q %q", cards[0].Position, cards[1].Position, cards[2].Position)
		}
	})
}

func TestCreateCardInNamedColumnAndForbidden(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		p, b, _ := projectWithCards(t, h, "COL", 0)
		sub := h.bus.Subscribe(h.ws.ID)
		defer sub.Close()

		c, err := h.svc.CreateCard(ctx, h.admin, commands.CreateCard{ProjectID: p.ID, ColumnID: b.Columns[2].ID, Title: "done already"})
		if err != nil {
			t.Fatal(err)
		}
		if c.ColumnID != b.Columns[2].ID {
			t.Fatal("column ignored")
		}
		ev := recvEvent(t, sub.C)
		if ev.Kind != events.CardCreated {
			t.Fatalf("event = %s", ev.Kind)
		}
		var cp events.CardCreatedPayload
		if err := json.Unmarshal([]byte(ev.Payload), &cp); err != nil {
			t.Fatal(err)
		}
		if cp.Number != c.Number || cp.Title != c.Title || cp.ColumnID != b.Columns[2].ID || cp.ColumnName != b.Columns[2].Name {
			t.Fatalf("payload = %+v", cp)
		}

		viewer := policy.Actor{UserID: "v", WorkspaceID: h.ws.ID, WorkspaceRole: policy.WorkspaceMember, ProjectRoles: map[string]policy.ProjectRole{p.ID: policy.ProjectViewer}}
		_, err = h.svc.CreateCard(ctx, viewer, commands.CreateCard{ProjectID: p.ID, Title: "nope"})
		_ = code(t, err, service.CodeForbidden)
	})
}

func TestMoveCardBetweenAndAcrossColumns(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, b, cards := projectWithCards(t, h, "MOV", 3)
		sub := h.bus.Subscribe(h.ws.ID)
		defer sub.Close()

		// Move card 3 between 1 and 2 (same column, after card 1).
		moved, soft, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[2].Key, ColumnID: b.Columns[0].ID, AfterKey: cards[0].Key})
		if err != nil || soft {
			t.Fatal(err, soft)
		}
		if cards[0].Position >= moved.Position || moved.Position >= cards[1].Position {
			t.Fatalf("position %q not between %q and %q", moved.Position, cards[0].Position, cards[1].Position)
		}
		if moved.Version != cards[2].Version+1 {
			t.Fatalf("version = %d", moved.Version)
		}
		ev := recvEvent(t, sub.C)
		if ev.Kind != events.CardMoved {
			t.Fatalf("event = %s", ev.Kind)
		}
		var mp events.CardMovedPayload
		if err := json.Unmarshal([]byte(ev.Payload), &mp); err != nil {
			t.Fatal(err)
		}
		if mp.FromColumnID != b.Columns[0].ID || mp.ToColumnID != b.Columns[0].ID {
			t.Fatalf("column ids = %+v", mp)
		}
		if mp.FromName != b.Columns[0].Name || mp.ToName != b.Columns[0].Name {
			t.Fatalf("column names = %+v", mp)
		}
		if mp.FromCategory != b.Columns[0].Category || mp.ToCategory != b.Columns[0].Category {
			t.Fatalf("column categories = %+v", mp)
		}

		// Move card 1 to the top of Done: completed_at is set, version +1 only.
		done, _, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[0].Key, ColumnID: b.Columns[2].ID, Position: commands.PositionTop})
		if err != nil {
			t.Fatal(err)
		}
		if done.ColumnID != b.Columns[2].ID || done.CompletedAt == nil || done.Version != cards[0].Version+1 {
			t.Fatalf("done = %+v", done)
		}

		// Back to the first column, bottom: completed_at clears.
		back, _, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[0].Key, ColumnID: b.Columns[0].ID})
		if err != nil {
			t.Fatal(err)
		}
		if back.CompletedAt != nil || back.Position <= cards[1].Position {
			t.Fatalf("back = %+v", back)
		}
	})
}

func TestMoveCardVersionConflict(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, b, cards := projectWithCards(t, h, "VER", 1)
		_, _, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{Meta: commands.Meta{ExpectedVersion: 99}, CardKey: cards[0].Key, ColumnID: b.Columns[1].ID})
		se := code(t, err, service.CodeConflict)
		if se.CurrentVersion != 1 {
			t.Fatalf("current version = %d", se.CurrentVersion)
		}
		// Forbidden beats conflict: a viewer with a stale version sees E_FORBIDDEN.
		viewer := policy.Actor{UserID: "v", WorkspaceID: h.ws.ID, WorkspaceRole: policy.WorkspaceMember, ProjectRoles: map[string]policy.ProjectRole{cards[0].ProjectID: policy.ProjectViewer}}
		_, _, err = h.svc.MoveCard(ctx, viewer, commands.MoveCard{Meta: commands.Meta{ExpectedVersion: 99}, CardKey: cards[0].Key, ColumnID: b.Columns[1].ID})
		_ = code(t, err, service.CodeForbidden)
	})
}

func TestMoveCardAcrossColumnsWithSamePositionString(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		p, b, cards := projectWithCards(t, h, "SAME", 1) // card X at "V" in column 0
		y, err := h.svc.CreateCard(ctx, h.admin, commands.CreateCard{ProjectID: p.ID, ColumnID: b.Columns[2].ID, Title: "y"})
		if err != nil {
			t.Fatal(err)
		}
		if y.Position != cards[0].Position {
			t.Fatalf("test setup: positions differ (%q vs %q)", y.Position, cards[0].Position)
		}
		// X's own position string must not be skipped in a column where it
		// belongs to another card; the result must sort above Y.
		moved, _, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[0].Key, ColumnID: b.Columns[2].ID, Position: commands.PositionTop})
		if err != nil {
			t.Fatal(err)
		}
		if moved.Position >= y.Position {
			t.Fatalf("moved %q is not above %q", moved.Position, y.Position)
		}
	})
}

func TestMoveCardMissingSiblingIsSoftConflict(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, b, cards := projectWithCards(t, h, "SOFT", 3)
		moved, soft, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[0].Key, ColumnID: b.Columns[0].ID, AfterKey: "SOFT-999"})
		if err != nil || !soft {
			t.Fatalf("missing sibling: err=%v soft=%v", err, soft)
		}
		if moved.Position <= cards[2].Position {
			t.Fatalf("not appended at the bottom: %q vs %q", moved.Position, cards[2].Position)
		}
		// Sibling exists but lives in another column: same treatment.
		_, _, err = h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[1].Key, ColumnID: b.Columns[1].ID})
		if err != nil {
			t.Fatal(err)
		}
		_, soft, err = h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[2].Key, ColumnID: b.Columns[0].ID, BeforeKey: cards[1].Key})
		if err != nil || !soft {
			t.Fatalf("sibling elsewhere: err=%v soft=%v", err, soft)
		}
	})
}

// TestSelfAnchoredMove covers both halves of a move whose anchor is the
// moving card itself: within its own column the request asks for nothing
// and is a no-op, but with a different target column the anchor cannot be
// honoured, so the card lands at the bottom of the target and the move is
// reported as a soft conflict (spec 4.5).
func TestSelfAnchoredMove(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, b, cards := projectWithCards(t, h, "SELF", 2)

		same, soft, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[0].Key, ColumnID: b.Columns[0].ID, AfterKey: cards[0].Key})
		if err != nil || soft {
			t.Fatalf("same column: err=%v soft=%v", err, soft)
		}
		if same.ColumnID != cards[0].ColumnID || same.Version != cards[0].Version {
			t.Fatalf("same column changed the card: %+v", same)
		}

		moved, soft, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[0].Key, ColumnID: b.Columns[2].ID, AfterKey: cards[0].Key})
		if err != nil {
			t.Fatal(err)
		}
		if !soft {
			t.Fatal("self anchor in another column is not a soft conflict")
		}
		if moved.ColumnID != b.Columns[2].ID {
			t.Fatalf("column = %q, want %q", moved.ColumnID, b.Columns[2].ID)
		}
		if moved.Version != cards[0].Version+1 {
			t.Fatalf("version = %d, want %d", moved.Version, cards[0].Version+1)
		}
		if moved.CompletedAt == nil {
			t.Fatal("moving into a done column did not set completed_at")
		}
	})
}

func TestMoveEnqueuesRebalanceWhenKeysGrow(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, b, cards := projectWithCards(t, h, "GROW", 2)
		// Alternating "move to top" prepends keys and grows them one digit
		// every few moves; the rebalance job must appear once a key passes
		// 64 characters.
		for i := 0; i < 1000; i++ {
			key := cards[i%2].Key
			if _, _, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: key, ColumnID: b.Columns[0].ID, Position: commands.PositionTop}); err != nil {
				t.Fatal(err)
			}
			jobs, err := st.Q().ListJobsByState(ctx, sqlitegen.ListJobsByStateParams{State: "queued", Lim: 10})
			if err != nil {
				t.Fatal(err)
			}
			for _, j := range jobs {
				if j.Kind == "rank.rebalance" && strings.Contains(j.Payload, b.Columns[0].ID) {
					return
				}
			}
		}
		t.Fatal("no rank.rebalance job after 1000 prepends")
	})
}

func TestMoveCardRejectsForeignColumn(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, _, cards := projectWithCards(t, h, "AAA", 1)
		_, other, _ := projectWithCards(t, h, "BBB", 0)
		_, _, err := h.svc.MoveCard(ctx, h.admin, commands.MoveCard{CardKey: cards[0].Key, ColumnID: other.Columns[0].ID})
		_ = code(t, err, service.CodeValidation)
	})
}
