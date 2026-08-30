package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/necrogami/kanboard/internal/core/commands"
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
		code(t, err, service.CodeConflict)
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
		code(t, err, service.CodeConflict)
		_, err = h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[0].Key, LabelIDs: []string{"not-a-label"}})
		code(t, err, service.CodeValidation)
		c, err = h.svc.SetCardLabels(ctx, h.admin, commands.SetCardLabels{CardKey: cards[0].Key})
		if err != nil || len(c.LabelIDs) != 0 {
			t.Fatalf("clear labels = %+v, %v", c, err)
		}
		c, err = h.svc.SetAssignees(ctx, h.admin, commands.SetAssignees{CardKey: cards[0].Key, UserIDs: []string{h.ws.AdminUserID}})
		if err != nil || len(c.AssigneeIDs) != 1 {
			t.Fatalf("assignees = %+v, %v", c, err)
		}
		_, err = h.svc.SetAssignees(ctx, h.admin, commands.SetAssignees{CardKey: cards[0].Key, UserIDs: []string{"stranger"}})
		code(t, err, service.CodeValidation)
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
		code(t, err, service.CodeConflict)
	})
}
