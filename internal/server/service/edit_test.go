package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/filter"
	"github.com/necrogami/kanboard/internal/server/service"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/storetest"
)

func TestUpdateCardFieldsAndEvents(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, _, cards := projectWithCards(t, h, "UPD", 1)
		title := "Renamed"
		due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		c, err := h.svc.UpdateCard(ctx, h.admin, commands.UpdateCard{CardKey: cards[0].Key, Title: &title, DueDate: &due})
		if err != nil {
			t.Fatal(err)
		}
		if c.Title != "Renamed" || c.DueDate == nil || !c.DueDate.Equal(due) || c.Version != 2 {
			t.Fatalf("card = %+v", c)
		}
		var n int
		if err := st.DB.QueryRow("SELECT count(*) FROM event WHERE kind = 'card.updated'").Scan(&n); err != nil || n != 2 {
			t.Fatalf("card.updated events = %d (title and due_date expected)", n)
		}
		c, err = h.svc.UpdateCard(ctx, h.admin, commands.UpdateCard{CardKey: cards[0].Key, ClearDueDate: true})
		if err != nil || c.DueDate != nil {
			t.Fatalf("clear due date: %+v, %v", c, err)
		}
		// Sending the same title again changes nothing: no version bump, no event.
		before := c.Version
		c, err = h.svc.UpdateCard(ctx, h.admin, commands.UpdateCard{CardKey: cards[0].Key, Title: &title})
		if err != nil || c.Version != before {
			t.Fatalf("no-op update bumped version: %d -> %d, %v", before, c.Version, err)
		}
		if err := st.DB.QueryRow("SELECT count(*) FROM event WHERE kind = 'card.updated'").Scan(&n); err != nil || n != 3 {
			t.Fatalf("card.updated events after no-op = %d, want 3", n)
		}
		// Stale version on a real change is E_CONFLICT with the current version.
		other := "Other"
		_, err = h.svc.UpdateCard(ctx, h.admin, commands.UpdateCard{Meta: commands.Meta{ExpectedVersion: 1}, CardKey: cards[0].Key, Title: &other})
		if se := code(t, err, service.CodeConflict); se.CurrentVersion != before {
			t.Fatalf("current version = %d, want %d", se.CurrentVersion, before)
		}
		// Description-only change: exactly one card.updated, naming the
		// field but carrying no old or new text.
		desc := "sensitive body"
		c, err = h.svc.UpdateCard(ctx, h.admin, commands.UpdateCard{CardKey: cards[0].Key, Description: &desc})
		if err != nil || c.Version != before+1 {
			t.Fatalf("description update: %+v, %v", c, err)
		}
		if err := st.DB.QueryRow("SELECT count(*) FROM event WHERE kind = 'card.updated'").Scan(&n); err != nil || n != 4 {
			t.Fatalf("card.updated events after description = %d, want 4", n)
		}
		var payload string
		if err := st.DB.QueryRow("SELECT payload FROM event WHERE kind = 'card.updated' ORDER BY seq DESC LIMIT 1").Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var up events.CardUpdatedPayload
		if err := json.Unmarshal([]byte(payload), &up); err != nil {
			t.Fatal(err)
		}
		if up.Field != "description" || up.Old != "" || up.New != "" {
			t.Fatalf("description payload = %+v", up)
		}
	})
}

func TestArchiveRestoreAndSearch(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		_, _, cards := projectWithCards(t, h, "ARC", 2)
		a, err := h.svc.ArchiveCard(ctx, h.admin, commands.ArchiveCard{CardKey: cards[0].Key})
		if err != nil || a.ArchivedAt == nil {
			t.Fatalf("archive: %+v, %v", a, err)
		}
		_, err = h.svc.ArchiveCard(ctx, h.admin, commands.ArchiveCard{CardKey: cards[0].Key})
		_ = code(t, err, service.CodeConflict)
		found, cursor, err := h.svc.SearchCards(ctx, h.admin, "ARC", filter.Filter{})
		if err != nil || len(found) != 1 || cursor != "" || found[0].Key != cards[1].Key {
			t.Fatalf("search active = %+v, %q, %v", found, cursor, err)
		}
		r, err := h.svc.RestoreCard(ctx, h.admin, commands.RestoreCard{CardKey: cards[0].Key})
		if err != nil || r.ArchivedAt != nil {
			t.Fatalf("restore: %+v, %v", r, err)
		}
		_, comments, err := h.svc.GetCard(ctx, h.admin, cards[0].Key)
		if err != nil || len(comments) != 0 {
			t.Fatalf("get: %v", err)
		}
	})
}

func TestCommentsLabelsAssignees(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		p, _, cards := projectWithCards(t, h, "CLA", 1)
		cm, err := h.svc.AddComment(ctx, h.admin, commands.AddComment{CardKey: cards[0].Key, Body: "hello"})
		if err != nil || cm.Body != "hello" || cm.AuthorID != h.ws.AdminUserID {
			t.Fatalf("comment = %+v, %v", cm, err)
		}
		lbl, err := h.svc.CreateLabel(ctx, h.admin, commands.CreateLabel{ProjectID: p.ID, Name: "bug", Color: "#FF0000"})
		if err != nil {
			t.Fatal(err)
		}
		c, err := h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[0].Key, LabelIDs: []string{lbl.ID}})
		if err != nil || len(c.LabelIDs) != 1 || c.Version != cards[0].Version+1 {
			t.Fatalf("labels = %+v, %v", c, err)
		}
		_, err = h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{Meta: commands.Meta{ExpectedVersion: cards[0].Version}, CardKey: cards[0].Key})
		_ = code(t, err, service.CodeConflict)
		_, err = h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[0].Key, LabelIDs: []string{"not-a-label"}})
		_ = code(t, err, service.CodeValidation)
		c, err = h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[0].Key})
		if err != nil || len(c.LabelIDs) != 0 {
			t.Fatalf("clear labels = %+v, %v", c, err)
		}
		// Re-sending the identical (empty) label set is a no-op: version
		// unchanged, no event.
		noopLabelVersion := c.Version
		c, err = h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[0].Key})
		if err != nil || c.Version != noopLabelVersion {
			t.Fatalf("no-op labels bumped version: %d -> %d, %v", noopLabelVersion, c.Version, err)
		}
		c, err = h.svc.SetAssignees(ctx, h.admin, commands.SetAssignees{CardKey: cards[0].Key, UserIDs: []string{h.ws.AdminUserID}})
		if err != nil || len(c.AssigneeIDs) != 1 {
			t.Fatalf("assignees = %+v, %v", c, err)
		}
		// Re-sending the identical assignee set is a no-op: version
		// unchanged, no event.
		noopAssigneeVersion := c.Version
		c, err = h.svc.SetAssignees(ctx, h.admin, commands.SetAssignees{CardKey: cards[0].Key, UserIDs: []string{h.ws.AdminUserID}})
		if err != nil || c.Version != noopAssigneeVersion {
			t.Fatalf("no-op assignees bumped version: %d -> %d, %v", noopAssigneeVersion, c.Version, err)
		}
		_, err = h.svc.SetAssignees(ctx, h.admin, commands.SetAssignees{CardKey: cards[0].Key, UserIDs: []string{"stranger"}})
		_ = code(t, err, service.CodeValidation)
		card, comments, err := h.svc.GetCard(ctx, h.admin, cards[0].Key)
		if err != nil || len(comments) != 1 || len(card.AssigneeIDs) != 1 {
			t.Fatalf("get = %+v, %d comments, %v", card, len(comments), err)
		}
		var n int
		if err := st.DB.QueryRow("SELECT count(*) FROM event WHERE kind IN ('label.added','label.removed','assignee.added','comment.added','label.created')").Scan(&n); err != nil || n != 5 {
			t.Fatalf("events = %d", n)
		}
	})
}

func TestCreateLabelDuplicateNameIsConflict(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		p, _, _ := projectWithCards(t, h, "DUPL", 0)
		if _, err := h.svc.CreateLabel(ctx, h.admin, commands.CreateLabel{ProjectID: p.ID, Name: "bug", Color: "#FF0000"}); err != nil {
			t.Fatal(err)
		}
		_, err := h.svc.CreateLabel(ctx, h.admin, commands.CreateLabel{ProjectID: p.ID, Name: "bug", Color: "#00FF00"})
		_ = code(t, err, service.CodeConflict)
	})
}

// TestBoardAndSearchGroupLabelsPerCard guards the batched label and
// assignee reads: GetBoard and SearchCards fetch them for the whole
// board or page in one query each and group them in Go, so a grouping
// slip would silently give a card another card's labels.
func TestBoardAndSearchGroupLabelsPerCard(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		p, _, cards := projectWithCards(t, h, "GRP", 3)

		bug, err := h.svc.CreateLabel(ctx, h.admin, commands.CreateLabel{ProjectID: p.ID, Name: "bug", Color: "#FF0000"})
		if err != nil {
			t.Fatal(err)
		}
		chore, err := h.svc.CreateLabel(ctx, h.admin, commands.CreateLabel{ProjectID: p.ID, Name: "chore", Color: "#00FF00"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[0].Key, LabelIDs: []string{bug.ID, chore.ID}}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[1].Key, LabelIDs: []string{chore.ID}}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.SetAssignees(ctx, h.admin, commands.SetAssignees{CardKey: cards[2].Key, UserIDs: []string{h.ws.AdminUserID}}); err != nil {
			t.Fatal(err)
		}

		check := func(what string, got []service.Card) {
			t.Helper()
			byKey := map[string]service.Card{}
			for _, c := range got {
				byKey[c.Key] = c
			}
			if len(byKey) != 3 {
				t.Fatalf("%s returned %d cards", what, len(got))
			}
			if ls := byKey[cards[0].Key].LabelIDs; len(ls) != 2 {
				t.Fatalf("%s: card 1 labels = %v", what, ls)
			}
			if ls := byKey[cards[1].Key].LabelIDs; len(ls) != 1 || ls[0] != chore.ID {
				t.Fatalf("%s: card 2 labels = %v", what, ls)
			}
			if ls := byKey[cards[2].Key].LabelIDs; len(ls) != 0 {
				t.Fatalf("%s: card 3 labels = %v", what, ls)
			}
			if as := byKey[cards[2].Key].AssigneeIDs; len(as) != 1 || as[0] != h.ws.AdminUserID {
				t.Fatalf("%s: card 3 assignees = %v", what, as)
			}
			if as := byKey[cards[0].Key].AssigneeIDs; len(as) != 0 {
				t.Fatalf("%s: card 1 assignees = %v", what, as)
			}
		}

		_, boardCards, err := h.svc.GetBoard(ctx, h.admin, "GRP")
		if err != nil {
			t.Fatal(err)
		}
		check("GetBoard", boardCards)

		found, _, err := h.svc.SearchCards(ctx, h.admin, "GRP", filter.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		check("SearchCards", found)
	})
}

// TestSearchRejectsMalformedCursor: the cursor is a bound parameter, so
// a bad one cannot inject anything, but returning an empty page for it
// hides the mistake from whoever built the request.
func TestSearchRejectsMalformedCursor(t *testing.T) {
	storetest.Each(t, func(t *testing.T, st *store.Store) {
		h := newHarness(t, st)
		ctx := context.Background()
		projectWithCards(t, h, "CUR", 2)
		_, _, err := h.svc.SearchCards(ctx, h.admin, "CUR", filter.Filter{Cursor: "not-an-id"})
		_ = code(t, err, service.CodeValidation)
		// A well-formed cursor still pages.
		page, next, err := h.svc.SearchCards(ctx, h.admin, "CUR", filter.Filter{Limit: 1})
		if err != nil || len(page) != 1 || next == "" {
			t.Fatalf("first page = %d, %q, %v", len(page), next, err)
		}
		rest, _, err := h.svc.SearchCards(ctx, h.admin, "CUR", filter.Filter{Cursor: next})
		if err != nil || len(rest) != 1 {
			t.Fatalf("second page = %d, %v", len(rest), err)
		}
	})
}
